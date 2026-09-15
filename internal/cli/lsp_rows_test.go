package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

const rowsPreviewCSV = `category,date,ac1
A,2019-01-31,10
A,2019-02-28,20
A,2020-01-31,110
A,2020-02-29,120
`

func writeRowsPreviewProject(t *testing.T, datasetYAML string, extra map[string]string) []config.Document {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"sales.csv": rowsPreviewCSV,
		"source.yaml": `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: ./sales.csv
`,
		"dataset.yaml": datasetYAML,
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := config.LoadDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	return docs
}

func findDoc(t *testing.T, docs []config.Document, name string) *config.Document {
	t.Helper()
	for i := range docs {
		if docs[i].Kind == "DataSet" && docs[i].Name == name {
			return &docs[i]
		}
	}
	t.Fatalf("dataset %s not found", name)
	return nil
}

func TestExecuteRowsPreview_DerivedSlot(t *testing.T) {
	docs := writeRowsPreviewProject(t, `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`, nil)
	cols, rows, truncated, err := executeRowsPreview(context.Background(), findDoc(t, docs, "sales"), docs, 10)
	if err != nil {
		t.Fatalf("executeRowsPreview: %v", err)
	}
	if truncated || len(rows) != 4 {
		t.Fatalf("rows = %d, truncated = %v", len(rows), truncated)
	}
	if len(cols) != 4 || cols[3] != "pp2" {
		t.Errorf("columns = %v, want [... pp2]", cols)
	}
	seen := false
	for _, r := range rows {
		if r["ac1"] == 120.0 {
			seen = true
			if r["pp2"] != 20.0 {
				t.Errorf("pp2 for 2020-02 = %v, want 20", r["pp2"])
			}
		}
	}
	if !seen {
		t.Error("row for 2020-02 missing")
	}
}

func TestExecuteRowsPreview_FileQueryAndSource(t *testing.T) {
	docs := writeRowsPreviewProject(t, `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: { $file: ./q.sql }
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: passthrough
spec:
  source: sales_csv
`, map[string]string{"q.sql": "SELECT category FROM sales_csv;"})
	cols, rows, _, err := executeRowsPreview(context.Background(), findDoc(t, docs, "sales"), docs, 2)
	if err != nil {
		t.Fatalf("$file dataset: %v", err)
	}
	if len(cols) != 1 || len(rows) != 2 {
		t.Errorf("$file dataset: cols %v rows %d", cols, len(rows))
	}
	cols, _, truncated, err := executeRowsPreview(context.Background(), findDoc(t, docs, "passthrough"), docs, 3)
	if err != nil {
		t.Fatalf("source dataset: %v", err)
	}
	if len(cols) != 3 || !truncated {
		t.Errorf("source dataset: cols %v truncated %v", cols, truncated)
	}
}

func TestExecuteRowsPreview_PrqlDerivedSlot(t *testing.T) {
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		t.Skip(err)
	}
	s, err := duckdb.OpenSession(context.Background(), opts)
	if err != nil {
		t.Skip(err)
	}
	if err := s.InstallAndLoadCommunityExtensions(context.Background(), []string{"prql"}); err != nil {
		_ = s.Close()
		t.Skip("prql extension unavailable:", err)
	}
	_ = s.Close()

	docs := writeRowsPreviewProject(t, `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  prql: |
    from sales_csv
    select {category, date, ac1}
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`, nil)
	cols, rows, _, err := executeRowsPreview(context.Background(), findDoc(t, docs, "sales"), docs, 10)
	if err != nil {
		t.Fatalf("executeRowsPreview: %v", err)
	}
	if len(cols) != 4 || cols[3] != "pp2" || len(rows) != 4 {
		t.Errorf("columns = %v, rows = %d", cols, len(rows))
	}
}
