package cli

import (
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/logx"
)

func TestCleanGlobalCacheDirPreservesCredentials(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"credentials.json", "config.toml", "state.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "templates", "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := cleanGlobalCacheDir(logx.Nop(), dir); err != nil {
		t.Fatal(err)
	}

	for _, kept := range []string{"credentials.json", "config.toml"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s must survive a global cache clean", kept)
		}
	}
	for _, gone := range []string{"state.json", "templates"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", gone)
		}
	}
}
