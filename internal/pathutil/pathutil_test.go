package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveWorkdir(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{
			name:    "empty defaults to current directory",
			dir:     "",
			wantErr: false,
		},
		{
			name:    "existing directory",
			dir:     tmpDir,
			wantErr: false,
		},
		{
			name:    "non-existent directory",
			dir:     "/non/existent/directory",
			wantErr: true,
		},
		{
			name:    "file instead of directory",
			dir:     tmpFile,
			wantErr: true,
		},
		{
			name:    "relative path dot",
			dir:     ".",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveWorkdir(tt.dir)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveWorkdir(%q) error = %v, wantErr %v", tt.dir, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Error("ResolveWorkdir() returned empty string, want non-empty")
			}
			if !tt.wantErr && !filepath.IsAbs(got) {
				t.Errorf("ResolveWorkdir() returned non-absolute path: %q", got)
			}
		})
	}
}

func TestResolveOutputDir(t *testing.T) {
	// Use platform-specific absolute path
	var absPath string
	if runtime.GOOS == "windows" {
		absPath = `C:\absolute\output`
	} else {
		absPath = "/absolute/output"
	}

	tests := []struct {
		name    string
		workdir string
		outDir  string
		want    string
	}{
		{
			name:    "empty outDir defaults to dist",
			workdir: "/project",
			outDir:  "",
			want:    filepath.Clean("/project/dist"),
		},
		{
			name:    "relative outDir joins with workdir",
			workdir: "/project",
			outDir:  "build",
			want:    filepath.Clean("/project/build"),
		},
		{
			name:    "nested relative outDir",
			workdir: "/project",
			outDir:  "out/artifacts",
			want:    filepath.Clean("/project/out/artifacts"),
		},
		{
			name:    "absolute outDir returned directly",
			workdir: "/project",
			outDir:  absPath,
			want:    filepath.Clean(absPath),
		},
		{
			name:    "path with dots gets cleaned",
			workdir: "/project",
			outDir:  "build/../dist",
			want:    filepath.Clean("/project/dist"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveOutputDir(tt.workdir, tt.outDir)
			if got != tt.want {
				t.Errorf("ResolveOutputDir(%q, %q) = %q, want %q", tt.workdir, tt.outDir, got, tt.want)
			}
		})
	}
}

func TestResolveGraphPath(t *testing.T) {
	tests := []struct {
		name    string
		pdfPath string
		want    string
	}{
		{
			name:    "standard PDF path",
			pdfPath: "/output/report.pdf",
			want:    "/output/report.pdf.bngraph",
		},
		{
			name:    "empty path returns empty",
			pdfPath: "",
			want:    "",
		},
		{
			name:    "relative PDF path",
			pdfPath: "dist/report.pdf",
			want:    "dist/report.pdf.bngraph",
		},
		{
			name:    "path without extension",
			pdfPath: "/output/report",
			want:    "/output/report.bngraph",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveGraphPath(tt.pdfPath)
			if got != tt.want {
				t.Errorf("ResolveGraphPath(%q) = %q, want %q", tt.pdfPath, got, tt.want)
			}
		})
	}
}

func TestResolveFilePath(t *testing.T) {
	// Use platform-specific absolute path
	var absPath string
	if runtime.GOOS == "windows" {
		absPath = `C:\absolute\file.pdf`
	} else {
		absPath = "/absolute/file.pdf"
	}

	tests := []struct {
		name     string
		baseDir  string
		filename string
		want     string
		wantErr  bool
	}{
		{
			name:     "relative filename",
			baseDir:  "/project/output",
			filename: "report.pdf",
			want:     filepath.Clean("/project/output/report.pdf"),
			wantErr:  false,
		},
		{
			name:     "nested relative filename",
			baseDir:  "/project",
			filename: "output/report.pdf",
			want:     filepath.Clean("/project/output/report.pdf"),
			wantErr:  false,
		},
		{
			name:     "absolute filename ignores baseDir",
			baseDir:  "/project/output",
			filename: absPath,
			want:     filepath.Clean(absPath),
			wantErr:  false,
		},
		{
			name:     "empty filename returns error",
			baseDir:  "/project",
			filename: "",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "path with dots gets cleaned",
			baseDir:  "/project/output",
			filename: "../report.pdf",
			want:     filepath.Clean("/project/report.pdf"),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveFilePath(tt.baseDir, tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveFilePath(%q, %q) error = %v, wantErr %v", tt.baseDir, tt.filename, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ResolveFilePath(%q, %q) = %q, want %q", tt.baseDir, tt.filename, got, tt.want)
			}
		})
	}
}

