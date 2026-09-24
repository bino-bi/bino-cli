package dataset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	reportspec "bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/runtimecfg"
	"bino.bi/bino/pkg/duckdb"
)

func TestExecute_CachesResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	cacheDir := filepath.Join(workdir, ".bino", "cache", "datasets")

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

	// The query itself fails (the dependency doesn't exist), so opt into
	// continue-on-error to reach the missing-dependency warning.
	_, warnings, err := Execute(ctx, workdir, docs, &ExecuteOptions{ContinueOnQueryError: true})
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

func TestExecute_UnknownSourceNoMissingDependencyWarning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test-dataset
spec:
  source: nonexistent_source
`)

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	_, warnings, err := Execute(ctx, workdir, docs, &ExecuteOptions{ContinueOnQueryError: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// The source is the query itself, so its failure already names the
	// unknown source; a missing dependency warning would only repeat it.
	foundQueryWarning := false
	for _, w := range warnings {
		if w.DataSet != "test-dataset" {
			continue
		}
		if contains(w.Message, "missing dependency") {
			t.Fatalf("unexpected missing dependency warning: %v", w)
		}
		if contains(w.Message, "nonexistent_source") {
			foundQueryWarning = true
		}
	}
	if !foundQueryWarning {
		t.Fatalf("expected query warning naming the source, got: %v", warnings)
	}
}

func TestExecute_QueryErrorFailsByDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: broken-dataset
spec:
  query: SELECT * FROM does_not_exist
`
	if err := os.WriteFile(filepath.Join(workdir, "dataset.yaml"), []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	_, _, err = Execute(ctx, workdir, docs, nil)
	if err == nil {
		t.Fatal("expected query error to fail execution, got nil")
	}
	if !contains(err.Error(), "broken-dataset") {
		t.Fatalf("error should name the failing dataset, got: %v", err)
	}
}

func TestExecute_ContinueOnQueryError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: broken-dataset
spec:
  query: SELECT * FROM does_not_exist
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: good-dataset
spec:
  query: SELECT 1 as value
`
	if err := os.WriteFile(filepath.Join(workdir, "dataset.yaml"), []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	results, warnings, err := Execute(ctx, workdir, docs, &ExecuteOptions{ContinueOnQueryError: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(results) != 1 || results[0].Name != "good-dataset" {
		t.Fatalf("expected only good-dataset result, got: %+v", results)
	}
	foundExecuteWarning := false
	for _, w := range warnings {
		if w.DataSet == "broken-dataset" && contains(w.Message, "execute:") {
			foundExecuteWarning = true
			break
		}
	}
	if !foundExecuteWarning {
		t.Fatalf("expected execute warning for broken-dataset, got: %v", warnings)
	}
}

func TestExecute_MaxQueryRowsExceeded(t *testing.T) {
	ctx := context.Background()
	workdir := t.TempDir()

	restore := runtimecfg.SetForTests(runtimecfg.Config{MaxQueryRows: 5})
	defer restore()

	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: too-many-rows
spec:
  query: SELECT * FROM range(10)
`
	if err := os.WriteFile(filepath.Join(workdir, "dataset.yaml"), []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	_, _, err = Execute(ctx, workdir, docs, nil)
	if err == nil {
		t.Fatal("expected row limit error, got nil")
	}
	if !contains(err.Error(), "BNR_MAX_QUERY_ROWS") {
		t.Fatalf("error should mention BNR_MAX_QUERY_ROWS, got: %v", err)
	}
}

