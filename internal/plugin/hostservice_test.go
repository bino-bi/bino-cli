package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"

	"bino.bi/bino/pkg/duckdb"
)

// testDuckDBOpener returns an opener that creates fresh in-memory sessions
// with the extension cache isolated to a per-test temp dir.
func testDuckDBOpener(t *testing.T) func(ctx context.Context) (*duckdb.Session, error) {
	t.Helper()
	cacheDir := t.TempDir()
	return func(ctx context.Context) (*duckdb.Session, error) {
		return duckdb.OpenSession(ctx, duckdb.Options{CacheDir: cacheDir})
	}
}

func TestQueryDuckDB_NilOpener(t *testing.T) {
	h := NewBinoHostServer()

	resp, err := h.QueryDuckDB(t.Context(), &pluginv1.QueryRequest{Sql: "SELECT 1"})
	if err == nil {
		t.Fatal("expected a clean error when no DuckDB opener is configured")
	}
	if resp != nil {
		t.Fatalf("expected nil response alongside the error, got %+v", resp)
	}
}

func TestQueryDuckDB_OpenerError(t *testing.T) {
	h := NewBinoHostServer()
	openErr := errors.New("session boom")
	h.SetDuckDBOpener(func(context.Context) (*duckdb.Session, error) { return nil, openErr })

	resp, err := h.QueryDuckDB(t.Context(), &pluginv1.QueryRequest{Sql: "SELECT 1"})
	if !errors.Is(err, openErr) {
		t.Fatalf("expected the opener error to be wrapped, got %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response alongside the error, got %+v", resp)
	}
}

// TestQueryDuckDB_FailingQuery pins the swallowed-error contract documented
// on QueryDuckDB: a failing query returns (response with ERROR diagnostic,
// nil error), never a Go error.
func TestQueryDuckDB_FailingQuery(t *testing.T) {
	h := NewBinoHostServer()
	h.SetDuckDBOpener(testDuckDBOpener(t))

	resp, err := h.QueryDuckDB(t.Context(), &pluginv1.QueryRequest{Sql: "SELECT * FROM no_such_table"})
	if err != nil {
		t.Fatalf("a failing query must not return a Go error, got %v", err)
	}
	if len(resp.GetDiagnostics()) != 1 {
		t.Fatalf("expected exactly 1 diagnostic, got %d: %+v", len(resp.GetDiagnostics()), resp.GetDiagnostics())
	}
	d := resp.GetDiagnostics()[0]
	if d.GetSeverity() != pluginv1.Severity_ERROR {
		t.Fatalf("diagnostic severity = %v, want ERROR", d.GetSeverity())
	}
	if d.GetSource() != "host" || d.GetStage() != "query" {
		t.Fatalf("diagnostic source/stage = %q/%q, want \"host\"/\"query\"", d.GetSource(), d.GetStage())
	}
	if d.GetMessage() == "" {
		t.Fatal("diagnostic message must carry the query error text")
	}
	if len(resp.GetJsonRows()) != 0 || len(resp.GetColumns()) != 0 {
		t.Fatalf("a failing query must carry no data, got rows=%s cols=%v", resp.GetJsonRows(), resp.GetColumns())
	}
}

func TestQueryDuckDB_Success(t *testing.T) {
	h := NewBinoHostServer()
	h.SetDuckDBOpener(testDuckDBOpener(t))

	t.Run("rows and columns round-trip as JSON", func(t *testing.T) {
		resp, err := h.QueryDuckDB(t.Context(), &pluginv1.QueryRequest{Sql: "SELECT 1 AS n, 'a' AS s"})
		if err != nil {
			t.Fatalf("QueryDuckDB: %v", err)
		}
		if len(resp.GetDiagnostics()) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", resp.GetDiagnostics())
		}
		if !reflect.DeepEqual(resp.GetColumns(), []string{"n", "s"}) {
			t.Fatalf("columns = %v, want [n s]", resp.GetColumns())
		}
		var rows []map[string]any
		if err := json.Unmarshal(resp.GetJsonRows(), &rows); err != nil {
			t.Fatalf("json_rows is not valid JSON: %v (%s)", err, resp.GetJsonRows())
		}
		want := []map[string]any{{"n": float64(1), "s": "a"}}
		if !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
	})

	t.Run("empty result serializes as an empty array, not null", func(t *testing.T) {
		resp, err := h.QueryDuckDB(t.Context(), &pluginv1.QueryRequest{Sql: "SELECT 1 AS n WHERE 1 = 0"})
		if err != nil {
			t.Fatalf("QueryDuckDB: %v", err)
		}
		if got := string(resp.GetJsonRows()); got != "[]" {
			t.Fatalf("json_rows = %q, want %q", got, "[]")
		}
		if !reflect.DeepEqual(resp.GetColumns(), []string{"n"}) {
			t.Fatalf("columns = %v, want [n]", resp.GetColumns())
		}
	})
}

