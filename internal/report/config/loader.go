package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/runtimecfg"
)

const bnignoreFile = ".bnignore"

var yamlExt = map[string]struct{}{
	".yaml": {},
	".yml":  {},
}

var ignoredDirNames = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	".idea":        {},
	".vscode":      {},
	".ds_store":    {},
	"node_modules": {},
	"vendor":       {},
	"venv":         {},
	".venv":        {},
	"dist":         {},
	"build":        {},
	"target":       {},
}

// LoadOptions configures how documents are loaded from a directory.
type LoadOptions struct {
	// Lenient skips strict schema validation and continues on errors.
	// Documents that fail to parse are skipped rather than aborting the entire load.
	// This is useful for IDE/LSP integrations that need partial results.
	Lenient bool

	// Lookup is an optional custom variable lookup function for ${VAR} expansion.
	// If nil, os.LookupEnv is used. Use ChainLookup to combine multiple lookups.
	Lookup LookupFunc

	// CollectedDirs, when non-nil, receives the absolute paths of all directories
	// visited during the walk. This allows callers to reuse the directory list
	// (e.g., for file-watcher registration) without a second walk.
	CollectedDirs *[]string

	// KindProvider supplies plugin-registered kinds. Plugin kinds bypass the
	// built-in JSON schema validation (they are validated by the plugin's own schema).
	// May be nil when no plugins are loaded.
	KindProvider KindProvider
}

// LoadDir walks the provided directory, finds YAML manifests, validates them
// against the spec schema, and returns their normalized JSON representation.
func LoadDir(ctx context.Context, dir string) ([]Document, error) {
	return LoadDirWithOptions(ctx, dir, LoadOptions{})
}

// LoadDirWithOptions walks the provided directory with configurable options.
// When opts.Lenient is true, validation errors are skipped and partial results returned.
func LoadDirWithOptions(ctx context.Context, dir string, opts LoadOptions) ([]Document, error) {
	dir = filepath.Clean(dir)
	cfg := runtimecfg.Current()

	// Default to env lookup if no custom lookup provided
	lookup := opts.Lookup
	if lookup == nil {
		lookup = EnvLookup()
	}

	ignore := loadBnignore(dir)

	var (
		docs       []Document
		fileCount  int
		totalBytes int64
	)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if shouldSkipDir(dir, path, d) {
				return filepath.SkipDir
			}
			if shouldIgnorePath(dir, path, true, ignore) {
				return filepath.SkipDir
			}
			if opts.CollectedDirs != nil {
				*opts.CollectedDirs = append(*opts.CollectedDirs, path)
			}
			return nil
		}

		if _, ok := yamlExt[strings.ToLower(filepath.Ext(path))]; !ok {
			return nil
		}

		if shouldIgnorePath(dir, path, false, ignore) {
			return nil
		}

		fileCount++
		if cfg.MaxManifestFiles > 0 && fileCount > cfg.MaxManifestFiles {
			return fmt.Errorf("manifest scan exceeded max files (%d)", cfg.MaxManifestFiles)
		}

		if cfg.MaxManifestBytes > 0 {
			size, sizeErr := entrySize(path, d)
			if sizeErr != nil {
				return sizeErr
			}
			totalBytes += size
			if totalBytes > cfg.MaxManifestBytes {
				return fmt.Errorf("manifest scan exceeded max bytes (%d)", cfg.MaxManifestBytes)
			}
		}

		fileDocs, err := loadFileWithLookup(ctx, path, cfg.MaxManifestDocs, opts.Lenient, lookup, opts.KindProvider)
		if err != nil {
			if opts.Lenient {
				// Skip file on error in lenient mode
				return nil
			}
			return err
		}

		docs = append(docs, fileDocs...)
		return nil
	})

	if walkErr != nil {
		return nil, walkErr
	}

	// Materialize inline definitions before validation.
	// This converts inline DataSet/DataSource definitions into synthetic documents
	// and rewrites references to use generated names.
	docs, err := MaterializeInlineDefinitions(docs)
	if err != nil {
		return nil, fmt.Errorf("materialize inline definitions: %w", err)
	}

	if !opts.Lenient {
		if err := ValidateDocuments(docs); err != nil {
			return nil, err
		}
	}

	return docs, nil
}

