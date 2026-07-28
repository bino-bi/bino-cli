package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupTasks(t *testing.T) {
	tests := []struct {
		name             string
		templateEngine   bool
		duckdbExtensions bool
		wantEngine       bool
		wantExtensions   bool
		wantChrome       bool
	}{
		{name: "no flags installs chrome only", wantChrome: true},
		{name: "template engine only", templateEngine: true, wantEngine: true},
		{name: "duckdb extensions only", duckdbExtensions: true, wantExtensions: true},
		{
			name:             "both explicit tasks skip chrome",
			templateEngine:   true,
			duckdbExtensions: true,
			wantEngine:       true,
			wantExtensions:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEngine, gotExtensions, gotChrome := setupTasks(tt.templateEngine, tt.duckdbExtensions)
			if gotEngine != tt.wantEngine || gotExtensions != tt.wantExtensions || gotChrome != tt.wantChrome {
				t.Errorf("setupTasks(%v, %v) = (%v, %v, %v), want (%v, %v, %v)",
					tt.templateEngine, tt.duckdbExtensions,
					gotEngine, gotExtensions, gotChrome,
					tt.wantEngine, tt.wantExtensions, tt.wantChrome)
			}
		})
	}
}

func TestSetupDuckDBExtensionsDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var out bytes.Buffer
	cmd := newSetupCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--duckdb-extensions", "--dry-run"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("setup --duckdb-extensions --dry-run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[dry-run]") {
		t.Errorf("dry-run output missing [dry-run] marker:\n%s", got)
	}
	for _, ext := range []string{"excel", "httpfs", "postgres", "mysql", "prql", "webdavfs"} {
		if !strings.Contains(got, ext) {
			t.Errorf("dry-run output does not mention %q:\n%s", ext, got)
		}
	}
	// A dry run must not touch the network or the filesystem.
	if _, err := os.Stat(filepath.Join(home, ".bino")); !os.IsNotExist(err) {
		t.Errorf("dry-run created %s", filepath.Join(home, ".bino"))
	}
}
