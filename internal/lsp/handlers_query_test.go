package lsp

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
		{"SELECT * FROM umsä", false, "from", "umsä", false, false},
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
	gate    chan struct{} // when set, Columns waits for it
}

func (b *queryBackend) Columns(ctx context.Context, name string) ([]string, error) {
	b.calls.Add(1)
	if b.gate != nil {
		select {
		case <-b.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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
	checkNow(s, u.FsPath())
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
	checkNow(s, u.FsPath())
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

// checkNow runs a file's query check as the current schedule would.
func checkNow(s *Server, path string) {
	s.checks.schedule(path, func(uint64) {})
	s.checks.mu.Lock()
	seq := s.checks.seq[path]
	s.checks.mu.Unlock()
	s.runQueryCheck(path, seq)
}

var errBrokenColumn = errors.New("query schema: Binder Error: Referenced column \"x\" not found\n\nLINE 1: SELECT x")

func TestQueryDiagnostics_SupersededAndClosedRunsAreDropped(t *testing.T) {
	be := &queryBackend{colErr: map[string]error{"ds": errBrokenColumn}}
	s := newQueryServer(be)
	s.queryDiagnostics.Store(true)
	u := openDoc(t, s, "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: SELECT x\n")
	path := u.FsPath()
	s.checks.schedule(path, func(uint64) {})
	s.checks.mu.Lock()
	old := s.checks.seq[path]
	s.checks.mu.Unlock()
	s.checks.schedule(path, func(uint64) {}) // saved again
	s.runQueryCheck(path, old)
	if _, ok := s.queryDiags[path]; ok {
		t.Fatal("a superseded check stored its result")
	}
	checkNow(s, path)
	if len(s.queryDiags[path]) != 1 {
		t.Fatalf("the current check must store its result, got %v", s.queryDiags[path])
	}
	s.forgetQueryCheck(u) // what DidClose does for query checks
	if _, ok := s.queryDiags[path]; ok {
		t.Error("closing the file must clear its query diagnostics")
	}
}

func TestQueryChecks_FallbackRerunsAfterRefresh(t *testing.T) {
	var q queryChecks
	runs := make(chan uint64, 4)
	q.schedule("/p.yaml", func(seq uint64) { runs <- seq })
	q.mu.Lock()
	p := q.pending["/p.yaml"]
	q.mu.Unlock()
	q.fire("/p.yaml", p, func(seq uint64) { runs <- seq }) // the fallback, no refresh yet
	<-runs
	q.projectChanged()
	select {
	case <-runs:
	case <-time.After(2 * time.Second):
		t.Fatal("a check that ran before the refresh was not re-run after it")
	}
	q.stop()
}

func TestDataCache_InvalidateDuringFetch(t *testing.T) {
	gate := make(chan struct{})
	be := &queryBackend{gate: gate}
	s := newQueryServer(be)
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, ok := s.data.columns(short, be, "sales"); ok {
		t.Fatal("the gated fetch cannot have finished")
	}
	s.data.invalidate()
	close(gate)
	cols, ok := s.data.columns(context.Background(), be, "sales")
	if !ok || len(cols) != 3 {
		t.Fatalf("a lookup after invalidate must start a fresh fetch, got %v %v", cols, ok)
	}
	if n := be.calls.Load(); n != 2 {
		t.Errorf("fetches = %d, want 2", n)
	}
}

func TestDataCache_CachesErrors(t *testing.T) {
	be := &queryBackend{colErr: map[string]error{"sales": errBrokenColumn}}
	s := newQueryServer(be)
	for range 3 {
		if _, ok := s.data.columns(context.Background(), be, "sales"); ok {
			t.Fatal("a broken dataset has no columns")
		}
	}
	if n := be.calls.Load(); n != 1 {
		t.Errorf("a broken dataset was fetched %d times, want 1", n)
	}
}

func TestQueryCompletion_PRQLQuotesWithBackticks(t *testing.T) {
	items := completeTableNames([]IndexDoc{{Kind: "DataSet", Name: "my-ds"}}, "", false, true)
	if got, _ := items[0].InsertText.Get(); got != "`my-ds`" {
		t.Errorf("PRQL insert text = %q, want backticks", got)
	}
	items = completeTableNames([]IndexDoc{{Kind: "DataSet", Name: "my-ds"}}, "", false, false)
	if got, _ := items[0].InsertText.Get(); got != `"my-ds"` {
		t.Errorf("SQL insert text = %q, want double quotes", got)
	}
}

func TestQueryDefinition_ClosedDocument(t *testing.T) {
	s := newTestServer()
	res, err := s.Definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri.File("/proj/closed.yaml")},
		},
	})
	if err != nil || res != nil {
		t.Errorf("want no result for a closed document, got %v %v", res, err)
	}
}

