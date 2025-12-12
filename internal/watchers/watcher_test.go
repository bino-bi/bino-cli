package watchers

import (
	"os"
	"path/filepath"
	"testing"

	gitignore "github.com/sabhiram/go-gitignore"
)

func TestShouldIgnorePathRespectsPatterns(t *testing.T) {
	tmp := t.TempDir()
	watcher := &Watcher{cfg: Config{Root: tmp}}
	watcher.ignore = gitignore.CompileIgnoreLines(
		"data/**",
		"!data/keep.yaml",
		"*.tmp",
	)

	if !watcher.shouldIgnorePath(filepath.Join(tmp, "data", "file.yaml"), false) {
		t.Fatalf("expected data/file.yaml to be ignored")
	}

	if watcher.shouldIgnorePath(filepath.Join(tmp, "data", "keep.yaml"), false) {
		t.Fatalf("expected keep.yaml to be re-included")
	}

	if !watcher.shouldIgnorePath(filepath.Join(tmp, "cache.tmp"), false) {
		t.Fatalf("expected cache.tmp to be ignored via glob")
	}

	if watcher.shouldIgnorePath(filepath.Join(tmp, "notes.yaml"), false) {
		t.Fatalf("expected notes.yaml to be watched")
	}
}

func TestRefreshIgnorePatternsReadsFile(t *testing.T) {
	tmp := t.TempDir()
	watcher := &Watcher{cfg: Config{Root: tmp}}
	ignorePath := filepath.Join(tmp, ignoreFileName)
	if err := os.WriteFile(ignorePath, []byte("reports/**\n"), 0o600); err != nil {
		t.Fatalf("write ignore file: %v", err)
	}

	if err := watcher.refreshIgnorePatterns(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if !watcher.shouldIgnorePath(filepath.Join(tmp, "reports", "draft.yaml"), false) {
		t.Fatalf("expected reports/draft.yaml to be ignored")
	}

	if watcher.shouldIgnorePath(filepath.Join(tmp, "layouts", "page.yaml"), false) {
		t.Fatalf("expected layouts/page.yaml to be watched")
	}
}
