package explorer

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
)

func setupTestSession(t *testing.T) *Session {
	t.Helper()
	ctx := context.Background()
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	raw := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSource",
		"metadata": {"name": "sales"},
		"spec": {
			"type": "inline",
			"content": [
				{"region": "North", "amount": 100},
				{"region": "South", "amount": 200},
				{"region": "East", "amount": 150}
			]
		}
	}`)
	docs := []config.Document{
		{Kind: "DataSource", Name: "sales", Raw: raw},
	}
	if err := sess.Refresh(ctx, docs); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return sess
}

func TestHandleMetadata(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__explorer/metadata", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp metadataResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(resp.Sources))
	}
	if resp.Sources[0].Name != "sales" {
		t.Errorf("expected source name 'sales', got %q", resp.Sources[0].Name)
	}
	if len(resp.Sources[0].Columns) < 1 {
		t.Error("expected at least 1 column")
	}
}

func TestHandleQuery(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	body := `{"sql": "SELECT * FROM sales ORDER BY region", "limit": 10, "offset": 0}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(resp.Columns))
	}
	if len(resp.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(resp.Rows))
	}
	if resp.DurationMs < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestHandleQueryPagination(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	// Get only first 2 rows
	body := `{"sql": "SELECT * FROM sales ORDER BY region", "limit": 2, "offset": 0}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Rows) != 2 {
		t.Errorf("expected 2 rows with limit, got %d", len(resp.Rows))
	}

	// Total should still be 3
	switch total := resp.TotalRows.(type) {
	case float64:
		if int(total) != 3 {
			t.Errorf("expected totalRows=3, got %v", total)
		}
	default:
		t.Errorf("expected numeric totalRows, got %T: %v", resp.TotalRows, resp.TotalRows)
	}
}

func TestHandleQueryRejectsWrites(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	tests := []struct {
		name string
		sql  string
	}{
		{"DROP", `DROP TABLE sales`},
		{"INSERT", `INSERT INTO sales VALUES ('West', 300)`},
		{"DELETE", `DELETE FROM sales WHERE region = 'North'`},
		{"CREATE", `CREATE TABLE hack (x INT)`},
		{"ALTER", `ALTER TABLE sales ADD COLUMN z INT`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"sql": "` + tt.sql + `"}`
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/query", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			var resp queryResponse
			json.NewDecoder(w.Body).Decode(&resp)

			if resp.Error == "" {
				t.Error("expected write operation to be rejected")
			}
		})
	}
}

func TestHandleSummarize(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	body := `{"name": "sales"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/summarize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Columns) == 0 {
		t.Error("expected columns from SUMMARIZE")
	}
	if len(resp.Rows) == 0 {
		t.Error("expected rows from SUMMARIZE")
	}
}

func TestHandleQueryEmptySQL(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	body := `{"sql": ""}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestIsWriteOperation(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM t", false},
		{"  select * from t", false},
		{"INSERT INTO t VALUES (1)", true},
		{"  insert into t values (1)", true},
		{"DROP TABLE t", true},
		{"ALTER TABLE t ADD COLUMN x INT", true},
		{"CREATE TABLE t (x INT)", true},
		{"DELETE FROM t", true},
		{"UPDATE t SET x = 1", true},
		{"TRUNCATE TABLE t", true},
		{"ATTACH 'foo' AS bar", true},
		{"DETACH bar", true},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", false},
		{"SUMMARIZE SELECT * FROM t", false},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			got := isWriteOperation(tt.sql)
			if got != tt.want {
				t.Errorf("isWriteOperation(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestHandleExport(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	body := `{"sql": "SELECT * FROM sales ORDER BY region"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/csv", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}

	records, err := csv.NewReader(w.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 4 { // header + 3 rows
		t.Fatalf("expected 4 CSV records, got %d: %v", len(records), records)
	}
	if records[0][0] != "amount" || records[0][1] != "region" {
		t.Errorf("header = %v, want [amount region]", records[0])
	}
	if records[1][0] != "150" || records[1][1] != "East" {
		t.Errorf("first data row = %v, want [150 East]", records[1])
	}
}

func TestHandleExportRejectsWrites(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	body := `{"sql": "DROP TABLE sales"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Error, "not allowed") {
		t.Errorf("error = %q, want write-rejection message", resp.Error)
	}
}

func TestHandleExportInvalidSQLReturnsJSONError(t *testing.T) {
	sess := setupTestSession(t)
	handler := Handler(sess)

	body := `{"sql": "SELECT * FROM does_not_exist"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON error (CSV stream must not start)", ct)
	}
	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected an error message for invalid SQL")
	}
}

// setupDerivedSession registers a CSV-free inline source and a dataset that
// derives pp2, the way the preview does on each refresh.
func setupDerivedSession(t *testing.T) *Session {
	t.Helper()
	ctx := context.Background()
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	source := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSource",
		"metadata": {"name": "sales"},
		"spec": {
			"type": "inline",
			"content": [
				{"category": "A", "date": "2019-03-31", "ac1": 30},
				{"category": "A", "date": "2020-03-31", "ac1": 130}
			]
		}
	}`)
	dataset := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSet",
		"metadata": {"name": "sales_pp"},
		"spec": {
			"query": "SELECT category, \"date\", \"ac1\"::DOUBLE AS ac1 FROM sales",
			"derive": {"pp2": {"from": "ac1", "shift": "1 year", "grain": "month"}}
		}
	}`)
	docs := []config.Document{
		{Kind: "DataSource", Name: "sales", Raw: source},
		{Kind: "DataSet", Name: "sales_pp", Raw: dataset},
	}
	if err := sess.Refresh(ctx, docs); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return sess
}

