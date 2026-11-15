package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/pkg/duckdb"
)

func TestCheckSessionMacros_Validation(t *testing.T) {
	ctx := context.Background()
	good := SessionMacro{Name: "stats_twice", SQL: "create or replace macro stats_twice(x) as x * 2", SmokeSQL: "SELECT stats_twice(1)"}

	macros, err := checkSessionMacros(ctx, PluginManifest{Name: "stats", SessionMacros: []SessionMacro{good}})
	if err != nil {
		t.Fatalf("good macro: %v", err)
	}
	if len(macros) != 1 || macros[0].Plugin != "stats" || macros[0].Name != "stats_twice" {
		t.Fatalf("macros = %v", macros)
	}
	if macros, err := checkSessionMacros(ctx, PluginManifest{Name: "My-Plugin"}); err != nil || macros != nil {
		t.Fatalf("a plugin without macros must pass whatever its name: %v, %v", macros, err)
	}

	bad := []struct {
		name   string
		plugin string
		macros []SessionMacro
		want   string
	}{
		{"plugin name", "my-plugin", []SessionMacro{good}, "may not register DuckDB macros"},
		{"wrong prefix", "stats", []SessionMacro{{Name: "other_twice", SQL: "CREATE OR REPLACE MACRO other_twice(x) AS x"}}, `must start with "stats_"`},
		{"bino prefix", "bino", []SessionMacro{{Name: "bino_twice", SQL: "CREATE OR REPLACE MACRO bino_twice(x) AS x"}}, "reserved for bino"},
		{"upper case", "stats", []SessionMacro{{Name: "stats_Twice", SQL: "CREATE OR REPLACE MACRO stats_Twice(x) AS x"}}, "must match"},
		{"not a create", "stats", []SessionMacro{{Name: "stats_twice", SQL: "CREATE TABLE stats_twice(x INT)"}}, "CREATE OR REPLACE MACRO stats_twice"},
		{"other macro name", "stats", []SessionMacro{{Name: "stats_twice", SQL: "CREATE OR REPLACE MACRO stats_thrice(x) AS x"}}, "CREATE OR REPLACE MACRO stats_twice"},
		{"temp macro", "stats", []SessionMacro{{Name: "stats_twice", SQL: "CREATE OR REPLACE TEMP MACRO stats_twice(x) AS x"}}, "CREATE OR REPLACE MACRO stats_twice"},
		{"two statements", "stats", []SessionMacro{{Name: "stats_twice", SQL: "CREATE OR REPLACE MACRO stats_twice(x) AS x; CREATE TABLE t(a INT)"}}, "must be exactly one statement"},
		{"two smoke statements", "stats", []SessionMacro{{Name: "stats_twice", SQL: good.SQL, SmokeSQL: "SELECT 1; SELECT 2"}}, "smoke query: must be exactly one statement"},
		{"declared twice", "stats", []SessionMacro{good, good}, "declared twice"},
		{"syntax error", "stats", []SessionMacro{{Name: "stats_twice", SQL: "CREATE OR REPLACE MACRO stats_twice(x) AS x +"}}, "syntax error"},
		{"failing smoke", "stats", []SessionMacro{{Name: "stats_twice", SQL: good.SQL, SmokeSQL: "SELECT stats_twice(nocol)"}}, "smoke query"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := checkSessionMacros(ctx, PluginManifest{Name: tc.plugin, SessionMacros: tc.macros})
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should contain %q", err, tc.want)
			}
			if tc.name != "plugin name" && !strings.Contains(err.Error(), tc.macros[0].Name) {
				t.Fatalf("error %q should name the macro %q", err, tc.macros[0].Name)
			}
		})
	}
}

func TestCheckBuiltinKinds(t *testing.T) {
	err := checkBuiltinKinds(PluginManifest{Name: "charts", Kinds: []KindRegistration{{KindName: "ChartsGauge"}, {KindName: "ChartTime"}}})
	if err == nil {
		t.Fatal("a plugin kind named like a built-in kind was accepted")
	}
	if !strings.Contains(err.Error(), "ChartTime") || !strings.Contains(err.Error(), "charts") {
		t.Fatalf("error %q should name the kind and the plugin", err)
	}
	if err := checkBuiltinKinds(PluginManifest{Name: "charts", Kinds: []KindRegistration{{KindName: "ChartsGauge"}}}); err != nil {
		t.Fatalf("own kind refused: %v", err)
	}
}

