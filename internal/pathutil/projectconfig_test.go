package pathutil

import (
	"os"
	"path/filepath"
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
