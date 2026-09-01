package pathutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeToml(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectConfig_WithPlugins(t *testing.T) {
	tmp := t.TempDir()
	writeToml(t, tmp, `
report-id = "test-123"

[plugins.salesforce]
version = "1.2.0"
path = "/usr/local/bin/bino-plugin-salesforce"
hook_timeout = "60s"

[plugins.salesforce.config]
api_version = "v59.0"
default_sandbox = "true"

[plugins.snowflake]
`)

	cfg, err := LoadProjectConfig(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(cfg.Plugins))
	}

	sf := cfg.Plugins["salesforce"]
	if sf.Version != "1.2.0" {
		t.Fatalf("expected version 1.2.0, got %q", sf.Version)
	}
	if sf.Path != "/usr/local/bin/bino-plugin-salesforce" {
		t.Fatalf("expected explicit path, got %q", sf.Path)
	}
	if sf.HookTimeout != "60s" {
		t.Fatalf("expected hook_timeout 60s, got %q", sf.HookTimeout)
	}
	if sf.Config["api_version"] != "v59.0" {
		t.Fatalf("expected api_version v59.0, got %q", sf.Config["api_version"])
	}
	if sf.Config["default_sandbox"] != "true" {
		t.Fatalf("expected default_sandbox true, got %q", sf.Config["default_sandbox"])
	}

	snow := cfg.Plugins["snowflake"]
	if snow.Version != "" {
		t.Fatalf("expected empty version for snowflake, got %q", snow.Version)
	}
}

func TestLoadProjectConfig_NoPlugins(t *testing.T) {
	tmp := t.TempDir()
	writeToml(t, tmp, `report-id = "test-123"`)

	cfg, err := LoadProjectConfig(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Plugins) > 0 {
		t.Fatalf("expected no plugins, got %d", len(cfg.Plugins))
	}
}

func TestLoadProjectConfig_WithLintConfig(t *testing.T) {
	tmp := t.TempDir()
	writeToml(t, tmp, `
report-id = "test-123"

[lint]
disable = ["no-unused-ds", "salesforce/field-access"]

[lint.severity]
"salesforce/field-access" = "warning"
`)

	cfg, err := LoadProjectConfig(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Lint.Disable) != 2 {
		t.Fatalf("expected 2 disabled rules, got %d", len(cfg.Lint.Disable))
	}
	if cfg.Lint.Disable[0] != "no-unused-ds" {
		t.Fatalf("expected first disabled rule 'no-unused-ds', got %q", cfg.Lint.Disable[0])
	}
	if cfg.Lint.Severity["salesforce/field-access"] != "warning" {
		t.Fatalf("expected severity override, got %q", cfg.Lint.Severity["salesforce/field-access"])
	}
}

func TestLoadProjectConfig_BackwardsCompatible(t *testing.T) {
	// An existing bino.toml with hooks and build config should still parse.
	tmp := t.TempDir()
	writeToml(t, tmp, `
report-id = "old-project"
engine-version = "v1.0.0"

[build.args]
format = "pdf"

[build.env.values]
API_KEY = "secret"
`)

	cfg, err := LoadProjectConfig(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ReportID != "old-project" {
		t.Fatalf("expected report-id 'old-project', got %q", cfg.ReportID)
	}
	if cfg.EngineVersion != "v1.0.0" {
		t.Fatalf("expected engine-version v1.0.0, got %q", cfg.EngineVersion)
	}
	if len(cfg.Plugins) > 0 {
		t.Fatal("expected no plugins in backwards-compatible config")
	}
}

func TestFindEngineVersionLine(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantLine int
		wantCol  int
	}{
		{
			name:     "first line",
			content:  "engine-version = \"v1.0.0\"\nreport-id = \"x\"\n",
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "later line",
			content:  "report-id = \"x\"\n\nengine-version = \"v1.0.0\"\n",
			wantLine: 3,
			wantCol:  1,
		},
		{
			name:     "leading spaces",
			content:  "report-id = \"x\"\n    engine-version = \"v1.0.0\"\n",
			wantLine: 2,
			wantCol:  5,
		},
		{
			name:     "leading tab",
			content:  "report-id = \"x\"\n\tengine-version = \"v1.0.0\"\n",
			wantLine: 2,
			wantCol:  2,
		},
		{
			name:     "missing returns 1,1",
			content:  "report-id = \"x\"\n",
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "commented out skipped",
			content:  "# engine-version = \"v0.5.0\"\nreport-id = \"x\"\nengine-version = \"v1.0.0\"\n",
			wantLine: 3,
			wantCol:  1,
		},
		{
			name:     "duplicate returns first",
			content:  "engine-version = \"v1.0.0\"\nengine-version = \"v2.0.0\"\n",
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "CRLF line endings",
			content:  "report-id = \"x\"\r\nengine-version = \"v1.0.0\"\r\n",
			wantLine: 2,
			wantCol:  1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			path := filepath.Join(tmp, "bino.toml")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			line, col := FindEngineVersionLine(path)
			if line != tc.wantLine || col != tc.wantCol {
				t.Errorf("FindEngineVersionLine = (%d, %d), want (%d, %d)",
					line, col, tc.wantLine, tc.wantCol)
			}
		})
	}
}

