package duckdb

import (
	"context"
	"strings"
	"testing"
)

func scalarInt(t *testing.T, s *Session, query string) (int, error) {
	t.Helper()
	var n int
	err := s.db.QueryRowContext(context.Background(), query).Scan(&n)
	return n, err
}

// These tests change process-wide state, so they do not run in parallel.
func TestSetPluginMacros_OpenAndNewSessions(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { _ = SetPluginMacros(ctx, nil) })

	before, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()

	twice := PluginMacro{Plugin: "demo", Name: "demo_twice", SQL: "CREATE OR REPLACE MACRO demo_twice(x) AS x * 2"}
	if err := SetPluginMacros(ctx, []PluginMacro{twice}); err != nil {
		t.Fatal(err)
	}
	after, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()

	for name, s := range map[string]*Session{"open session": before, "new session": after} {
		if n, err := scalarInt(t, s, "SELECT demo_twice(2)"); err != nil || n != 4 {
			t.Fatalf("%s: demo_twice(2) = %d, %v; want 4", name, n, err)
		}
		// The built-ins are still there.
		if _, err := s.db.ExecContext(ctx, "SELECT op(1.0)"); err != nil {
			t.Fatalf("%s: built-in op: %v", name, err)
		}
	}

	// A changed body replaces the macro; a macro may turn into a table macro.
	twice.SQL = "CREATE OR REPLACE MACRO demo_twice(x) AS x * 3"
	rows := PluginMacro{Plugin: "demo", Name: "demo_rows", SQL: "CREATE OR REPLACE MACRO demo_rows(x) AS x"}
	if err := SetPluginMacros(ctx, []PluginMacro{twice, rows}); err != nil {
		t.Fatal(err)
	}
	if n, err := scalarInt(t, before, "SELECT demo_twice(2)"); err != nil || n != 6 {
		t.Fatalf("after a body change demo_twice(2) = %d, %v; want 6", n, err)
	}
	rows.SQL = "CREATE OR REPLACE MACRO demo_rows(x) AS TABLE SELECT x AS v"
	if err := SetPluginMacros(ctx, []PluginMacro{twice, rows}); err != nil {
		t.Fatal(err)
	}
	if n, err := scalarInt(t, before, "SELECT v FROM demo_rows(7)"); err != nil || n != 7 {
		t.Fatalf("table macro demo_rows(7) = %d, %v; want 7", n, err)
	}

	// The scalar form is gone, in the open session too.
	if _, err := scalarInt(t, before, "SELECT demo_rows(7)"); err == nil {
		t.Fatal("the scalar demo_rows still works after it became a table macro")
	}
	rows.SQL = "CREATE OR REPLACE MACRO demo_rows(x) AS x + 1"
	if err := SetPluginMacros(ctx, []PluginMacro{twice, rows}); err != nil {
		t.Fatal(err)
	}
	if _, err := scalarInt(t, before, "SELECT v FROM demo_rows(7)"); err == nil {
		t.Fatal("the table demo_rows still works after it became a scalar macro")
	}
	if n, err := scalarInt(t, before, "SELECT demo_rows(7)"); err != nil || n != 8 {
		t.Fatalf("scalar demo_rows(7) = %d, %v; want 8", n, err)
	}

	got := PluginMacros()
	if len(got) != 2 || got[0].Name != "demo_twice" || got[1].Name != "demo_rows" {
		t.Fatalf("PluginMacros() = %v, want the given order", got)
	}

	// Removed macros are dropped, scalar and table.
	if err := SetPluginMacros(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT demo_twice(2)", "SELECT demo_rows(7)"} {
		if _, err := scalarInt(t, before, q); err == nil {
			t.Fatalf("%q still works after the macro was removed", q)
		}
	}
	fresh, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if _, err := scalarInt(t, fresh, "SELECT demo_twice(2)"); err == nil {
		t.Fatal("a new session still has the removed macro")
	}
}

func TestSetPluginMacros_ClosedSessionIsForgotten(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { _ = SetPluginMacros(ctx, nil) })
	s, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := SetPluginMacros(ctx, []PluginMacro{{Plugin: "demo", Name: "demo_one", SQL: "CREATE OR REPLACE MACRO demo_one() AS 1"}}); err != nil {
		t.Fatalf("a closed session was updated: %v", err)
	}
}