// queryRows runs sql and decodes the JSON rows. It fails the test on any
// ERROR diagnostic and returns the diagnostics for further checks.
func queryRows(t *testing.T, h *BinoHostServer, sql string) ([]map[string]any, []*pluginv1.Diagnostic) {
	t.Helper()
	resp, err := h.QueryDuckDB(t.Context(), &pluginv1.QueryRequest{Sql: sql})
	if err != nil {
		t.Fatalf("QueryDuckDB(%q): %v", sql, err)
	}
	for _, d := range resp.GetDiagnostics() {
		if d.GetSeverity() == pluginv1.Severity_ERROR {
			t.Fatalf("QueryDuckDB(%q): ERROR diagnostic: %s", sql, d.GetMessage())
		}
	}
	var rows []map[string]any
	if err := json.Unmarshal(resp.GetJsonRows(), &rows); err != nil {
		t.Fatalf("json_rows is not valid JSON: %v (%s)", err, resp.GetJsonRows())
	}
	return rows, resp.GetDiagnostics()
}

func TestQueryDuckDB_DatasetViews(t *testing.T) {
	sales := DatasetPayload{Name: "sales", JSONRows: []byte(`[{"m":"Jan","ac":1},{"m":"Feb","ac":2}]`)}

	t.Run("dataset is a view by name", func(t *testing.T) {
		h := NewBinoHostServer()
		h.SetDuckDBOpener(testDuckDBOpener(t))
		h.SetDatasets([]DatasetPayload{sales})
		t.Cleanup(h.Close)

		rows, diags := queryRows(t, h, "SELECT sum(ac) AS s FROM sales")
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		if want := []map[string]any{{"s": float64(3)}}; !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
	})

	t.Run("empty dataset is an empty view", func(t *testing.T) {
		h := NewBinoHostServer()
		h.SetDuckDBOpener(testDuckDBOpener(t))
		h.SetDatasets([]DatasetPayload{{Name: "empty", JSONRows: []byte("[]")}})
		t.Cleanup(h.Close)

		rows, diags := queryRows(t, h, "SELECT * FROM empty")
		if len(diags) != 0 || len(rows) != 0 {
			t.Fatalf("rows = %+v, diags = %+v, want no rows and no diagnostics", rows, diags)
		}
	})

	t.Run("plugin DDL dies with the session", func(t *testing.T) {
		h := NewBinoHostServer()
		h.SetDuckDBOpener(testDuckDBOpener(t))
		h.SetDatasets([]DatasetPayload{sales})
		t.Cleanup(h.Close)

		queryRows(t, h, "DROP VIEW sales")
		rows, _ := queryRows(t, h, "SELECT count(*) AS n FROM sales")
		if want := []map[string]any{{"n": float64(2)}}; !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
	})

	t.Run("nested column does not break other datasets", func(t *testing.T) {
		h := NewBinoHostServer()
		h.SetDuckDBOpener(testDuckDBOpener(t))
		h.SetDatasets([]DatasetPayload{{Name: "tagged", JSONRows: []byte(`[{"tags":["a","b"]}]`)}, sales})
		t.Cleanup(h.Close)

		rows, diags := queryRows(t, h, "SELECT count(*) AS n FROM sales")
		if want := []map[string]any{{"n": float64(2)}}; !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		rows, _ = queryRows(t, h, "SELECT len(tags) AS n FROM tagged")
		if want := []map[string]any{{"n": float64(2)}}; !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
	})

	t.Run("broken dataset gives one warning, not an error", func(t *testing.T) {
		h := NewBinoHostServer()
		h.SetDuckDBOpener(testDuckDBOpener(t))
		h.SetDatasets([]DatasetPayload{{Name: "broken", JSONRows: []byte(`not json`)}, sales})
		t.Cleanup(h.Close)

		rows, diags := queryRows(t, h, "SELECT count(*) AS n FROM sales")
		if want := []map[string]any{{"n": float64(2)}}; !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
		if len(diags) != 1 || diags[0].GetSeverity() != pluginv1.Severity_WARNING || !strings.Contains(diags[0].GetMessage(), `"broken"`) {
			t.Fatalf("diags = %+v, want one WARNING naming the dataset", diags)
		}
	})

	t.Run("no views before SetDatasets or after SetDatasets(nil)", func(t *testing.T) {
		h := NewBinoHostServer()
		h.SetDuckDBOpener(testDuckDBOpener(t))
		t.Cleanup(h.Close)

		const countViews = "SELECT count(*) AS n FROM duckdb_views() WHERE NOT internal"
		zero := []map[string]any{{"n": float64(0)}}
		if rows, _ := queryRows(t, h, countViews); !reflect.DeepEqual(rows, zero) {
			t.Fatalf("before SetDatasets: rows = %+v, want %+v", rows, zero)
		}
		h.SetDatasets([]DatasetPayload{sales})
		queryRows(t, h, "SELECT * FROM sales")
		h.SetDatasets(nil)
		if rows, _ := queryRows(t, h, countViews); !reflect.DeepEqual(rows, zero) {
			t.Fatalf("after SetDatasets(nil): rows = %+v, want %+v", rows, zero)
		}
	})

	t.Run("string column keeps leading zeros", func(t *testing.T) {
		h := NewBinoHostServer()
		h.SetDuckDBOpener(testDuckDBOpener(t))
		h.SetDatasets([]DatasetPayload{{Name: "codes", JSONRows: []byte(`[{"code":"001"}]`)}})
		t.Cleanup(h.Close)

		rows, _ := queryRows(t, h, "SELECT code FROM codes")
		if want := []map[string]any{{"code": "001"}}; !reflect.DeepEqual(rows, want) {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
	})
}

func TestQueryDuckDB_DatasetFilesLifecycle(t *testing.T) {
	h := NewBinoHostServer()
	h.SetDuckDBOpener(testDuckDBOpener(t))
	h.SetDatasets([]DatasetPayload{{Name: "sales", JSONRows: []byte(`[{"ac":1}]`)}})
	t.Cleanup(h.Close)

	viewDir := func() string {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return h.viewDir
	}
	if dir := viewDir(); dir != "" {
		t.Fatalf("files written before any query: %s", dir)
	}

	queryRows(t, h, "SELECT * FROM sales")
	first := viewDir()
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("dataset dir missing after a query: %v", err)
	}

	h.SetDatasets([]DatasetPayload{{Name: "sales", JSONRows: []byte(`[{"ac":5}]`)}})
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("SetDatasets kept the old dir %s (stat err %v)", first, err)
	}
	rows, _ := queryRows(t, h, "SELECT ac FROM sales")
	if want := []map[string]any{{"ac": float64(5)}}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}

	// After Close the host is still usable and writes a fresh dir, as on a
	// plugin reload that reuses the host service.
	second := viewDir()
	h.Close()
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("Close kept %s (stat err %v)", second, err)
	}
	rows, _ = queryRows(t, h, "SELECT ac FROM sales")
	if want := []map[string]any{{"ac": float64(5)}}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("after Close: rows = %+v, want %+v", rows, want)
	}
	third := viewDir()
	if third == "" || third == second {
		t.Fatalf("after Close: dir = %q, want a fresh dir", third)
	}
	if _, err := os.Stat(third); err != nil {
		t.Fatalf("fresh dir missing: %v", err)
	}
}

