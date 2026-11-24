package lsp

import (
	"encoding/json"
	"strconv"
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
			Code:     codeToken(lspCode(d)),
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

// codeMissingProperty is the LSP code of a schema finding for a missing
// required property. The backend reports it as schema-validation (its lint
// rule id), so it is split off here, once, for the quick-fix layer to key on.
const codeMissingProperty = "missing-property"

// lspCode returns the stable code a diagnostic is published with.
func lspCode(d Diag) string {
	if d.Code == "schema-validation" && missingProperty(d.Message) != "" {
		return codeMissingProperty
	}
	return d.Code
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

// diagRange resolves a diagnostic to a buffer range. A dotted Field is
// resolved to its YAML node (the value for a scalar, else the key; the parent
// key when the field itself is missing); an explicit Line/Column is narrowed
// to the scalar starting there. Anything else spans the rest of the line.
func diagRange(doc *Document, nodes []*yaml.Node, d Diag) protocol.Range {
	root := nodeAtPosition(nodes, d.Position)
	if d.Field != "" && root != nil {
		if r, ok := fieldRange(doc, root, d.Field); ok {
			return r
		}
	}
	line, col := d.Line, d.Column
	if line == 0 && d.Field != "" && root != nil {
		if l, c, ok := reportspec.ResolvePathPosition(root, d.Field); ok {
			line, col = l, c
		}
	}
	if line == 0 {
		line = 1
	}
	if col == 0 {
		col = 1
	}
	if d.Column > 0 {
		if n := scalarAt(nodes, line, col); n != nil {
			if r, ok := scalarRange(doc, n); ok {
				return r
			}
		}
	}
	return lineSpan(doc, line, col)
}

// fieldRange walks a dotted path like ResolvePathPosition and returns the range
// of the node that best marks it: the scalar value when the whole path exists,
// otherwise the key of the deepest segment that was found.
func fieldRange(doc *Document, root *yaml.Node, field string) (protocol.Range, bool) {
	if field == "" || field == "(root)" {
		return protocol.Range{}, false
	}
	parts := strings.Split(field, ".")
	cur := root
	var key *yaml.Node
	for i := 0; i < len(parts); {
		consumed := 0
		switch cur.Kind {
		case yaml.MappingNode:
			for end := len(parts); end > i; end-- {
				name := strings.Join(parts[i:end], ".")
				for j := 0; j+1 < len(cur.Content); j += 2 {
					if cur.Content[j].Value == name {
						key, cur = cur.Content[j], cur.Content[j+1]
						consumed = end - i
						break
					}
				}
				if consumed > 0 {
					break
				}
			}
		case yaml.SequenceNode:
			if idx, err := strconv.Atoi(parts[i]); err == nil && idx >= 0 && idx < len(cur.Content) {
				cur, key = cur.Content[idx], nil
				consumed = 1
			}
		default:
		}
		if consumed == 0 {
			if key == nil {
				return protocol.Range{}, false
			}
			return scalarRange(doc, key)
		}
		i += consumed
	}
	if r, ok := scalarRange(doc, cur); ok {
		return r, true
	}
	if key != nil {
		return scalarRange(doc, key)
	}
	return protocol.Range{}, false
}

// scalarAt returns the scalar node (key or value) that starts at line/col.
func scalarAt(nodes []*yaml.Node, line, col int) *yaml.Node {
	var found *yaml.Node
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil || found != nil {
			return
		}
		if n.Kind == yaml.ScalarNode && n.Line == line && n.Column == col {
			found = n
			return
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return found
}

// scalarRange returns the source span of a single-line scalar, quotes
// included. Block and multi-line scalars, empty values and tokens that do not
// match the source (tags, anchors) yield ok=false.
func scalarRange(doc *Document, n *yaml.Node) (protocol.Range, bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return protocol.Range{}, false
	}
	text, ok := doc.lineText(n.Line)
	if !ok {
		return protocol.Range{}, false
	}
	runes := []rune(text)
	start := n.Column - 1
	if start < 0 || start >= len(runes) {
		return protocol.Range{}, false
	}
	rest := runes[start:]
	width := 0
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		width = closingQuote(rest, '"')
	case n.Style&yaml.SingleQuotedStyle != 0:
		width = closingQuote(rest, '\'')
	case n.Value != "" && !strings.Contains(n.Value, "\n") && strings.HasPrefix(string(rest), n.Value):
		width = utf8.RuneCountInString(n.Value)
	}
	if width == 0 {
		return protocol.Range{}, false
	}
	return protocol.Range{
		Start: protocol.Position{Line: clampU32(n.Line - 1), Character: clampU32(utf16CharIn(text, n.Column))},
		End:   protocol.Position{Line: clampU32(n.Line - 1), Character: clampU32(utf16CharIn(text, n.Column+width))},
	}, true
}

// closingQuote returns the rune width of a quoted token that starts at s[0],
// or 0 when it does not close on this line. A doubled single quote and a
// backslash in double quotes are escapes.
func closingQuote(s []rune, q rune) int {
	for i := 1; i < len(s); i++ {
		switch {
		case q == '"' && s[i] == '\\':
			i++
		case s[i] == q && q == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i++
		case s[i] == q:
			return i + 1
		}
	}
	return 0
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
