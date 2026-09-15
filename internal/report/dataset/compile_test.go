package dataset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func compileDoc(t *testing.T, name string, spec map[string]any, file string) Compiled {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"spec": spec})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(config.Document{Kind: "DataSet", Name: name, File: file, Raw: raw})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return c
}

func compileErr(t *testing.T, spec map[string]any) error {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"spec": spec})
	_, err := Compile(config.Document{Kind: "DataSet", Name: "ds", Raw: raw})
	if err == nil {
		t.Fatalf("Compile: expected error for %v", spec)
	}
	return err
}

func TestCompile_UndeclaredIsByteIdentical(t *testing.T) {
	t.Parallel()

	t.Run("inline query", func(t *testing.T) {
		q := "SELECT 1 AS ac1, '2020-01-31' AS date;\n"
		c := compileDoc(t, "ds", map[string]any{"query": q}, "")
		if c.Query != q || c.Setup != nil || c.Prql || c.View != "" {
			t.Errorf("unexpected %+v", c)
		}
	})

	t.Run("file query", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "q.sql"), []byte("SELECT 1"), 0o644); err != nil {
			t.Fatal(err)
		}
		c := compileDoc(t, "ds", map[string]any{"query": map[string]any{"$file": "q.sql"}}, filepath.Join(dir, "manifest.yaml"))
		if c.Query != "SELECT 1" || c.Setup != nil {
			t.Errorf("unexpected %+v", c)
		}
	})

	t.Run("source pass-through", func(t *testing.T) {
		c := compileDoc(t, "ds", map[string]any{"source": "sales"}, "")
		if c.Query != `SELECT * FROM "sales"` || c.Setup != nil {
			t.Errorf("unexpected %+v", c)
		}
	})

	t.Run("inline refs rewritten", func(t *testing.T) {
		c := compileDoc(t, "ds", map[string]any{
			"query":        "SELECT * FROM @inline(0)",
			"dependencies": []string{"_inline_ds_abc123"},
		}, "")
		if c.Query != `SELECT * FROM "_inline_ds_abc123"` {
			t.Errorf("unexpected query %q", c.Query)
		}
	})

	t.Run("missing query is an error", func(t *testing.T) {
		err := compileErr(t, map[string]any{})
		if !strings.Contains(err.Error(), "no query, prql, or source") {
			t.Errorf("unexpected error %v", err)
		}
	})

	t.Run("inline ref without dependencies is an error", func(t *testing.T) {
		compileErr(t, map[string]any{"query": "SELECT * FROM @inline(0)"})
	})
}

func TestCompile_PrqlBecomesDelimitedView(t *testing.T) {
	t.Parallel()
	c := compileDoc(t, "sales", map[string]any{"prql": "from sales_csv\nselect {ac1, date};\n"}, "")
	if !c.Prql {
		t.Error("Prql = false")
	}
	wantSetup := []string{`CREATE OR REPLACE VIEW "_bino_ds_sales" AS (| from sales_csv
select {ac1, date} |)`}
	if len(c.Setup) != 1 || c.Setup[0] != wantSetup[0] {
		t.Errorf("Setup = %q, want %q", c.Setup, wantSetup)
	}
	if c.Query != `SELECT * FROM "_bino_ds_sales"` {
		t.Errorf("Query = %q", c.Query)
	}
}

func TestCompile_DeriveWrapsView(t *testing.T) {
	t.Parallel()
	c := compileDoc(t, "sales", map[string]any{
		"query": "SELECT * FROM sales_csv",
		"derive": map[string]any{
			"pp2": map[string]any{"from": "ac1", "shift": "1 year", "grain": "month"},
		},
	}, "")
	if c.View != "_bino_ds_sales" {
		t.Errorf("View = %q", c.View)
	}
	if len(c.Setup) != 1 || c.Setup[0] != `CREATE OR REPLACE VIEW "_bino_ds_sales" AS (SELECT * FROM sales_csv)` {
		t.Errorf("Setup = %q", c.Setup)
	}
	want := `SELECT * EXCLUDE (shifted), shifted AS pp2 FROM bino_shift('_bino_ds_sales', 'ac1', '1 year', 'month')`
	if c.Query != want {
		t.Errorf("Query = %q\nwant  %q", c.Query, want)
	}
}