// TestQueryDuckDB_DatasetDirOutlivesSetDatasets covers a SetDatasets from
// another render while a query still reads the old files.
func TestQueryDuckDB_DatasetDirOutlivesSetDatasets(t *testing.T) {
	h := NewBinoHostServer()
	h.SetDatasets([]DatasetPayload{{Name: "sales", JSONRows: []byte(`[{"ac":1}]`)}})
	t.Cleanup(h.Close)

	views, dir, err := h.datasetViews()
	if err != nil {
		t.Fatalf("datasetViews: %v", err)
	}
	h.SetDatasets(nil)
	if _, err := os.Stat(views[0].file); err != nil {
		t.Fatalf("SetDatasets removed a file a query still reads: %v", err)
	}
	h.releaseViews(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dropped dir %s kept after its last query (stat err %v)", dir, err)
	}
}

func TestGetDocument(t *testing.T) {
	h := NewBinoHostServer()
	h.SetDocuments([]DocumentPayload{
		{File: "a.yaml", Position: 1, Kind: "DataSet", Name: "revenue", Raw: []byte(`{"kind":"DataSet"}`)},
		{File: "b.yaml", Position: 2, Kind: "Table", Name: "revenue", Raw: []byte(`{"kind":"Table"}`)},
	})

	tests := []struct {
		name      string
		req       *pluginv1.GetDocumentRequest
		wantFound bool
		wantKind  string
	}{
		{"exact kind and name match", &pluginv1.GetDocumentRequest{Kind: "Table", Name: "revenue"}, true, "Table"},
		{"empty kind is a wildcard, first match wins", &pluginv1.GetDocumentRequest{Name: "revenue"}, true, "DataSet"},
		{"name miss returns Found false, not an error", &pluginv1.GetDocumentRequest{Name: "nope"}, false, ""},
		{"kind mismatch returns Found false", &pluginv1.GetDocumentRequest{Kind: "Text", Name: "revenue"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := h.GetDocument(t.Context(), tt.req)
			if err != nil {
				t.Fatalf("GetDocument must never error on a miss: %v", err)
			}
			if resp.GetFound() != tt.wantFound {
				t.Fatalf("Found = %v, want %v", resp.GetFound(), tt.wantFound)
			}
			if !tt.wantFound {
				if resp.GetDocument() != nil {
					t.Fatalf("miss must carry no document, got %+v", resp.GetDocument())
				}
				return
			}
			if resp.GetDocument().GetKind() != tt.wantKind {
				t.Fatalf("kind = %q, want %q", resp.GetDocument().GetKind(), tt.wantKind)
			}
		})
	}

	t.Run("payload fields survive the conversion", func(t *testing.T) {
		resp, err := h.GetDocument(t.Context(), &pluginv1.GetDocumentRequest{Kind: "Table", Name: "revenue"})
		if err != nil {
			t.Fatalf("GetDocument: %v", err)
		}
		d := resp.GetDocument()
		if d.GetFile() != "b.yaml" || d.GetPosition() != 2 || d.GetName() != "revenue" || string(d.GetRaw()) != `{"kind":"Table"}` {
			t.Fatalf("document payload mangled: %+v", d)
		}
	})
}

