package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// IntrospectColumns returns the column names for a DataSource or DataSet by name.
// The name can be prefixed with "$" to explicitly request a DataSource lookup.
// This function is designed for IDE/LSP integrations that need schema information.
//
// The function registers all DataSources as views and then queries the schema
// via SELECT * FROM <name> LIMIT 0.
func IntrospectColumns(ctx context.Context, docs []config.Document, name string) ([]string, error) {
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

	return extractColumns(ctx, targetDoc, docs)
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
	defer session.Close()

	// Register all DataSources as views
	_, err = RegisterViews(ctx, session, allDocs)
	if err != nil {
		return nil, fmt.Errorf("register views: %w", err)
	}

	// Build the query based on document type
	var query string
	switch doc.Kind {
	case "DataSource":
		// DataSource is already a view, just select from it
		query = fmt.Sprintf("SELECT * FROM \"%s\"", doc.Name)

	case "DataSet":
		// DataSet has a custom query
		var payload struct {
			Spec struct {
				Query string `json:"query"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("parse dataset spec: %w", err)
		}
		if payload.Spec.Query == "" {
			return nil, fmt.Errorf("dataset missing query")
		}
		// Strip trailing semicolons to avoid syntax errors when wrapping
		query = strings.TrimSuffix(strings.TrimSpace(payload.Spec.Query), ";")

	default:
		return nil, fmt.Errorf("unsupported kind: %s", doc.Kind)
	}

	// Use LIMIT 0 to get schema without data
	schemaQuery := fmt.Sprintf("SELECT * FROM (%s) AS _schema LIMIT 0", query)
	rows, err := session.DB().QueryContext(ctx, schemaQuery)
	if err != nil {
		return nil, fmt.Errorf("query schema: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}

	return columns, nil
}

// BuildDataSourceQuery constructs a DuckDB query for a DataSource document.
// This returns a simple SELECT * FROM "<name>" since DataSources are views.
// This is kept for backward compatibility with dataset execution.
func BuildDataSourceQuery(doc *config.Document, allDocs []config.Document) (string, error) {
	if doc.Kind != "DataSource" {
		return "", fmt.Errorf("expected DataSource, got %s", doc.Kind)
	}
	// Since DataSources are views, just select from the view name
	return fmt.Sprintf("SELECT * FROM \"%s\"", doc.Name), nil
}
