package spec

import (
	"reflect"
	"strings"
	"testing"
)

func TestFindNameUses(t *testing.T) {
	for _, tt := range []struct {
		text string
		prql bool
		want []int
	}{
		{"SELECT * FROM sales", false, []int{14}},
		{`FROM "sales" JOIN sales_2024`, false, []int{6}},
		{"FROM presales, t.sales, my-sales, @acme/sales", false, nil},
		{"WHERE x = 'sales' -- sales\nFROM sales", false, []int{32}},
		{"from sales # sales", true, []int{5}},
		{"from `sales`", true, []int{6}},
		// Column names and aliases are not table uses.
		{"SELECT region, SUM(amount) AS sales FROM orders", false, nil},
		{"SELECT sales FROM orders WHERE sales > 0", false, nil},
		{"SELECT sales AS x FROM sales", false, []int{23}},
		// Comma-separated tables of one FROM clause, with aliases.
		{"SELECT 1 FROM a x, sales s WHERE s.id = x.id", false, []int{19}},
		{"SELECT 1 FROM a, (SELECT 1 FROM b) t, sales", false, []int{38}},
		{"SELECT 1 FROM a WHERE x IN (1, sales)", false, nil},
		// A qualifier counts only when the query reads the table.
		{"SELECT sales.id FROM sales", false, []int{7, 21}},
		{"SELECT sales.id FROM orders sales", false, nil},
		{"FROM db.sales", false, nil},
		{"FROM sales.x", false, nil},
		// A CTE of that name shadows the table.
		{"WITH sales AS (SELECT 1) SELECT * FROM sales", false, nil},
		{"WITH a AS (SELECT 1), sales (x) AS (SELECT 1) SELECT * FROM sales", false, nil},
		{"/* FROM sales */ SELECT 1 FROM\n  sales", false, []int{33}},
		// PRQL: "..." is a string, side: and alias = are skipped.
		{`from orders | filter category == "sales"`, true, nil},
		{"from o = orders | join side:left s = sales (==id)", true, []int{37}},
		{"from sales | select {sales.id, sales_total}", true, []int{5, 21}},
		{"let sales = (from x)\nfrom sales", true, nil},
		{"from orders | derive {sales = amount * 2}", true, nil},
	} {
		if got := findNameUses(tt.text, "sales", tt.prql); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("findNameUses(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestQueryRefs_SharedAndUnreadableFiles(t *testing.T) {
	idx, err := BuildNameIndex(map[string]string{
		"/p/ds.yaml": "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: d1\nspec:\n  query:\n    $file: q.sql\n---\n" +
			"apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: d2\nspec:\n  query:\n    $file: ./q.sql\n---\n" +
			"apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: d3\nspec:\n  query:\n    $file: missing.sql\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"/p/q.sql": "SELECT * FROM sales"}
	refs, skipped, err := idx.QueryRefs("sales", func(p string) (string, bool) { c, ok := files[p]; return c, ok })
	if err != nil {
		t.Fatalf("QueryRefs: %v", err)
	}
	if len(refs) != 1 || refs[0].File != "/p/q.sql" {
		t.Errorf("refs = %+v, want one use in /p/q.sql", refs)
	}
	if len(skipped) != 1 || !strings.HasSuffix(skipped[0], "missing.sql") {
		t.Errorf("skipped = %v, want [/p/missing.sql]", skipped)
	}
}
