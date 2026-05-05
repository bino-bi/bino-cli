package explorer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/spec"
)

// resolvedDatasetSQL returns the SQL string the dataset would execute against
// the explorer DuckDB session. Mirrors the resolution logic in
// dataset.executor.runJob:
//
//   - spec.source       → SELECT * FROM "<source>"
//   - spec.prql (str)   → returned verbatim (DuckDB's PRQL extension parses it)
//   - spec.prql ($file) → file contents read relative to the doc's directory
//   - spec.query (str)  → returned verbatim
//   - spec.query ($file)→ file contents read relative to the doc's directory
//
// Any @inline(N) placeholders are rewritten to the synthetic datasource view
// names listed in spec.dependencies. Returns an empty string and a non-nil
// error if the dataset has no usable query.
func resolvedDatasetSQL(doc config.Document) (string, error) {
	var payload struct {
		Spec struct {
			Source       string          `json:"source"`
			Query        spec.QueryField `json:"query"`
			Prql         spec.QueryField `json:"prql"`
			Dependencies []string        `json:"dependencies"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		return "", fmt.Errorf("parse dataset spec: %w", err)
	}

	baseDir := filepath.Dir(doc.File)

	var query string
	switch {
	case payload.Spec.Source != "":
		query = fmt.Sprintf("SELECT * FROM %q", payload.Spec.Source)
	case !payload.Spec.Prql.IsEmpty():
		resolved, err := resolveQueryField(payload.Spec.Prql, baseDir)
		if err != nil {
			return "", fmt.Errorf("resolve prql: %w", err)
		}
		query = resolved
	case !payload.Spec.Query.IsEmpty():
		resolved, err := resolveQueryField(payload.Spec.Query, baseDir)
		if err != nil {
			return "", fmt.Errorf("resolve query: %w", err)
		}
		query = resolved
	default:
		return "", fmt.Errorf("dataset has no source, query, or prql")
	}

	if dataset.HasInlineRefs(query) {
		rewritten, err := dataset.RewriteInlineRefs(query, payload.Spec.Dependencies)
		if err != nil {
			return "", fmt.Errorf("rewrite inline refs: %w", err)
		}
		query = rewritten
	}

	return query, nil
}

// resolveQueryField returns the inline query string verbatim, or reads the
// referenced file when the field uses the $file form.
func resolveQueryField(q spec.QueryField, baseDir string) (string, error) {
	if q.Inline != "" {
		return q.Inline, nil
	}
	if q.File == "" {
		return "", nil
	}
	path := q.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	content, err := os.ReadFile(path) //nolint:gosec // G304: path comes from a manifest authored by the developer running preview
	if err != nil {
		return "", fmt.Errorf("read query file %s: %w", q.File, err)
	}
	return string(content), nil
}
