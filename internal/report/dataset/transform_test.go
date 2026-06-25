package dataset

import (
	"reflect"
	"strings"
	"testing"
)

// norm collapses runs of whitespace to a single space and trims, so golden SQL
// can be written readably without matching the generator's exact spacing.
func norm(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

const baseQ = "SELECT region, product, sales, sku FROM raw"

func leaf(col, op string, val any) filterNode {
	return filterNode{Leaf: &filterCondition{Column: col, Op: op, Value: val}}
}

func TestBuildWrappedQuery_Success(t *testing.T) {
	tests := []struct {
		name string
		spec dataSetSpec
		want string
		args []any
	}{
		{
			name: "no-op identity",
			spec: dataSetSpec{},
			want: baseQ,
			args: nil,
		},
		{
			name: "filter equal",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "equal", "EMEA")}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" = ?)`,
			args: []any{"EMEA"},
		},
		{
			name: "filter notEqual",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "notEqual", "EMEA")}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" <> ?)`,
			args: []any{"EMEA"},
		},
		{
			name: "filter equal null -> IS NULL",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "equal", nil)}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" IS NULL)`,
			args: nil,
		},
		{
			name: "filter notEqual null -> IS NOT NULL",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "notEqual", nil)}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" IS NOT NULL)`,
			args: nil,
		},
		{
			name: "filter gt/gte/lt/lte",
			spec: dataSetSpec{Filter: &filterGroup{Op: "and", Conditions: []filterNode{
				leaf("a", "gt", 1.0), leaf("b", "gte", 2.0), leaf("c", "lt", 3.0), leaf("d", "lte", 4.0),
			}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE ` +
				`(_bn_base."a" > ? AND _bn_base."b" >= ? AND _bn_base."c" < ? AND _bn_base."d" <= ?)`,
			args: []any{1.0, 2.0, 3.0, 4.0},
		},
		{
			name: "filter in",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "in", []any{"EMEA", "APAC"})}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" IN (?, ?))`,
			args: []any{"EMEA", "APAC"},
		},
		{
			name: "filter notIn",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "notIn", []any{"X", "Y"})}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" NOT IN (?, ?))`,
			args: []any{"X", "Y"},
		},
		{
			name: "filter empty in -> FALSE",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "in", []any{})}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (FALSE)`,
			args: nil,
		},
		{
			name: "filter empty notIn -> TRUE",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("region", "notIn", []any{})}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (TRUE)`,
			args: nil,
		},
		{
			name: "filter regex",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("product", "regex", "^A")}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (regexp_matches(_bn_base."product", ?))`,
			args: []any{"^A"},
		},
		{
			name: "nested AND/OR parenthesization and arg order",
			spec: dataSetSpec{Filter: &filterGroup{Op: "and", Conditions: []filterNode{
				leaf("a", "equal", 1.0),
				{Group: &filterGroup{Op: "or", Conditions: []filterNode{
					leaf("b", "equal", 2.0),
					leaf("c", "equal", 3.0),
				}}},
			}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE ` +
				`(_bn_base."a" = ? AND (_bn_base."b" = ? OR _bn_base."c" = ?))`,
			args: []any{1.0, 2.0, 3.0},
		},
		{
			name: "groupBy sum + countDistinct",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns: []string{"region"},
				Aggregates: []aggregate{
					{Column: "sales", Fn: "sum", As: "total"},
					{Column: "sku", Fn: "countDistinct", As: "skus"},
				},
			}},
			want: `SELECT _bn_base."region", sum(_bn_base."sales") AS "total", count(DISTINCT _bn_base."sku") AS "skus" ` +
				`FROM (` + baseQ + `) AS _bn_base GROUP BY _bn_base."region"`,
			args: nil,
		},
		{
			name: "groupBy avg/min/max",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns: []string{"region"},
				Aggregates: []aggregate{
					{Column: "sales", Fn: "avg", As: "a"},
					{Column: "sales", Fn: "min", As: "mn"},
					{Column: "sales", Fn: "max", As: "mx"},
				},
			}},
			want: `SELECT _bn_base."region", avg(_bn_base."sales") AS "a", min(_bn_base."sales") AS "mn", max(_bn_base."sales") AS "mx" ` +
				`FROM (` + baseQ + `) AS _bn_base GROUP BY _bn_base."region"`,
			args: nil,
		},
		{
			name: "groupBy count(*)",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns:    []string{"region"},
				Aggregates: []aggregate{{Column: "*", Fn: "count", As: "n"}},
			}},
			want: `SELECT _bn_base."region", count(*) AS "n" FROM (` + baseQ + `) AS _bn_base GROUP BY _bn_base."region"`,
			args: nil,
		},
		{
			name: "groupBy count(col)",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns:    []string{"region"},
				Aggregates: []aggregate{{Column: "sku", Fn: "count", As: "n"}},
			}},
			want: `SELECT _bn_base."region", count(_bn_base."sku") AS "n" FROM (` + baseQ + `) AS _bn_base GROUP BY _bn_base."region"`,
			args: nil,
		},
		{
			name: "groupBy first with orderBy desc",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns:    []string{"region"},
				Aggregates: []aggregate{{Column: "sales", Fn: "first", As: "f", OrderBy: "date", OrderDesc: true}},
			}},
			want: `SELECT _bn_base."region", first(_bn_base."sales" ORDER BY _bn_base."date" DESC) AS "f" ` +
				`FROM (` + baseQ + `) AS _bn_base GROUP BY _bn_base."region"`,
			args: nil,
		},
		{
			name: "groupBy last without orderBy",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns:    []string{"region"},
				Aggregates: []aggregate{{Column: "sales", Fn: "last", As: "l"}},
			}},
			want: `SELECT _bn_base."region", last(_bn_base."sales") AS "l" FROM (` + baseQ + `) AS _bn_base GROUP BY _bn_base."region"`,
			args: nil,
		},
		{
			name: "index hash",
			spec: dataSetSpec{IndexColumns: []indexColumn{{Column: "rowGroupIndex", Fn: "hash", Of: "region"}}},
			want: `SELECT _bn_base.*, hash(_bn_base."region") AS "rowGroupIndex" FROM (` + baseQ + `) AS _bn_base`,
			args: nil,
		},
		{
			name: "index rowNumber",
			spec: dataSetSpec{IndexColumns: []indexColumn{{Column: "categoryIndex", Fn: "rowNumber", Over: "region"}}},
			want: `SELECT _bn_base.*, row_number() OVER (ORDER BY _bn_base."region") AS "categoryIndex" FROM (` + baseQ + `) AS _bn_base`,
			args: nil,
		},
		{
			name: "index rank",
			spec: dataSetSpec{IndexColumns: []indexColumn{{Column: "categoryIndex", Fn: "rank", Over: "region"}}},
			want: `SELECT _bn_base.*, rank() OVER (ORDER BY _bn_base."region") AS "categoryIndex" FROM (` + baseQ + `) AS _bn_base`,
			args: nil,
		},
		{
			name: "index denseRank with partition + desc",
			spec: dataSetSpec{IndexColumns: []indexColumn{
				{Column: "categoryIndex", Fn: "denseRank", Over: "region", OverDesc: true, Partition: []string{"product"}},
			}},
			want: `SELECT _bn_base.*, dense_rank() OVER (PARTITION BY _bn_base."product" ORDER BY _bn_base."region" DESC) AS "categoryIndex" ` +
				`FROM (` + baseQ + `) AS _bn_base`,
			args: nil,
		},
		{
			name: "index raw expr",
			spec: dataSetSpec{IndexColumns: []indexColumn{{Column: "subCategoryIndex", Expr: "row_number() OVER ()"}}},
			want: `SELECT _bn_base.*, row_number() OVER () AS "subCategoryIndex" FROM (` + baseQ + `) AS _bn_base`,
			args: nil,
		},
		{
			name: "full three-layer composition",
			spec: dataSetSpec{
				Filter: &filterGroup{Op: "and", Conditions: []filterNode{
					leaf("region", "in", []any{"EMEA", "APAC"}),
					leaf("sales", "gte", 1000.0),
				}},
				GroupBy: &groupBy{
					Columns: []string{"region"},
					Aggregates: []aggregate{
						{Column: "sales", Fn: "sum", As: "total"},
						{Column: "sku", Fn: "countDistinct", As: "skus"},
					},
				},
				IndexColumns: []indexColumn{
					{Column: "categoryIndex", Fn: "denseRank", Over: "region"},
					{Column: "rowGroupIndex", Fn: "hash", Of: "region"},
				},
			},
			want: `SELECT _bn_grouped.*, ` +
				`dense_rank() OVER (ORDER BY _bn_grouped."region") AS "categoryIndex", ` +
				`hash(_bn_grouped."region") AS "rowGroupIndex" ` +
				`FROM (` +
				`SELECT _bn_base."region", sum(_bn_base."sales") AS "total", count(DISTINCT _bn_base."sku") AS "skus" ` +
				`FROM (` +
				`SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base ` +
				`WHERE (_bn_base."region" IN (?, ?) AND _bn_base."sales" >= ?)` +
				`) AS _bn_base ` +
				`GROUP BY _bn_base."region"` +
				`) AS _bn_grouped`,
			args: []any{"EMEA", "APAC", 1000.0},
		},
		{
			name: "subset filter + index (no group)",
			spec: dataSetSpec{
				Filter:       &filterGroup{Conditions: []filterNode{leaf("region", "equal", "EMEA")}},
				IndexColumns: []indexColumn{{Column: "categoryIndex", Fn: "denseRank", Over: "region"}},
			},
			want: `SELECT _bn_base.*, dense_rank() OVER (ORDER BY _bn_base."region") AS "categoryIndex" ` +
				`FROM (` +
				`SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."region" = ?)` +
				`) AS _bn_base`,
			args: []any{"EMEA"},
		},
		{
			name: "identifier quoting of weird/reserved names",
			spec: dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf(`we"ird`, "equal", "x")}}},
			want: `SELECT _bn_base.* FROM (` + baseQ + `) AS _bn_base WHERE (_bn_base."we""ird" = ?)`,
			args: []any{"x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, args, _, err := buildWrappedQuery(tt.spec, baseQ, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if norm(got) != norm(tt.want) {
				t.Errorf("SQL mismatch\n got: %s\nwant: %s", norm(got), norm(tt.want))
			}
			if !reflect.DeepEqual(args, tt.args) {
				t.Errorf("args mismatch\n got: %#v\nwant: %#v", args, tt.args)
			}
		})
	}
}

