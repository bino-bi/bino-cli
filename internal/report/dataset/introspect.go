package dataset

import (
	"context"
	"fmt"
	"os"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/pkg/duckdb"
)

// IntrospectColumns returns the column names for a DataSource or DataSet by name.
// The name can be prefixed with "$" to explicitly request a DataSource lookup.
// This function is designed for IDE/LSP integrations that need schema information.
//
// The function registers all DataSources as views, compiles a DataSet the same
// way the build does (source / prql / query, $file, @inline, derive/assert) and
// queries the schema with LIMIT 0.
func IntrospectColumns(ctx context.Context, docs []config.Document, name string) ([]string, error) {
	// Duplicate names resolve as the artefacts' constraints select them, so the
	// result never comes from a definition the build would exclude.
	docs, ambiguous := config.ResolveForIntrospection(docs)
	if err := ambiguous.Lookup(docs, name); err != nil {
		return nil, err
	}

	// Find the target document (DataSource or DataSet)
	var targetDoc *config.Document
	isDataSource := strings.HasPrefix(name, "$")
	lookupName := strings.TrimPrefix(name, "$")

	for i := range docs {
		doc := &docs[i]
		if doc.Name != lookupName {
			continue
		}
		if isDataSource && doc.Kind == "DataSource" {
			targetDoc = doc
			break
		}
		if !isDataSource && doc.Kind == "DataSet" {
			targetDoc = doc
			break
		}
		// Also accept DataSource without $ prefix as fallback
		if !isDataSource && doc.Kind == "DataSource" {
			targetDoc = doc
			// Don't break, prefer DataSet if both exist
		}
	}

	if targetDoc == nil {
		return nil, fmt.Errorf("document not found: %s", name)
	}

	cols, err := extractColumns(ctx, targetDoc, docs)
	return cols, ambiguous.Explain(err)
}

// extractColumns runs a query against a datasource/dataset and returns column names.
// It registers all DataSources as views first, then queries the target.
func extractColumns(ctx context.Context, doc *config.Document, allDocs []config.Document) ([]string, error) {
	// Open a DuckDB session
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		return nil, fmt.Errorf("duckdb options: %w", err)
	}

	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("duckdb open: %w", err)
	}
	defer session.Close() //nolint:errcheck // teardown of an ephemeral in-memory session

	// Create temp directory for inline datasource CSV files
	tempDir, err := os.MkdirTemp("", "bino-introspect-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Register all DataSources as views
	_, err = datasource.RegisterViews(ctx, session, allDocs, &datasource.ViewsOptions{
		TempDir: tempDir,
	})
	if err != nil {
		return nil, fmt.Errorf("register views: %w", err)
	}

	switch doc.Kind {
	case "DataSource":
		return schemaColumns(ctx, session, fmt.Sprintf("SELECT * FROM %q LIMIT 0", doc.Name), nil)
	case "DataSet":
		return ProbeColumns(ctx, session, *doc)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", doc.Kind)
	}
}

// ProbeColumns compiles a DataSet the way the build does, runs its setup and
// its query with LIMIT 0 on session, and returns the result columns with the
// constants stamped on. The DataSources must already be registered as views.
func ProbeColumns(ctx context.Context, session *duckdb.Session, doc config.Document) ([]string, error) {
	compiled, err := Compile(doc)
	if err != nil {
		return nil, err
	}
	if compiled.Prql {
		if err := session.LoadPRQL(ctx); err != nil {
			return nil, err
		}
	}
	if err := RunSetup(ctx, session, compiled); err != nil {
		return nil, err
	}
	return schemaColumns(ctx, session, LimitQuery(compiled.Query, 0), compiled.Constants)
}

func schemaColumns(ctx context.Context, session *duckdb.Session, schemaQuery string, constants []Constant) ([]string, error) {
	rows, err := session.DB().QueryContext(ctx, schemaQuery)
	if err != nil {
		return nil, fmt.Errorf("query schema: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}
	columns, _ = StampConstants(nil, columns, constants)

	return columns, nil
}
