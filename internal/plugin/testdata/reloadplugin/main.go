// Command reloadplugin is the plugin the reload tests start. It reports the
// version in BINO_RELOAD_TEST_VERSION, so one build can stand in for two.
// BINO_RELOAD_TEST_MACRO_SQL adds one DuckDB macro (name in
// BINO_RELOAD_TEST_MACRO_NAME, default reloadtest_twice; smoke query in
// BINO_RELOAD_TEST_MACRO_SMOKE). BINO_RELOAD_TEST_KIND adds a component kind.
package main

import (
	"os"

	sdk "github.com/bino-bi/bino-plugin-sdk"
)

func main() {
	version := os.Getenv("BINO_RELOAD_TEST_VERSION")
	if version == "" {
		version = "1.0.0"
	}
	opts := &sdk.PluginOpts{Name: "reloadtest", Version: version}
	if sql := os.Getenv("BINO_RELOAD_TEST_MACRO_SQL"); sql != "" {
		name := os.Getenv("BINO_RELOAD_TEST_MACRO_NAME")
		if name == "" {
			name = "reloadtest_twice"
		}
		opts.SessionMacros = []sdk.SessionMacro{{Name: name, SQL: sql, SmokeSQL: os.Getenv("BINO_RELOAD_TEST_MACRO_SMOKE")}}
	}
	if kind := os.Getenv("BINO_RELOAD_TEST_KIND"); kind != "" {
		opts.Kinds = []sdk.Kind{{Name: kind, Category: sdk.KindComponent}}
	}
	sdk.Serve(opts)
}
