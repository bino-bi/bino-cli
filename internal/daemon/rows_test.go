package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeDerivedProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"sales.csv": "category,date,ac1\nA,2019-01-31,10\nA,2019-02-28,20\nA,2020-01-31,110\nA,2020-02-29,120\n",
		"source.yaml": `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: ./sales.csv
`,
		"dataset.yaml": `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales_prql
spec:
  prql: |
    from sales_csv
    select {category, date, ac1}
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func getJSON(t *testing.T, handler http.HandlerFunc, target string) map[string]any {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d: %s", target, w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: decode: %v", target, err)
	}
	return body
}

func columnNames(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, _ := body["columns"].([]any)
	names := make([]string, 0, len(raw))
	for _, c := range raw {
		names = append(names, c.(string))
	}
	return names
}

func TestRowsAndColumns_DerivedDataset(t *testing.T) {
	root := writeDerivedProject(t)
	srv := newWizardTestServer(t, root)
	if err := srv.state.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	cols := getJSON(t, srv.handleColumns, "/columns?name=sales")
	if cols["error"] != nil && cols["error"] != "" {
		t.Fatalf("columns error: %v", cols["error"])
	}
	if names := columnNames(t, cols); len(names) != 4 || names[3] != "pp2" {
		t.Errorf("columns = %v, want [category date ac1 pp2]", names)
	}

	rows := getJSON(t, srv.handleRows, "/rows?name=sales&limit=10")
	if rows["error"] != nil && rows["error"] != "" {
		t.Fatalf("rows error: %v", rows["error"])
	}
	if names := columnNames(t, rows); len(names) != 4 || names[3] != "pp2" {
		t.Errorf("row columns = %v", names)
	}
	data, _ := rows["rows"].([]any)
	if len(data) != 4 {
		t.Fatalf("rows = %d, want 4", len(data))
	}
	seen := false
	for _, r := range data {
		row := r.(map[string]any)
		if row["ac1"] == 120.0 {
			seen = true
			if row["pp2"] != 20.0 {
				t.Errorf("pp2 for 2020-02 = %v, want 20", row["pp2"])
			}
		}
	}
	if !seen {
		t.Error("row for 2020-02 missing")
	}

	// The same for a PRQL dataset when the extension is available.
	prql := getJSON(t, srv.handleColumns, "/columns?name=sales_prql")
	if e, _ := prql["error"].(string); e != "" {
		t.Skipf("prql dataset unavailable: %s", e)
	}
	if names := columnNames(t, prql); len(names) != 4 || names[3] != "pp2" {
		t.Errorf("prql columns = %v", names)
	}
	prqlRows := getJSON(t, srv.handleRows, "/rows?name=sales_prql&limit=10")
	if e, _ := prqlRows["error"].(string); e != "" {
		t.Fatalf("prql rows error: %s", e)
	}
	if data, _ := prqlRows["rows"].([]any); len(data) != 4 {
		t.Errorf("prql rows = %d", len(data))
	}
}
