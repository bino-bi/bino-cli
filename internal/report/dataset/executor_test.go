package dataset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestExecute_CachesResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	cacheDir := filepath.Join(workdir, ".bncache", "datasets")

	// Create a simple dataset document with no dependencies
	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test-dataset
spec:
  query: SELECT 1 as value, 'hello' as message
`
	datasetFile := filepath.Join(workdir, "dataset.yaml")
	if err := os.WriteFile(datasetFile, []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	// First execution
	results, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "test-dataset" {
		t.Fatalf("unexpected result name: %s", results[0].Name)
	}
	if len(warnings) != 0 {
		t.Logf("warnings: %v", warnings)
	}

	// Verify cache file was created
	files, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 cache file, got %d", len(files))
	}

	// Parse the result data
	var rows []map[string]any
	if err := json.Unmarshal(results[0].Data, &rows); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["message"] != "hello" {
		t.Fatalf("unexpected message: %v", rows[0]["message"])
	}

	// Second execution should use cache
	results2, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if len(results2) != 1 {
		t.Fatalf("expected 1 cached result, got %d", len(results2))
	}
	if string(results2[0].Data) != string(results[0].Data) {
		t.Fatalf("cached result differs from original")
	}
}

func TestExecute_MissingDependencyWarning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	// Create a dataset with a missing dependency
	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test-dataset
spec:
  query: SELECT * FROM nonexistent_source
  dependencies:
    - nonexistent_source
`
	datasetFile := filepath.Join(workdir, "dataset.yaml")
	if err := os.WriteFile(datasetFile, []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	_, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Should have a warning about missing dependency
	foundMissingWarning := false
	for _, w := range warnings {
		if w.DataSet == "test-dataset" && contains(w.Message, "missing dependency") {
			foundMissingWarning = true
			break
		}
	}
	if !foundMissingWarning {
		t.Fatalf("expected missing dependency warning, got: %v", warnings)
	}
}

func TestComputeDigest(t *testing.T) {
	t.Parallel()

	data1 := []byte(`{"query": "SELECT 1"}`)
	data2 := []byte(`{"query": "SELECT 2"}`)

	digest1 := computeDigest(data1)
	digest2 := computeDigest(data2)

	if digest1 == digest2 {
		t.Fatal("different data should produce different digests")
	}
	if len(digest1) != 64 {
		t.Fatalf("expected 64-char hex digest, got %d chars", len(digest1))
	}

	// Same data should produce same digest
	digest1Again := computeDigest(data1)
	if digest1 != digest1Again {
		t.Fatal("same data should produce same digest")
	}
}

func TestExecute_InlineDataSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	// Create an inline datasource
	datasourceYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: inline_products
spec:
  type: inline
  content:
    - name: Coffee
      price: 3.50
    - name: Tea
      price: 2.50
    - name: Water
      price: 1.00
`
	datasourceFile := filepath.Join(workdir, "datasource.yaml")
	if err := os.WriteFile(datasourceFile, []byte(datasourceYAML), 0o644); err != nil {
		t.Fatalf("write datasource file: %v", err)
	}

	// Create a dataset that queries the inline datasource
	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: product_prices
spec:
  query: SELECT * FROM inline_products ORDER BY price
  dependencies:
    - inline_products
`
	datasetFile := filepath.Join(workdir, "dataset.yaml")
	if err := os.WriteFile(datasetFile, []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	results, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Should have no warnings about the datasource
	for _, w := range warnings {
		if contains(w.Message, "inline_products") {
			t.Fatalf("unexpected warning about inline datasource: %v", w)
		}
	}

	// Should have exactly one result
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "product_prices" {
		t.Fatalf("unexpected result name: %s", results[0].Name)
	}

	// Parse and verify the data
	var rows []map[string]any
	if err := json.Unmarshal(results[0].Data, &rows); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// Should be ordered by price (ascending)
	if rows[0]["name"] != "Water" {
		t.Errorf("expected first row to be Water, got %v", rows[0]["name"])
	}
	if rows[2]["name"] != "Coffee" {
		t.Errorf("expected last row to be Coffee, got %v", rows[2]["name"])
	}
}

func TestExecute_InlineDataSourceCacheInvalidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	// Create initial inline datasource
	datasourceYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: inline_values
spec:
  type: inline
  content:
    - value: 100
`
	datasourceFile := filepath.Join(workdir, "datasource.yaml")
	if err := os.WriteFile(datasourceFile, []byte(datasourceYAML), 0o644); err != nil {
		t.Fatalf("write datasource file: %v", err)
	}

	// Create a dataset that queries the inline datasource
	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sum_values
spec:
  query: SELECT SUM(value) as total FROM inline_values
  dependencies:
    - inline_values
`
	datasetFile := filepath.Join(workdir, "dataset.yaml")
	if err := os.WriteFile(datasetFile, []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	// First execution
	results1, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}

	var rows1 []map[string]any
	if err := json.Unmarshal(results1[0].Data, &rows1); err != nil {
		t.Fatalf("unmarshal first result: %v", err)
	}
	total1 := rows1[0]["total"]

	// Update the inline datasource with different values
	datasourceYAML2 := `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: inline_values
spec:
  type: inline
  content:
    - value: 200
    - value: 300
`
	if err := os.WriteFile(datasourceFile, []byte(datasourceYAML2), 0o644); err != nil {
		t.Fatalf("write updated datasource file: %v", err)
	}

	// Reload docs
	docs2, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("reload docs: %v", err)
	}

	// Second execution should get new values (cache should be invalidated)
	results2, _, err := Execute(ctx, workdir, docs2, nil)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}

	var rows2 []map[string]any
	if err := json.Unmarshal(results2[0].Data, &rows2); err != nil {
		t.Fatalf("unmarshal second result: %v", err)
	}
	total2 := rows2[0]["total"]

	// Totals should be different (100 vs 500)
	if total1 == total2 {
		t.Fatalf("cache was not invalidated: both totals are %v", total1)
	}

	// Verify the new total is 500 (200 + 300)
	// DuckDB returns int64, so we need to check the type
	switch v := total2.(type) {
	case float64:
		if v != 500 {
			t.Fatalf("expected total 500, got %v", v)
		}
	case int64:
		if v != 500 {
			t.Fatalf("expected total 500, got %v", v)
		}
	default:
		t.Fatalf("unexpected total type %T: %v", total2, total2)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