func TestHandleMetadataAndQuery_DerivedDataset(t *testing.T) {
	sess := setupDerivedSession(t)
	handler := Handler(sess)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__explorer/metadata", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var meta metadataResponse
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if len(meta.Datasets) != 1 || meta.Datasets[0].SQLError != "" {
		t.Fatalf("datasets = %+v", meta.Datasets)
	}
	sqlText := meta.Datasets[0].SQL
	if !strings.Contains(sqlText, "bino_shift('_bino_ds_sales_pp', 'ac1', '1 year', 'month')") {
		t.Errorf("published SQL = %q", sqlText)
	}

	// The browser posts the published SQL back; the view must already exist.
	body, _ := json.Marshal(map[string]any{"sql": sqlText, "limit": 10})
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__explorer/query", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("query error: %s", resp.Error)
	}
	if len(resp.Columns) != 4 || resp.Columns[3].Name != "pp2" {
		t.Errorf("columns = %v", resp.Columns)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows = %d", len(resp.Rows))
	}
}

// A PRQL dataset takes the same view path; bare PRQL cannot be wrapped in a
// subquery, so the explorer must run the published SQL against the (| |) view.
func TestHandleMetadataAndQuery_PrqlDerivedDataset(t *testing.T) {
	ctx := context.Background()
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { sess.Close() })

	source := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSource",
		"metadata": {"name": "sales"},
		"spec": {
			"type": "inline",
			"content": [
				{"category": "A", "date": "2019-03-31", "ac1": 30},
				{"category": "A", "date": "2020-03-31", "ac1": 130}
			]
		}
	}`)
	dataset := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSet",
		"metadata": {"name": "sales_prql"},
		"spec": {
			"prql": "from sales\nselect {category, date, ac1}",
			"derive": {"pp2": {"from": "ac1", "shift": "1 year", "grain": "month"}}
		}
	}`)
	docs := []config.Document{
		{Kind: "DataSource", Name: "sales", Raw: source},
		{Kind: "DataSet", Name: "sales_prql", Raw: dataset},
	}
	if err := sess.Refresh(ctx, docs); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	handler := Handler(sess)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/__explorer/metadata", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var meta metadataResponse
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if len(meta.Datasets) != 1 {
		t.Fatalf("datasets = %+v", meta.Datasets)
	}
	if e := meta.Datasets[0].SQLError; e != "" {
		if strings.Contains(strings.ToLower(e), "prql") {
			t.Skipf("prql extension unavailable: %s", e)
		}
		t.Fatalf("sql error: %s", e)
	}

	body, _ := json.Marshal(map[string]any{"sql": meta.Datasets[0].SQL, "limit": 10})
	req = httptest.NewRequestWithContext(ctx, http.MethodPost, "/__explorer/query", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp queryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if resp.Error != "" {
		if strings.Contains(strings.ToLower(resp.Error), "prql") {
			t.Skipf("prql extension unavailable: %s", resp.Error)
		}
		t.Fatalf("query error: %s", resp.Error)
	}
	if len(resp.Columns) != 4 || resp.Columns[3].Name != "pp2" {
		t.Errorf("columns = %v", resp.Columns)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows = %d", len(resp.Rows))
	}
}
