package markdown

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderFiles(t *testing.T) {
	// Create temp directory with test markdown files
	tmpDir := t.TempDir()

	// Create test markdown files
	md1 := `# Introduction

This is the introduction.

## Features

- Feature 1
- Feature 2
`
	md2 := `# Getting Started

Follow these steps:

1. Install the package
2. Configure settings
3. Run the application

` + "```go\nfmt.Println(\"Hello\")\n```\n"

	if err := os.WriteFile(filepath.Join(tmpDir, "intro.md"), []byte(md1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "getting-started.md"), []byte(md2), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		files    []string
		opts     RenderOptions
		wantErr  bool
		contains []string
	}{
		{
			name:  "single file",
			files: []string{"intro.md"},
			opts: RenderOptions{
				BaseDir: tmpDir,
			},
			contains: []string{
				"<h1",
				"Introduction",
				"<li>Feature 1</li>",
			},
		},
		{
			name:  "multiple files with page breaks",
			files: []string{"intro.md", "getting-started.md"},
			opts: RenderOptions{
				BaseDir:               tmpDir,
				PageBreakBetweenFiles: true,
			},
			contains: []string{
				"Introduction",
				"Getting Started",
				"bn-page-break",
			},
		},
		{
			name:  "with table of contents",
			files: []string{"intro.md", "getting-started.md"},
			opts: RenderOptions{
				BaseDir:         tmpDir,
				TableOfContents: true,
			},
			contains: []string{
				"bn-toc",
			},
		},
		{
			name:    "missing file",
			files:   []string{"nonexistent.md"},
			opts:    RenderOptions{BaseDir: tmpDir},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderFiles(context.Background(), tt.files, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			html := string(result)
			for _, want := range tt.contains {
				if !strings.Contains(html, want) {
					t.Errorf("result should contain %q", want)
				}
			}
		})
	}
}

func TestWrapDocument(t *testing.T) {
	content := []byte("<h1>Test</h1><p>Content</p>")

	tests := []struct {
		name     string
		opts     DocumentOptions
		contains []string
	}{
		{
			name: "basic document",
			opts: DocumentOptions{
				Title:       "Test Document",
				Format:      "a4",
				Orientation: "portrait",
			},
			contains: []string{
				"<!DOCTYPE html>",
				"<title>Test Document</title>",
				"<h1>Test</h1>",
				"size: A4",
				"<bn-context>",
				"</bn-context>",
			},
		},
		{
			name: "landscape orientation",
			opts: DocumentOptions{
				Title:       "Landscape Doc",
				Format:      "letter",
				Orientation: "landscape",
			},
			contains: []string{
				"size: letter landscape",
			},
		},
		{
			name: "with custom CSS",
			opts: DocumentOptions{
				Title:      "Styled Doc",
				Format:     "a4",
				Stylesheet: "body { color: red; }",
			},
			contains: []string{
				"body { color: red; }",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WrapDocument(content, tt.opts)
			html := string(result)

			for _, want := range tt.contains {
				if !strings.Contains(html, want) {
					t.Errorf("result should contain %q", want)
				}
			}
		})
	}
}

func TestLoadStylesheet(t *testing.T) {
	tmpDir := t.TempDir()

	cssContent := "body { margin: 0; }"
	cssFile := filepath.Join(tmpDir, "style.css")
	if err := os.WriteFile(cssFile, []byte(cssContent), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		baseDir string
		path    string
		want    string
		wantErr bool
	}{
		{
			name:    "load existing file",
			baseDir: tmpDir,
			path:    "style.css",
			want:    cssContent,
		},
		{
			name:    "empty path",
			baseDir: tmpDir,
			path:    "",
			want:    "",
		},
		{
			name:    "missing file",
			baseDir: tmpDir,
			path:    "nonexistent.css",
			wantErr: true,
		},
		{
			name:    "absolute path",
			baseDir: "",
			path:    cssFile,
			want:    cssContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LoadStylesheet(tt.baseDir, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestGetPageSize(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"a4", "A4"},
		{"a5", "A5"},
		{"letter", "letter"},
		{"legal", "legal"},
		{"unknown", "A4"}, // default
		{"", "A4"},        // empty defaults to A4
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := getPageSize(tt.format)
			if got != tt.want {
				t.Errorf("getPageSize(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}
