// Package filehash provides file content hashing utilities for cache invalidation.
//
// This package centralizes file hashing logic used by the graph builder for
// dependency tracking and by the dataset executor for cache key computation.
// When datasource files change, dependent datasets are automatically invalidated.
package filehash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
)

// FileDigest captures a file path and its content hash for dependency tracking.
type FileDigest struct {
	Path      string
	Hash      string
	Ephemeral bool
}

// HashFile computes the SHA256 hash of a file and returns it as a hex string.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes computes a SHA256 digest from multiple byte slices.
func HashBytes(chunks ...[]byte) []byte {
	h := sha256.New()
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		h.Write(chunk)
	}
	return h.Sum(nil)
}

// EphemeralHash generates a time-based hash for external resources (URLs, databases).
// These resources cannot be reliably hashed, so each call produces a unique hash.
func EphemeralHash(seed string) string {
	data := fmt.Sprintf("%s:%d", seed, time.Now().UnixNano())
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// DefaultGlobForType returns the default file glob pattern for a given source type.
func DefaultGlobForType(sourceType string) string {
	switch sourceType {
	case "excel":
		return "*.xlsx"
	case "csv":
		return "*.csv"
	case "parquet":
		return "*.parquet"
	default:
		return "*"
	}
}

// ResolveAndHashFiles resolves a path pattern and computes hashes for all matching files.
// Supports local paths, globs, and directories. URLs are marked as ephemeral.
// The glob pattern is re-evaluated on each call to detect new files.
func ResolveAndHashFiles(baseDir, path, sourceType string) ([]FileDigest, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// URLs (http, https, s3) are ephemeral - can't hash remote content reliably
	if pathutil.IsURL(path) {
		return []FileDigest{{Path: path, Hash: EphemeralHash(path), Ephemeral: true}}, nil
	}

	resolved, err := pathutil.Resolve(baseDir, path)
	if err != nil {
		return nil, err
	}

	search := resolved

	// Check if path is a directory or contains glob patterns
	if !pathutil.HasGlob(path) {
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			// Auto-generate glob pattern for directory
			search = filepath.ToSlash(filepath.Join(resolved, DefaultGlobForType(sourceType)))
		} else {
			// Single file - hash it directly
			hash, err := HashFile(resolved)
			if err != nil {
				return nil, err
			}
			return []FileDigest{{Path: resolved, Hash: hash}}, nil
		}
	}

	// Expand glob pattern
	matches, err := filepath.Glob(search)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("path pattern %s matched no files", path)
	}

	// Sort for deterministic ordering
	sort.Strings(matches)

	var digests []FileDigest
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		hash, err := HashFile(p)
		if err != nil {
			return nil, err
		}
		digests = append(digests, FileDigest{Path: filepath.ToSlash(p), Hash: hash})
	}

	if len(digests) == 0 {
		return nil, fmt.Errorf("path pattern %s matched no files", path)
	}

	return digests, nil
}

