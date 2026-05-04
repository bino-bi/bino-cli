//go:build ignore

// Package main bundles the preview and serve UI entrypoints into static
// JS files that are embedded into the bino binary. Run via:
//
//	go generate ./internal/cli/web/...
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"
)

func main() {
	wd, err := os.Getwd()
	must(err)

	if _, err := os.Stat(filepath.Join(wd, "node_modules")); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr,
			"web/bundle: node_modules missing — skipping bundle regeneration. "+
				"Run 'npm ci' inside internal/cli/web/ to enable.")
		return
	}

	must(os.MkdirAll(filepath.Join(wd, "static"), 0o755))

	result := api.Build(api.BuildOptions{
		EntryPointsAdvanced: []api.EntryPoint{
			{InputPath: "preview/preview-app.js", OutputPath: "preview"},
			{InputPath: "serve/serve-app.js", OutputPath: "serve"},
		},
		Outdir:            "static",
		Bundle:            true,
		Format:            api.FormatESModule,
		Target:            api.ES2022,
		Platform:          api.PlatformBrowser,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		LegalComments:     api.LegalCommentsEndOfFile,
		Write:             true,
		LogLevel:          api.LogLevelWarning,
	})
	if len(result.Errors) > 0 {
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