func TestQueryCompletion_NoPaddingAfterSQLCast(t *testing.T) {
	s := newTestServer()
	u := openDoc(t, s, "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: |\n    SELECT a::\n")
	res, _ := s.Completion(context.Background(), completionParams(u, 5, 14))
	list, ok := res.(protocol.CompletionItemSlice)
	if !ok || len(list) == 0 {
		t.Fatalf("want items after a cast, got %T", res)
	}
	for _, it := range list {
		if v, ok := it.InsertText.Get(); ok && strings.HasPrefix(v, " ") {
			t.Fatalf("item %q got a leading space inside a query", it.Label)
		}
	}
}

func TestQueryErrorRange_FoldedFlagNotComment(t *testing.T) {
	be := &queryBackend{colErr: map[string]error{"ds": errors.New("Binder Error: bad\n\nLINE 2: x")}}
	s := newQueryServer(be)
	s.queryDiagnostics.Store(true)
	u := openDoc(t, s, "kind: DataSet\nmetadata:\n  name: ds\nspec:\n  query: | # a > b\n    SELECT\n      x\n")
	checkNow(s, u.FsPath())
	if d := s.queryDiags[u.FsPath()]; len(d) != 1 || d[0].Range.Start.Line != 6 {
		t.Errorf("a '>' in the header comment must not mark the block folded: %v", d)
	}
}

// cancelBackend cancels the check's base context when it reaches DataSet at.
type cancelBackend struct {
	queryBackend
	at     string
	cancel context.CancelFunc
}

func (b *cancelBackend) Columns(ctx context.Context, name string) ([]string, error) {
	if name == b.at && b.cancel != nil {
		b.cancel()
		return nil, context.Canceled
	}
	return b.queryBackend.Columns(ctx, name)
}

func TestQueryDiagnostics_BudgetKeepsUncheckedResults(t *testing.T) {
	be := &cancelBackend{queryBackend: queryBackend{colErr: map[string]error{"a": errBrokenColumn, "b": errBrokenColumn, "c": errBrokenColumn}}, at: "b"}
	base := newTestServer()
	fb, _ := base.backend.(*fakeBackend)
	be.fakeBackend = *fb
	s := NewServer(be, base.log, true, "/proj")
	s.queryDiagnostics.Store(true)
	doc := "kind: DataSet\nmetadata:\n  name: a\nspec:\n  query: SELECT x\n---\n" +
		"kind: DataSet\nmetadata:\n  name: b\nspec:\n  query: SELECT x\n---\n" +
		"kind: DataSet\nmetadata:\n  name: c\nspec:\n  query: SELECT x\n"
	u := openDoc(t, s, doc)
	path := u.FsPath()
	checkNow(s, path)
	if len(s.queryDiags[path]) != 3 {
		t.Fatalf("first check: want 3 diagnostics, got %v", s.queryDiags[path])
	}

	// a is fixed; the budget runs out at b, so b and c keep their findings.
	delete(be.colErr, "a")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx, be.cancel = ctx, cancel
	checkNow(s, path)
	lines := make([]uint32, 0, len(s.queryDiags[path]))
	for _, d := range s.queryDiags[path] {
		lines = append(lines, d.Range.Start.Line)
	}
	if len(lines) != 2 || lines[0] != 10 || lines[1] != 16 {
		t.Errorf("diagnostic lines = %v, want [10 16] (b and c)", lines)
	}
}
