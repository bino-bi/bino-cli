package explorer

import (
	"context"
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
