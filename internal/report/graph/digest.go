package graph

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/filehash"
)

// hashDataSource computes the digest for a DataSource manifest.
// For SQL datasources (postgres_query, mysql_query), the query is hashed
// rather than stored directly in attributes for security.
// The ephemeral attribute is set to "true" for sources that will be refetched
// on every build (databases, URLs, files outside workdir).
func (b *builder) hashDataSource(doc config.Document, spec dataSourceSpec) ([]byte, map[string]string, error) {
	attrs := map[string]string{
		"type": spec.Type,
	}
	var digests []filehash.FileDigest
	var hasEphemeral bool
	baseDir := filepath.Dir(doc.File)
	switch spec.Type {
	case "excel", "csv", "parquet":
		if spec.Path != "" {
			patternDigests, err := filehash.ResolveAndHashFiles(baseDir, spec.Path, spec.Type)
			if err != nil {
				return nil, nil, err
			}
			digests = append(digests, patternDigests...)
			// Check if any file is ephemeral (URL)
			for _, d := range patternDigests {
				if d.Ephemeral {
					hasEphemeral = true
					break
				}
			}
		}
	case "postgres_query", "mysql_query":
		// Include connection info and a hash of the query in the digest.
		// The query itself is not stored in attributes for security.
		path := formatSQLDisplay(spec.Type, spec.Connection)
		queryHash := hashQueryString(spec.Query)
		digests = append(digests, filehash.FileDigest{
			Path:      path,
			Hash:      filehash.EphemeralHash(doc.Name + ":" + path + ":" + queryHash),
			Ephemeral: true,
		})
		hasEphemeral = true
	}

	// Set ephemeral attribute for visibility in .bngraph files
	if hasEphemeral {
		attrs["ephemeral"] = "true"
	}

	if src := formatSources(digests); src != "" {
		attrs["sources"] = src
	}
	digest := sha256.New()
	digest.Write(doc.Raw)
	for _, d := range digests {
		digest.Write([]byte(d.Hash))
	}
	return digest.Sum(nil), attrs, nil
}

// hashDataSet computes the digest for a DataSet manifest.
// Dependencies are resolved to DataSource nodes; missing ones are tracked
// in attributes for warning output but do not cause errors.
func (b *builder) hashDataSet(doc config.Document, spec dataSetSpec) ([]byte, map[string]string, []string, error) {
	attrs := make(map[string]string)
	var depIDs []string
	var missing []string

	for _, depName := range spec.Dependencies {
		if target, ok := b.dataSourceIndex[depName]; ok {
			depIDs = append(depIDs, target)
		} else {
			missing = append(missing, depName)
		}
	}

	if len(spec.Dependencies) > 0 {
		attrs["dependencies"] = strings.Join(spec.Dependencies, ",")
	}
	if len(missing) > 0 {
		attrs["dependenciesMissing"] = strings.Join(missing, ",")
	}

	digest := sha256.New()
	digest.Write(doc.Raw)
	return digest.Sum(nil), attrs, depIDs, nil
}

// digestPath computes the digest for a single file path.
func (b *builder) digestPath(baseDir, candidate string) (filehash.FileDigest, error) {
	digests, err := filehash.ResolveAndHashFiles(baseDir, candidate, "")
	if err != nil {
		return filehash.FileDigest{}, err
	}
	if len(digests) == 0 {
		return filehash.FileDigest{}, fmt.Errorf("path %s matched no files", candidate)
	}
	if len(digests) > 1 {
		return filehash.FileDigest{}, fmt.Errorf("path %s matched multiple files, expected single file", candidate)
	}
	return digests[0], nil
}

// formatSQLDisplay returns a human-readable SQL connection string.
// For postgres_query and mysql_query, displays connection info without the query.
func formatSQLDisplay(kind string, conn *sqlConnection) string {
	if conn == nil {
		return kind
	}
	host := strings.TrimSpace(conn.Host)
	if host == "" {
		host = "localhost"
	}
	db := strings.TrimSpace(conn.Database)
	if db == "" {
		db = "default"
	}
	// Strip the _query suffix for display purposes
	displayKind := strings.TrimSuffix(kind, "_query")
	return fmt.Sprintf("%s://%s/%s", displayKind, host, db)
}

// hashQueryString returns a short, stable hash of the query for caching.
// This avoids exposing the full query in graph attributes.
func hashQueryString(query string) string {
	h := sha256.Sum256([]byte(query))
	return fmt.Sprintf("%x", h[:8])
}

// formatSources formats a list of source digests for display.
func formatSources(digests []filehash.FileDigest) string {
	if len(digests) == 0 {
		return ""
	}
	parts := make([]string, 0, len(digests))
	for _, d := range digests {
		path := d.Path
		if path == "" {
			continue
		}
		if d.Ephemeral {
			parts = append(parts, fmt.Sprintf("%s (ephemeral)", path))
		} else {
			parts = append(parts, path)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// hashBytes computes a SHA256 digest from multiple byte slices.
// This is a thin wrapper around filehash.HashBytes for internal use.
func hashBytes(chunks ...[]byte) []byte {
	return filehash.HashBytes(chunks...)
}
