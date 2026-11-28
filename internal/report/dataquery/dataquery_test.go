package dataquery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// writeProject writes a project with a monthly CSV that misses 2024-03, a
// DataSet with derive setup views and an inline DataSource, and returns its
// documents resolved for introspection.
func writeProject(t *testing.T) (string, []config.Document, config.Ambiguities) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"data/sales.csv": "region,date,ac1,code\nNorth,2024-01-01,10.0,1\nSouth,2024-01-01,20.0,2\nNorth,2024-02-01,30.0,3\nNorth,2024-04-01,40.0,4\nSouth,2024-05-01,50.5,5\n",
		"secret.txt":     "do not read\n",
		"sources.yaml": `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: ./data/sales.csv
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: regions
spec:
  type: inline
  inline:
    content:
      - { region: North, manager: Ann }
      - { region: South, manager: Bob }
`,
		"datasets.yaml": `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: monthly
spec:
  query: SELECT region AS category, "date", ac1, CAST(code AS VARCHAR) AS code FROM sales_csv
  derive:
    pp1: { from: ac1, shift: 1 month, grain: month }
`,
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := config.LoadDirWithOptions(context.Background(), root, config.LoadOptions{Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	docs, amb := config.ResolveForIntrospection(docs)
	return root, docs, amb
}

func TestQueryGroupByReturnsTypes(t *testing.T) {
	_, docs, amb := writeProject(t)
	res, err := Query(context.Background(), docs, amb, "SELECT region, sum(ac1) AS total, count(*) AS n FROM sales_csv GROUP BY region ORDER BY region", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Limit != DefaultLimit || res.Truncated {
		t.Fatalf("limit=%d truncated=%v", res.Limit, res.Truncated)
	}
	want := []string{"region VARCHAR", "total DOUBLE", "n BIGINT"}
	for i, c := range res.Columns {
		if got := c.Name + " " + c.Type; got != want[i] {
			t.Errorf("column %d = %q, want %q", i, got, want[i])
		}
	}
	if len(res.Rows) != 2 || res.Rows[0][0] != "North" || res.Rows[0][1] != 80.0 {
		t.Fatalf("rows = %v", res.Rows)
	}
}

func TestQueryDataSetAndInlineByName(t *testing.T) {
	_, docs, amb := writeProject(t)
	res, err := Query(context.Background(), docs, amb, `SELECT m.category, r.manager, sum(m.ac1), sum(m.pp1) FROM monthly m JOIN regions r ON r.region = m.category WHERE m."date" = DATE '2024-02-01' GROUP BY ALL ORDER BY 1`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || res.Rows[0][1] != "Ann" || res.Rows[0][2] != 30.0 || res.Rows[0][3] != 10.0 {
		t.Fatalf("rows = %v", res.Rows)
	}
}

func TestQueryTruncates(t *testing.T) {
	_, docs, amb := writeProject(t)
	res, err := Query(context.Background(), docs, amb, "SELECT * FROM sales_csv ORDER BY ac1;", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || !res.Truncated {
		t.Fatalf("rows=%d truncated=%v", len(res.Rows), res.Truncated)
	}
}

func TestQueryLimitAboveMaximum(t *testing.T) {
	_, docs, amb := writeProject(t)
	_, err := Query(context.Background(), docs, amb, "SELECT 1", MaxLimit+1)
	if err == nil || !strings.Contains(err.Error(), "above the maximum of 1000") {
		t.Fatalf("err = %v", err)
	}
}

func TestQueryBinderErrorVerbatim(t *testing.T) {
	_, docs, amb := writeProject(t)
	_, err := Query(context.Background(), docs, amb, "SELECT no_such_column FROM sales_csv", 0)
	if err == nil || !strings.HasPrefix(err.Error(), "Binder Error: Referenced column \"no_such_column\" not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestQueryRefusals(t *testing.T) {
	root, docs, amb := writeProject(t)
	secret := filepath.ToSlash(filepath.Join(root, "secret.txt"))
	out := filepath.ToSlash(filepath.Join(root, "data", "out.csv"))
	cases := []struct {
		sql, want string
	}{
		{"INSERT INTO sales_csv VALUES ('x', DATE '2024-01-01', 1, '1')", "only one SELECT statement"},
		{"COPY (SELECT * FROM sales_csv) TO '" + out + "'", "only one SELECT statement"},
		{"ATTACH '" + filepath.ToSlash(filepath.Join(root, "data", "x.db")) + "' AS x", "only one SELECT statement"},
		{"CREATE TABLE t AS SELECT 1", "only one SELECT statement"},
		{"SET enable_external_access = true", "only one SELECT statement"},
		{"SELECT 1; SELECT 2", "2 statements found"},
		{"SELECT 1; COPY (SELECT 1) TO '" + out + "'", "only one SELECT statement"},
		{"SELECT * FROM read_csv('/etc/passwd')", "table function read_csv() is not allowed"},
		{"SELECT * FROM read_text('" + secret + "')", "table function read_text() is not allowed"},
		{"SELECT count(*) FROM glob('" + filepath.ToSlash(root) + "/*')", "table function glob() is not allowed"},
		{"SELECT * FROM duckdb_secrets()", "table function duckdb_secrets() is not allowed"},
		{"SELECT region FROM sales_csv WHERE region IN (SELECT content FROM read_text('" + secret + "'))", "table function read_text() is not allowed"},
		{"SELECT * FROM '" + secret + "'", "unknown table"},
		{"SELECT * FROM memory.main.sales_csv", "qualified table name"},
		{"SELECT * FROM duckdb_settings", "unknown table"},
		{"WITH duckdb_databases AS (SELECT * FROM duckdb_databases) SELECT * FROM duckdb_databases", "unknown table"},
		{"DESCRIBE sales_csv", "DESCRIBE, SUMMARIZE and SHOW are not allowed"},
		{"SELEC 1", "Parser Error: syntax error"},
	}
	for _, c := range cases {
		_, err := Query(context.Background(), docs, amb, c.sql, 0)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.sql, err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "data", "out.csv")); !os.IsNotExist(err) {
		t.Errorf("a refused COPY wrote a file: %v", err)
	}
}

func TestQueryCTEAndRecursiveCTE(t *testing.T) {
	_, docs, amb := writeProject(t)
	res, err := Query(context.Background(), docs, amb, "WITH t AS (SELECT region FROM sales_csv), r(n) AS (SELECT 1) SELECT count(*) FROM t, r", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows[0][0] != int64(5) {
		t.Fatalf("rows = %v", res.Rows)
	}
	if _, err := Query(context.Background(), docs, amb, "WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM c WHERE n < 3) SELECT * FROM c", 0); err != nil {
		t.Fatal(err)
	}
}

// TestLockedSessionBlocksFiles checks the session gate on its own, without
// the SQL check in front of it: outside the data directory nothing reads,
// and the configuration cannot be changed back.
func TestLockedSessionBlocksFiles(t *testing.T) {
	root, docs, amb := writeProject(t)
	ctx := context.Background()
	s, err := openSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	target := newCatalog(docs).entries["sales_csv"].doc
	if _, err := s.prepare(ctx, docs, amb, []*config.Document{target}); err != nil {
		t.Fatal(err)
	}
	if err := s.lock(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM sales_csv").Scan(&n); err != nil || n != 5 {
		t.Fatalf("view: n=%d err=%v", n, err)
	}
	secret := filepath.ToSlash(filepath.Join(root, "secret.txt"))
	for _, q := range []string{
		"SELECT * FROM read_text('" + secret + "')",
		"SELECT * FROM read_csv('/etc/passwd')",
		"SELECT * FROM glob('/etc/*')",
		"SET GLOBAL enable_external_access = true",
		"SET GLOBAL allowed_directories = ['/']",
		"LOAD spatial",
	} {
		if _, err := s.conn.ExecContext(ctx, q); err == nil {
			t.Errorf("%s: no error on the locked session", q)
		}
	}
}