func TestFindEngineVersionLine_MissingFile(t *testing.T) {
	line, col := FindEngineVersionLine(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if line != 1 || col != 1 {
		t.Errorf("FindEngineVersionLine on missing file = (%d, %d), want (1, 1)", line, col)
	}
}

func TestLoadProjectConfig_NoPackageTable(t *testing.T) {
	// A bino.toml without [package] must load exactly as before.
	tmp := t.TempDir()
	writeToml(t, tmp, `
report-id = "test-123"
engine-version = "v1.0.0"

[dependencies]
"@acme/kit" = "1.2.3"
`)

	cfg, err := LoadProjectConfig(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Package != nil {
		t.Fatalf("expected nil Package, got %+v", cfg.Package)
	}
	if cfg.ReportID != "test-123" {
		t.Fatalf("expected report-id 'test-123', got %q", cfg.ReportID)
	}
	if cfg.EngineVersion != "v1.0.0" {
		t.Fatalf("expected engine-version v1.0.0, got %q", cfg.EngineVersion)
	}
	if cfg.Dependencies["@acme/kit"] != "1.2.3" {
		t.Fatalf("expected dependency 1.2.3, got %q", cfg.Dependencies["@acme/kit"])
	}
}

func TestLoadProjectConfig_BarePackageTable(t *testing.T) {
	// An empty [package] table is still a predef marker.
	tmp := t.TempDir()
	writeToml(t, tmp, `
report-id = "test-123"

[package]
`)

	cfg, err := LoadProjectConfig(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Package == nil {
		t.Fatal("expected non-nil Package for a bare [package] table")
	}
	if cfg.Package.Name != "" {
		t.Fatalf("expected empty package name, got %q", cfg.Package.Name)
	}
}

func TestLoadProjectConfig_PackageFields(t *testing.T) {
	tmp := t.TempDir()
	writeToml(t, tmp, `
report-id = "test-123"

[package]
name = "@acme/starter-kit"
description = "A starter kit"
tags = ["sales", "finance"]
category = "dashboards"
visibility = "public"
compat-engine = ">=1.0.0"
compat-cli = "^0.9"
include = ["components", "styles/corporate.yaml"]
preview = "mocks/preview.yaml#demo"
`)

	cfg, err := LoadProjectConfig(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReportID != "test-123" {
		t.Fatalf("expected report-id to coexist with [package], got %q", cfg.ReportID)
	}
	pkg := cfg.Package
	if pkg == nil {
		t.Fatal("expected non-nil Package")
	}
	if pkg.Name != "@acme/starter-kit" {
		t.Fatalf("name = %q", pkg.Name)
	}
	if pkg.Description != "A starter kit" {
		t.Fatalf("description = %q", pkg.Description)
	}
	if len(pkg.Tags) != 2 || pkg.Tags[0] != "sales" || pkg.Tags[1] != "finance" {
		t.Fatalf("tags = %v", pkg.Tags)
	}
	if pkg.Category != "dashboards" {
		t.Fatalf("category = %q", pkg.Category)
	}
	if pkg.Visibility != "public" {
		t.Fatalf("visibility = %q", pkg.Visibility)
	}
	if pkg.CompatEngine != ">=1.0.0" {
		t.Fatalf("compat-engine = %q", pkg.CompatEngine)
	}
	if pkg.CompatCLI != "^0.9" {
		t.Fatalf("compat-cli = %q", pkg.CompatCLI)
	}
	if len(pkg.Include) != 2 || pkg.Include[0] != "components" || pkg.Include[1] != "styles/corporate.yaml" {
		t.Fatalf("include = %v", pkg.Include)
	}
	if pkg.Preview != "mocks/preview.yaml#demo" {
		t.Fatalf("preview = %q", pkg.Preview)
	}
}

func TestPackageConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		pkg     PackageConfig
		wantErr string
	}{
		{
			name: "valid",
			pkg: PackageConfig{
				Name:         "@acme/starter-kit",
				Visibility:   "public",
				CompatEngine: ">=1.0.0",
				CompatCLI:    "^0.9",
				Include:      []string{".", "components", "styles/corporate.yaml"},
				Preview:      "mocks/preview.yaml#demo",
			},
		},
		{
			name:    "missing name",
			pkg:     PackageConfig{},
			wantErr: `[package] name is required`,
		},
		{
			name:    "name without scope",
			pkg:     PackageConfig{Name: "acme/kit"},
			wantErr: `invalid [package] name "acme/kit": expected @scope/name with lowercase a-z0-9_- segments`,
		},
		{
			name:    "name with uppercase scope",
			pkg:     PackageConfig{Name: "@Acme/kit"},
			wantErr: `invalid [package] name "@Acme/kit": expected @scope/name with lowercase a-z0-9_- segments`,
		},
		{
			name:    "name with three segments",
			pkg:     PackageConfig{Name: "@acme/kit/extra"},
			wantErr: `invalid [package] name "@acme/kit/extra": expected @scope/name with lowercase a-z0-9_- segments`,
		},
		{
			name:    "unknown visibility",
			pkg:     PackageConfig{Name: "@acme/kit", Visibility: "secret"},
			wantErr: `invalid [package] visibility "secret": expected "public" or "private"`,
		},
		{
			name:    "unparseable compat-engine",
			pkg:     PackageConfig{Name: "@acme/kit", CompatEngine: "not-a-range"},
			wantErr: `parse [package] compat-engine "not-a-range"`,
		},
		{
			name:    "unparseable compat-cli",
			pkg:     PackageConfig{Name: "@acme/kit", CompatCLI: "not-a-range"},
			wantErr: `parse [package] compat-cli "not-a-range"`,
		},
		{
			name:    "absolute include entry",
			pkg:     PackageConfig{Name: "@acme/kit", Include: []string{"/etc"}},
			wantErr: `invalid [package] include entry "/etc": must be a project-relative path inside the project`,
		},
		{
			name:    "escaping include entry",
			pkg:     PackageConfig{Name: "@acme/kit", Include: []string{"../x"}},
			wantErr: `invalid [package] include entry "../x": must be a project-relative path inside the project`,
		},
		{
			name:    "empty include entry",
			pkg:     PackageConfig{Name: "@acme/kit", Include: []string{""}},
			wantErr: `invalid [package] include entry "": must be a project-relative path inside the project`,
		},
		{
			name:    "preview without hash",
			pkg:     PackageConfig{Name: "@acme/kit", Preview: "mocks/preview.yaml"},
			wantErr: `invalid [package] preview "mocks/preview.yaml": expected "path#definition-name"`,
		},
		{
			name:    "preview without path",
			pkg:     PackageConfig{Name: "@acme/kit", Preview: "#name"},
			wantErr: `invalid [package] preview "#name": expected "path#definition-name"`,
		},
		{
			name:    "preview with two hashes",
			pkg:     PackageConfig{Name: "@acme/kit", Preview: "a#b#c"},
			wantErr: `invalid [package] preview "a#b#c": expected "path#definition-name"`,
		},
		{
			name:    "preview path escaping the project",
			pkg:     PackageConfig{Name: "@acme/kit", Preview: "../other/preview.yaml#demo"},
			wantErr: `invalid [package] preview "../other/preview.yaml#demo": path must be project-relative`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.pkg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestPackageConfigEffectiveVisibility(t *testing.T) {
	cases := []struct {
		name string
		pkg  *PackageConfig
		want string
	}{
		{name: "empty defaults to private", pkg: &PackageConfig{}, want: "private"},
		{name: "declared public", pkg: &PackageConfig{Visibility: "Public"}, want: "public"},
		{name: "declared private", pkg: &PackageConfig{Visibility: "private"}, want: "private"},
		{name: "nil receiver", pkg: nil, want: "private"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pkg.EffectiveVisibility(); got != tc.want {
				t.Fatalf("EffectiveVisibility() = %q, want %q", got, tc.want)
			}
		})
	}
}
