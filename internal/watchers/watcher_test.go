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

func TestShouldIgnorePathAllowsRegistry(t *testing.T) {
	tmp := t.TempDir()
	watcher := &Watcher{cfg: Config{Root: tmp}}

	cases := []struct {
		rel    string
		isDir  bool
		ignore bool
	}{
		{rel: ".bino", isDir: true, ignore: false},
		{rel: ".bino/registry", isDir: true, ignore: false},
		{rel: ".bino/registry/acme", isDir: true, ignore: false},
		{rel: ".bino/registry/acme/table.yml", isDir: false, ignore: false},
		{rel: ".bino/cache", isDir: true, ignore: true},
		{rel: ".bino/cache/data.yaml", isDir: false, ignore: true},
		{rel: ".bino/plugins/bino-plugin-x", isDir: true, ignore: true},
		{rel: ".bino/daemon.port", isDir: false, ignore: true},
	}
	for _, tc := range cases {
		got := watcher.shouldIgnorePath(filepath.Join(tmp, filepath.FromSlash(tc.rel)), tc.isDir)
		if got != tc.ignore {
			t.Errorf("shouldIgnorePath(%q) = %v, want %v", tc.rel, got, tc.ignore)
		}
	}
}

func TestShouldIgnorePathRegistryBypassesBnignore(t *testing.T) {
	// Installed packages are lock-managed content: a .bnignore ignoring
	// `.bino/` (the recommended .gitignore entry, commonly mirrored) or a
	// specific scope must not suppress refreshes for dependencies — this
	// mirrors the loader's registry second pass.
	tmp := t.TempDir()
	watcher := &Watcher{cfg: Config{Root: tmp}}
	watcher.ignore = gitignore.CompileIgnoreLines(".bino/", ".bino/registry/acme/")

	for _, rel := range []string{".bino", ".bino/registry", ".bino/registry/acme", ".bino/registry/acme/table.yml"} {
		if watcher.shouldIgnorePath(filepath.Join(tmp, filepath.FromSlash(rel)), rel != ".bino/registry/acme/table.yml") {
			t.Errorf("registry path %q must stay watched despite .bnignore", rel)
		}
	}
}