func TestGetDatasetResult(t *testing.T) {
	h := NewBinoHostServer()
	h.SetDatasets([]DatasetPayload{
		{Name: "revenue", JSONRows: []byte(`[{"m":100}]`), Columns: []string{"m"}},
	})

	t.Run("hit returns the full payload", func(t *testing.T) {
		resp, err := h.GetDatasetResult(t.Context(), &pluginv1.GetDatasetResultRequest{Name: "revenue"})
		if err != nil {
			t.Fatalf("GetDatasetResult: %v", err)
		}
		if !resp.GetFound() {
			t.Fatal("expected Found true")
		}
		ds := resp.GetDataset()
		if ds.GetName() != "revenue" || string(ds.GetJsonRows()) != `[{"m":100}]` || !reflect.DeepEqual(ds.GetColumns(), []string{"m"}) {
			t.Fatalf("dataset payload mangled: %+v", ds)
		}
	})

	t.Run("miss returns Found false, not an error", func(t *testing.T) {
		resp, err := h.GetDatasetResult(t.Context(), &pluginv1.GetDatasetResultRequest{Name: "nope"})
		if err != nil {
			t.Fatalf("GetDatasetResult must never error on a miss: %v", err)
		}
		if resp.GetFound() || resp.GetDataset() != nil {
			t.Fatalf("expected an empty miss, got %+v", resp)
		}
	})
}

func TestListDocuments(t *testing.T) {
	h := NewBinoHostServer()
	h.SetDocuments([]DocumentPayload{
		{File: "a.yaml", Position: 1, Kind: "DataSet", Name: "revenue"},
		{File: "a.yaml", Position: 2, Kind: "DataSet", Name: "costs"},
		{File: "b.yaml", Position: 1, Kind: "Table", Name: "summary"},
	})

	tests := []struct {
		name       string
		kindFilter string
		wantNames  []string
	}{
		{"empty kind filter is a wildcard matching all kinds", "", []string{"revenue", "costs", "summary"}},
		{"kind filter narrows to matching documents", "DataSet", []string{"revenue", "costs"}},
		{"unmatched filter returns an empty list, not an error", "Text", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := h.ListDocuments(t.Context(), &pluginv1.ListDocumentsRequest{KindFilter: tt.kindFilter})
			if err != nil {
				t.Fatalf("ListDocuments: %v", err)
			}
			names := make([]string, 0, len(resp.GetDocuments()))
			for _, d := range resp.GetDocuments() {
				names = append(names, d.GetName())
			}
			if !reflect.DeepEqual(names, tt.wantNames) {
				t.Fatalf("documents = %v, want %v", names, tt.wantNames)
			}
		})
	}
}
