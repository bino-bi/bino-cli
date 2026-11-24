package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"go.lsp.dev/protocol"

	"bino.bi/bino/internal/pathutil"
)

// isBinoTOML reports whether a buffer is a project's bino.toml. The client
// sends it with a filename selector, so it may arrive as plaintext or toml.
func isBinoTOML(path string) bool {
	return filepath.Base(path) == pathutil.ProjectConfigFile
}

// tomlDiagnostics decodes the buffer the way LoadProjectConfig does, in strict
// mode, so it reports what the build fails on (syntax, wrong types) plus the
// keys the build silently ignores. The result is never nil, so publishing it
// clears earlier findings.
func tomlDiagnostics(doc *Document) []protocol.Diagnostic {
	var cfg pathutil.ProjectConfig
	err := toml.NewDecoder(strings.NewReader(doc.Text)).DisallowUnknownFields().Decode(&cfg)
	out := []protocol.Diagnostic{}
	if err == nil {
		return out
	}
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		var raw map[string]any
		_ = toml.Unmarshal([]byte(doc.Text), &raw) //nolint:errcheck // the strict decode above already parsed it
		for i := range strict.Errors {
			e := &strict.Errors[i]
			row, col := e.Position()
			key := strings.Join(e.Key(), ".")
			if cmd, name, ok := envTableKey(e.Key()); ok {
				// Variables may sit directly under [<cmd>.env] (ParseProjectConfig).
				if _, isString := envValue(raw, cmd, name).(string); !isString {
					out = append(out, tomlDiag(keySpan(doc, row, col), protocol.DiagnosticSeverityWarning, "toml-type",
						fmt.Sprintf("'%s' must be a string: bino ignores it", key)))
				}
				continue
			}
			if strings.EqualFold(e.Key()[0], "trust") {
				// The loader keeps these on the policy (TrustConfig.Err): commands that apply it fail.
				out = append(out, tomlDiag(keySpan(doc, row, col), protocol.DiagnosticSeverityError, "toml-unknown-key",
					fmt.Sprintf("unknown key '%s': commands that apply the trust policy fail on it", key)))
				continue
			}
			out = append(out, tomlDiag(keySpan(doc, row, col), protocol.DiagnosticSeverityWarning, "toml-unknown-key",
				fmt.Sprintf("unknown key '%s': bino ignores it", key)))
		}
		return out
	}
	var de *toml.DecodeError
	if !errors.As(err, &de) {
		return append(out, tomlDiag(lineSpan(doc, 1, 1), protocol.DiagnosticSeverityError, "toml-syntax", err.Error()))
	}
	row, col := de.Position()
	msg := strings.TrimPrefix(de.Error(), "toml: ")
	if key := de.Key(); len(key) > 0 {
		return append(out, tomlDiag(valueSpan(doc, row, col), protocol.DiagnosticSeverityError, "toml-type",
			typeMessage(strings.Join(key, "."), msg)))
	}
	return append(out, tomlDiag(valueSpan(doc, row, col), protocol.DiagnosticSeverityError, "toml-syntax", "TOML syntax error: "+msg))
}

// envTableKey matches a key written directly under [<cmd>.env].
func envTableKey(key []string) (cmd, name string, ok bool) {
	if len(key) != 3 || key[1] != "env" || !slices.Contains(pathutil.EnvCommands, key[0]) {
		return "", "", false
	}
	return key[0], key[2], true
}

func envValue(raw map[string]any, cmd, name string) any {
	table, _ := raw[cmd].(map[string]any)
	env, _ := table["env"].(map[string]any)
	return env[name]
}

func tomlDiag(r protocol.Range, sev protocol.DiagnosticSeverity, code, msg string) protocol.Diagnostic {
	return protocol.Diagnostic{
		Range:    r,
		Severity: sev,
		Message:  protocol.String(msg),
		Source:   protocol.NewOptional("bino"),
		Code:     protocol.String(code),
	}
}

