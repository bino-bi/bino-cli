package cli

import (
	"path/filepath"
	"testing"
)

func TestPathWithinRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	tests := []struct {
		name string
		file string
		want bool
	}{
		{"file directly in root", filepath.Join(root, "a.yaml"), true},
		{"file in subdir", filepath.Join(root, "sub", "b.yaml"), true},
		{"root itself", root, true},
		{"non-clean path inside root", filepath.Join(root, "sub", "..", "a.yaml"), true},
		{"parent of root", filepath.Dir(root), false},
		{"sibling escaping via ..", filepath.Join(root, "..", "evil.yaml"), false},
		{"unrelated absolute path", filepath.Join(t.TempDir(), "c.yaml"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pathWithinRoot(root, tt.file); got != tt.want {
				t.Errorf("pathWithinRoot(%q, %q) = %v, want %v", root, tt.file, got, tt.want)
			}
		})
	}
}
