package lsp

import (
	"context"
	"encoding/json"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const rangeDoc = `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: t
spec:
  dataset: sales
  title: "Revenue 2024"
`

func span(r protocol.Range) [3]uint32 {
	return [3]uint32{r.Start.Line, r.Start.Character, r.End.Character}
}

// TestBackfillDiagnostics_NodeRanges: a diagnostic covers its YAML node, not
// the rest of the line.
func TestBackfillDiagnostics_NodeRanges(t *testing.T) {
	doc := &Document{Text: rangeDoc}
	cases := []struct {
		name string
		diag Diag
		want [3]uint32 // line, start, end (0-based, UTF-16)
	}{
		{"scalar value", Diag{Field: "spec.dataset", Line: 6, Column: 12, Message: "bad"}, [3]uint32{5, 11, 16}},
		{"quoted value keeps its quotes", Diag{Field: "spec.title", Message: "bad"}, [3]uint32{6, 9, 23}},
		{"missing property marks the parent key", Diag{Field: "spec", Line: 6, Column: 3, Message: "missing property 'type'"}, [3]uint32{4, 0, 4}},
		{"missing nested field falls back to the deepest key", Diag{Field: "metadata.labels.env", Message: "bad"}, [3]uint32{2, 0, 8}},
		{"line/col on a key", Diag{Line: 4, Column: 3, Message: "lint"}, [3]uint32{3, 2, 6}},
		{"line only spans the line", Diag{Line: 2, Message: "yaml"}, [3]uint32{1, 0, 11}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.diag.Severity = "error"
			got := backfillDiagnostics(doc, []Diag{c.diag})
			if len(got) != 1 {
				t.Fatalf("got %d diagnostics", len(got))
			}
			if s := span(got[0].Range); s != c.want {
				t.Errorf("range = %v, want %v", s, c.want)
			}
		})
	}
}

// TestBackfillDiagnostics_Codes: every diagnostic a quick fix acts on carries a
// stable code, and the messages stay as the backend wrote them.
func TestBackfillDiagnostics_Codes(t *testing.T) {
	doc := &Document{Text: rangeDoc + "  sub: ${MISSING}\n"}
	diags := backfillDiagnostics(doc, []Diag{
		{Field: "spec", Message: "missing property 'type'", Code: "schema-validation", Severity: "error"},
		{Field: "spec.title", Message: "got number, want string", Code: "schema-validation", Severity: "error"},
		{Message: "Unresolved environment variable: MISSING", Code: "missing-env-var", Severity: "warning"},
		{Message: `DataSet "x" reads DataSource "sales" but does not declare it`, Code: "dataset-dependency-undeclared", Severity: "warning"},
	})
	want := []string{codeMissingProperty, "schema-validation", "missing-env-var", "dataset-dependency-undeclared"}
	if len(diags) != len(want) {
		t.Fatalf("got %d diagnostics, want %d", len(diags), len(want))
	}
	for i, d := range diags {
		if got := diagCode(d.Code); got != want[i] {
			t.Errorf("diagnostic %d code = %q, want %q", i, got, want[i])
		}
	}
	if got := diagMessage(diags[0].Message); got != "missing property 'type'" {
		t.Errorf("message changed to %q", got)
	}
}

// TestCodeAction_KeyedByCode: the insert-field quick fix follows the code, not
// the presence of Data or the message text.
func TestCodeAction_KeyedByCode(t *testing.T) {
	s := newTestServer()
	u := uri.File("/proj/r.yaml")
	s.docs.Set(u, "kind: Table\nmetadata:\n  name: t\nspec:\n  dataset: sales\n", 1)
	data, _ := json.Marshal(map[string]any{"field": "spec", "doc": 1, "prop": "title"})
	run := func(code string) []protocol.CommandOrCodeAction {
		d := protocol.Diagnostic{
			Range:   protocol.Range{Start: protocol.Position{Line: 3}, End: protocol.Position{Line: 3, Character: 4}},
			Message: protocol.String("some other wording"),
			Code:    protocol.String(code),
			Data:    protocol.LSPAny(data),
		}
		actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Range:        d.Range,
			Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{d}},
		})
		return actions
	}
	if _, ok := codeActionTitles(run(codeMissingProperty))["Add missing field 'title'"]; !ok {
		t.Error("missing-property code must offer the insert-field fix")
	}
	if _, ok := codeActionTitles(run("schema-validation"))["Add missing field 'title'"]; ok {
		t.Error("only the missing-property code may offer the insert-field fix")
	}
}