func TestExecute_MaxQueryRowsWithinLimit(t *testing.T) {
	ctx := context.Background()
	workdir := t.TempDir()

	restore := runtimecfg.SetForTests(runtimecfg.Config{MaxQueryRows: 10})
	defer restore()

	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: within-limit
spec:
  query: SELECT * FROM range(10)
`
	if err := os.WriteFile(filepath.Join(workdir, "dataset.yaml"), []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	results, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestExecute_MaxQueryDurationExceeded(t *testing.T) {
	ctx := context.Background()
	workdir := t.TempDir()

	restore := runtimecfg.SetForTests(runtimecfg.Config{MaxQueryDuration: time.Millisecond})
	defer restore()

	// A cross join over range() is expensive enough to reliably exceed 1ms.
	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: slow-dataset
spec:
  query: SELECT count(*) AS n FROM range(100000000) a, range(100) b
`
	if err := os.WriteFile(filepath.Join(workdir, "dataset.yaml"), []byte(datasetYAML), 0o644); err != nil {
		t.Fatalf("write dataset file: %v", err)
	}

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	_, _, err = Execute(ctx, workdir, docs, nil)
	if err == nil {
		t.Fatal("expected duration limit error, got nil")
	}
	if !contains(err.Error(), "BNR_MAX_QUERY_DURATION_MS") {
		t.Fatalf("error should mention BNR_MAX_QUERY_DURATION_MS, got: %v", err)
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

func TestExecute_ExternalSQLFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	// Create a directory for SQL files
	queriesDir := filepath.Join(workdir, "queries")
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatalf("create queries dir: %v", err)
	}

	// Create an external SQL file
	sqlContent := `SELECT 42 as answer, 'external' as source`
	sqlFile := filepath.Join(queriesDir, "test.sql")
	if err := os.WriteFile(sqlFile, []byte(sqlContent), 0o644); err != nil {
		t.Fatalf("write SQL file: %v", err)
	}

	// Create a dataset that references the external SQL file
	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: external-sql-dataset
spec:
  query:
    $file: ./queries/test.sql
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
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "external-sql-dataset" {
		t.Fatalf("unexpected result name: %s", results[0].Name)
	}
	if len(warnings) != 0 {
		t.Logf("warnings: %v", warnings)
	}

	// Parse and verify the data
	var rows []map[string]any
	if err := json.Unmarshal(results[0].Data, &rows); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["source"] != "external" {
		t.Fatalf("unexpected source: %v", rows[0]["source"])
	}
}

func TestExecute_ExternalSQLFileCacheInvalidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()

	// Create a directory for SQL files
	queriesDir := filepath.Join(workdir, "queries")
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatalf("create queries dir: %v", err)
	}

	// Create an external SQL file
	sqlFile := filepath.Join(queriesDir, "values.sql")
	if err := os.WriteFile(sqlFile, []byte(`SELECT 100 as value`), 0o644); err != nil {
		t.Fatalf("write SQL file: %v", err)
	}

	// Create a dataset that references the external SQL file
	datasetYAML := `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: cached-sql-dataset
spec:
  query:
    $file: ./queries/values.sql
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

	// Update the external SQL file
	if err := os.WriteFile(sqlFile, []byte(`SELECT 500 as value`), 0o644); err != nil {
		t.Fatalf("write updated SQL file: %v", err)
	}

	// Reload docs (same YAML, but SQL file changed)
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

	// Values should be different (100 vs 500)
	val1 := rows1[0]["value"]
	val2 := rows2[0]["value"]

	if val1 == val2 {
		t.Fatalf("cache was not invalidated: both values are %v", val1)
	}

	// Verify the new value is 500
	switch v := val2.(type) {
	case float64:
		if v != 500 {
			t.Fatalf("expected value 500, got %v", v)
		}
	case int64:
		if v != 500 {
			t.Fatalf("expected value 500, got %v", v)
		}
	default:
		t.Fatalf("unexpected value type %T: %v", val2, val2)
	}
}

const salesDataSourceYAML = `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: ./sales.csv
`

func TestExecute_SourceCacheInvalidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dataset string
	}{
		{
			name: "named source",
			dataset: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  source: sales_csv
`,
		},
		{
			name: "named source also in dependencies",
			dataset: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  source: sales_csv
  dependencies:
    - sales_csv
`,
		},
		{
			name: "inline source",
			dataset: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  source:
    type: csv
    path: ./sales.csv
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workdir := t.TempDir()
			writeTestFile(t, workdir, "sales.csv", "region,amount\nnorth,1\n")
			writeTestFile(t, workdir, "datasource.yaml", salesDataSourceYAML)
			writeTestFile(t, workdir, "dataset.yaml", tt.dataset)

			if got := executeSalesAmount(t, workdir); got != 1 {
				t.Fatalf("first execute: amount = %v, want 1", got)
			}

			writeTestFile(t, workdir, "sales.csv", "region,amount\nnorth,999\n")

			if got := executeSalesAmount(t, workdir); got != 999 {
				t.Fatalf("second execute: amount = %v, want 999 (stale cache)", got)
			}
		})
	}
}

func TestComputeDigestWithDeps_SourceInDependenciesHashedOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "sales.csv", "region,amount\nnorth,1\n")
	writeTestFile(t, workdir, "datasource.yaml", salesDataSourceYAML)
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  source: sales_csv
  dependencies:
    - sales_csv
`)

	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	dataSourceIndex := make(map[string]config.Document)
	var dataSetDoc config.Document
	for _, doc := range docs {
		switch doc.Kind {
		case "DataSource":
			dataSourceIndex[doc.Name] = doc
		case "DataSet":
			dataSetDoc = doc
		}
	}

	spec, err := parseDataSetSpec(dataSetDoc.Raw)
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	withSource, _ := computeDigestWithDeps(dataSetDoc, spec, dataSourceIndex)
	spec.Source = ""
	depsOnly, _ := computeDigestWithDeps(dataSetDoc, spec, dataSourceIndex)

	if withSource != depsOnly {
		t.Fatalf("source listed in dependencies changed the digest: %s != %s", withSource, depsOnly)
	}
}

