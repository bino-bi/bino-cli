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
// The Source field allows direct pass-through to a DataSource without a query.
type dataSetSpec struct {
	Query        queryField `json:"query"`
	Prql         queryField `json:"prql"`
	Source       string     `json:"source"`
	Dependencies []string   `json:"dependencies"`
}

// queryField represents a query that can be either an inline string or a file reference.
// It supports both formats:
//   - Inline: "SELECT * FROM table"
//   - File reference: { "$file": "./queries/sales.sql" }
type queryField struct {
	Inline string // Inline query string
	File   string // Path to external file (from $file)
}

// UnmarshalJSON implements custom unmarshaling for queryField.
// It handles both string values and object values with $file key.
func (q *queryField) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		q.Inline = str
		return nil
	}

	// Try to unmarshal as an object with $file
	var obj struct {
		File string `json:"$file"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		q.File = obj.File
		return nil
	}

	return fmt.Errorf("query must be a string or an object with $file key")
}

// IsEmpty returns true if the query field has no value.
func (q queryField) IsEmpty() bool {
	return q.Inline == "" && q.File == ""
}

// HasFile returns true if the query references an external file.
func (q queryField) HasFile() bool {
	return q.File != ""
}

// layoutSpec represents the layout structure containing children.
type layoutSpec struct {
	Children []layoutChild `json:"children"`
}

// layoutChild represents a single child element within a layout.
// It can be either an inline child (with spec) or a reference to a standalone document (with ref).
// When ref is set, the referenced document's spec is used as the base,
// and any spec fields provided here act as overrides.
// When optional is true and the ref is missing, the child is skipped gracefully instead of erroring.
type layoutChild struct {
	Kind     string          `json:"kind"`
	Ref      string          `json:"ref,omitempty"`
	Optional bool            `json:"optional,omitempty"`
	Spec     json.RawMessage `json:"spec,omitempty"`
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
// For Tree components, it also extracts datasets from nodes.
// For Grid components, it also extracts datasets from children.
func extractDatasets(raw json.RawMessage) ([]string, error) {
	var payload struct {
		Dataset  reportspec.DatasetList `json:"dataset"`
		Nodes    []treeNodeSpec         `json:"nodes"`
		Children []gridChildSpec        `json:"children"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("dataset field: %w", err)
	}

	datasets := payload.Dataset.Strings()

	// Also extract datasets from Tree nodes
	for _, node := range payload.Nodes {
		if len(node.Spec) == 0 {
			continue
		}
		var nodePayload struct {
			Dataset reportspec.DatasetList `json:"dataset"`
		}
		if err := json.Unmarshal(node.Spec, &nodePayload); err != nil {
			continue
		}
		datasets = append(datasets, nodePayload.Dataset.Strings()...)
	}

	// Also extract datasets from Grid children
	for _, child := range payload.Children {
		if len(child.Spec) == 0 {
			continue
		}
		var childPayload struct {
			Dataset reportspec.DatasetList `json:"dataset"`
		}
		if err := json.Unmarshal(child.Spec, &childPayload); err != nil {
			continue
		}
		datasets = append(datasets, childPayload.Dataset.Strings()...)
	}

	return datasets, nil
}

// treeNodeSpec represents a node in a Tree for dataset extraction.
type treeNodeSpec struct {
	ID   string          `json:"id"`
	Kind string          `json:"kind"`
	Ref  string          `json:"ref,omitempty"`
	Spec json.RawMessage `json:"spec,omitempty"`
}

// gridChildSpec represents a child in a Grid for graph building and dataset extraction.
type gridChildSpec struct {
	Row      json.RawMessage `json:"row"`
	Column   json.RawMessage `json:"column"`
	Kind     string          `json:"kind"`
	Ref      string          `json:"ref,omitempty"`
	Optional bool            `json:"optional,omitempty"`
	Spec     json.RawMessage `json:"spec,omitempty"`
}

// reportArtefactSpec represents the parsed specification for a ReportArtefact manifest.
type reportArtefactSpec struct {
	LayoutPages []layoutPageRef `json:"layoutPages"`
}

// layoutPageRef represents a layout page reference with optional params.
// Used for graph building to track parameterized page instances.
type layoutPageRef struct {
	Page   string            `json:"page,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling for layoutPageRef.
// It handles both string values (just page name) and object values (page + params).
func (r *layoutPageRef) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		r.Page = str
		return nil
	}

	// Try to unmarshal as an object with page and params
	var obj struct {
		Page   string            `json:"page"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("layoutPages item must be string or {page, params}: %w", err)
	}
	r.Page = obj.Page
	r.Params = obj.Params
	return nil
}

// parseReportArtefactSpec extracts the spec from a ReportArtefact manifest.
func parseReportArtefactSpec(raw json.RawMessage) (reportArtefactSpec, error) {
	var payload struct {
		Spec reportArtefactSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return reportArtefactSpec{}, err
	}
	return payload.Spec, nil
}
