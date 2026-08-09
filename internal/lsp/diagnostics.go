package lsp

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"
	"gopkg.in/yaml.v3"

	reportspec "bino.bi/bino/internal/report/spec"
)

// backfillDiagnostics converts backend diagnostics into LSP diagnostics anchored
// to ranges in the live buffer. Data/schema diagnostics carry a line/col or a
// dotted Field (resolved via ResolvePathPosition); env-var diagnostics carry no
// position and are anchored by locating the ${VAR} occurrence.
func backfillDiagnostics(doc *Document, diags []Diag) []protocol.Diagnostic {
	nodes, _ := reportspec.ParseYAMLNodes(doc.Text) //nolint:errcheck // lenient parse; unparsable docs contribute no positions
	out := make([]protocol.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Code == "missing-env-var" {
			out = append(out, envVarDiagnostics(doc, d)...)
			continue
		}
		out = append(out, protocol.Diagnostic{
			Range:    diagRange(doc, nodes, d),
			Severity: severity(d.Severity),
			Message:  protocol.String(messageWithHint(d)),
			Source:   protocol.NewOptional("bino"),
			Code:     codeToken(d.Code),
			Data:     missingFieldData(d),
		})
	}
	return out
}

// messageWithHint appends the actionable hint as a second message line.
// Data/quick-fix extraction (missingFieldData) reads the ORIGINAL d.Message,
// so the suffix never confuses the message parsers.
func messageWithHint(d Diag) string {
	if d.Hint == "" {
		return d.Message
	}
	return d.Message + "\nhint: " + d.Hint
}

// missingFieldData carries the parent path + missing property of a
// "missing property 'x'" diagnostic so the code-action layer can insert the
// field without re-deriving the path. LSP preserves Data across the
// publishDiagnostics → codeAction round-trip.
func missingFieldData(d Diag) protocol.LSPAny {
	prop := missingProperty(d.Message)
	if prop == "" {
		return nil
	}
	b, err := json.Marshal(map[string]any{"field": d.Field, "doc": d.Position, "prop": prop})
	if err != nil {
		return nil
	}
	return protocol.LSPAny(b)
}

// missingProperty extracts x from a "missing property 'x'" message.
func missingProperty(message string) string {
	const marker = "missing property '"
	i := strings.Index(message, marker)
	if i < 0 {
		return ""
	}
	rest := message[i+len(marker):]
	j := strings.IndexByte(rest, '\'')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// diagRange resolves a diagnostic to a buffer range: an explicit Line/Column
// when present, else the dotted Field via ResolvePathPosition, else line 1.
func diagRange(doc *Document, nodes []*yaml.Node, d Diag) protocol.Range {
	line, col := d.Line, d.Column
	if line == 0 && d.Field != "" {
		if node := nodeAtPosition(nodes, d.Position); node != nil {
			if l, c, ok := reportspec.ResolvePathPosition(node, d.Field); ok {
				line, col = l, c
			}
		}
	}
	if line == 0 {
		line = 1
	}
	if col == 0 {
		col = 1
	}
	return lineSpan(doc, line, col)
}

// envVarDiagnostics anchors a missing-env-var diagnostic at each ${VAR}
// occurrence in the buffer (one diagnostic per site).
func envVarDiagnostics(doc *Document, d Diag) []protocol.Diagnostic {
	name := strings.TrimSpace(strings.TrimPrefix(d.Message, "Unresolved environment variable:"))
	if name == "" || name == d.Message {
		return []protocol.Diagnostic{{
			Range:    lineSpan(doc, 1, 1),
			Severity: severity(d.Severity),
			Message:  protocol.String(d.Message),
			Source:   protocol.NewOptional("bino"),
			Code:     codeToken(d.Code),
		}}
	}
	needle := "${" + name
	var out []protocol.Diagnostic
	for off := 0; ; {
		i := strings.Index(doc.Text[off:], needle)
		if i < 0 {
			break
		}
		start := off + i
		// Extend the range to the closing brace when present.
		end := start + len(needle)
		if brace := strings.IndexByte(doc.Text[start:], '}'); brace >= 0 {
			end = start + brace + 1
		}
		out = append(out, protocol.Diagnostic{
			Range:    protocol.Range{Start: doc.OffsetToPosition(start), End: doc.OffsetToPosition(end)},
			Severity: severity(d.Severity),
			Message:  protocol.String(d.Message),
			Source:   protocol.NewOptional("bino"),
			Code:     codeToken(d.Code),
		})
		off = end
	}
	if len(out) == 0 {
		out = append(out, protocol.Diagnostic{
			Range:    lineSpan(doc, 1, 1),
			Severity: severity(d.Severity),
			Message:  protocol.String(d.Message),
			Source:   protocol.NewOptional("bino"),
			Code:     codeToken(d.Code),
		})
	}
	return out
}

// lineSpan returns a range from a 1-based (line, rune col) to the end of that
// line, converted to UTF-16 characters against the buffer.
func lineSpan(doc *Document, line, col int) protocol.Range {
	text, ok := doc.lineText(line)
	if !ok {
		return protocol.Range{
			Start: protocol.Position{Line: clampU32(line - 1), Character: clampU32(col - 1)},
			End:   protocol.Position{Line: clampU32(line - 1), Character: clampU32(col - 1)},
		}
	}
	endCol := utf8.RuneCountInString(text) + 1
	if endCol < col {
		endCol = col
	}
	return protocol.Range{
		Start: protocol.Position{Line: clampU32(line - 1), Character: clampU32(utf16CharIn(text, col))},
		End:   protocol.Position{Line: clampU32(line - 1), Character: clampU32(utf16CharIn(text, endCol))},
	}
}

// nodeAtPosition returns the document root node for a 1-based document ordinal.
func nodeAtPosition(nodes []*yaml.Node, position int) *yaml.Node {
	if position <= 0 || position > len(nodes) {
		if len(nodes) > 0 {
			return nodes[0]
		}
		return nil
	}
	return nodes[position-1]
}

func severity(s string) protocol.DiagnosticSeverity {
	switch s {
	case "error":
		return protocol.DiagnosticSeverityError
	case "warning":
		return protocol.DiagnosticSeverityWarning
	case "info":
		return protocol.DiagnosticSeverityInformation
	case "hint":
		return protocol.DiagnosticSeverityHint
	default:
		return protocol.DiagnosticSeverityWarning
	}
}

func codeToken(code string) protocol.ProgressToken {
	if code == "" {
		return nil
	}
	return protocol.String(code)
}
