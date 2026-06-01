package cli

// AppMetadata contains project-level information displayed by the about command.
var AppMetadata = struct {
	Name        string
	Description string
	URL         string
	Author      string
	Email       string
	Years       string
	License     string
}{
	Name:        "bino",
	Description: "A Go-based CLI that validates YAML report bundles, runs DuckDB-backed data pipelines, and renders HTML/PDF artefacts via chromedp.",
	URL:         "https://github.com/bino-bi/bino-cli",
	Author:      "Sven Herrmann",
	Email:       "sven@bino.bi",
	Years:       "2024–2026",
	License:     "AGPL-3.0-or-later",
}

// DependencyInfo describes an external Go module dependency.
type DependencyInfo struct {
	Module  string // Go module path
	Version string // Version from go.mod
	URL     string // Canonical URL (usually GitHub)
	License string // SPDX license identifier
}

// DirectDependencies lists the direct dependencies (Go modules and bundled
// JS libraries) shipped with bino along with their licenses as discovered
// from their upstream repositories.
var DirectDependencies = []DependencyInfo{
	{
		Module:  "github.com/spf13/cobra",
		Version: "v1.8.0",
		URL:     "https://github.com/spf13/cobra",
		License: "Apache-2.0",
	},
	{
		Module:  "github.com/fatih/color",
		Version: "v1.18.0",
		URL:     "https://github.com/fatih/color",
		License: "MIT",
	},
	{
		Module:  "github.com/briandowns/spinner",
		Version: "v1.23.2",
		URL:     "https://github.com/briandowns/spinner",
		License: "Apache-2.0",
	},
	{
		Module:  "github.com/duckdb/duckdb-go/v2",
		Version: "v2.5.2",
		URL:     "https://github.com/duckdb/duckdb-go",
		License: "MIT",
	},
	{
		Module:  "github.com/chromedp/chromedp",
		Version: "v0.13.1",
		URL:     "https://github.com/chromedp/chromedp",
		License: "MIT",
	},
	{
		Module:  "github.com/chromedp/cdproto",
		Version: "v0.0.0-20250222",
		URL:     "https://github.com/chromedp/cdproto",
		License: "MIT",
	},
	{
		Module:  "github.com/santhosh-tekuri/jsonschema/v6",
		Version: "v6.0.2",
		URL:     "https://github.com/santhosh-tekuri/jsonschema",
		License: "Apache-2.0",
	},
	{
		Module:  "gopkg.in/yaml.v3",
		Version: "v3.0.1",
		URL:     "https://github.com/go-yaml/yaml",
		License: "MIT + Apache-2.0",
	},
	{
		Module:  "github.com/fsnotify/fsnotify",
		Version: "v1.7.0",
		URL:     "https://github.com/fsnotify/fsnotify",
		License: "BSD-3-Clause",
	},
	{
		Module:  "github.com/google/uuid",
		Version: "v1.6.0",
		URL:     "https://github.com/google/uuid",
		License: "BSD-3-Clause",
	},
	{
		Module:  "github.com/digitorus/pdfsign",
		Version: "v0.0.0-20250819",
		URL:     "https://github.com/digitorus/pdfsign",
		License: "BSD-2-Clause",
	},
	{
		Module:  "github.com/sabhiram/go-gitignore",
		Version: "v0.0.0-20210923",
		URL:     "https://github.com/sabhiram/go-gitignore",
		License: "MIT",
	},
	{
		Module:  "golang.org/x/term",
		Version: "v0.37.0",
		URL:     "https://github.com/golang/term",
		License: "BSD-3-Clause",
	},
	{
		Module:  "lit",
		Version: "3.2.1",
		URL:     "https://github.com/lit/lit",
		License: "BSD-3-Clause",
	},
	{
		Module:  "idiomorph",
		Version: "0.7.4",
		URL:     "https://github.com/bigskysoftware/idiomorph",
		License: "BSD-2-Clause",
	},
}
