package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ResolveSourceFiles expands a list of source paths (which may include glob patterns)
// into a deduplicated, sorted list of absolute file paths. Only .md files are returned.
//
// The function:
// - Expands glob patterns (e.g., "*.md", "**/*.md", "docs/*.md")
// - Resolves paths relative to baseDir
// - Filters to only include .md files
// - Deduplicates overlapping patterns
// - Sorts alphabetically for deterministic ordering
func ResolveSourceFiles(baseDir string, sources []string) ([]string, error) {
	if len(sources) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{})
	var result []string

	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}

		// Resolve relative to baseDir
		pattern := source
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(baseDir, pattern)
		}

		// Check if it's a glob pattern
		if isGlobPattern(pattern) {
			matches, err := doublestar.FilepathGlob(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid glob pattern %q: %w", source, err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("no files match pattern %q", source)
			}
			for _, match := range matches {
				if !isMarkdownFile(match) {
					continue
				}
				absPath, err := filepath.Abs(match)
				if err != nil {
					return nil, fmt.Errorf("resolve path %q: %w", match, err)
				}
				if _, ok := seen[absPath]; !ok {
					seen[absPath] = struct{}{}
					result = append(result, absPath)
				}
			}
		} else {
			// Direct file path
			absPath, err := filepath.Abs(pattern)
			if err != nil {
				return nil, fmt.Errorf("resolve path %q: %w", source, err)
			}
			if !isMarkdownFile(absPath) {
				return nil, fmt.Errorf("source %q is not a markdown file (must have .md extension)", source)
			}
			// Check if the file exists
			if _, err := os.Stat(absPath); err != nil {
				if os.IsNotExist(err) {
					return nil, fmt.Errorf("source file %q does not exist", source)
				}
				return nil, fmt.Errorf("check source file %q: %w", source, err)
			}
			if _, ok := seen[absPath]; !ok {
				seen[absPath] = struct{}{}
				result = append(result, absPath)
			}
		}
	}

	// Sort alphabetically for deterministic ordering
	slices.Sort(result)

	return result, nil
}

// isGlobPattern checks if a path contains glob metacharacters.
func isGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

// isMarkdownFile checks if a file has a .md extension.
func isMarkdownFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md"
}