func TestExecute_EphemeralSourceSkipsCache(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	writeTestFile(t, workdir, "sales.csv", "region,amount\nnorth,1\n")
	writeTestFile(t, workdir, "datasource.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: ./sales.csv
  ephemeral: true
`)
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  source: sales_csv
`)

	if got := executeSalesAmount(t, workdir); got != 1 {
		t.Fatalf("first execute: amount = %v, want 1", got)
	}

	files, err := os.ReadDir(filepath.Join(workdir, ".bino", "cache", "datasets"))
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no cache file for an ephemeral source, got %d", len(files))
	}

	writeTestFile(t, workdir, "sales.csv", "region,amount\nnorth,999\n")

	if got := executeSalesAmount(t, workdir); got != 999 {
		t.Fatalf("second execute: amount = %v, want 999", got)
	}
}

// writeTestFile writes content to name inside dir.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// executeSalesAmount loads workdir, executes its datasets and returns the
// amount in the first row of the "sales" dataset.
func executeSalesAmount(t *testing.T, workdir string) float64 {
	t.Helper()

	ctx := context.Background()
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}

	results, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	for _, result := range results {
		if result.Name != "sales" {
			continue
		}
		var rows []map[string]any
		if err := json.Unmarshal(result.Data, &rows); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		amount, ok := rows[0]["amount"].(float64)
		if !ok {
			t.Fatalf("unexpected amount %T: %v", rows[0]["amount"], rows[0]["amount"])
		}
		return amount
	}
	t.Fatalf("no result for dataset sales")
	return 0
}

func TestQueryField_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantInline string
		wantFile   string
		wantErr    bool
	}{
		{
			name:       "inline string",
			input:      `"SELECT * FROM table"`,
			wantInline: "SELECT * FROM table",
			wantFile:   "",
		},
		{
			name:       "file reference",
			input:      `{"$file": "./queries/test.sql"}`,
			wantInline: "",
			wantFile:   "./queries/test.sql",
		},
		{
			name:       "empty string",
			input:      `""`,
			wantInline: "",
			wantFile:   "",
		},
		{
			name:    "invalid type",
			input:   `123`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var q reportspec.QueryField
			err := json.Unmarshal([]byte(tt.input), &q)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if q.Inline != tt.wantInline {
				t.Errorf("Inline = %q, want %q", q.Inline, tt.wantInline)
			}
			if q.File != tt.wantFile {
				t.Errorf("File = %q, want %q", q.File, tt.wantFile)
			}
		})
	}
}

