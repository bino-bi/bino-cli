package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
		columns: map[string][]string{
			"sales":     {"ac1", "pp1", "category"},
			"sales_src": {"region", "amount", "period"},
		},
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

// refSchema mirrors the real schema shape: a kind's spec is a $ref to a $defs
// entry that composes a base via allOf, with enums on leaf fields.
const refSchema = `{
  "properties": {"kind": {"enum": ["Table"]}},
  "$defs": {
    "tableBase": {"properties": {"title": {"description": "The title."}}},
    "tableSpec": {"allOf": [
      {"$ref": "#/$defs/tableBase"},
      {"properties": {"orderDirection": {"enum": ["asc", "desc"]}}}
    ]}
  },
  "allOf": [
    {"if": {"properties": {"kind": {"const": "Table"}}},
     "then": {"properties": {"spec": {"$ref": "#/$defs/tableSpec"}}}}
  ]
}`

func TestCompletion_SchemaRefAndAllOf(t *testing.T) {
	be := &fakeBackend{schema: json.RawMessage(refSchema)}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(be, log, true, "/proj")
	doc := "kind: Table\nmetadata:\n  name: t\nspec:\n  orderDirection: desc\n"
	u := openDoc(t, s, doc)
	// enum value completion at end of "  orderDirection: desc"
	res, _ := s.Completion(context.Background(), completionParams(u, 4, 22))
	labels := completionLabels(t, res)
	if !contains(labels, "asc") || !contains(labels, "desc") {
		t.Errorf("orderDirection enum (via $ref+allOf) should offer asc/desc, got %v", labels)
	}
	// field-name completion under spec should surface allOf-composed fields
	doc2 := "kind: Table\nmetadata:\n  name: t\nspec:\n  \n"
	u2 := openDoc(t, s, doc2)
	res2, _ := s.Completion(context.Background(), completionParams(u2, 4, 2))
	labels2 := completionLabels(t, res2)
	if !contains(labels2, "title") || !contains(labels2, "orderDirection") {
		t.Errorf("field completion should surface $ref+allOf fields title/orderDirection, got %v", labels2)
	}
}

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

func TestCompletion_InQueryColumns(t *testing.T) {
	s := newTestServer()
	doc := "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  source: sales_src\n  query: |\n    SELECT \n"
	u := openDoc(t, s, doc)
	// Cursor inside the query block scalar, after "SELECT ".
	res, _ := s.Completion(context.Background(), completionParams(u, 6, 11))
	labels := completionLabels(t, res)
	for _, want := range []string{"region", "amount", "period"} {
		if !contains(labels, want) {
			t.Errorf("in-query completion should offer source column %q (got %v)", want, labels)
		}
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

// capturingClient records every PublishDiagnostics call so tests can inspect
// what was actually sent to the editor.
type capturingClient struct {
	protocol.UnimplementedClient
	mu    sync.Mutex
	calls []*protocol.PublishDiagnosticsParams
}

func (c *capturingClient) PublishDiagnostics(_ context.Context, p *protocol.PublishDiagnosticsParams) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, p)
	return nil
}

func (c *capturingClient) last() *protocol.PublishDiagnosticsParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[len(c.calls)-1]
}