func TestRelPath(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		target string
		want   string
	}{
		{
			name:   "simple relative path",
			base:   "/project",
			target: "/project/output/report.pdf",
			want:   "output/report.pdf",
		},
		{
			name:   "same directory",
			base:   "/project",
			target: "/project/report.pdf",
			want:   "report.pdf",
		},
		{
			name:   "empty target returns empty",
			base:   "/project",
			target: "",
			want:   "",
		},
		{
			name:   "empty base returns target with slashes",
			base:   "",
			target: "/project/report.pdf",
			want:   "/project/report.pdf",
		},
		{
			name:   "parent directory traversal",
			base:   "/project/src",
			target: "/project/output/report.pdf",
			want:   "../output/report.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelPath(tt.base, tt.target)
			if got != tt.want {
				t.Errorf("RelPath(%q, %q) = %q, want %q", tt.base, tt.target, got, tt.want)
			}
		})
	}
}

func TestResolveInitDir(t *testing.T) {
	tests := []struct {
		name       string
		dir        string
		defaultDir string
		wantErr    bool
	}{
		{
			name:       "empty uses default",
			dir:        "",
			defaultDir: "./rainbow-report",
			wantErr:    false,
		},
		{
			name:       "whitespace uses default",
			dir:        "   ",
			defaultDir: "./rainbow-report",
			wantErr:    false,
		},
		{
			name:       "explicit directory",
			dir:        "./my-report",
			defaultDir: "./rainbow-report",
			wantErr:    false,
		},
		{
			name:       "path with trailing dots",
			dir:        "./report/../output",
			defaultDir: "./rainbow-report",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveInitDir(tt.dir, tt.defaultDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveInitDir(%q, %q) error = %v, wantErr %v", tt.dir, tt.defaultDir, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Error("ResolveInitDir() returned empty string, want non-empty")
			}
			if !tt.wantErr && !filepath.IsAbs(got) {
				t.Errorf("ResolveInitDir() returned non-absolute path: %q", got)
			}
		})
	}
}