// dataSourceSpec is a minimal representation of a DataSource manifest spec
// for extracting the type, path, and ephemeral fields.
type dataSourceSpec struct {
	Type      string          `json:"type"`
	Path      string          `json:"path"`
	Ephemeral *bool           `json:"ephemeral,omitempty"`
	Inline    *inlineSpec     `json:"inline,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// inlineSpec captures the inline content from a DataSource manifest.
type inlineSpec struct {
	Content json.RawMessage `json:"content"`
}

// HashDataSourceFiles computes a combined hash of all files referenced by a DataSource.
// For inline sources, returns a hash of the inline content.
// Returns an empty string for database sources (ephemeral).
// For file-based sources (csv, excel, parquet), returns a SHA256 hash of all file hashes.
func HashDataSourceFiles(doc config.Document) (string, error) {
	if doc.Kind != "DataSource" {
		return "", nil
	}

	var payload struct {
		Spec dataSourceSpec `json:"spec"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		return "", fmt.Errorf("parse datasource spec: %w", err)
	}

	spec := payload.Spec
	sourceType := strings.ToLower(strings.TrimSpace(spec.Type))

	// Only file-based sources have hashable files
	switch sourceType {
	case "csv", "excel", "parquet":
		// Continue to hash files
	case "inline":
		// Hash the inline content for cache invalidation
		return hashInlineContent(spec)
	case "postgres_query", "mysql_query", "postgres", "mysql":
		// Database sources are ephemeral - return time-based hash to invalidate cache
		return EphemeralHash(doc.Name + ":" + sourceType), nil
	default:
		return "", nil
	}

	if strings.TrimSpace(spec.Path) == "" {
		return "", nil
	}

	baseDir := filepath.Dir(doc.File)
	digests, err := ResolveAndHashFiles(baseDir, spec.Path, sourceType)
	if err != nil {
		return "", err
	}

	// Check if all digests are ephemeral
	allEphemeral := true
	for _, d := range digests {
		if !d.Ephemeral {
			allEphemeral = false
			break
		}
	}
	if allEphemeral {
		// All sources are ephemeral (URLs) - return time-based hash to invalidate cache
		return EphemeralHash(doc.Name + ":all-ephemeral"), nil
	}

	// Combine all file hashes into a single hash
	h := sha256.New()
	for _, d := range digests {
		h.Write([]byte(d.Hash))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashDataSourceFilesMultiple computes a combined hash for multiple datasources.
// This is useful for computing a cache key that depends on multiple datasources.
// Returns an empty string if no datasources have hashable files.
func HashDataSourceFilesMultiple(docs []config.Document) (string, error) {
	var hashes []string
	for _, doc := range docs {
		hash, err := HashDataSourceFiles(doc)
		if err != nil {
			return "", err
		}
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}

	if len(hashes) == 0 {
		return "", nil
	}

	// Sort for deterministic ordering
	sort.Strings(hashes)

	h := sha256.New()
	for _, hash := range hashes {
		h.Write([]byte(hash))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsEphemeralSource determines whether a DataSource document should be treated as ephemeral.
// Ephemeral sources are refetched on every build because their data may change without
// the manifest changing (e.g., database queries, remote URLs).
//
// The workdir parameter is used to determine if local files are inside the watchable
// directory tree. Files outside workdir are treated as ephemeral since the file watcher
// cannot detect changes to them.
func IsEphemeralSource(doc config.Document, workdir string) bool {
	if doc.Kind != "DataSource" {
		return false
	}

	var payload struct {
		Spec struct {
			Type      string `json:"type"`
			Path      string `json:"path"`
			Ephemeral *bool  `json:"ephemeral,omitempty"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		// Can't parse spec, treat as ephemeral to be safe
		return true
	}

	spec := payload.Spec

	// If explicitly set, use that value
	if spec.Ephemeral != nil {
		return *spec.Ephemeral
	}

	sourceType := strings.ToLower(strings.TrimSpace(spec.Type))

	// Database sources are always ephemeral by default
	switch sourceType {
	case "postgres_query", "mysql_query":
		return true
	case "inline":
		return false
	}

	// For file-based sources, check if path is a URL or outside workdir
	path := strings.TrimSpace(spec.Path)
	if path == "" {
		return false
	}

	// URLs (http, https, s3) are always ephemeral
	if pathutil.IsURL(path) {
		return true
	}

	// Local file: check if it's inside the workdir (watchable)
	// If workdir is empty, treat as ephemeral to be safe
	if workdir == "" {
		return true
	}

	// Resolve the path relative to the datasource's base directory
	baseDir := filepath.Dir(doc.File)
	resolved, err := pathutil.Resolve(baseDir, path)
	if err != nil {
		// Can't resolve path, treat as ephemeral to be safe
		return true
	}

	// Check if resolved path is inside workdir
	rel, err := filepath.Rel(workdir, resolved)
	if err != nil {
		return true
	}

	// If relative path starts with "..", it's outside workdir
	if strings.HasPrefix(rel, "..") {
		return true
	}

	// Local file inside workdir - not ephemeral (file watcher handles it)
	return false
}

// hashInlineContent computes a hash of the inline datasource content.
// This is used for cache invalidation when inline data changes.
func hashInlineContent(spec dataSourceSpec) (string, error) {
	// Get the inline content from either spec.Inline.Content or spec.Content
	var content json.RawMessage
	switch {
	case spec.Inline != nil && len(spec.Inline.Content) > 0:
		content = spec.Inline.Content
	case len(spec.Content) > 0:
		content = spec.Content
	default:
		// No inline content
		return "", nil
	}

	// Hash the raw content bytes
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}