func (c *capturingClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// TestRefreshProjectDiagnostics_MergesWithDraftAndClearsWhenClean guards the
// live-diagnostics gap: ValidateProject (lint/env-var/engine-compat, whole
// project on disk) must actually reach the editor, and must not be clobbered
// by — nor clobber — the per-keystroke ValidateDraft publishes, since
// PublishDiagnostics fully replaces a document's diagnostic set per call.
func TestRefreshProjectDiagnostics_MergesWithDraftAndClearsWhenClean(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "report.yaml")
	if err := os.WriteFile(file, []byte(tableDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	be := &fakeBackend{diags: []Diag{
		{File: file, Line: 1, Column: 1, Severity: "warning", Message: "lint: something", Code: "lint-rule"},
	}}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(be, log, true, dir)
	s.ctx = context.Background()
	client := &capturingClient{}
	s.client = client

	s.refreshProjectDiagnostics()
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 publish call from refreshProjectDiagnostics, got %d", len(client.calls))
	}
	if got := len(client.last().Diagnostics); got != 1 {
		t.Fatalf("expected 1 project diagnostic published, got %d", got)
	}

	// A subsequent draft (per-keystroke) publish for the same file must merge
	// with, not replace, the cached project diagnostic.
	u := uri.File(file)
	s.publishDraft(u, 2, []protocol.Diagnostic{{Message: protocol.String("schema error")}})
	if got := len(client.last().Diagnostics); got != 2 {
		t.Fatalf("draft publish should merge with cached project diagnostics, got %d: %+v", got, client.last().Diagnostics)
	}

	// A follow-up project refresh that finds the file clean must clear the
	// stale project diagnostic while preserving the still-current draft one.
	be.diags = nil
	s.refreshProjectDiagnostics()
	if got := len(client.last().Diagnostics); got != 1 {
		t.Fatalf("clean project revalidation should clear the stale lint diagnostic, got %d: %+v", got, client.last().Diagnostics)
	}
}

// newRealSchemaServer builds a server over the SHIPPED document schema, so the
// end-to-end completion cases exercise the real composition shapes.
func newRealSchemaServer(t *testing.T) *Server {
	t.Helper()
	raw, err := os.ReadFile("../schema/jsonschema/document.schema.json")
	if err != nil {
		t.Fatalf("read real schema: %v", err)
	}
	be := &fakeBackend{
		schema: json.RawMessage(raw),
		index:  []IndexDoc{{Kind: "Table", Name: "rev_table", File: "components.yaml", Position: 1}},
	}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	return NewServer(be, log, true, "/proj")
}

const pageDoc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: rev_table
`

// TestCompletion_NestedChildKind is the screenshot bug end to end: `kind:`
// inside a layout's children must offer the child-kind enum, not nothing.
func TestCompletion_NestedChildKind(t *testing.T) {
	s := newRealSchemaServer(t)
	u := openDoc(t, s, pageDoc)
	res, err := s.Completion(context.Background(), completionParams(u, 5, 12)) // on "Table"
	if err != nil {
		t.Fatal(err)
	}
	labels := completionLabels(t, res)
	for _, want := range []string{"Table", "Text", "ChartTime", "Tree"} {
		if !contains(labels, want) {
			t.Errorf("nested kind completion missing %q (got %v)", want, labels)
		}
	}
}

// TestCompletion_NestedChildKindEmpty: the same position with nothing typed
// yet (`- kind: `) — the exact "No suggestions." state.
func TestCompletion_NestedChildKindEmpty(t *testing.T) {
	s := newRealSchemaServer(t)
	doc := "kind: LayoutPage\nmetadata:\n  name: page\nspec:\n  children:\n    - kind: \n"
	u := openDoc(t, s, doc)
	res, err := s.Completion(context.Background(), completionParams(u, 5, 12))
	if err != nil {
		t.Fatal(err)
	}
	labels := completionLabels(t, res)
	if !contains(labels, "Table") || !contains(labels, "Text") {
		t.Fatalf("empty nested kind completion should offer the child enum (got %v)", labels)
	}
}

// TestCompletion_ChildKeys: key completion inside a layout child offers the
// child's own keys (ref/params/...), not the component's spec fields, and
// filters keys already present.
func TestCompletion_ChildKeys(t *testing.T) {
	s := newRealSchemaServer(t)
	doc := "kind: LayoutPage\nmetadata:\n  name: page\nspec:\n  children:\n    - kind: Table\n      \n"
	u := openDoc(t, s, doc)
	res, err := s.Completion(context.Background(), completionParams(u, 6, 6))
	if err != nil {
		t.Fatal(err)
	}
	labels := completionLabels(t, res)
	for _, want := range []string{"ref", "optional", "params", "spec"} {
		if !contains(labels, want) {
			t.Errorf("child key completion missing %q (got %v)", want, labels)
		}
	}
	if contains(labels, "dataset") {
		t.Errorf("component spec fields leaked into the child key position (got %v)", labels)
	}
	if contains(labels, "kind") {
		t.Errorf("already-present key 'kind' re-offered (got %v)", labels)
	}
}

// TestCompletion_RootAndMetadataKeys: key completion is depth-aware — the
// document root offers apiVersion/metadata/spec, and metadata offers its own
// fields, never the component's spec fields.
func TestCompletion_RootAndMetadataKeys(t *testing.T) {
	s := newRealSchemaServer(t)

	u := openDoc(t, s, "kind: Table\n\n")
	res, err := s.Completion(context.Background(), completionParams(u, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	labels := completionLabels(t, res)
	for _, want := range []string{"apiVersion", "metadata", "spec"} {
		if !contains(labels, want) {
			t.Errorf("root key completion missing %q (got %v)", want, labels)
		}
	}
	if contains(labels, "dataset") {
		t.Errorf("spec fields leaked into the root key position (got %v)", labels)
	}
	if contains(labels, "kind") {
		t.Errorf("already-present root key 'kind' re-offered (got %v)", labels)
	}

	u2 := openDoc(t, s, "kind: Table\nmetadata:\n  \nspec:\n  dataset: sales\n")
	res2, err := s.Completion(context.Background(), completionParams(u2, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	labels2 := completionLabels(t, res2)
	if !contains(labels2, "name") {
		t.Errorf("metadata key completion missing name (got %v)", labels2)
	}
	if contains(labels2, "dataset") {
		t.Errorf("spec fields leaked into the metadata key position (got %v)", labels2)
	}
}

// TestCompletion_RequiredFirstWithDetail: required fields sort first and carry
// a type/required/default detail.
func TestCompletion_RequiredFirstWithDetail(t *testing.T) {
	s := newRealSchemaServer(t)
	u := openDoc(t, s, "kind: Table\nmetadata:\n  name: t\nspec:\n  \n")
	res, err := s.Completion(context.Background(), completionParams(u, 4, 2))
	if err != nil {
		t.Fatal(err)
	}
	var items []protocol.CompletionItem
	switch v := res.(type) {
	case protocol.CompletionItemSlice:
		items = v
	case *protocol.CompletionList:
		items = v.Items
	}
	var checked bool
	for _, it := range items {
		if it.Label != "dataset" {
			continue
		}
		if st, ok := it.SortText.Get(); !ok || !strings.HasPrefix(st, "0_") {
			t.Errorf("required field dataset must sort first, SortText=%q", st)
		}
		if d, ok := it.Detail.Get(); !ok || !strings.Contains(d, "required") {
			t.Errorf("required field dataset must say so in its detail, Detail=%q", d)
		}
		checked = true
	}
	if !checked {
		t.Fatalf("dataset item not found in spec key completion")
	}
}

// TestHover_NestedChildKindAndKey: hover works at depth — a child `kind:`
// value explains the component slot, a spec key shows its schema metadata.
func TestHover_NestedChildKindAndKey(t *testing.T) {
	s := newRealSchemaServer(t)
	u := openDoc(t, s, pageDoc)
	h, err := s.Hover(context.Background(), hoverParams(u, 5, 12))
	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fatal("expected hover for the child kind value")
	}
	mc, ok := h.Contents.(*protocol.MarkupContent)
	if !ok || mc.Value == "" {
		t.Fatalf("child kind hover should carry the schema description, got %+v", h.Contents)
	}

	u2 := openDoc(t, s, "kind: Table\nmetadata:\n  name: t\nspec:\n  grouped: true\n")
	h2, err := s.Hover(context.Background(), hoverParams(u2, 4, 3)) // on the `grouped` key
	if err != nil {
		t.Fatal(err)
	}
	if h2 == nil {
		t.Fatal("expected hover for the grouped key")
	}
	mc2, ok := h2.Contents.(*protocol.MarkupContent)
	if !ok || !strings.Contains(mc2.Value, "grouped") {
		t.Fatalf("key hover should name the field, got %+v", h2.Contents)
	}
}

// errSchemaBackend simulates a backend whose schema fetch fails (cold daemon).
type errSchemaBackend struct{ fakeBackend }

func (e *errSchemaBackend) MergedSchema(context.Context) (json.RawMessage, error) {
	return nil, errors.New("schema not warm")
}

// slowSchemaBackend simulates a wedged backend that never answers until the
// request context is cancelled.
type slowSchemaBackend struct{ fakeBackend }

func (b *slowSchemaBackend) MergedSchema(ctx context.Context) (json.RawMessage, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestCompletion_ColdSchemaIncomplete: a failed schema fetch must yield an
// incomplete list — never a cacheable complete empty result — so the client
// re-queries once the backend warms instead of showing "No suggestions." for
// the rest of the typing session.
func TestCompletion_ColdSchemaIncomplete(t *testing.T) {
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(&errSchemaBackend{}, log, true, "/proj")
	u := openDoc(t, s, tableDoc)
	res, err := s.Completion(context.Background(), completionParams(u, 0, 6))
	if err != nil {
		t.Fatal(err)
	}
	list, ok := res.(*protocol.CompletionList)
	if !ok || !list.IsIncomplete {
		t.Fatalf("cold-schema completion should be an incomplete list, got %T: %+v", res, res)
	}
}

// TestCompletion_SlowBackendWithinBudget: a wedged backend must not block the
// completion popup; the fetch budget bounds the wait and the result is marked
// incomplete for a later retry.
func TestCompletion_SlowBackendWithinBudget(t *testing.T) {
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(&slowSchemaBackend{}, log, true, "/proj")
	u := openDoc(t, s, tableDoc)
	start := time.Now()
	res, err := s.Completion(context.Background(), completionParams(u, 0, 6))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("completion blocked %v on a wedged backend; fetch budget not applied", elapsed)
	}
	list, ok := res.(*protocol.CompletionList)
	if !ok || !list.IsIncomplete {
		t.Fatalf("slow-backend completion should be an incomplete list, got %T: %+v", res, res)
	}
}

// TestCompletion_ColdIndexIncomplete: reference completion with no index yet
// must be marked incomplete so the client re-queries once the index warms.
func TestCompletion_ColdIndexIncomplete(t *testing.T) {
	be := &fakeBackend{schema: json.RawMessage(testSchema)} // index deliberately nil
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(be, log, true, "/proj")
	u := openDoc(t, s, tableDoc)
	res, err := s.Completion(context.Background(), completionParams(u, 4, 12)) // on the dataset value
	if err != nil {
		t.Fatal(err)
	}
	list, ok := res.(*protocol.CompletionList)
	if !ok || !list.IsIncomplete {
		t.Fatalf("cold-index ref completion should be an incomplete list, got %T: %+v", res, res)
	}
}

// TestCompletion_PadsAfterColon: with ':' as a trigger character (and ' ' no
// longer one), accepting a value completion fired directly on the colon must
// insert a leading space so the result is `kind: Table`, not `kind:Table`.
func TestCompletion_PadsAfterColon(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, "kind:")
	res, err := s.Completion(context.Background(), completionParams(u, 0, 5))
	if err != nil {
		t.Fatal(err)
	}
	labels := completionLabels(t, res)
	if !contains(labels, "Table") {
		t.Fatalf("kind completion right after the colon should offer Table (got %v)", labels)
	}
	var items []protocol.CompletionItem
	switch v := res.(type) {
	case protocol.CompletionItemSlice:
		items = v
	case *protocol.CompletionList:
		items = v.Items
	}
	for _, it := range items {
		if it.Label != "Table" {
			continue
		}
		got, ok := it.InsertText.Get()
		if !ok || got != " Table" {
			t.Fatalf("item after ':' should insert %q, got %q (present=%v)", " Table", got, ok)
		}
		return
	}
	t.Fatal("Table item not found")
}

// TestCompletion_NoPadAfterSpace: the same value completion with a space
// already typed must insert the bare label.
func TestCompletion_NoPadAfterSpace(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, "kind: ")
	res, err := s.Completion(context.Background(), completionParams(u, 0, 6))
	if err != nil {
		t.Fatal(err)
	}
	var items []protocol.CompletionItem
	switch v := res.(type) {
	case protocol.CompletionItemSlice:
		items = v
	case *protocol.CompletionList:
		items = v.Items
	}
	for _, it := range items {
		if it.Label != "Table" {
			continue
		}
		if got, ok := it.InsertText.Get(); ok && strings.HasPrefix(got, " ") {
			t.Fatalf("item after 'kind: ' must not be space-padded, got insert text %q", got)
		}
		return
	}
	t.Fatal("Table item not found")
}

// projectDiagServer builds a server whose backend reports one project-level
// diagnostic (code `code`) for an on-disk copy of tableDoc, with a capturing
// client wired in. It returns the server, client, and the file path.
func projectDiagServer(t *testing.T, code string) (*Server, *capturingClient, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "report.yaml")
	if err := os.WriteFile(file, []byte(tableDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	be := &fakeBackend{diags: []Diag{
		{File: file, Line: 1, Column: 1, Severity: "warning", Message: "finding", Code: code},
	}}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(be, log, true, dir)
	s.ctx = context.Background()
	client := &capturingClient{}
	s.client = client
	return s, client, file
}

// TestPublishFile_DedupesDraftCoveredProjectDiags: while a draft entry exists
// for an open file, project diagnostics of the classes ValidateDraft
// reproduces (schema-validation etc.) must be dropped from the merge — an
// open-and-saved invalid file otherwise shows every schema error twice.
func TestPublishFile_DedupesDraftCoveredProjectDiags(t *testing.T) {
	s, client, file := projectDiagServer(t, "schema-validation")
	s.refreshProjectDiagnostics()
	if got := len(client.last().Diagnostics); got != 1 {
		t.Fatalf("expected the project diagnostic alone before any draft, got %d", got)
	}

	u := uri.File(file)
	s.publishDraft(u, 1, []protocol.Diagnostic{{
		Message: protocol.String("missing property 'spec'"),
		Code:    protocol.String("schema-validation"),
	}})
	diags := client.last().Diagnostics
	if len(diags) != 1 {
		t.Fatalf("draft + same-class project diagnostic must dedupe to 1, got %d: %+v", len(diags), diags)
	}
	if msg := diags[0].Message; msg != protocol.String("missing property 'spec'") {
		t.Fatalf("the draft diagnostic must win the merge, got %v", msg)
	}

	// Closing the buffer clears the draft entry; the project diagnostic returns.
	s.clearDraft(u)
	if got := len(client.last().Diagnostics); got != 1 {
		t.Fatalf("after close the project diagnostic must be republished, got %d", got)
	}
	if msg := client.last().Diagnostics[0].Message; msg != protocol.String("finding") {
		t.Fatalf("after close the project diagnostic must remain, got %v", msg)
	}
}

// TestServer_DidCloseClearsDraftDiagnostics: closing a buffer must clear its
// draft diagnostics and keep the on-disk project findings.
func TestServer_DidCloseClearsDraftDiagnostics(t *testing.T) {
	s, client, file := projectDiagServer(t, "lint-rule")
	s.analyzer = NewAnalyzer(context.Background(), s.backend, s.docs, s.publishDraft, s.log, 0)
	s.refreshProjectDiagnostics()

	u := uri.File(file)
	s.docs.Set(u, tableDoc, 1)
	s.publishDraft(u, 1, []protocol.Diagnostic{{
		Message: protocol.String("schema error"),
		Code:    protocol.String("schema-validation"),
	}})
	if got := len(client.last().Diagnostics); got != 2 {
		t.Fatalf("expected draft + lint project diagnostic before close, got %d", got)
	}

	err := s.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
	})
	if err != nil {
		t.Fatal(err)
	}
	diags := client.last().Diagnostics
	if len(diags) != 1 {
		t.Fatalf("close must clear draft diagnostics and keep project ones, got %d: %+v", len(diags), diags)
	}
	if msg := diags[0].Message; msg != protocol.String("finding") {
		t.Fatalf("the surviving diagnostic must be the project finding, got %v", msg)
	}
}

// TestInitialized_PublishesProjectDiagnosticsAtStartup: project-wide findings
// must reach the editor after the handshake, not only after the first on-disk
// change.
func TestInitialized_PublishesProjectDiagnosticsAtStartup(t *testing.T) {
	s, client, _ := projectDiagServer(t, "lint-rule")
	if err := s.Initialized(context.Background(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if client.count() > 0 {
			if got := len(client.last().Diagnostics); got != 1 {
				t.Fatalf("expected the startup project diagnostic, got %d", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Initialized never published the project diagnostics")
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
