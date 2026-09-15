package lsp

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
	"gopkg.in/yaml.v3"
)

func codeActionTitles(actions []protocol.CommandOrCodeAction) map[string]*protocol.CodeAction {
	out := make(map[string]*protocol.CodeAction)
	for _, a := range actions {
		if ca, ok := a.(*protocol.CodeAction); ok {
			out[ca.Title] = ca
		}
	}
	return out
}

func TestCodeAction_ScaffoldMissingDataset(t *testing.T) {
	s := newTestServer()
	u := uri.File("/proj/r.yaml")
	s.docs.Set(u, "kind: Table\nmetadata:\n  name: t\nspec:\n  dataset: missing_ds\n", 1)

	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        protocol.Range{Start: protocol.Position{Line: 4, Character: 12}, End: protocol.Position{Line: 4, Character: 12}},
	})
	ca, ok := codeActionTitles(actions)["Create DataSet 'missing_ds'"]
	if !ok {
		t.Fatalf("expected a scaffold action, got %d actions", len(actions))
	}
	edits := ca.Edit.Changes[u]
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "kind: DataSet") || !strings.Contains(edits[0].NewText, "name: missing_ds") {
		t.Errorf("scaffold edit should append a DataSet stub, got %q", edits[0].NewText)
	}
}

func TestCodeAction_ScaffoldSkipsExisting(t *testing.T) {
	s, _, reportURI := navServer(t) // report.yaml references the existing DataSet "sales"
	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: reportURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 4, Character: 12}, End: protocol.Position{Line: 4, Character: 12}},
	})
	if _, ok := codeActionTitles(actions)["Create DataSet 'sales'"]; ok {
		t.Error("should not offer to scaffold an already-declared dataset")
	}
}

func TestCodeAction_InsertMissingField(t *testing.T) {
	s := newTestServer()
	u := uri.File("/proj/r.yaml")
	s.docs.Set(u, "kind: Table\nmetadata:\n  name: t\nspec:\n  dataset: sales\n", 1)

	data, _ := json.Marshal(map[string]any{"field": "spec", "doc": 1, "prop": "title"})
	diag := protocol.Diagnostic{
		Range:   protocol.Range{Start: protocol.Position{Line: 3, Character: 0}, End: protocol.Position{Line: 3, Character: 4}},
		Message: protocol.String("missing property 'title'"),
		Code:    protocol.String("schema-validation"),
		Data:    protocol.LSPAny(data),
	}
	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        diag.Range,
		Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{diag}},
	})
	ca, ok := codeActionTitles(actions)["Add missing field 'title'"]
	if !ok {
		t.Fatalf("expected an insert-field action, got %d actions", len(actions))
	}
	edits := ca.Edit.Changes[u]
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "title:") {
		t.Errorf("insert-field edit should add 'title:', got %q", edits[0].NewText)
	}
}

