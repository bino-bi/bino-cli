package dataset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisableProjectCacheWritesNothing(t *testing.T) {
	DisableProjectCache()
	t.Cleanup(func() { projectCacheOff.Store(false) })
	root := t.TempDir()
	if err := probeWritable(filepath.Join(root, ".bino", "cache", "datasets")); err == nil {
		t.Fatal("probeWritable succeeded with the project cache off")
	}
	dir, cleanup, err := inlineScratchDir(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if rel, err := filepath.Rel(root, dir); err == nil && !strings.HasPrefix(rel, "..") {
		t.Errorf("inline scratch dir %s is inside the project", dir)
	}
	if _, err := os.Stat(filepath.Join(root, ".bino")); !os.IsNotExist(err) {
		t.Errorf(".bino was created: %v", err)
	}
}
