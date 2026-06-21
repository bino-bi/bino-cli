package lsp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"bino.bi/bino/internal/logx"
)

// fakeBackend is a canned Backend for handler tests — no daemon, no DuckDB.
type fakeBackend struct {
	schema  json.RawMessage
	index   []IndexDoc
	columns map[string][]string
	diags   []Diag
}

func (f *fakeBackend) Start(context.Context) error { return nil }
func (f *fakeBackend) Close() error                { return nil }
func (f *fakeBackend) OnProjectChange(func())      {}
func (f *fakeBackend) Index(context.Context) ([]IndexDoc, error) {
	return f.index, nil
}
func (f *fakeBackend) MergedSchema(context.Context) (json.RawMessage, error) { return f.schema, nil }
func (f *fakeBackend) Columns(_ context.Context, name string) ([]string, error) {
	return f.columns[name], nil
}
func (f *fakeBackend) GraphDeps(context.Context, string, string, string, int) (GraphResult, error) {
	return GraphResult{}, nil
}
func (f *fakeBackend) ValidateDraft(context.Context, []byte) ([]Diag, error) { return f.diags, nil }
func (f *fakeBackend) ValidateProject(context.Context, bool) (bool, []Diag, error) {
	return len(f.diags) == 0, f.diags, nil
}

const testSchema = `{
  "properties": {"kind": {"enum": ["Table", "ChartTime", "DataSet"]}},
  "allOf": [
    {"if": {"properties": {"kind": {"const": "Table"}}},
     "then": {"properties": {"spec": {"properties": {
        "title": {"description": "The table title."},
        "type": {"enum": ["list", "sum"]},
        "dataset": {}, "scenarios": {}, "variances": {}
     }}}}}
  ]
}`

func newTestServer() *Server {
	be := &fakeBackend{
		schema: json.RawMessage(testSchema),
		index: []IndexDoc{
			{Kind: "DataSet", Name: "sales", File: "data.yaml", Position: 1},
			{Kind: "DataSource", Name: "sales_src", File: "data.yaml", Position: 2},
		},
		columns: map[string][]string{"sales": {"ac1", "pp1", "category"}},
	}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	return NewServer(be, log, true, "/proj")
}

func openDoc(t *testing.T, s *Server, text string) uri.URI {
	t.Helper()
	u := uri.File("/proj/report.yaml")
	s.docs.Set(u, text, 1)
	return u
}

func completionLabels(t *testing.T, res protocol.CompletionResult) []string {
	t.Helper()
	var items []protocol.CompletionItem
	switch v := res.(type) {
	case protocol.CompletionItemSlice:
		items = v
	case *protocol.CompletionList:
		items = v.Items
	case nil:
		return nil
	default:
		t.Fatalf("unexpected completion result type %T", res)
	}
	labels := make([]string, len(items))
	for i, it := range items {
		labels[i] = it.Label
	}
	return labels
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

const tableDoc = `kind: Table
metadata:
  name: rev_table
spec:
  dataset: sales
  scenarios:
    - ac1
  variances:
    - dac1_pp1_pos
  title: Revenue
`

func TestCompletion_Kind(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, tableDoc)
	res, _ := s.Completion(context.Background(), completionParams(u, 0, 6)) // on "Table"
	labels := completionLabels(t, res)
	for _, want := range []string{"Table", "ChartTime", "DataSet"} {
		if !contains(labels, want) {
			t.Errorf("kind completion missing %q (got %v)", want, labels)
		}
	}
}

func TestCompletion_ScenarioFiltersToColumns(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, tableDoc)
	// Line 7 (0-based 6) is "    - ac1" under scenarios.
	res, _ := s.Completion(context.Background(), completionParams(u, 6, 7))
	labels := completionLabels(t, res)
	if !contains(labels, "ac1") || !contains(labels, "pp1") {
		t.Errorf("scenario completion should include ac1/pp1 (got %v)", labels)
	}
	if contains(labels, "fc1") {
		t.Errorf("scenario completion should be filtered to the dataset's columns; fc1 leaked (got %v)", labels)
	}
	if !contains(labels, "auto") {
		t.Errorf("scenario completion should include 'auto' (got %v)", labels)
	}
}

func TestCompletion_DatasetRef(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, tableDoc)
	res, _ := s.Completion(context.Background(), completionParams(u, 4, 12)) // on "sales"
	labels := completionLabels(t, res)
	if !contains(labels, "sales") {
		t.Errorf("dataset completion should include the DataSet 'sales' (got %v)", labels)
	}
	if contains(labels, "sales_src") {
		t.Errorf("dataset completion should be filtered to DataSet kind; sales_src leaked (got %v)", labels)
	}
}

func TestCompletion_SpecFields(t *testing.T) {
	s := newTestServer()
	// A blank line under spec invites a new field.
	doc := "kind: Table\nmetadata:\n  name: t\nspec:\n  dataset: sales\n  \n"
	u := openDoc(t, s, doc)
	res, _ := s.Completion(context.Background(), completionParams(u, 5, 2))
	labels := completionLabels(t, res)
	if !contains(labels, "title") || !contains(labels, "scenarios") {
		t.Errorf("field completion should surface spec fields title/scenarios (got %v)", labels)
	}
}

func TestHover_Scenario(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, tableDoc)
	h, _ := s.Hover(context.Background(), hoverParams(u, 6, 7))
	if h == nil {
		t.Fatal("expected hover for scenario token")
	}
	mc, ok := h.Contents.(*protocol.MarkupContent)
	if !ok || !strings.Contains(mc.Value, "Actual") {
		t.Errorf("scenario hover should explain ac1 as Actual (got %+v)", h.Contents)
	}
}

func TestHover_FieldDescription(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, tableDoc)
	// "title:" value on the last content line.
	h, _ := s.Hover(context.Background(), hoverParams(u, 9, 10))
	if h == nil {
		t.Fatal("expected hover for the title field")
	}
	mc, ok := h.Contents.(*protocol.MarkupContent)
	if !ok || !strings.Contains(mc.Value, "table title") {
		t.Errorf("field hover should carry the schema description (got %q)", mc.Value)
	}
}

func completionParams(u uri.URI, line, char uint32) *protocol.CompletionParams {
	return &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     protocol.Position{Line: line, Character: char},
		},
	}
}

func hoverParams(u uri.URI, line, char uint32) *protocol.HoverParams {
	return &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     protocol.Position{Line: line, Character: char},
		},
	}
}