func TestCompile_TwoDerivedSlotsNest(t *testing.T) {
	t.Parallel()
	c := compileDoc(t, "sales", map[string]any{
		"source": "sales_csv",
		"derive": map[string]any{
			"pp2": map[string]any{"from": "ac1", "shift": "1 year", "grain": "month"},
			"pp1": map[string]any{"from": "ac1", "shift": "1 month", "grain": "month"},
		},
	}, "")
	wantSetup := []string{
		`CREATE OR REPLACE VIEW "_bino_ds_sales" AS (SELECT * FROM "sales_csv")`,
		`CREATE OR REPLACE VIEW "_bino_ds_sales__pp1" AS (SELECT * EXCLUDE (shifted), shifted AS pp1 FROM bino_shift('_bino_ds_sales', 'ac1', '1 month', 'month'))`,
	}
	if len(c.Setup) != 2 || c.Setup[0] != wantSetup[0] || c.Setup[1] != wantSetup[1] {
		t.Errorf("Setup = %q\nwant  %q", c.Setup, wantSetup)
	}
	want := `SELECT * EXCLUDE (shifted), shifted AS pp2 FROM bino_shift('_bino_ds_sales__pp1', 'ac1', '1 year', 'month')`
	if c.Query != want {
		t.Errorf("Query = %q", c.Query)
	}
}

func TestCompile_AssertOnlyReadsView(t *testing.T) {
	t.Parallel()
	c := compileDoc(t, "sales", map[string]any{
		"query":  "SELECT * FROM sales_csv",
		"assert": map[string]any{"pp1": map[string]any{"from": "ac1", "shift": "1 year", "grain": "month"}},
	}, "")
	if c.Query != `SELECT * FROM "_bino_ds_sales"` || len(c.Setup) != 1 {
		t.Errorf("unexpected %+v", c)
	}
}

func TestCompile_RejectsBadDeclarations(t *testing.T) {
	t.Parallel()
	ok := map[string]any{"from": "ac1", "shift": "1 year", "grain": "month"}
	cases := []struct {
		name string
		spec map[string]any
		want string
	}{
		{"slot in both maps", map[string]any{"query": "SELECT 1",
			"derive": map[string]any{"pp1": ok}, "assert": map[string]any{"pp1": ok}},
			"slot pp1 declared in both derive and assert"},
		{"pp5", map[string]any{"query": "SELECT 1", "derive": map[string]any{"pp5": ok}}, "not a previous-period slot"},
		{"ac1 as key", map[string]any{"query": "SELECT 1", "derive": map[string]any{"ac1": ok}}, "not a previous-period slot"},
		{"bad from", map[string]any{"query": "SELECT 1", "derive": map[string]any{"pp1": map[string]any{"from": "revenue", "shift": "1 year", "grain": "month"}}}, "not a scenario slot"},
		{"bad shift", map[string]any{"query": "SELECT 1", "derive": map[string]any{"pp1": map[string]any{"from": "ac1", "shift": "one year", "grain": "month"}}}, "shift"},
		{"bad grain", map[string]any{"query": "SELECT 1", "assert": map[string]any{"pp1": map[string]any{"from": "ac1", "shift": "1 year", "grain": "fortnight"}}}, "grain"},
		{"missing grain", map[string]any{"query": "SELECT 1", "assert": map[string]any{"pp1": map[string]any{"from": "ac1", "shift": "1 year"}}}, "grain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileErr(t, tc.spec)
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestLimitQuery(t *testing.T) {
	t.Parallel()
	got := LimitQuery("SELECT 1;\n", 5)
	if got != "SELECT * FROM (SELECT 1) AS _q LIMIT 5" {
		t.Errorf("LimitQuery = %q", got)
	}
}

func TestNeedsPrql(t *testing.T) {
	t.Parallel()
	mk := func(kind string, spec map[string]any) config.Document {
		raw, _ := json.Marshal(map[string]any{"spec": spec})
		return config.Document{Kind: kind, Raw: raw}
	}
	if NeedsPrql([]config.Document{mk("DataSet", map[string]any{"query": "SELECT 1"})}) {
		t.Error("sql dataset should not need prql")
	}
	if !NeedsPrql([]config.Document{mk("DataSet", map[string]any{"prql": "from t"})}) {
		t.Error("inline prql should need prql")
	}
	if !NeedsPrql([]config.Document{mk("DataSet", map[string]any{"prql": map[string]any{"$file": "q.prql"}})}) {
		t.Error("prql file reference should need prql")
	}
	if NeedsPrql([]config.Document{mk("Table", map[string]any{"prql": "from t"})}) {
		t.Error("prql on a non-dataset kind is ignored")
	}
}
