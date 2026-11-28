package dataquery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/report/config"
)

// writeProject writes a project with a monthly CSV that misses 2024-03, a
// DataSet with derive setup views and an inline DataSource, and returns its
// documents resolved for introspection.
func writeProject(t *testing.T) (string, []config.Document, config.Ambiguities) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"data/sales.csv":  "region,date,ac1,code\nNorth,2024-01-01,10.0,1\nSouth,2024-01-01,20.0,2\nNorth,2024-02-01,30.0,3\nNorth,2024-04-01,40.0,4\nSouth,2024-05-01,50.5,5\n",
		"secret.txt":      "do not read\n",
		"data/secret.csv": "token,v\nhunter2,1\n",
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
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: direct
spec:
  query: SELECT * FROM read_csv('ROOT/data/secret.csv')
`,
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		content = strings.ReplaceAll(content, "ROOT", filepath.ToSlash(root))
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
		{`WITH RECURSIVE "` + filepath.ToSlash(root) + `/data/secret.csv"(token, v) AS (SELECT * FROM "` + filepath.ToSlash(root) + `/data/secret.csv" UNION ALL SELECT token, v FROM "` + filepath.ToSlash(root) + `/data/secret.csv" WHERE false) SELECT * FROM "` + filepath.ToSlash(root) + `/data/secret.csv", sales_csv`, "CTE name"},
		{"WITH RECURSIVE duckdb_constraints(x) AS (SELECT 1 FROM duckdb_constraints UNION ALL SELECT 1 FROM duckdb_constraints WHERE false) SELECT * FROM duckdb_constraints", `unknown table "duckdb_constraints"`},
		{"WITH RECURSIVE t(x) AS (SELECT table_name FROM information_schema.tables UNION ALL SELECT x FROM t WHERE false) SELECT * FROM t", "qualified table name"},
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
	sibling := filepath.ToSlash(filepath.Join(root, "data", "secret.csv"))
	for _, q := range []string{
		"SELECT * FROM read_text('" + secret + "')",
		"SELECT * FROM read_csv('" + sibling + "')",
		"SELECT * FROM '" + sibling + "'",
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
	for setting, want := range map[string]string{"threads": "2", "max_temp_directory_size": "953.6 MiB", "enable_external_access": "false", "lock_configuration": "true"} {
		var got string
		if err := s.conn.QueryRowContext(ctx, "SELECT CAST(current_setting('"+setting+"') AS VARCHAR)").Scan(&got); err != nil || !strings.Contains(got, want) {
			t.Errorf("%s = %q (%v), want %q", setting, got, err, want)
		}
	}
	// The plan check on its own refuses a scan the targets do not make.
	err = checkPlan(ctx, s.conn, "SELECT * FROM duckdb_constraints", []*config.Document{target}, map[string]string{"sales_csv": "SELECT * FROM sales_csv"})
	if err == nil || !strings.Contains(err.Error(), "duckdb_constraints") {
		t.Errorf("checkPlan: %v", err)
	}
	if err := checkPlan(ctx, s.conn, "SELECT region FROM sales_csv", []*config.Document{target}, map[string]string{"sales_csv": "SELECT * FROM sales_csv"}); err != nil {
		t.Errorf("checkPlan of a view: %v", err)
	}
}

func TestQueryDataSetReadingAFileDirectly(t *testing.T) {
	_, docs, amb := writeProject(t)
	_, err := Query(context.Background(), docs, amb, "SELECT * FROM direct", 0)
	if err == nil || !strings.Contains(err.Error(), "Permission Error") || !strings.Contains(err.Error(), "only through DataSources") {
		t.Fatalf("err = %v", err)
	}
}

func TestAcquireRespectsContext(t *testing.T) {
	var releases []func()
	for range maxConcurrent {
		r, err := acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquire(ctx); err == nil {
		t.Fatal("acquire succeeded with every slot taken")
	}
	for _, r := range releases {
		r()
	}
	r, err := acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r()
}

func TestNormalizeTimes(t *testing.T) {
	inf := time.Date(5881580, time.July, 11, 0, 0, 0, 0, time.UTC)
	if _, err := json.Marshal([]any{normalize(inf), normalize([]any{inf})}); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	d := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	if got := normalizeTyped(d, "DATE"); got != "2024-03-01" {
		t.Errorf("DATE = %v", got)
	}
	if got := normalizeTyped(time.Date(1, 1, 1, 10, 11, 12, 500_000_000, time.UTC), "TIME"); got != "10:11:12.5" {
		t.Errorf("TIME = %v", got)
	}
	if got := normalizeTyped(d.Add(90*time.Minute), "TIMESTAMP"); got != "2024-03-01T01:30:00Z" {
		t.Errorf("TIMESTAMP = %v", got)
	}
}

func TestQueryCapsCellsAndResultSize(t *testing.T) {
	_, docs, amb := writeProject(t)
	ctx := context.Background()
	res, err := Query(ctx, docs, amb, "SELECT repeat('x', 10000) AS s, [repeat('y', 5000)] AS l", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, cell := range res.Rows[0] {
		str, _ := cell.(string)
		enc, _ := json.Marshal(str)
		if len(enc) > maxCellBytes+len(cutMark) || len(enc) < maxCellBytes-8 || !strings.HasSuffix(str, cutMark) {
			t.Errorf("cell %d: %d encoded bytes, want it cut to %d", i, len(enc), maxCellBytes)
		}
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "2 cells") {
		t.Errorf("notes = %v", res.Notes)
	}

	res, err = Query(ctx, docs, amb, "WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM c WHERE n < 1000) SELECT n, repeat('x', 4000) FROM c", MaxLimit)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Rows) >= 1000 || len(res.Rows) < 400 || len(res.Notes) == 0 {
		t.Fatalf("rows=%d truncated=%v notes=%v", len(res.Rows), res.Truncated, res.Notes)
	}
}

func TestQueryRefusesHugeGenerators(t *testing.T) {
	_, docs, amb := writeProject(t)
	for _, q := range []string{
		"SELECT count(*) FROM (SELECT unnest(generate_series(1, 3000000000)) x)",
		"SELECT len(range(0, 1e12))",
		"SELECT len(range(100000000))",
		"SELECT len(range(0, 50000000, 2))",
		"SELECT length(repeat('ab', 10000000))",
	} {
		if _, err := Query(context.Background(), docs, amb, q, 0); err == nil || !strings.Contains(err.Error(), "is refused") {
			t.Errorf("%s: err = %v", q, err)
		}
	}
	res, err := Query(context.Background(), docs, amb, "SELECT unnest(generate_series(1, 10)) AS x, repeat('ab', 3) AS r", 0)
	if err != nil || len(res.Rows) != 10 {
		t.Fatalf("small generator: rows=%d err=%v", len(res.Rows), err)
	}
}

func TestQueryInterruptedAtTimeout(t *testing.T) {
	_, docs, amb := writeProject(t)
	old := queryCallTimeout
	queryCallTimeout = time.Second
	t.Cleanup(func() { queryCallTimeout = old })
	from := "sales_csv a"
	for i := range 13 {
		from += fmt.Sprintf(", sales_csv t%d", i)
	}
	start := time.Now()
	_, err := Query(context.Background(), docs, amb, "SELECT count(*) FROM "+from, 0)
	if took := time.Since(start); err == nil || took > 4*time.Second {
		t.Fatalf("err=%v after %v", err, took)
	}
}

func TestQueryRefusesComputedGeneratorSizes(t *testing.T) {
	_, docs, amb := writeProject(t)
	for q, want := range map[string]string{
		"SELECT len(range(0, 10000000 * 100))":                  "is refused",
		"SELECT len(range(0, 10000000000::BIGINT))":             "is refused",
		"SELECT length(repeat(repeat('x', 1000), 200000))":      "is refused",
		"SELECT len(range(0, n)) FROM (SELECT 5 AS n)":          "must be a constant",
		"SELECT repeat(region, 2) FROM sales_csv":               "may only repeat a constant",
		"SELECT length(lpad('x', 2000000, ' '))":                "is refused",
		"SELECT len(list_resize([1], 20000000))":                "is refused",
		"SELECT len(array_resize([1], n)) FROM (SELECT 5 AS n)": "must be a constant",
		"SELECT len(repeat([1, 2], 6000000))":                   "is refused",
	} {
		if _, err := Query(context.Background(), docs, amb, q, 0); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", q, err, want)
		}
	}
	res, err := Query(context.Background(), docs, amb, "SELECT len(range(0, 10 * 10, 2)), lpad('x', 5, ' '), len(list_resize([1], 3)), repeat(repeat('ab', 2), 3), len(range(-5::INT, 5))", 0)
	if err != nil || len(res.Rows) != 1 || res.Rows[0][0] != int64(50) || res.Rows[0][3] != "abababababab" {
		t.Fatalf("small constants: rows=%v err=%v", res.Rows, err)
	}
}

func TestCutJSONMatchesEncoding(t *testing.T) {
	inputs := []string{
		strings.Repeat("<", 5000),
		strings.Repeat("ä€ \"\\\n\x01", 900),
		strings.Repeat("ok\xff\xfe", 3000),
		"short <&>",
	}
	for _, in := range inputs {
		head, size, cut := cutJSON(in, maxCellBytes)
		enc, _ := json.Marshal(head)
		if len(enc) != size || size > maxCellBytes || (cut && size < maxCellBytes-6) {
			t.Errorf("cutJSON: encoded %d, reported %d, cut %v", len(enc), size, cut)
		}
		out, n, _ := capCell(in)
		enc, _ = json.Marshal(out)
		if len(enc) != n || n > maxCellBytes+len(cutMark) {
			t.Errorf("capCell: encoded %d, reported %d", len(enc), n)
		}
	}
}

func TestQueryBudgetInsideAWideRow(t *testing.T) {
	_, docs, amb := writeProject(t)
	old := maxResultBytes
	maxResultBytes = 10 << 10
	t.Cleanup(func() { maxResultBytes = old })
	cols := make([]string, 10)
	for i := range cols {
		cols[i] = fmt.Sprintf("repeat('<', 4000) AS c%d", i)
	}
	res, err := Query(context.Background(), docs, amb, "SELECT "+strings.Join(cols, ", ")+" FROM sales_csv", 0)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := json.Marshal(res.Rows)
	if !res.Truncated || len(res.Rows) != 1 || len(enc) > maxResultBytes+maxCellBytes+len(cutMark)*10 || res.Rows[0][9] != cutMark {
		t.Fatalf("rows=%d truncated=%v bytes=%d notes=%v", len(res.Rows), res.Truncated, len(enc), res.Notes)
	}
}

func TestQueryCapsColumns(t *testing.T) {
	_, docs, amb := writeProject(t)
	cols := make([]string, maxColumns+50)
	for i := range cols {
		cols[i] = fmt.Sprintf("%d AS c%d", i, i)
	}
	res, err := Query(context.Background(), docs, amb, "SELECT "+strings.Join(cols, ", "), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Columns) != maxColumns || len(res.Rows[0]) != maxColumns || len(res.Notes) == 0 {
		t.Fatalf("columns=%d cells=%d notes=%v", len(res.Columns), len(res.Rows[0]), res.Notes)
	}
}
