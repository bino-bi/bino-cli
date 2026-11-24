package lsp

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestQueryCompletion_TablesAfterFrom(t *testing.T) {
	tests := []struct {
		name      string
		doc       string
		line, col uint32
	}{
		{"block scalar", "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: |\n    SELECT *\n    FROM \n", 6, 9},
		{"inline", "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: SELECT * FROM sa\n", 4, 26},
		{"join", "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: |\n    SELECT * FROM a JOIN \n", 5, 25},
		{"prql", "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  prql: |\n    from \n", 5, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			u := openDoc(t, s, tt.doc)
			res, err := s.Completion(context.Background(), completionParams(u, tt.line, tt.col))
			if err != nil {
				t.Fatal(err)
			}
			labels := completionLabels(t, res)
			if !contains(labels, "sales") || !contains(labels, "sales_src") {
				t.Errorf("want DataSet and DataSource names, got %v", labels)
			}
			if contains(labels, "SELECT") || contains(labels, "region") {
				t.Errorf("after FROM only table names belong in the list, got %v", labels)
			}
		})
	}
}

func TestQueryCompletion_KeywordsFunctionsAndColumns(t *testing.T) {
	s := newTestServer()
	doc := "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  dependencies: [sales_src]\n  query: |\n    SELECT su\n"
	u := openDoc(t, s, doc)
	res, _ := s.Completion(context.Background(), completionParams(u, 6, 13))
	labels := completionLabels(t, res)
	for _, want := range []string{"region", "SELECT", "WHERE", "sum", "strftime"} {
		if !contains(labels, want) {
			t.Errorf("missing %q in %d items", want, len(labels))
		}
	}
}

func TestQueryCompletion_NothingInsideStringLiteral(t *testing.T) {
	s := newTestServer()
	doc := "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: |\n    SELECT * FROM t WHERE a = 'FROM \n"
	u := openDoc(t, s, doc)
	res, _ := s.Completion(context.Background(), completionParams(u, 5, 35))
	if labels := completionLabels(t, res); len(labels) != 0 {
		t.Errorf("no completion inside a string literal, got %v", labels)
	}
}

func TestScanQuery(t *testing.T) {
	tests := []struct {
		text   string
		prql   bool
		prev   string
		part   string
		quoted bool
		lit    bool
	}{
		{"SELECT * FROM sal", false, "from", "sal", false, false},
		{"select * from\n  ", false, "from", "", false, false},
		{"SELECT * FROM a, ", false, ",", "", false, false},
		{"SELECT * FROM \"my-d", false, "from", "my-d", true, false},
		{"SELECT 1 -- from ", false, "", "", false, true},
		{"SELECT /* from */ x FROM ", false, "from", "", false, false},
		{"from x\njoin `a-b", true, "join", "a-b", true, false},
		{"from x # join ", true, "", "", false, true},
	}
	for _, tt := range tests {
		got := scanQuery(tt.text, tt.prql)
		if got.prev != tt.prev || got.partial != tt.part || got.quoted != tt.quoted || got.inLiteral != tt.lit {
			t.Errorf("scanQuery(%q) = %+v", tt.text, got)
		}
	}
}

func TestQueryDefinition_FromName(t *testing.T) {
	s := newTestServer()
	dataURI := uri.File("/proj/data.yaml")
	s.docs.Set(dataURI, "kind: DataSource\nmetadata:\n  name: sales_src\nspec:\n  type: csv\n", 1)
	u := openDoc(t, s, "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: |\n    SELECT *\n    FROM sales_src\n")
	res, _ := s.Definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     protocol.Position{Line: 6, Character: 12},
		},
	})
	locs, ok := res.(protocol.LocationSlice)
	if !ok || len(locs) != 1 {
		t.Fatalf("expected one location, got %T %v", res, res)
	}
	if locs[0].URI != dataURI || locs[0].Range.Start.Line != 2 {
		t.Errorf("definition = %v, want data.yaml line 2", locs[0])
	}
}

// queryBackend adds column errors, typed samples and call counts to fakeBackend.
type queryBackend struct {
	fakeBackend
	colErr  map[string]error
	calls   atomic.Int32
	samples map[string]DatasetSample
}

func (b *queryBackend) Columns(ctx context.Context, name string) ([]string, error) {
	b.calls.Add(1)
	if err := b.colErr[name]; err != nil {
		return nil, err
	}
	return b.fakeBackend.Columns(ctx, name)
}

func (b *queryBackend) Sample(_ context.Context, name string, _ int) (DatasetSample, error) {
	if smp, ok := b.samples[name]; ok {
		return smp, nil
	}
	return DatasetSample{}, errors.New("no sample")
}

func newQueryServer(be *queryBackend) *Server {
	base := newTestServer()
	fb, _ := base.backend.(*fakeBackend)
	be.fakeBackend = *fb
	return NewServer(be, base.log, true, "/proj")
}