func TestCacheDir(t *testing.T) {
	tests := []struct {
		name   string
		subdir string
	}{
		{
			name:   "cdn cache",
			subdir: "cdn",
		},
		{
			name:   "data cache",
			subdir: "data",
		},
		{
			name:   "empty subdir",
			subdir: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CacheDir(tt.subdir)
			if err != nil {
				t.Errorf("CacheDir(%q) unexpected error: %v", tt.subdir, err)
				return
			}
			if got == "" {
				t.Error("CacheDir() returned empty string")
			}
			if !filepath.IsAbs(got) {
				t.Errorf("CacheDir() returned non-absolute path: %q", got)
			}
			// Verify .bn is in the path
			if filepath.Base(filepath.Dir(got)) != ".bn" && tt.subdir != "" {
				t.Errorf("CacheDir() path should contain .bn: %q", got)
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "create new directory",
			path:    filepath.Join(tmpDir, "new-dir"),
			wantErr: false,
		},
		{
			name:    "create nested directory",
			path:    filepath.Join(tmpDir, "a", "b", "c"),
			wantErr: false,
		},
		{
			name:    "existing directory is ok",
			path:    tmpDir,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureDir(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureDir(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				info, statErr := os.Stat(tt.path)
				if statErr != nil {
					t.Errorf("EnsureDir(%q) directory not created: %v", tt.path, statErr)
				} else if !info.IsDir() {
					t.Errorf("EnsureDir(%q) did not create a directory", tt.path)
				}
			}
		})
	}
}

func TestIsURL(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"https://example.com/data.json", true},
		{"http://localhost:8080/api", true},
		{"s3://bucket/key", true},
		{"./local/file.json", false},
		{"/absolute/path/file.csv", false},
		{"relative/path.parquet", false},
		{"", false},
		{"  ", false},
		{"HTTPS://UPPER.COM", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := IsURL(tt.path)
			if got != tt.want {
				t.Errorf("IsURL(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestHasScheme(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"https://example.com", true},
		{"http://localhost", true},
		{"s3://bucket/key", true},
		{"ftp://server/file", false},
		{"./local/file.json", false},
		{"/absolute/path", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := HasScheme(tt.path)
			if got != tt.want {
				t.Errorf("HasScheme(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestHasGlob(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"*.json", true},
		{"data/*.csv", true},
		{"file[0-9].txt", true},
		{"data?.json", true},
		{"plain/path/file.json", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := HasGlob(tt.path)
			if got != tt.want {
				t.Errorf("HasGlob(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		baseDir   string
		candidate string
		wantErr   bool
	}{
		{
			name:      "relative path",
			baseDir:   tmpDir,
			candidate: "subdir/file.json",
			wantErr:   false,
		},
		{
			name:      "absolute path",
			baseDir:   tmpDir,
			candidate: "/absolute/path/file.json",
			wantErr:   false,
		},
		{
			name:      "empty path",
			baseDir:   tmpDir,
			candidate: "",
			wantErr:   true,
		},
		{
			name:      "whitespace only",
			baseDir:   tmpDir,
			candidate: "   ",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.baseDir, tt.candidate)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Error("Resolve() returned empty string, want non-empty")
			}
		})
	}
}

func TestBaseDir(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "file path",
			path: testFile,
			want: tmpDir,
		},
		{
			name: "directory path",
			path: tmpDir,
			want: tmpDir,
		},
		{
			name: "non-existent path",
			path: "/non/existent/file.txt",
			want: "/non/existent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BaseDir(tt.path)
			if got != tt.want {
				t.Errorf("BaseDir(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestDefaultGlobPattern(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"json", "*.json"},
		{"JSON", "*.json"},
		{"csv", "*.csv"},
		{"parquet", "*.parquet"},
		{"excel", "*.xlsx"},
		{"unknown", "*"},
		{"", "*"},
		{"  json  ", "*.json"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := DefaultGlobPattern(tt.format)
			if got != tt.want {
				t.Errorf("DefaultGlobPattern(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestFindProjectRoot(t *testing.T) {
	t.Run("finds bino.toml in current directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, ProjectConfigFile)
		if err := os.WriteFile(configPath, []byte("report-id = \"test-id\"\n"), 0o644); err != nil {
			t.Fatalf("failed to create bino.toml: %v", err)
		}

		got, err := FindProjectRoot(tmpDir)
		if err != nil {
			t.Fatalf("FindProjectRoot(%q) error = %v", tmpDir, err)
		}
		if got != tmpDir {
			t.Errorf("FindProjectRoot(%q) = %q, want %q", tmpDir, got, tmpDir)
		}
	})

	t.Run("finds bino.toml in parent directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, ProjectConfigFile)
		if err := os.WriteFile(configPath, []byte("report-id = \"test-id\"\n"), 0o644); err != nil {
			t.Fatalf("failed to create bino.toml: %v", err)
		}

		subDir := filepath.Join(tmpDir, "sub", "nested")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("failed to create subdirectory: %v", err)
		}

		got, err := FindProjectRoot(subDir)
		if err != nil {
			t.Fatalf("FindProjectRoot(%q) error = %v", subDir, err)
		}
		if got != tmpDir {
			t.Errorf("FindProjectRoot(%q) = %q, want %q", subDir, got, tmpDir)
		}
	})

	t.Run("returns error when bino.toml not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "no-project")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("failed to create subdirectory: %v", err)
		}

		_, err := FindProjectRoot(subDir)
		if err == nil {
			t.Error("FindProjectRoot() should return error when bino.toml not found")
		}
		if err != ErrProjectRootNotFound {
			t.Errorf("FindProjectRoot() error = %v, want ErrProjectRootNotFound", err)
		}
	})

	t.Run("nested bino.toml finds closest one", func(t *testing.T) {
		// Create parent project
		parentDir := t.TempDir()
		parentConfig := filepath.Join(parentDir, ProjectConfigFile)
		if err := os.WriteFile(parentConfig, []byte("report-id = \"parent-id\"\n"), 0o644); err != nil {
			t.Fatalf("failed to create parent bino.toml: %v", err)
		}

		// Create nested project
		nestedDir := filepath.Join(parentDir, "nested-project")
		if err := os.MkdirAll(nestedDir, 0o755); err != nil {
			t.Fatalf("failed to create nested directory: %v", err)
		}
		nestedConfig := filepath.Join(nestedDir, ProjectConfigFile)
		if err := os.WriteFile(nestedConfig, []byte("report-id = \"nested-id\"\n"), 0o644); err != nil {
			t.Fatalf("failed to create nested bino.toml: %v", err)
		}

		// Start from nested project's subdirectory
		subDir := filepath.Join(nestedDir, "src")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("failed to create src directory: %v", err)
		}

		got, err := FindProjectRoot(subDir)
		if err != nil {
			t.Fatalf("FindProjectRoot(%q) error = %v", subDir, err)
		}
		if got != nestedDir {
			t.Errorf("FindProjectRoot(%q) = %q, want %q (closest bino.toml)", subDir, got, nestedDir)
		}
	})

	t.Run("empty startDir defaults to current directory", func(t *testing.T) {
		// This test just verifies no error for empty input
		_, err := FindProjectRoot("")
		// Error is expected since we're likely not in a bino project
		if err != nil && err != ErrProjectRootNotFound {
			t.Errorf("FindProjectRoot(\"\") unexpected error = %v", err)
		}
	})
}

func TestProjectConfigPath(t *testing.T) {
	got := ProjectConfigPath("/some/project")
	want := filepath.Join("/some/project", ProjectConfigFile)
	if got != want {
		t.Errorf("ProjectConfigPath(\"/some/project\") = %q, want %q", got, want)
	}
}
