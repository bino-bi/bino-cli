package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGlobalConfig writes ~/.bino/config.toml under the (faked) home dir.
func writeGlobalConfig(t *testing.T, content string) {
	t.Helper()
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(".bino", "config.toml")
	if !strings.HasSuffix(path, want) {
		t.Errorf("path = %q, want suffix %q", path, want)
	}
}

func TestLoadGlobalConfig(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cfg, err := LoadGlobalConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Registry.URL != "" {
			t.Errorf("URL = %q, want empty", cfg.Registry.URL)
		}
	})

	t.Run("valid file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeGlobalConfig(t, "[registry]\nurl = \"https://registry.corp.example.com\"\n")
		cfg, err := LoadGlobalConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Registry.URL != "https://registry.corp.example.com" {
			t.Errorf("URL = %q", cfg.Registry.URL)
		}
	})

	t.Run("unknown keys tolerated", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeGlobalConfig(t, "[registry]\nurl = \"https://r.example.com\"\n[future]\nsetting = true\n")
		cfg, err := LoadGlobalConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Registry.URL != "https://r.example.com" {
			t.Errorf("URL = %q", cfg.Registry.URL)
		}
	})

	t.Run("malformed file errors", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeGlobalConfig(t, "[[registry\n")
		if _, err := LoadGlobalConfig(); err == nil {
			t.Fatal("expected error for malformed TOML")
		}
	})
}