func TestQueryField_ResolveQuery(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()

	// Create a test SQL file
	sqlContent := "SELECT * FROM test_table"
	sqlFile := filepath.Join(workdir, "test.sql")
	if err := os.WriteFile(sqlFile, []byte(sqlContent), 0o644); err != nil {
		t.Fatalf("write SQL file: %v", err)
	}

	tests := []struct {
		name    string
		field   reportspec.QueryField
		baseDir string
		want    string
		wantErr bool
	}{
		{
			name:    "inline query",
			field:   reportspec.QueryField{Inline: "SELECT 1"},
			baseDir: workdir,
			want:    "SELECT 1",
		},
		{
			name:    "file reference",
			field:   reportspec.QueryField{File: "test.sql"},
			baseDir: workdir,
			want:    sqlContent,
		},
		{
			name:    "relative path with ./",
			field:   reportspec.QueryField{File: "./test.sql"},
			baseDir: workdir,
			want:    sqlContent,
		},
		{
			name:    "empty field",
			field:   reportspec.QueryField{},
			baseDir: workdir,
			want:    "",
		},
		{
			name:    "missing file",
			field:   reportspec.QueryField{File: "nonexistent.sql"},
			baseDir: workdir,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.field.ResolveQuery(tt.baseDir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveQuery() = %q, want %q", got, tt.want)
			}
		})
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

func TestExecute_ConstantsStampRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: products
spec:
  type: inline
  content:
    - category: Coffee
      ac1: 3.5
    - category: Tea
      ac1: 2.5
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: by_query
spec:
  query: SELECT category, ac1 FROM products ORDER BY category
  dependencies: [products]
  constants:
    unit: kEUR
    factor: 1000
    spec:
      any:
        scenarios: [ac1, pl1]
      table:
        barColumns: ac1,pl1
        thereof:
          - rowGroup: Revenue
            category: Applications
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: by_source
spec:
  source: products
  constants:
    unit: kEUR
`)
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	results, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	byName := map[string][]map[string]any{}
	for _, r := range results {
		var rows []map[string]any
		if err := json.Unmarshal(r.Data, &rows); err != nil {
			t.Fatalf("unmarshal %s: %v", r.Name, err)
		}
		byName[r.Name] = rows
	}

	want := map[string]any{
		"_unit":                          "kEUR",
		"_factor":                        1000.0,
		"_spec_any_scenarios":            "ac1,pl1",
		"_spec_table_barColumns":         "ac1,pl1",
		"_spec_table_thereof_0_rowGroup": "Revenue",
		"_spec_table_thereof_0_category": "Applications",
	}
	rows := byName["by_query"]
	if len(rows) != 2 {
		t.Fatalf("by_query rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		for col, v := range want {
			if row[col] != v {
				t.Errorf("by_query %s = %v, want %v", col, row[col], v)
			}
		}
		if row["category"] == nil {
			t.Errorf("query column missing: %v", row)
		}
	}

	rows = byName["by_source"]
	if len(rows) != 2 {
		t.Fatalf("by_source rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row["_unit"] != "kEUR" {
			t.Errorf("by_source _unit = %v", row["_unit"])
		}
	}
}

func TestExecute_ConstantShadowsQueryColumn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: SELECT 1 AS ac1, 'EUR' AS _unit
  constants:
    unit: kEUR
`)
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	results, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if warnings[0].DataSet != "sales" || warnings[0].Message != "constants.unit shadows query column _unit" {
		t.Errorf("unexpected warning %+v", warnings[0])
	}
	var rows []map[string]any
	if err := json.Unmarshal(results[0].Data, &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rows) != 1 || rows[0]["_unit"] != "kEUR" {
		t.Errorf("rows = %v, want the constant to win", rows)
	}
}