func TestBuildWrappedQuery_NoOpIsByteIdentical(t *testing.T) {
	got, args, warns, err := buildWrappedQuery(dataSetSpec{}, baseQ, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != baseQ {
		t.Errorf("no-op query not byte-identical:\n got: %q\nwant: %q", got, baseQ)
	}
	if args != nil {
		t.Errorf("no-op args should be nil, got %#v", args)
	}
	if warns != nil {
		t.Errorf("no-op warnings should be nil, got %#v", warns)
	}
}

func TestBuildWrappedQuery_FirstWithoutOrderByWarns(t *testing.T) {
	spec := dataSetSpec{GroupBy: &groupBy{
		Columns:    []string{"region"},
		Aggregates: []aggregate{{Column: "sales", Fn: "first", As: "f"}},
	}}
	_, _, warns, err := buildWrappedQuery(spec, baseQ, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "nondeterministic") {
		t.Errorf("expected a nondeterminism warning, got %#v", warns)
	}
}

func TestBuildWrappedQuery_LenientIndexNameWarns(t *testing.T) {
	spec := dataSetSpec{IndexColumns: []indexColumn{{Column: "catagoryIndex", Fn: "hash", Of: "region"}}}
	_, _, warns, err := buildWrappedQuery(spec, baseQ, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "typo") {
		t.Errorf("expected a typo warning for catagoryIndex, got %#v", warns)
	}
}

func TestBuildWrappedQuery_Errors(t *testing.T) {
	tests := []struct {
		name    string
		spec    dataSetSpec
		isPRQL  bool
		wantSub string
	}{
		{
			name:    "PRQL with transform",
			spec:    dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("a", "equal", 1.0)}}},
			isPRQL:  true,
			wantSub: "not supported with prql",
		},
		{
			name:    "groupBy no aggregates",
			spec:    dataSetSpec{GroupBy: &groupBy{Columns: []string{"region"}}},
			wantSub: "requires at least one aggregate",
		},
		{
			name: "duplicate aggregate as",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns: []string{"region"},
				Aggregates: []aggregate{
					{Column: "sales", Fn: "sum", As: "total"},
					{Column: "sku", Fn: "sum", As: "total"},
				},
			}},
			wantSub: "collides",
		},
		{
			name: "aggregate as collides with group column",
			spec: dataSetSpec{GroupBy: &groupBy{
				Columns:    []string{"region"},
				Aggregates: []aggregate{{Column: "sales", Fn: "sum", As: "region"}},
			}},
			wantSub: "collides",
		},
		{
			name: "duplicate index column name",
			spec: dataSetSpec{IndexColumns: []indexColumn{
				{Column: "categoryIndex", Fn: "hash", Of: "a"},
				{Column: "categoryIndex", Fn: "hash", Of: "b"},
			}},
			wantSub: "duplicate indexColumn",
		},
		{
			name: "index column collides with grouped column",
			spec: dataSetSpec{
				GroupBy:      &groupBy{Columns: []string{"region"}, Aggregates: []aggregate{{Column: "sales", Fn: "sum", As: "total"}}},
				IndexColumns: []indexColumn{{Column: "total", Fn: "hash", Of: "region"}},
			},
			wantSub: "collides",
		},
		{
			name:    "hash without of",
			spec:    dataSetSpec{IndexColumns: []indexColumn{{Column: "rowGroupIndex", Fn: "hash"}}},
			wantSub: "requires 'of'",
		},
		{
			name:    "window without over",
			spec:    dataSetSpec{IndexColumns: []indexColumn{{Column: "categoryIndex", Fn: "denseRank"}}},
			wantSub: "requires 'over'",
		},
		{
			name:    "index with both fn and expr",
			spec:    dataSetSpec{IndexColumns: []indexColumn{{Column: "categoryIndex", Fn: "hash", Of: "a", Expr: "1"}}},
			wantSub: "exactly one of 'fn' or 'expr'",
		},
		{
			name:    "index with neither fn nor expr",
			spec:    dataSetSpec{IndexColumns: []indexColumn{{Column: "categoryIndex"}}},
			wantSub: "exactly one of 'fn' or 'expr'",
		},
		{
			name:    "regex with non-string",
			spec:    dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("a", "regex", 1.0)}}},
			wantSub: "requires a string value",
		},
		{
			name:    "in with scalar",
			spec:    dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("a", "in", "x")}}},
			wantSub: "requires an array value",
		},
		{
			name:    "in with null element",
			spec:    dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("a", "in", []any{"x", nil})}}},
			wantSub: "null list elements",
		},
		{
			name:    "equal with array value",
			spec:    dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("a", "equal", []any{"x"})}}},
			wantSub: "requires a scalar value",
		},
		{
			name:    "unknown filter op",
			spec:    dataSetSpec{Filter: &filterGroup{Conditions: []filterNode{leaf("a", "between", 1.0)}}},
			wantSub: "unknown filter op",
		},
		{
			name:    "unknown aggregate fn",
			spec:    dataSetSpec{GroupBy: &groupBy{Columns: []string{"region"}, Aggregates: []aggregate{{Column: "sales", Fn: "median", As: "m"}}}},
			wantSub: "unknown aggregate fn",
		},
		{
			name:    "unknown filter group op",
			spec:    dataSetSpec{Filter: &filterGroup{Op: "xor", Conditions: []filterNode{leaf("a", "equal", 1.0)}}},
			wantSub: "must be 'and' or 'or'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := buildWrappedQuery(tt.spec, baseQ, tt.isPRQL)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}
