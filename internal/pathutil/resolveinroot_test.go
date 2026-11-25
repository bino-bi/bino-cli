package pathutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "inner")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"a.yaml", "sub/a.yaml", "new/dir/a.yaml", "sub/../a.yaml", filepath.Join(root, "a.yaml"), "inner/a.yaml", "."} {
		if _, err := ResolveInRoot(root, p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"../a.yaml", "sub/../../a.yaml", filepath.Join(outside, "a.yaml"), "/etc/passwd", "escape/a.yaml", "escape/new/a.yaml"} {
		if _, err := ResolveInRoot(root, p); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("%q: got %v, want ErrOutsideRoot", p, err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "missing.yaml"), filepath.Join(root, "dangling.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInRoot(root, "dangling.yaml"); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("dangling symlink: got %v, want ErrOutsideRoot", err)
	}
	if _, err := ResolveInRoot(filepath.Join(root, "not", "yet"), "a.yaml"); err != nil {
		t.Errorf("root that does not exist yet: %v", err)
	}
	if _, err := ResolveInRoot(root, ""); err == nil {
		t.Error("empty path accepted")
	}
}