func TestQueryDiagnostics_ErrorOnBlockLine(t *testing.T) {
	be := &queryBackend{colErr: map[string]error{
		"ds": errors.New("query schema: Binder Error: Referenced column \"amout\" not found in FROM clause!\n" +
			"Candidate bindings: \"amount\"\n\nLINE 2:   amout\n          ^"),
	}}
	s := newQueryServer(be)
	s.queryDiagnostics.Store(true)
	doc := "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: |\n    SELECT region,\n      amout\n    FROM sales_src\n"
	u := openDoc(t, s, doc)
	s.runQueryCheck(u.FsPath())
	diags := s.queryDiags[u.FsPath()]
	if len(diags) != 1 {
		t.Fatalf("want one diagnostic, got %v", diags)
	}
	d := diags[0]
	if d.Range.Start.Line != 6 || d.Range.Start.Character != 6 {
		t.Errorf("range = %v, want line 6 (0-based) from the text start", d.Range)
	}
	msg := string(d.Message.(protocol.String))
	if !strings.HasPrefix(msg, "Binder Error: Referenced column") || strings.Contains(msg, "LINE") {
		t.Errorf("message = %q", msg)
	}
}

func TestQueryDiagnostics_IgnoresNonDuckDBErrors(t *testing.T) {
	be := &queryBackend{colErr: map[string]error{"ds": errors.New("document not found: ds")}}
	s := newQueryServer(be)
	s.queryDiagnostics.Store(true)
	u := openDoc(t, s, "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: SELECT 1\n")
	s.runQueryCheck(u.FsPath())
	if diags := s.queryDiags[u.FsPath()]; len(diags) != 0 {
		t.Errorf("want no diagnostics, got %v", diags)
	}
}

func TestQueryDiagnostics_Setting(t *testing.T) {
	s := newTestServer()
	if _, err := s.Initialize(context.Background(), &protocol.InitializeParams{InitializationOptions: protocol.LSPAny(`{"queryDiagnostics":true}`)}); err != nil {
		t.Fatal(err)
	}
	if !s.queryDiagnostics.Load() {
		t.Fatal("initializationOptions.queryDiagnostics did not enable the check")
	}
	s.queryDiags = map[string][]protocol.Diagnostic{"/proj/a.yaml": {{}}}
	_ = s.DidChangeConfiguration(context.Background(), &protocol.DidChangeConfigurationParams{Settings: protocol.LSPAny(`{"bino":{"queryDiagnostics":false}}`)})
	if s.queryDiagnostics.Load() || len(s.queryDiags) != 0 {
		t.Error("bino.queryDiagnostics=false must disable the check and clear its diagnostics")
	}
}

func TestParseDuckDBError(t *testing.T) {
	msg, line, ok := parseDuckDBError("query: Parser Error: syntax error at or near \"FORM\"\n\nLINE 3: FORM x\n        ^")
	if !ok || line != 3 || msg != "Parser Error: syntax error at or near \"FORM\"" {
		t.Errorf("got %q %d %v", msg, line, ok)
	}
	if _, _, ok := parseDuckDBError("context deadline exceeded"); ok {
		t.Error("a timeout is not a DuckDB error")
	}
}

func TestHover_DatasetSample(t *testing.T) {
	be := &queryBackend{samples: map[string]DatasetSample{
		"sales": {Kind: "DataSet", Columns: []string{"region", "amount"}, Types: []string{"VARCHAR", "DOUBLE"},
			Rows: []map[string]any{{"region": "EU|north", "amount": 12.5}, {"region": nil, "amount": 3.0}}},
	}}
	s := newQueryServer(be)
	u := openDoc(t, s, tableDoc)
	h, _ := s.Hover(context.Background(), hoverParams(u, 4, 12)) // on `dataset: sales`
	if h == nil {
		t.Fatal("expected a hover")
	}
	md := h.Contents.(*protocol.MarkupContent).Value
	for _, want := range []string{"region (VARCHAR)", "amount (DOUBLE)", "EU\\|north", "| NULL |", "12.5"} {
		if !strings.Contains(md, want) {
			t.Errorf("hover lacks %q:\n%s", want, md)
		}
	}

	q := openDoc(t, s, "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: SELECT * FROM sales\n")
	h, _ = s.Hover(context.Background(), hoverParams(q, 4, 26))
	if h == nil || !strings.Contains(h.Contents.(*protocol.MarkupContent).Value, "region (VARCHAR)") {
		t.Errorf("hover on a FROM name should show the sample, got %v", h)
	}
}

func TestColumnCache_InvalidatedOnProjectChange(t *testing.T) {
	be := &queryBackend{}
	s := newQueryServer(be)
	u := openDoc(t, s, tableDoc)
	complete := func() { _, _ = s.Completion(context.Background(), completionParams(u, 6, 7)) }
	complete()
	complete()
	if n := be.calls.Load(); n != 1 {
		t.Fatalf("columns fetched %d times, want 1 (cached)", n)
	}
	s.onProjectChange()
	complete()
	if n := be.calls.Load(); n != 2 {
		t.Errorf("columns fetched %d times after a project change, want 2", n)
	}
}