func TestCodeAction_AddDependency(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		position int    // 1-based document ordinal of the finding
		want     string // full text after the fix
		wantDeps []any  // fallback cases: the re-encoded spec.dependencies
	}{ // want and wantDeps both empty: no action expected
		{
			name: "appends to a 4-space list without touching the rest",
			text: "apiVersion: bino.bi/v1alpha1\n" +
				"kind: DataSet\n" +
				"metadata:\n" +
				"    name: combined\n" +
				"spec:\n" +
				"    dependencies:\n" +
				"        - type: csv\n" +
				"          path: ./x.csv\n" +
				"        - orders_csv\n" +
				"\n" +
				"    query: |\n" +
				"        SELECT * \n" +
				"        FROM orders_csv JOIN sales_csv USING (id)\n",
			position: 1,
			want: "apiVersion: bino.bi/v1alpha1\n" +
				"kind: DataSet\n" +
				"metadata:\n" +
				"    name: combined\n" +
				"spec:\n" +
				"    dependencies:\n" +
				"        - type: csv\n" +
				"          path: ./x.csv\n" +
				"        - orders_csv\n" +
				"        - sales_csv\n" +
				"\n" +
				"    query: |\n" +
				"        SELECT * \n" +
				"        FROM orders_csv JOIN sales_csv USING (id)\n",
		},
		{
			name: "appends to a list indented like its key",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"  - orders_csv # first\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
			want: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"  - orders_csv # first\n" +
				"  - sales_csv\n" +
				"  query: SELECT * FROM sales_csv\n",
		},
		{
			name: "keeps CRLF line endings",
			text: "kind: DataSet\r\n" +
				"spec:\r\n" +
				"  dependencies:\r\n" +
				"    - orders_csv\r\n" +
				"  query: SELECT * FROM sales_csv\r\n",
			position: 1,
			want: "kind: DataSet\r\n" +
				"spec:\r\n" +
				"  dependencies:\r\n" +
				"    - orders_csv\r\n" +
				"    - sales_csv\r\n" +
				"  query: SELECT * FROM sales_csv\r\n",
		},
		{
			name: "creates a missing list and keeps comments",
			text: "kind: DataSet\n" +
				"metadata:\n" +
				"  name: combined\n" +
				"spec:\n" +
				"  # totals per region\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
			want: "kind: DataSet\n" +
				"metadata:\n" +
				"  name: combined\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"    - sales_csv\n" +
				"  # totals per region\n" +
				"  query: SELECT * FROM sales_csv\n",
		},
		{
			name: "edits the DataSet in the second document",
			text: "kind: DataSource\n" +
				"metadata:\n" +
				"  name: sales_csv\n" +
				"spec:\n" +
				"  type: csv\n" +
				"  path: ./sales.csv\n" +
				"---\n" +
				"kind: DataSet\n" +
				"metadata:\n" +
				"  name: combined\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"    - orders_csv\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 2,
			want: "kind: DataSource\n" +
				"metadata:\n" +
				"  name: sales_csv\n" +
				"spec:\n" +
				"  type: csv\n" +
				"  path: ./sales.csv\n" +
				"---\n" +
				"kind: DataSet\n" +
				"metadata:\n" +
				"  name: combined\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"    - orders_csv\n" +
				"    - sales_csv\n" +
				"  query: SELECT * FROM sales_csv\n",
		},
		{
			name: "already listed",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"    - sales_csv\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
		},
		{
			name: "range in a non-DataSet document",
			text: "kind: DataSource\n" +
				"spec:\n" +
				"  query: SELECT * FROM sales_csv\n" +
				"---\n" +
				"kind: DataSet\n" +
				"spec:\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
		},
		{
			name: "flow list falls back to re-encoding",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies: [orders_csv]\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
			wantDeps: []any{"orders_csv", "sales_csv"},
		},
		{
			name: "null dependencies falls back to re-encoding",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
			wantDeps: []any{"sales_csv"},
		},
		{
			name: "inline object as last item falls back to re-encoding",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"    - orders_csv\n" +
				"    - type: csv\n" +
				"      path: ./x.csv\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
			wantDeps: []any{"orders_csv", map[string]any{"type": "csv", "path": "./x.csv"}, "sales_csv"},
		},
		{
			name: "multi-line last item falls back to re-encoding",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"    - orders\n" +
				"      _csv\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 1,
			wantDeps: []any{"orders _csv", "sales_csv"},
		},
		{
			name: "list with an anchor indents like its items",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"    dependencies: &deps\n" +
				"        - orders_csv\n" +
				"    query: SELECT * FROM sales_csv\n",
			position: 1,
			want: "kind: DataSet\n" +
				"spec:\n" +
				"    dependencies: &deps\n" +
				"        - orders_csv\n" +
				"        - sales_csv\n" +
				"    query: SELECT * FROM sales_csv\n",
		},
		{
			name: "creates a missing list in a 4-space CRLF file",
			text: "kind: DataSet\r\n" +
				"spec:\r\n" +
				"    query: SELECT * FROM sales_csv\r\n",
			position: 1,
			want: "kind: DataSet\r\n" +
				"spec:\r\n" +
				"    dependencies:\r\n" +
				"        - sales_csv\r\n" +
				"    query: SELECT * FROM sales_csv\r\n",
		},
		{
			name: "finding shifted by an empty document onto a DataSet that does not read the name",
			text: "kind: DataSet\n" +
				"spec:\n" +
				"  dependencies:\n" +
				"    - orders_csv\n" +
				"  query: SELECT * FROM orders_csv\n" +
				"---\n" +
				"# retired manifest\n" +
				"---\n" +
				"kind: DataSet\n" +
				"spec:\n" +
				"  query: SELECT * FROM sales_csv\n",
			position: 2, // the loader skips the empty document
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			u := uri.File("/proj/r.yaml")
			s.docs.Set(u, tt.text, 1)
			diags := backfillDiagnostics(&Document{Text: tt.text}, []Diag{{
				Position: tt.position,
				Field:    "spec.query",
				Code:     "dataset-dependency-undeclared",
				Severity: "warning",
				Message:  `query reads DataSource "sales_csv" but spec.dependencies does not list it; the dataset cache will not notice when it changes`,
			}})
			actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: u},
				Range:        diags[0].Range,
				Context:      protocol.CodeActionContext{Diagnostics: diags},
			})
			ca, ok := codeActionTitles(actions)["Add sales_csv to dependencies"]
			if tt.want == "" && tt.wantDeps == nil {
				if ok {
					t.Fatalf("expected no action, got edit %+v", ca.Edit.Changes[u])
				}
				return
			}
			if !ok {
				t.Fatalf("expected an add-dependency action, got %d actions", len(actions))
			}
			got := applyEdits(tt.text, ca.Edit.Changes[u])
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("result mismatch\n got: %q\nwant: %q", got, tt.want)
				}
				return
			}
			var parsed struct {
				Spec struct {
					Dependencies []any `yaml:"dependencies"`
				} `yaml:"spec"`
			}
			if err := yaml.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("result is not valid YAML: %v\n%s", err, got)
			}
			if !reflect.DeepEqual(parsed.Spec.Dependencies, tt.wantDeps) {
				t.Errorf("dependencies = %v, want %v\n%s", parsed.Spec.Dependencies, tt.wantDeps, got)
			}
		})
	}
}

// applyEdits applies text edits to text, converting their UTF-16 protocol
// positions back to byte offsets.
func applyEdits(text string, edits []protocol.TextEdit) string {
	doc := &Document{Text: text}
	offset := func(p protocol.Position) int {
		line, col := doc.PositionToLineCol(p)
		lineText, _ := doc.lineText(line)
		start := doc.lineStarts()[line-1]
		cur := 1
		for i := range lineText {
			if cur == col {
				return start + i
			}
			cur++
		}
		return start + len(lineText)
	}
	sorted := slices.Clone(edits)
	slices.SortFunc(sorted, func(a, b protocol.TextEdit) int { return offset(b.Range.Start) - offset(a.Range.Start) })
	for _, e := range sorted {
		text = text[:offset(e.Range.Start)] + e.NewText + text[offset(e.Range.End):]
	}
	return text
}