// reloadTestProject writes a project that declares the test plugin and
// returns its root and the installed binary.
func reloadTestProject(t *testing.T) (root, bin string) {
	t.Helper()
	root = t.TempDir()
	bin = filepath.Join(root, "bin", filepath.Base(reloadPluginBin))
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	installBinary(t, reloadPluginBin, bin)
	toml := fmt.Sprintf("[plugins.reloadtest]\npath = %q\n", bin)
	if err := os.WriteFile(filepath.Join(root, "bino.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, bin
}

func TestManager_SessionMacros(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and starts a plugin")
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = duckdb.SetPluginMacros(context.Background(), nil) })
	root, bin := reloadTestProject(t)
	cfg, err := pathutil.LoadProjectConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	twice := func(s *duckdb.Session) (int, error) {
		var n int
		err := s.DB().QueryRowContext(ctx, "SELECT reloadtest_twice(2)").Scan(&n)
		return n, err
	}

	// A session that is open before the plugins load, like a long-lived
	// preview session after a bino.toml edit.
	live, err := duckdb.OpenSession(ctx, duckdb.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()

	t.Setenv("BINO_RELOAD_TEST_MACRO_SQL", "CREATE OR REPLACE MACRO reloadtest_twice(x) AS x * 2")
	t.Setenv("BINO_RELOAD_TEST_MACRO_SMOKE", "SELECT reloadtest_twice(1)")
	mgr := NewManager(logx.Nop())
	t.Cleanup(func() { mgr.ShutdownAll(context.Background()) })
	if err := mgr.LoadAll(ctx, cfg, root, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if n, err := twice(live); err != nil || n != 4 {
		t.Fatalf("open session: reloadtest_twice(2) = %d, %v; want 4", n, err)
	}
	fresh, err := duckdb.OpenSession(ctx, duckdb.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if n, err := twice(fresh); err != nil || n != 4 {
		t.Fatalf("new session: reloadtest_twice(2) = %d, %v; want 4", n, err)
	}

	// A rebuild with another macro body is live after the reload, in the
	// sessions that stayed open.
	t.Setenv("BINO_RELOAD_TEST_MACRO_SQL", "CREATE OR REPLACE MACRO reloadtest_twice(x) AS x * 3")
	installBinary(t, reloadPluginBin, bin)
	if err := mgr.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if n, err := twice(live); err != nil || n != 6 {
		t.Fatalf("after reload: reloadtest_twice(2) = %d, %v; want 6", n, err)
	}

	// A rebuild whose macro is broken fails the reload and names the macro.
	// The old macro goes away with the plugin.
	t.Setenv("BINO_RELOAD_TEST_MACRO_SMOKE", "SELECT reloadtest_twice(nocol)")
	installBinary(t, reloadPluginBin, bin)
	err = mgr.Reload(ctx)
	if err == nil || !strings.Contains(err.Error(), "reloadtest_twice") {
		t.Fatalf("Reload with a failing smoke query: %v, want an error naming the macro", err)
	}
	if _, err := twice(live); err == nil {
		t.Fatal("the macro of a plugin that failed to load is still registered")
	}

	// A rebuild without macros loads again and registers none.
	t.Setenv("BINO_RELOAD_TEST_MACRO_SQL", "")
	installBinary(t, reloadPluginBin, bin)
	if err := mgr.Reload(ctx); err != nil {
		t.Fatalf("Reload without macros: %v", err)
	}
	if len(duckdb.PluginMacros()) != 0 {
		t.Fatalf("macros left after the plugin dropped them: %v", duckdb.PluginMacros())
	}
}

func TestManager_LoadRefusals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and starts a plugin")
	}
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"built-in kind", map[string]string{"BINO_RELOAD_TEST_KIND": "ChartTime"}, []string{"ChartTime", "reloadtest", "built-in"}},
		{"macro prefix", map[string]string{
			"BINO_RELOAD_TEST_MACRO_NAME": "other_twice",
			"BINO_RELOAD_TEST_MACRO_SQL":  "CREATE OR REPLACE MACRO other_twice(x) AS x",
		}, []string{"other_twice", `"reloadtest_"`}},
		{"bino name", map[string]string{
			"BINO_RELOAD_TEST_MACRO_NAME": "bino_twice",
			"BINO_RELOAD_TEST_MACRO_SQL":  "CREATE OR REPLACE MACRO bino_twice(x) AS x",
		}, []string{"bino_twice", "reserved"}},
		{"macro syntax", map[string]string{
			"BINO_RELOAD_TEST_MACRO_SQL": "CREATE OR REPLACE MACRO reloadtest_twice(x) AS x +",
		}, []string{"reloadtest_twice", "syntax error"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { _ = duckdb.SetPluginMacros(context.Background(), nil) })
			root, _ := reloadTestProject(t)
			cfg, err := pathutil.LoadProjectConfig(root)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			mgr := NewManager(logx.Nop())
			t.Cleanup(func() { mgr.ShutdownAll(context.Background()) })
			err = mgr.LoadAll(context.Background(), cfg, root, "1.0.0")
			if err == nil {
				t.Fatal("the plugin loaded")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q should contain %q", err, want)
				}
			}
			if len(mgr.Registry().AllPlugins()) != 0 || len(duckdb.PluginMacros()) != 0 {
				t.Fatal("a refused plugin left something registered")
			}
		})
	}
}