var decodeTypeRe = regexp.MustCompile(`^cannot decode TOML (\w+) into .* of type (\S+)$`)

// typeMessage rewrites go-toml's Go-typed mismatch message for a TOML author.
func typeMessage(key, msg string) string {
	m := decodeTypeRe.FindStringSubmatch(msg)
	if m == nil {
		return fmt.Sprintf("wrong type for '%s': %s", key, msg)
	}
	want := "a table"
	switch {
	case strings.HasPrefix(m[2], "[]"):
		want = "an array"
	case m[2] == "string":
		want = "a string"
	case m[2] == "bool":
		want = "a boolean"
	}
	return fmt.Sprintf("'%s' must be %s, not a TOML %s", key, want, m[1])
}

// keySpan spans the key token starting at a 1-based row/col.
func keySpan(doc *Document, row, col int) protocol.Range {
	return tokenSpan(doc, row, col, func(r rune) bool {
		return r == '=' || r == ']' || r == ' ' || r == '\t'
	})
}

// valueSpan spans from row/col to the end of the line, minus a trailing comment.
func valueSpan(doc *Document, row, col int) protocol.Range {
	return tokenSpan(doc, row, col, func(r rune) bool { return r == '#' })
}

func tokenSpan(doc *Document, row, col int, stop func(rune) bool) protocol.Range {
	text, ok := doc.lineText(row)
	if !ok || col < 1 {
		return lineSpan(doc, row, col)
	}
	runes := []rune(text)
	end := col - 1
	for end < len(runes) && !stop(runes[end]) {
		end++
	}
	for end > col-1 && (runes[end-1] == ' ' || runes[end-1] == '\t') {
		end--
	}
	if end <= col-1 {
		return lineSpan(doc, row, col)
	}
	return protocol.Range{
		Start: protocol.Position{Line: clampU32(row - 1), Character: clampU32(utf16CharIn(text, col))},
		End:   protocol.Position{Line: clampU32(row - 1), Character: clampU32(utf16CharIn(text, end+1))},
	}
}

// tomlSchemaNode is the part of the bino.toml JSON schema completion needs.
type tomlSchemaNode struct {
	Ref                  string                     `json:"$ref"`
	Type                 string                     `json:"type"`
	Description          string                     `json:"description"`
	Properties           map[string]*tomlSchemaNode `json:"properties"`
	AdditionalProperties json.RawMessage            `json:"additionalProperties"`
	Items                *tomlSchemaNode            `json:"items"`
	Definitions          map[string]*tomlSchemaNode `json:"definitions"`
}

var tomlSchema = sync.OnceValue(func() *tomlSchemaNode {
	var root tomlSchemaNode
	if err := json.Unmarshal(pathutil.ProjectConfigSchema, &root); err != nil {
		return &tomlSchemaNode{}
	}
	return &root
})

func (n *tomlSchemaNode) resolve() *tomlSchemaNode {
	for n != nil && n.Ref != "" {
		n = tomlSchema().Definitions[strings.TrimPrefix(n.Ref, "#/definitions/")]
	}
	return n
}

// table returns the schema a key opens as a table: an object, or the items
// of an array of tables such as [[trust.signer]]. Other keys yield nil.
func (n *tomlSchemaNode) table() *tomlSchemaNode {
	n = n.resolve()
	if n == nil {
		return nil
	}
	if n.Type == "array" {
		n = n.Items.resolve()
	}
	if n == nil || n.Type != "object" {
		return nil
	}
	return n
}

// child returns the schema of a table's key: a declared property, or the
// value schema of a map such as [plugins.<name>].
func (n *tomlSchemaNode) child(key string) *tomlSchemaNode {
	n = n.resolve()
	if n == nil {
		return nil
	}
	if p, ok := n.Properties[key]; ok {
		if t := p.table(); t != nil {
			return t
		}
		return p.resolve()
	}
	var v tomlSchemaNode
	if json.Unmarshal(n.AdditionalProperties, &v) != nil || (v.Type == "" && v.Ref == "") {
		return nil
	}
	return v.resolve()
}