func TestExecute_ConstantsCachedEqualsFresh(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: SELECT 1 AS ac1
  constants:
    unit: kEUR
`)
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	fresh, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("fresh execute: %v", err)
	}
	files, err := os.ReadDir(filepath.Join(workdir, ".bino", "cache", "datasets"))
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files = %d (err %v), want 1", len(files), err)
	}
	cached, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("cached execute: %v", err)
	}
	if string(cached[0].Data) != string(fresh[0].Data) {
		t.Fatalf("cached rows differ from fresh rows:\n%s\n%s", cached[0].Data, fresh[0].Data)
	}
	if !contains(string(cached[0].Data), `"_unit":"kEUR"`) {
		t.Fatalf("cached rows lack the constant: %s", cached[0].Data)
	}
}

func TestExecute_DefaultsCachedEqualsFresh(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: >-
    SELECT * FROM (VALUES ('A', 'ac1', 'kEUR'), ('B', 'pl1', NULL))
    AS t(category, _spec_table_barColumns, _spec_any_measureUnit)
  constants:
    spec:
      table:
        grouped: true
`)
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	fresh, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("fresh execute: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	want := Defaults{
		"table": {"barColumns": json.RawMessage(`["ac1","pl1"]`), "grouped": json.RawMessage(`true`)},
		"any":   {"measureUnit": json.RawMessage(`"kEUR"`)},
	}
	if !reflect.DeepEqual(fresh[0].Defaults, want) {
		t.Fatalf("fresh defaults = %v, want %v", fresh[0].Defaults, want)
	}
	cached, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("cached execute: %v", err)
	}
	if !reflect.DeepEqual(cached[0].Defaults, fresh[0].Defaults) {
		t.Fatalf("cached defaults = %v, fresh = %v", cached[0].Defaults, fresh[0].Defaults)
	}
}

func TestExecute_DefaultsClashWarns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: SELECT * FROM (VALUES ('A', 'kEUR'), ('B', 'EUR')) AS t(category, _spec_table_measureUnit)
`)
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	results, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(warnings) != 1 || warnings[0].Message != "measureUnit: 2 distinct values (kEUR, EUR) in dataset sales" {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(results[0].Defaults) != 0 {
		t.Fatalf("clashing field must be dropped: %v", results[0].Defaults)
	}
}

func TestExecute_ObjectConstantsRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: SELECT 'Revenue' AS rowGroup, 'Applications' AS category, 1 AS ac1
  constants:
    spec:
      table:
        thereof:
          - rowGroup: Revenue
            category: Applications
      chartstructure:
        stack: { by: scenarios }
`)
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	fresh, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("fresh execute: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	want := Defaults{
		"table":          {"thereof": json.RawMessage(`[{"category":"Applications","rowGroup":"Revenue"}]`)},
		"chartstructure": {"stack": json.RawMessage(`{"by":"scenarios"}`)},
	}
	if !reflect.DeepEqual(fresh[0].Defaults, want) {
		t.Fatalf("fresh defaults = %v, want %v", fresh[0].Defaults, want)
	}
	cached, _, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("cached execute: %v", err)
	}
	if !reflect.DeepEqual(cached[0].Defaults, want) {
		t.Fatalf("cached defaults = %v", cached[0].Defaults)
	}
}