func loadFileWithLookup(ctx context.Context, path string, maxDocs int, lenient bool, lookup LookupFunc, kindProvider KindProvider) ([]Document, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// First pass: parse WITHOUT expansion to find LayoutPage param definitions
	// This allows us to preserve param references for later expansion
	paramNames := collectLayoutPageParamNamesFromYAML(string(content))

	// Create lookup that skips param names (preserves them as-is for later expansion)
	paramPreservingLookup := func(name string) (string, bool) {
		if _, isParam := paramNames[name]; isParam {
			// Return a placeholder that will be preserved and expanded later
			return "${" + name + "}", true
		}
		return lookup(name)
	}

	// Expand variables before YAML parsing, but preserve param references
	expanded, missingVars := ExpandVars(string(content), paramPreservingLookup)

	// Filter out param names from missingVars since they're intentionally preserved
	filteredMissingVars := make([]string, 0, len(missingVars))
	for _, v := range missingVars {
		if _, isParam := paramNames[v]; !isParam {
			filteredMissingVars = append(filteredMissingVars, v)
		}
	}

	decoder := yaml.NewDecoder(strings.NewReader(expanded))
	var (
		docs  []Document
		index int
	)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var manifest map[string]any
		decodeErr := decoder.Decode(&manifest)
		if errors.Is(decodeErr, io.EOF) {
			break
		}
		if decodeErr != nil {
			if lenient {
				continue
			}
			return nil, fmt.Errorf("decode %s: %w", path, decodeErr)
		}

		if len(manifest) == 0 {
			continue
		}

		index++
		if maxDocs > 0 && index > maxDocs {
			if lenient {
				break
			}
			return nil, fmt.Errorf("%s contains more than %d manifest documents", path, maxDocs)
		}

		rawJSON, err := json.Marshal(manifest)
		if err != nil {
			if lenient {
				continue
			}
			return nil, fmt.Errorf("marshal %s: %w", path, err)
		}

		// In lenient mode, check for bino apiVersion before validation
		if lenient {
			var header documentHeader
			if err := json.Unmarshal(rawJSON, &header); err != nil {
				continue
			}
			if !strings.HasPrefix(header.APIVersion, "bino.bi/") {
				continue
			}
			constraints, err := spec.ParseMixedConstraints(header.Metadata.Constraints)
			if err != nil {
				continue // Skip documents with invalid constraints in lenient mode
			}
			docs = append(docs, Document{
				File:           path,
				Position:       index,
				Kind:           header.Kind,
				Name:           header.Metadata.Name,
				Labels:         header.Metadata.Labels,
				Constraints:    constraints,
				Params:         header.Metadata.Params,
				Raw:            rawJSON,
				MissingEnvVars: filteredMissingVars,
			})
			continue
		}

		// Check if this is a plugin kind — if so, skip built-in schema validation.
		// Plugin kinds are validated by their own JSON Schema via the schema aggregator.
		if kindProvider != nil {
			var peek struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(rawJSON, &peek) == nil && IsPluginKind(peek.Kind, kindProvider) {
				var header documentHeader
				if err := json.Unmarshal(rawJSON, &header); err != nil {
					return nil, fmt.Errorf("header %s document %d: %w", path, index, err)
				}
				constraints, err := spec.ParseMixedConstraints(header.Metadata.Constraints)
				if err != nil {
					return nil, fmt.Errorf("%s document %d: invalid constraints: %w", path, index, err)
				}
				docs = append(docs, Document{
					File:           path,
					Position:       index,
					Kind:           header.Kind,
					Name:           header.Metadata.Name,
					Labels:         header.Metadata.Labels,
					Constraints:    constraints,
					Params:         header.Metadata.Params,
					Raw:            rawJSON,
					MissingEnvVars: filteredMissingVars,
				})
				continue
			}
		}

		if err := spec.ValidateDocument(rawJSON); err != nil {
			// Enrich SchemaValidationError with file, position, and line info
			var schemaErr *spec.SchemaValidationError
			if errors.As(err, &schemaErr) {
				schemaErr.File = path
				schemaErr.DocPosition = index
				schemaErr.Source = expanded

				// Parse YAML nodes to resolve line/column positions
				nodes, parseErr := spec.ParseYAMLNodes(expanded)
				if parseErr == nil && index-1 >= 0 && index-1 < len(nodes) {
					docNode := nodes[index-1]
					for i := range schemaErr.Errors {
						line, col, ok := spec.ResolvePathPosition(docNode, schemaErr.Errors[i].Field)
						if ok {
							schemaErr.Errors[i].Line = line
							schemaErr.Errors[i].Column = col
						}
					}
				}

				return nil, schemaErr
			}
			return nil, fmt.Errorf("%s document %d: %w", path, index, err)
		}

		var header documentHeader
		if err := json.Unmarshal(rawJSON, &header); err != nil {
			return nil, fmt.Errorf("header %s document %d: %w", path, index, err)
		}

		constraints, err := spec.ParseMixedConstraints(header.Metadata.Constraints)
		if err != nil {
			return nil, fmt.Errorf("%s document %d: invalid constraints: %w", path, index, err)
		}

		docs = append(docs, Document{
			File:           path,
			Position:       index,
			Kind:           header.Kind,
			Name:           header.Metadata.Name,
			Labels:         header.Metadata.Labels,
			Constraints:    constraints,
			Params:         header.Metadata.Params,
			Raw:            rawJSON,
			MissingEnvVars: filteredMissingVars,
		})
	}

	return docs, nil
}