// tableNames lists every table path the schema declares (map entries such as
// [plugins.<name>] excluded: their names are the user's).
func tableNames() []string {
	var out []string
	var walk func(prefix string, n *tomlSchemaNode)
	walk = func(prefix string, n *tomlSchemaNode) {
		for name, p := range n.resolve().Properties {
			p = p.table()
			if p == nil {
				continue
			}
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			out = append(out, path)
			walk(path, p)
		}
	}
	walk("", tomlSchema())
	sort.Strings(out)
	return out
}

var tableHeaderRe = regexp.MustCompile(`^\s*\[\[?\s*([^\]]*?)\s*\]`)

// tomlCompletion offers table names after `[` and the keys of the current
// table at the start of a line. Values are left to the user.
func tomlCompletion(doc *Document, pos protocol.Position) []protocol.CompletionItem {
	line := int(pos.Line) + 1
	text, ok := doc.lineText(line)
	if !ok {
		return nil
	}
	_, col := doc.PositionToLineCol(pos)
	runes := []rune(text)
	if col-1 > len(runes) {
		return nil
	}
	before := string(runes[:col-1])
	trimmed := strings.TrimLeft(before, " \t")
	indent := utf8.RuneCountInString(before) - utf8.RuneCountInString(trimmed)

	if strings.HasPrefix(trimmed, "[") {
		typed := strings.TrimLeft(trimmed, "[")
		start := indent + (utf8.RuneCountInString(trimmed) - utf8.RuneCountInString(typed))
		rng := lineRange(text, line, start+1, col)
		var items []protocol.CompletionItem
		for _, name := range tableNames() {
			items = append(items, tomlItem(name, name, protocol.CompletionItemKindModule, schemaAt(name), rng))
		}
		return items
	}
	if strings.ContainsAny(trimmed, "= \t#") {
		return nil // past the key
	}
	table := currentTable(doc, line)
	node := schemaAt(table)
	if node == nil {
		return nil
	}
	rng := lineRange(text, line, indent+1, col)
	var items []protocol.CompletionItem
	for name, p := range node.Properties {
		if p.table() != nil {
			continue // a table, offered after `[`
		}
		p = p.resolve()
		items = append(items, tomlItem(name, name+" = ", protocol.CompletionItemKindProperty, p, rng))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return items
}

// schemaAt resolves a dotted table path ("" is the root).
func schemaAt(path string) *tomlSchemaNode {
	n := tomlSchema().resolve()
	if path == "" {
		return n
	}
	for _, seg := range strings.Split(path, ".") {
		if n = n.child(strings.Trim(seg, `"' `)); n == nil {
			return nil
		}
	}
	return n
}

// currentTable returns the path of the last table header above line.
func currentTable(doc *Document, line int) string {
	for l := line - 1; l >= 1; l-- {
		text, _ := doc.lineText(l)
		if m := tableHeaderRe.FindStringSubmatch(text); m != nil {
			return m[1]
		}
	}
	return ""
}

func lineRange(text string, line, startCol, endCol int) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: clampU32(line - 1), Character: clampU32(utf16CharIn(text, startCol))},
		End:   protocol.Position{Line: clampU32(line - 1), Character: clampU32(utf16CharIn(text, endCol))},
	}
}

func tomlItem(label, insert string, kind protocol.CompletionItemKind, n *tomlSchemaNode, rng protocol.Range) protocol.CompletionItem {
	it := protocol.CompletionItem{
		Label:    label,
		Kind:     kind,
		TextEdit: &protocol.TextEdit{Range: rng, NewText: insert},
	}
	if n != nil {
		if n.Type != "" {
			it.Detail = protocol.NewOptional(n.Type)
		}
		if n.Description != "" {
			it.Documentation = protocol.String(n.Description)
		}
	}
	return it
}
