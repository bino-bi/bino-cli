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

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/runtimecfg"
)

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
			return nil
		}

		if _, ok := yamlExt[strings.ToLower(filepath.Ext(path))]; !ok {
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

		fileDocs, err := loadFileWithLookup(ctx, path, cfg.MaxManifestDocs, opts.Lenient, lookup)
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

func loadFile(ctx context.Context, path string, maxDocs int, lenient bool) ([]Document, error) {
	return loadFileWithLookup(ctx, path, maxDocs, lenient, EnvLookup())
}

func loadFileWithLookup(ctx context.Context, path string, maxDocs int, lenient bool, lookup LookupFunc) ([]Document, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// Expand variables before YAML parsing using the provided lookup
	expanded, missingVars := ExpandVars(string(content), lookup)

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
				Raw:            rawJSON,
				MissingEnvVars: missingVars,
			})
			continue
		}

		if err := spec.ValidateDocument(rawJSON); err != nil {
			return nil, fmt.Errorf("%s document %d: %w", path, index+1, err)
		}

		var header documentHeader
		if err := json.Unmarshal(rawJSON, &header); err != nil {
			return nil, fmt.Errorf("header %s document %d: %w", path, index+1, err)
		}

		constraints, err := spec.ParseMixedConstraints(header.Metadata.Constraints)
		if err != nil {
			return nil, fmt.Errorf("%s document %d: invalid constraints: %w", path, index+1, err)
		}

		docs = append(docs, Document{
			File:           path,
			Position:       index,
			Kind:           header.Kind,
			Name:           header.Metadata.Name,
			Labels:         header.Metadata.Labels,
			Constraints:    constraints,
			Raw:            rawJSON,
			MissingEnvVars: missingVars,
		})
	}

	return docs, nil
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
