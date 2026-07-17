//go:build ignore

// Package main bundles the preview and serve UI entrypoints into static
// JS files that are embedded into the bino binary. Run via:
//
//	go generate ./internal/web/...
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
				"Run 'npm ci' inside internal/web/ to enable.")
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

	copyFonts(wd)
}

// copyFonts copies the IBM Plex Mono woff2 subsets used by shared/fonts.css
// from the @fontsource package into static/fonts/ so they are committed and
// embedded alongside the JS bundles.
func copyFonts(wd string) {
	srcDir := filepath.Join(wd, "node_modules", "@fontsource", "ibm-plex-mono", "files")
	dstDir := filepath.Join(wd, "static", "fonts")
	must(os.MkdirAll(dstDir, 0o755))
	for _, name := range []string{
		"ibm-plex-mono-latin-400-normal.woff2",
		"ibm-plex-mono-latin-500-normal.woff2",
		"ibm-plex-mono-latin-600-normal.woff2",
	} {
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		must(err)
		must(os.WriteFile(filepath.Join(dstDir, name), data, 0o644))
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
