// Command reloadplugin is the plugin the reload tests start. It reports the
// version in BINO_RELOAD_TEST_VERSION, so one build can stand in for two.
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
	sdk.Serve(&sdk.PluginOpts{Name: "reloadtest", Version: version})
}
