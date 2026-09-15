package dataset

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestReadTables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	writeTestFile(t, dir, "reads.sql", "SELECT * FROM file_src")

	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	query := func(q string) map[string]any { return map[string]any{"query": q} }

	tests := []struct {
		name   string
		spec   map[string]any
		want   []string
		wantOK bool
	}{
		{
			name:   "cte and quoted table",
			spec:   query(`WITH s AS (SELECT * FROM sales_csv) SELECT * FROM s JOIN "Regions" r USING (id)`),
			want:   []string{"Regions", "sales_csv"},
			wantOK: true,
		},
		{
			name:   "cte reading another cte",
			spec:   query(`WITH a AS (SELECT 1 FROM x), b AS (SELECT * FROM a JOIN y ON true) SELECT * FROM b`),
			want:   []string{"x", "y"},
			wantOK: true,
		},
		{
			name:   "comments, strings and subquery",
			spec:   query("-- FROM ghost\nSELECT 'FROM ghost2' AS sales_csv, (SELECT max(x) FROM sub_src) FROM a, b"),
			want:   []string{"a", "b", "sub_src"},
			wantOK: true,
		},
		{
			name:   "from-first syntax",
			spec:   query(`from sales_csv select region`),
			want:   []string{"sales_csv"},
			wantOK: true,
		},
		{
			name:   "table function reading a file",
			spec:   query(`SELECT * FROM read_csv('x.csv')`),
			wantOK: true,
		},
		{
			name:   "multiple statements",
			spec:   query(`SELECT 1; SELECT * FROM two`),
			want:   []string{"two"},
			wantOK: true,
		},
		{
			name: "pivot cannot be serialized",
			spec: query(`PIVOT sales_csv ON region USING sum(amount)`),
		},
		{
			name: "syntax error",
			spec: query(`SELEC broken`),
		},
		{
			name:   "file query",
			spec:   map[string]any{"query": map[string]any{"$file": "reads.sql"}},
			want:   []string{"file_src"},
			wantOK: true,
		},
		{
			name:   "inline reference",
			spec:   map[string]any{"query": "SELECT * FROM @inline(0)", "dependencies": []string{"_inline_datasource_abc"}},
			want:   []string{"_inline_datasource_abc"},
			wantOK: true,
		},
		{
			name:   "cte named like the table it reads",
			spec:   query(`WITH sales AS (SELECT * FROM main.sales) SELECT * FROM sales`),
			want:   []string{"sales"},
			wantOK: true,
		},
		{
			name:   "table in an attached database",
			spec:   query(`SELECT * FROM otherdb.main.sales`),
			wantOK: true,
		},
		{
			name:   "table in another schema",
			spec:   query(`SELECT * FROM other_schema.sales`),
			wantOK: true,
		},
		{
			name:   "fully qualified default catalog",
			spec:   query(`SELECT * FROM memory.main.sales`),
			want:   []string{"sales"},
			wantOK: true,
		},
		{
			name:   "cte name in another case",
			spec:   query(`WITH Base AS (SELECT * FROM x) SELECT * FROM base`),
			want:   []string{"x"},
			wantOK: true,
		},
		{
			name:   "shift macro",
			spec:   query(`SELECT * FROM bino_shift('actuals_csv', 'ac1', '1 year', 'month')`),
			want:   []string{"actuals_csv"},
			wantOK: true,
		},
		{
			name:   "query_table",
			spec:   query(`SELECT * FROM query_table('sales')`),
			want:   []string{"sales"},
			wantOK: true,
		},
		{
			name:   "same table in two spellings",
			spec:   query(`SELECT * FROM Sales JOIN SALES ON true`),
			want:   []string{"SALES"},
			wantOK: true,
		},
		{
			name: "source pass-through",
			spec: map[string]any{"source": "sales_csv"},
		},
		{
			name: "prql",
			spec: map[string]any{"prql": "from sales_csv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"spec": tt.spec})
			if err != nil {
				t.Fatal(err)
			}
			doc := config.Document{Kind: "DataSet", Name: "ds", File: filepath.Join(dir, "dataset.yaml"), Raw: raw}

			got, ok := ReadTables(ctx, conn, doc)
			if ok != tt.wantOK || !slices.Equal(got, tt.want) {
				t.Errorf("ReadTables() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
