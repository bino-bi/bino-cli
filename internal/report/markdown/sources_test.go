package markdown

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSourceFiles(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir := t.TempDir()

	// Create test files
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}

	files := []string{
		filepath.Join(tmpDir, "README.md"),
		filepath.Join(docsDir, "intro.md"),
		filepath.Join(docsDir, "chapter1.md"),
		filepath.Join(docsDir, "chapter2.md"),
		filepath.Join(docsDir, "notes.txt"), // non-md file
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("# Test"), 0644); err != nil {
			t.Fatalf("write file %s: %v", f, err)
		}
	}

	tests := []struct {
		name          string
		sources       []string
		wantCount     int
		wantErr       bool
		errContains   string
	}{
		{
			name:      "single file",
			sources:   []string{"README.md"},
			wantCount: 1,
		},
		{
			name:      "multiple files",
			sources:   []string{"README.md", "docs/intro.md"},
			wantCount: 2,
		},
		{
			name:      "glob pattern",
			sources:   []string{"docs/*.md"},
			wantCount: 3, // intro.md, chapter1.md, chapter2.md
		},
		{
			name:      "mixed explicit and glob",
			sources:   []string{"README.md", "docs/*.md"},
			wantCount: 4,
		},
		{
			name:      "deduplication",
			sources:   []string{"docs/intro.md", "docs/*.md"},
			wantCount: 3, // intro.md should only appear once
		},
		{
			name:      "empty sources",
			sources:   []string{},
			wantCount: 0,
		},
		{
			name:        "non-md file rejected",
			sources:     []string{"docs/notes.txt"},
			wantErr:     true,
			errContains: "not a markdown file",
		},
		{
			name:        "missing file",
			sources:     []string{"nonexistent.md"},
			wantErr:     true,
			errContains: "does not exist",
		},
		{
			name:        "empty glob match",
			sources:     []string{"empty/*.md"},
			wantErr:     true,
			errContains: "no files match pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveSourceFiles(tmpDir, tt.sources)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.wantCount {
				t.Errorf("got %d files, want %d", len(result), tt.wantCount)
			}

			// Verify all results are absolute paths to .md files
			for _, path := range result {
				if !filepath.IsAbs(path) {
					t.Errorf("path %q is not absolute", path)
				}
				if filepath.Ext(path) != ".md" {
					t.Errorf("path %q is not a .md file", path)
				}
			}

			// Verify results are sorted
			for i := 1; i < len(result); i++ {
				if result[i] < result[i-1] {
					t.Errorf("results not sorted: %q comes after %q", result[i], result[i-1])
				}
			}
		})
	}
}

func TestIsGlobPattern(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"*.md", true},
		{"docs/*.md", true},
		{"**/*.md", true},
		{"[a-z].md", true},
		{"file?.md", true},
		{"README.md", false},
		{"docs/intro.md", false},
		{"/absolute/path/to/file.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isGlobPattern(tt.path)
			if got != tt.want {
				t.Errorf("isGlobPattern(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsMarkdownFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"README.md", true},
		{"docs/intro.MD", true},
		{"notes.txt", false},
		{"file.mdx", false},
		{"file", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isMarkdownFile(tt.path)
			if got != tt.want {
				t.Errorf("isMarkdownFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
