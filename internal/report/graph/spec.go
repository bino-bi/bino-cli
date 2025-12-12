package graph

import (
	"encoding/json"
	"fmt"
	"strings"

	reportspec "bino.bi/bino/internal/report/spec"
)

// dataSourceSpec represents the parsed specification for a DataSource manifest.
// The type field determines which other fields are required:
//   - inline: requires Content or Inline block
//   - excel, csv, parquet: requires Path (local, http(s)://, s3://)
//   - postgres_query, mysql_query: requires Connection and Query
//
// All types support an optional Filter field for SQL filtering.
type dataSourceSpec struct {
	Type    string          `json:"type"`
	Inline  *inlineSpec     `json:"inline"`
	Content json.RawMessage `json:"content"`

	// Path is used for file-based sources (excel, csv, parquet).
	// Supports local paths, globs, http(s)://, and s3:// URLs.
	Path string `json:"path"`

	// Connection holds all non-credential SQL database connection parameters (postgres_query, mysql_query).
	// This includes host, port, database, user, and an optional reference to a ConnectionSecret.
	// The ConnectionSecret contains only credentials (password), not connection details.
	Connection *sqlConnection `json:"connection"`
	// Query is the SQL query to execute against the remote database.
	Query string `json:"query"`

	// Filter is an optional SQL SELECT statement applied to all source types.
	Filter string `json:"filter"`
}

type inlineSpec struct {
	Content json.RawMessage `json:"content"`
}

// sqlConnection holds all non-credential connection parameters for SQL databases.
// Connection details (host, port, database, user) are defined here.
// Credentials (password) must be provided via a ConnectionSecret referenced by Secret.
// The ConnectionSecret contains only authentication credentials, not connection details.
type sqlConnection struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Schema   string `json:"schema"`
	User     string `json:"user"`
	// Secret references a ConnectionSecret manifest (kind: ConnectionSecret) by name.
	// The secret must have type matching the datasource (postgres or mysql) and
	// contains only credentials (password/passwordFromEnv), not connection details.
	// When set, DuckDB uses the named secret for authentication.
	Secret string `json:"secret"`
}

// dataSetSpec represents the parsed specification for a DataSet manifest.
// A DataSet executes a SQL query against DuckDB, optionally depending on
// one or more DataSource manifests which are materialized as tables.
type dataSetSpec struct {
	Query        string   `json:"query"`
	Dependencies []string `json:"dependencies"`
}

// layoutSpec represents the layout structure containing children.
type layoutSpec struct {
	Children []layoutChild `json:"children"`
}

// layoutChild represents a single child element within a layout.
type layoutChild struct {
	Kind string          `json:"kind"`
	Spec json.RawMessage `json:"spec"`
}

// parseDataSourceSpec extracts the spec from a DataSource manifest.
func parseDataSourceSpec(raw json.RawMessage) (dataSourceSpec, error) {
	var payload struct {
		Spec dataSourceSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return dataSourceSpec{}, err
	}
	payload.Spec.Type = strings.TrimSpace(strings.ToLower(payload.Spec.Type))
	if payload.Spec.Type == "" {
		return dataSourceSpec{}, fmt.Errorf("spec.type is required")
	}
	return payload.Spec, nil
}

// parseDataSetSpec extracts the spec from a DataSet manifest.
func parseDataSetSpec(raw json.RawMessage) (dataSetSpec, error) {
	var payload struct {
		Spec dataSetSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return dataSetSpec{}, err
	}
	return payload.Spec, nil
}

// extractDatasets parses the dataset field from a component manifest.
func extractDatasets(raw json.RawMessage) ([]string, error) {
	var payload struct {
		Dataset reportspec.DatasetList `json:"dataset"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("dataset field: %w", err)
	}
	return payload.Dataset.Strings(), nil
}
