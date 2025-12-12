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
	Description: "A Go-based CLI that validates YAML report bundles, runs DuckDB-backed data pipelines, and renders HTML/PDF artefacts via Playwright.",
	URL:         "https://github.com/bino-bi/bino-cli",
	Author:      "Sven Herrmann",
	Email:       "sven@bino.bi",
	Years:       "2024–2025",
	License:     "Apache-2.0",
}

// DependencyInfo describes an external Go module dependency.
type DependencyInfo struct {
	Module  string // Go module path
	Version string // Version from go.mod
	URL     string // Canonical URL (usually GitHub)
	License string // SPDX license identifier
}

// DirectDependencies lists the direct (require) dependencies from go.mod
// along with their licenses as discovered from their GitHub repositories.
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
		Module:  "github.com/playwright-community/playwright-go",
		Version: "v0.5200.1",
		URL:     "https://github.com/playwright-community/playwright-go",
		License: "MIT",
	},
	{
		Module:  "github.com/xeipuuv/gojsonschema",
		Version: "v1.2.0",
		URL:     "https://github.com/xeipuuv/gojsonschema",
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
}