// A read-only project, e.g. a read-only mount, still runs: the dataset cache
// is off and inline CSVs go to a temp dir that is removed afterwards.
func TestExecute_ReadOnlyProject(t *testing.T) {
	workdir, docs := writeInlineProject(t)
	makeReadOnly(t, workdir)
	requireCwdUntouched(t)
	tmp := isolateTempDir(t)

	ctx, logs := captureLogs()
	results, warnings, err := Execute(ctx, workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	requireProductPrices(t, results, warnings)

	if n := strings.Count(logs.String(), "cache off"); n != 1 {
		t.Errorf("cache off logged %d times, want 1:\n%s", n, logs)
	}
	requireNoBinoDir(t, workdir)
	if left := globIn(t, tmp, "bino-*"); len(left) != 0 {
		t.Errorf("temp entries left behind: %v", left)
	}
}

// A shared session on a read-only project keeps inline CSVs in its scratch
// dir, so the views work on every call until the session is closed.
func TestExecute_ReadOnlyProjectSharedSession(t *testing.T) {
	workdir, docs := writeInlineProject(t)
	makeReadOnly(t, workdir)
	requireCwdUntouched(t)
	extDir := t.TempDir()
	tmp := isolateTempDir(t)

	ctx := context.Background()
	s, err := duckdb.OpenSession(ctx, duckdb.Options{CacheDir: extDir})
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	t.Cleanup(func() { s.Close() }) // on early failure; Close is idempotent

	opts := &ExecuteOptions{Session: s}
	for i := 1; i <= 2; i++ {
		results, warnings, err := Execute(ctx, workdir, docs, opts)
		if err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
		requireProductPrices(t, results, warnings)
		if dirs := globIn(t, tmp, "bino-inline-*"); len(dirs) != 1 {
			t.Fatalf("after execute %d: inline dirs = %v, want one", i, dirs)
		}
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}
	requireNoBinoDir(t, workdir)
	if left := globIn(t, tmp, "bino-*"); len(left) != 0 {
		t.Errorf("temp entries left after close: %v", left)
	}
}

// A read-only checkout that already has the cache dirs must not use them:
// MkdirAll succeeds there, so only a real write shows they are read-only.
func TestExecute_ReadOnlyProjectWithCacheDirs(t *testing.T) {
	workdir, docs := writeInlineProject(t)
	requireCwdUntouched(t)
	tmp := isolateTempDir(t)

	// Fill the cache while writable, then make its entry stale: a read-only
	// run must query again instead of reading it.
	if _, _, err := Execute(context.Background(), workdir, docs, nil); err != nil {
		t.Fatalf("writable execute: %v", err)
	}
	binoDir := filepath.Join(workdir, ".bino")
	cacheDir := filepath.Join(binoDir, "cache", "datasets")
	inlineDir := filepath.Join(binoDir, "cache", "datasources")
	cached := globIn(t, cacheDir, "*")
	if len(cached) != 1 {
		t.Fatalf("cache entries = %v, want one", cached)
	}
	stale := []byte(`[{"name":"Stale","price":0}]`)
	if err := os.WriteFile(cached[0], stale, 0o600); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	if err := os.MkdirAll(inlineDir, 0o755); err != nil {
		t.Fatalf("create %s: %v", inlineDir, err)
	}
	makeReadOnly(t, workdir, binoDir, filepath.Join(binoDir, "cache"), cacheDir, inlineDir)

	results, warnings, err := Execute(context.Background(), workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	requireProductPrices(t, results, warnings)

	if got := globIn(t, cacheDir, "*"); len(got) != 1 || got[0] != cached[0] {
		t.Errorf("cache entries = %v, want only %s", got, cached[0])
	}
	if got, err := os.ReadFile(cached[0]); err != nil || !bytes.Equal(got, stale) {
		t.Errorf("cache entry changed: %q, err %v", got, err)
	}
	if got := globIn(t, inlineDir, "*"); len(got) != 0 {
		t.Errorf("%s: unexpected entries %v", inlineDir, got)
	}
	if left := globIn(t, tmp, "bino-*"); len(left) != 0 {
		t.Errorf("temp entries left behind: %v", left)
	}
}

// A writable project keeps inline CSVs in .bino/cache/datasources, also on a
// shared session.
func TestExecute_WritableProjectSharedSession(t *testing.T) {
	workdir, docs := writeInlineProject(t)
	extDir := t.TempDir()
	tmp := isolateTempDir(t)

	ctx := context.Background()
	s, err := duckdb.OpenSession(ctx, duckdb.Options{CacheDir: extDir})
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	results, warnings, err := Execute(ctx, workdir, docs, &ExecuteOptions{Session: s})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	requireProductPrices(t, results, warnings)

	if csvs := globIn(t, filepath.Join(workdir, ".bino", "cache", "datasources"), "*.csv"); len(csvs) != 1 {
		t.Errorf("inline CSVs in project = %v, want one", csvs)
	}
	if dirs := globIn(t, tmp, "bino-*"); len(dirs) != 0 {
		t.Errorf("temp entries for a writable project: %v", dirs)
	}
}

// Any probe error turns the cache off, not only a permission error: a
// read-only mount gives EROFS. A file named .bino gives ENOTDIR.
func TestExecute_UnusableCacheDir(t *testing.T) {
	workdir, docs := writeInlineProject(t)
	requireCwdUntouched(t)
	tmp := isolateTempDir(t)
	binoFile := filepath.Join(workdir, ".bino")
	if err := os.WriteFile(binoFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	results, warnings, err := Execute(context.Background(), workdir, docs, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	requireProductPrices(t, results, warnings)
	if info, err := os.Stat(binoFile); err != nil || info.IsDir() {
		t.Errorf(".bino changed: %v", err)
	}
	if left := globIn(t, tmp, "bino-*"); len(left) != 0 {
		t.Errorf("temp entries left behind: %v", left)
	}
}

// writeInlineProject writes the inline fixture of TestExecute_InlineDataSource
// into a new workdir and loads it.
func writeInlineProject(t *testing.T) (string, []config.Document) {
	t.Helper()
	workdir := t.TempDir()
	writeTestFile(t, workdir, "datasource.yaml", `
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
`)
	writeTestFile(t, workdir, "dataset.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: product_prices
spec:
  query: SELECT * FROM inline_products ORDER BY price
  dependencies:
    - inline_products
`)
	docs, err := config.LoadDir(context.Background(), workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	return workdir, docs
}

// requireProductPrices checks for the three inline rows ordered by price and
// no warnings.
func requireProductPrices(t *testing.T, results []Result, warnings []Warning) {
	t.Helper()
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(results) != 1 || results[0].Name != "product_prices" {
		t.Fatalf("results = %+v, want one product_prices result", results)
	}
	var rows []map[string]any
	if err := json.Unmarshal(results[0].Data, &rows); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(rows) != 3 || rows[0]["name"] != "Water" || rows[2]["name"] != "Coffee" {
		t.Fatalf("rows = %v, want Water, Tea, Coffee", rows)
	}
}

// makeReadOnly makes dirs read-only for the test, like a read-only mount.
func makeReadOnly(t *testing.T, dirs ...string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("read-only dirs need a non-root user on unix")
	}
	for _, dir := range dirs {
		// Registered after t.TempDir, so it runs before the removal.
		t.Cleanup(func() {
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Errorf("restore %s: %v", dir, err)
			}
		})
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatalf("chmod %s: %v", dir, err)
		}
	}
}

// requireCwdUntouched runs the test in a fresh cwd and fails if anything is
// written there, e.g. a cache file from a cwd-relative path.
func requireCwdUntouched(t *testing.T) {
	t.Helper()
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Cleanup(func() {
		if entries, err := os.ReadDir(cwd); err != nil || len(entries) != 0 {
			t.Errorf("cwd after test: entries %v, err %v", entries, err)
		}
	})
}

// isolateTempDir points os.TempDir at a fresh dir and returns it.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	return tmp
}

// globIn returns the entries in dir that match pattern.
func globIn(t *testing.T, dir, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	return matches
}

func requireNoBinoDir(t *testing.T, workdir string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(workdir, ".bino")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf(".bino in read-only workdir: %v", err)
	}
}

func captureLogs() (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	log := logx.NewTerminalWithColor(buf, buf, false, true)
	return logx.WithLogger(context.Background(), log), buf
}
