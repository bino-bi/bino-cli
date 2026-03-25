// Package pathutil provides common path resolution and file content utilities
// shared across the report generation pipeline and CLI commands.
package pathutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectConfigFile is the filename that marks the root of a Bino reporting project.
const ProjectConfigFile = "bino.toml"

// ErrProjectRootNotFound is returned when no bino.toml is found in the directory hierarchy.
var ErrProjectRootNotFound = errors.New("bino.toml not found: not inside a bino project (run 'bino init' to create one)")

// DefaultHTTPTimeout is the default timeout for HTTP requests when loading remote content.
const DefaultHTTPTimeout = 30 * time.Second

// ResolveWorkdir converts a relative or empty directory path to an absolute path
// and validates that it exists and is a directory.
// If dir is empty, it defaults to the current working directory (".").
func ResolveWorkdir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir %q: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat workdir %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %s is not a directory", abs)
	}
	return abs, nil
}

// ResolveOutputDir returns an absolute path for the output directory.
// If outDir is empty, it defaults to "dist".
// If outDir is relative, it's resolved against workdir.
// If outDir is absolute, it's cleaned and returned directly.
func ResolveOutputDir(workdir, outDir string) string {
	if outDir == "" {
		outDir = "dist"
	}
	if filepath.IsAbs(outDir) {
		return filepath.Clean(outDir)
	}
	return filepath.Clean(filepath.Join(workdir, outDir))
}

// ResolveGraphPath returns the graph file path corresponding to a PDF output path.
// It appends ".bngraph" to the PDF path.
// Returns empty string if pdfPath is empty.
func ResolveGraphPath(pdfPath string) string {
	if pdfPath == "" {
		return ""
	}
	return pdfPath + ".bngraph"
}

// ResolveFilePath resolves a filename to an absolute path within a base directory.
// If filename is already absolute, it's cleaned and returned directly.
// If filename is relative, it's joined with baseDir.
// Returns an error if filename is empty.
func ResolveFilePath(baseDir, filename string) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("filename cannot be empty")
	}
	if filepath.IsAbs(filename) {
		return filepath.Clean(filename), nil
	}
	return filepath.Clean(filepath.Join(baseDir, filename)), nil
}

// RelPath returns the relative path from base to target.
// If computing relative path fails, returns target with forward slashes.
// If target is empty, returns empty string.
func RelPath(base, target string) string {
	if target == "" {
		return ""
	}
	if base == "" {
		return filepath.ToSlash(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

// ResolveInitDir resolves the target directory for init command.
// If dir is empty, it defaults to defaultDir.
// Returns the absolute path and an error if resolution fails.
func ResolveInitDir(dir, defaultDir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = defaultDir
	}
	cleaned := filepath.Clean(strings.TrimSpace(dir))
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve directory %s: %w", cleaned, err)
	}
	return abs, nil
}

// FindProjectRoot searches for a bino.toml file starting from the given directory
// and walking up the directory hierarchy (like git finds .git).
// Returns the absolute path to the directory containing bino.toml, or
// ErrProjectRootNotFound if no bino.toml is found before reaching the filesystem root.
func FindProjectRoot(startDir string) (string, error) {
	if startDir == "" {
		startDir = "."
	}
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve start directory %q: %w", startDir, err)
	}

	current := abs
	for {
		configPath := filepath.Join(current, ProjectConfigFile)
		if _, err := os.Stat(configPath); err == nil {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root without finding bino.toml
			return "", ErrProjectRootNotFound
		}
		current = parent
	}
}

// ProjectConfigPath returns the full path to bino.toml within a project root directory.
func ProjectConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ProjectConfigFile)
}

// EnsureDir creates the directory (and all parent directories) if it doesn't exist.
// Uses mode 0o755 for created directories.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// CacheDir returns a cache directory path within the user's home directory.
// The path is constructed as ~/.bino/<subdir>.
func CacheDir(subdir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, ".bino", subdir), nil
}

// IsURL returns true if the given path looks like a remote URL (contains "://").
func IsURL(path string) bool {
	lowered := strings.ToLower(strings.TrimSpace(path))
	return strings.Contains(lowered, "://")
}

// HasScheme returns true if the path starts with a known URL scheme.
func HasScheme(path string) bool {
	lowered := strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(lowered, "http://") ||
		strings.HasPrefix(lowered, "https://") ||
		strings.HasPrefix(lowered, "s3://")
}

// HasGlob returns true if the path contains glob characters (*, ?, [).
func HasGlob(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

// Resolve resolves a candidate path relative to a base directory.
// If the candidate is absolute, it is cleaned and returned directly.
// Otherwise, it is joined with baseDir and converted to an absolute path.
// The returned path uses forward slashes for cross-platform consistency.
func Resolve(baseDir, candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	var abs string
	if filepath.IsAbs(candidate) {
		abs = filepath.Clean(candidate)
	} else {
		abs = filepath.Join(baseDir, candidate)
	}
	resolved, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", candidate, err)
	}
	return filepath.ToSlash(resolved), nil
}

// BaseDir returns the directory portion of a file path.
// For files, this returns the parent directory.
// For directories, this returns the path itself.
func BaseDir(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return filepath.Dir(path)
	}
	if info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

// LoadContent loads content from a local file path or a remote URL.
// For URLs (paths containing "://"), it performs an HTTP GET request.
// For local paths, it reads the file directly.
// The baseDir is used to resolve relative local paths.
func LoadContent(ctx context.Context, baseDir, path string) ([]byte, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}

	if IsURL(trimmed) {
		return loadRemoteContent(ctx, trimmed)
	}

	resolved, err := Resolve(baseDir, trimmed)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

// loadRemoteContent fetches content from a remote URL.
func loadRemoteContent(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{Timeout: DefaultHTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return data, nil
}

// DefaultGlobPattern returns a default file glob pattern based on the format.
// Supported formats: json, csv, parquet, excel.
// Returns "*" for unknown formats.
func DefaultGlobPattern(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return "*.json"
	case "csv":
		return "*.csv"
	case "parquet":
		return "*.parquet"
	case "excel":
		return "*.xlsx"
	default:
		return "*"
	}
}