func TestCheckPluginMacros(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { _ = SetPluginMacros(ctx, nil) })

	ok := PluginMacro{Plugin: "demo", Name: "demo_spread", SQL: "CREATE OR REPLACE MACRO demo_spread(x) AS max(x) - min(x)", SmokeSQL: "SELECT demo_spread(v) FROM (VALUES (1), (3)) t(v)"}
	if err := CheckPluginMacros(ctx, []PluginMacro{ok}); err != nil {
		t.Fatalf("good macro: %v", err)
	}
	// The check session has no plugin macros, so a reload can check the
	// macros that are already live.
	if err := SetPluginMacros(ctx, []PluginMacro{ok}); err != nil {
		t.Fatal(err)
	}
	if err := CheckPluginMacros(ctx, []PluginMacro{ok}); err != nil {
		t.Fatalf("macro that is already live: %v", err)
	}

	bad := []struct {
		name  string
		macro PluginMacro
		want  string
	}{
		{"syntax error", PluginMacro{Name: "demo_bad", SQL: "CREATE OR REPLACE MACRO demo_bad(x) AS x +"}, "syntax error"},
		{"replaces a function", PluginMacro{Name: "abs", SQL: "CREATE OR REPLACE MACRO abs(x) AS x"}, "already exists"},
		{"replaces a bino function", PluginMacro{Name: "bino_shift", SQL: "CREATE OR REPLACE MACRO bino_shift(x) AS x"}, "already exists"},
		{"creates another name", PluginMacro{Name: "demo_a", SQL: "CREATE OR REPLACE MACRO demo_b(x) AS x"}, "did not create"},
		// The body reads a table given by the caller, so DuckDB can bind it
		// only in a call. Only the smoke query finds the wrong column.
		{"table macro with an unknown column", PluginMacro{
			Name:     "demo_tbl",
			SQL:      "CREATE OR REPLACE MACRO demo_tbl(t) AS TABLE SELECT nocol FROM query_table(t)",
			SmokeSQL: "WITH src AS (SELECT 1 AS a) SELECT * FROM demo_tbl(src)",
		}, "smoke query"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPluginMacros(ctx, []PluginMacro{tc.macro})
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.macro.Name) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should name %q and contain %q", err, tc.macro.Name, tc.want)
			}
		})
	}
}

// A macro may call one declared before it, whatever the names sort to.
func TestSetPluginMacros_KeepsDeclaredOrder(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { _ = SetPluginMacros(ctx, nil) })
	macros := []PluginMacro{
		{Plugin: "demo", Name: "demo_z", SQL: "CREATE OR REPLACE MACRO demo_z(x) AS x * 2"},
		{Plugin: "demo", Name: "demo_a", SQL: "CREATE OR REPLACE MACRO demo_a(x) AS demo_z(x) + 1", SmokeSQL: "SELECT demo_a(1)"},
	}
	if err := CheckPluginMacros(ctx, macros); err != nil {
		t.Fatal(err)
	}
	open, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	if err := SetPluginMacros(ctx, macros); err != nil {
		t.Fatal(err)
	}
	fresh, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer fresh.Close()
	for name, s := range map[string]*Session{"open session": open, "new session": fresh} {
		if n, err := scalarInt(t, s, "SELECT demo_a(1)"); err != nil || n != 3 {
			t.Fatalf("%s: demo_a(1) = %d, %v; want 3", name, n, err)
		}
	}
}

func TestCheckPluginMacros_OneStatement(t *testing.T) {
	ctx := context.Background()
	cases := map[string]bool{
		"CREATE OR REPLACE MACRO d_f(x) AS x":                                                 true,
		"CREATE OR REPLACE MACRO d_f(x) AS x ; \n":                                            true,
		"CREATE OR REPLACE MACRO d_f(x) AS x; -- done":                                        true,
		"CREATE OR REPLACE MACRO d_f(x) AS x || ';'":                                          true,
		"CREATE OR REPLACE MACRO d_f(x) AS x || 'it''s; fine'":                                true,
		"CREATE OR REPLACE MACRO d_f(x) AS /* a; b */ x":                                      true,
		"CREATE OR REPLACE MACRO d_f(x) AS x || $$a;b$$":                                      true,
		"CREATE OR REPLACE MACRO d_f(x) AS x; CREATE TABLE t(a INT)":                          false,
		"CREATE OR REPLACE MACRO d_f() AS E'\\''; CREATE TABLE t AS SELECT 1; --'":            false,
		"CREATE OR REPLACE MACRO d_f(x$y$) AS 1; CREATE TABLE t AS SELECT 1 AS z$y$":          false,
		"CREATE OR REPLACE MACRO d_f() AS 1 /* /* */ ' */ ; CREATE TABLE t AS SELECT 1; -- '": false,
	}
	for sql, want := range cases {
		err := CheckPluginMacros(ctx, []PluginMacro{{Name: "d_f", SQL: sql}})
		if (err == nil) != want {
			t.Errorf("CheckPluginMacros(%q) = %v, want ok=%v", sql, err, want)
		}
		if !want && !strings.Contains(err.Error(), "exactly one statement") {
			t.Errorf("CheckPluginMacros(%q) = %v, want the one-statement error", sql, err)
		}
	}
	if err := CheckPluginMacros(ctx, []PluginMacro{{Name: "d_f", SQL: "CREATE OR REPLACE MACRO d_f(x) AS x", SmokeSQL: "SELECT d_f(1); SELECT 2"}}); err == nil || !strings.Contains(err.Error(), "smoke query: must be exactly one statement") {
		t.Errorf("two smoke statements: %v", err)
	}
}