// collectLayoutPageParamNamesFromYAML does a quick parse of YAML content to find
// all parameter names defined in LayoutPage metadata.params sections.
// This is used to preserve param references during variable expansion.
func collectLayoutPageParamNamesFromYAML(content string) map[string]struct{} {
	paramNames := make(map[string]struct{})

	decoder := yaml.NewDecoder(strings.NewReader(content))
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		if doc == nil {
			continue
		}

		// Check if this is a LayoutPage
		kind, _ := doc["kind"].(string)
		if kind != "LayoutPage" {
			continue
		}

		// Extract metadata.params
		metadata, ok := doc["metadata"].(map[string]any)
		if !ok {
			continue
		}
		params, ok := metadata["params"].([]any)
		if !ok {
			continue
		}

		// Collect param names (and _LABEL variants for select type)
		for _, p := range params {
			param, ok := p.(map[string]any)
			if !ok {
				continue
			}
			name, ok := param["name"].(string)
			if ok && name != "" {
				paramNames[name] = struct{}{}
				// For select params, also preserve the _LABEL variant
				paramType, _ := param["type"].(string)
				if paramType == "select" {
					paramNames[name+"_LABEL"] = struct{}{}
				}
			}
		}
	}

	return paramNames
}

func shouldSkipDir(root, current string, entry fs.DirEntry) bool {
	if entry == nil {
		return false
	}
	cleanRoot := filepath.Clean(root)
	cleanCurrent := filepath.Clean(current)
	if cleanRoot == cleanCurrent {
		return false
	}
	name := strings.ToLower(entry.Name())
	_, ignored := ignoredDirNames[name]
	return ignored
}

func entrySize(path string, entry fs.DirEntry) (int64, error) {
	info, err := entry.Info()
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}
	return info.Size(), nil
}

// loadBnignore reads the .bnignore file from the root directory and compiles
// the gitignore-style patterns. Returns nil if the file does not exist.
func loadBnignore(root string) *gitignore.GitIgnore {
	path := filepath.Join(root, bnignoreFile)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil
	}
	ignore, err := gitignore.CompileIgnoreFile(path)
	if err != nil {
		return nil
	}
	return ignore
}

// shouldIgnorePath checks whether a path matches the .bnignore patterns.
func shouldIgnorePath(root, path string, isDir bool, ignore *gitignore.GitIgnore) bool {
	if ignore == nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	rel = filepath.ToSlash(rel)
	if ignore.MatchesPath(rel) {
		return true
	}
	if isDir {
		return ignore.MatchesPath(rel + "/")
	}
	return false
}
