package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DatasetRef represents a dataset binding that can be either:
//   - A string reference to a named DataSet document (e.g., "my_dataset")
//   - An inline DataSet definition with query and dependencies
//
// This union type enables inline DataSet definitions in component specs
// (ChartStructure, Table, etc.) while maintaining backward compatibility
// with string references.
type DatasetRef struct {
	// Ref is set when the reference is a string (named DataSet).
	Ref string

	// Inline is set when the reference is an inline DataSet definition.
	Inline *InlineDataSet
}

// IsRef returns true if this is a string reference to a named DataSet.
func (d DatasetRef) IsRef() bool {
	return d.Ref != ""
}

// IsInline returns true if this is an inline DataSet definition.
func (d DatasetRef) IsInline() bool {
	return d.Inline != nil
}

// UnmarshalJSON implements custom unmarshaling for DatasetRef.
// It handles both string values and object values.
func (d *DatasetRef) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	// Try string first (named reference)
	var ref string
	if err := json.Unmarshal(data, &ref); err == nil {
		d.Ref = ref
		return nil
	}

	// Try inline object
	var inline InlineDataSet
	if err := json.Unmarshal(data, &inline); err != nil {
		return fmt.Errorf("dataset must be a string reference or inline DataSet object: %w", err)
	}
	d.Inline = &inline
	return nil
}

// MarshalJSON implements custom marshaling for DatasetRef.
func (d DatasetRef) MarshalJSON() ([]byte, error) {
	if d.Ref != "" {
		return json.Marshal(d.Ref)
	}
	if d.Inline != nil {
		return json.Marshal(d.Inline)
	}
	return []byte("null"), nil
}

// DataSourceRef represents a datasource dependency that can be either:
//   - A string reference to a named DataSource document (e.g., "my_source")
//   - An inline DataSource definition with type and configuration
//
// This union type enables inline DataSource definitions in DataSet specs
// while maintaining backward compatibility with string references.
type DataSourceRef struct {
	// Ref is set when the reference is a string (named DataSource).
	Ref string

	// Inline is set when the reference is an inline DataSource definition.
	Inline *InlineDataSource
}

// IsRef returns true if this is a string reference to a named DataSource.
func (d DataSourceRef) IsRef() bool {
	return d.Ref != ""
}

// IsInline returns true if this is an inline DataSource definition.
func (d DataSourceRef) IsInline() bool {
	return d.Inline != nil
}

// UnmarshalJSON implements custom unmarshaling for DataSourceRef.
// It handles both string values and object values.
func (d *DataSourceRef) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	// Try string first (named reference)
	var ref string
	if err := json.Unmarshal(data, &ref); err == nil {
		d.Ref = ref
		return nil
	}

	// Try inline object
	var inline InlineDataSource
	if err := json.Unmarshal(data, &inline); err != nil {
		return fmt.Errorf("dependency must be a string reference or inline DataSource object: %w", err)
	}
	d.Inline = &inline
	return nil
}

// MarshalJSON implements custom marshaling for DataSourceRef.
func (d DataSourceRef) MarshalJSON() ([]byte, error) {
	if d.Ref != "" {
		return json.Marshal(d.Ref)
	}
	if d.Inline != nil {
		return json.Marshal(d.Inline)
	}
	return []byte("null"), nil
}

// InlineDataSet represents a DataSet defined inline within a component.
// It contains only the spec fields (no apiVersion, kind, or metadata).
// During parse-time materialization, inline datasets are converted to
// synthetic Document structs with generated hash-based names.
type InlineDataSet struct {
	// Query is an SQL query string or $file reference.
	// Mutually exclusive with Prql and Source.
	Query QueryField `json:"query,omitempty"`

	// Prql is a PRQL query string or $file reference.
	// Mutually exclusive with Query and Source.
	Prql QueryField `json:"prql,omitempty"`

	// Source is a direct pass-through reference to a DataSource.
	// When set, the DataSet becomes an alias without transformation.
	// Mutually exclusive with Query and Prql.
	Source *DataSourceRef `json:"source,omitempty"`

	// Dependencies lists the DataSources referenced by the query.
	// Each dependency can be a string reference or inline DataSource.
	// Inline DataSources are referenced in queries via @inline(N) syntax,
	// where N is the zero-based index in this array.
	Dependencies []DataSourceRef `json:"dependencies,omitempty"`
}

// HasQuery returns true if a query is specified.
func (ds InlineDataSet) HasQuery() bool {
	return !ds.Query.IsEmpty()
}

// HasPrql returns true if a PRQL query is specified.
func (ds InlineDataSet) HasPrql() bool {
	return !ds.Prql.IsEmpty()
}

// HasSource returns true if a direct source pass-through is specified.
func (ds InlineDataSet) HasSource() bool {
	return ds.Source != nil
}

// InlineDataSource represents a DataSource defined inline within a DataSet.
// It contains only the spec fields (no apiVersion, kind, or metadata).
// During parse-time materialization, inline datasources are converted to
// synthetic Document structs with generated hash-based names.
type InlineDataSource struct {
	// Type is the datasource type: inline, csv, excel, parquet, postgres_query, mysql_query.
	Type string `json:"type"`

	// Inline contains embedded data content for type "inline".
	Inline json.RawMessage `json:"inline,omitempty"`

	// Content is an alternative field for embedded data content.
	Content json.RawMessage `json:"content,omitempty"`

	// Path is the file path or URL for file-based sources (csv, excel, parquet).
	// Supports local paths, globs, http(s)://, and s3:// URLs.
	Path string `json:"path,omitempty"`

	// Connection holds SQL database connection parameters for postgres_query/mysql_query.
	Connection json.RawMessage `json:"connection,omitempty"`

	// Query is the SQL query for database sources (postgres_query, mysql_query).
	Query string `json:"query,omitempty"`

	// Sheet specifies the Excel sheet name or index (for type "excel").
	Sheet string `json:"sheet,omitempty"`

	// Columns specifies column definitions for CSV/inline sources.
	Columns json.RawMessage `json:"columns,omitempty"`

	// Ephemeral marks the datasource as non-cacheable.
	// When true, datasets using this source skip caching.
	Ephemeral *bool `json:"ephemeral,omitempty"`
}

// QueryField represents a query that can be either an inline string or a file reference.
// It supports both formats:
//   - Inline: "SELECT * FROM table"
//   - File reference: { "$file": "./queries/sales.sql" }
type QueryField struct {
	// Inline is the query string when specified directly.
	Inline string

	// File is the path to an external file when using $file syntax.
	File string
}

// IsEmpty returns true if the query field has no value.
func (q QueryField) IsEmpty() bool {
	return q.Inline == "" && q.File == ""
}

// HasFile returns true if the query references an external file.
func (q QueryField) HasFile() bool {
	return q.File != ""
}

// UnmarshalJSON implements custom unmarshaling for QueryField.
// It handles both string values and object values with $file key.
func (q *QueryField) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

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
	if err := json.Unmarshal(data, &obj); err == nil && obj.File != "" {
		q.File = obj.File
		return nil
	}

	return fmt.Errorf("query must be a string or an object with $file key")
}

// MarshalJSON implements custom marshaling for QueryField.
func (q QueryField) MarshalJSON() ([]byte, error) {
	if q.File != "" {
		return json.Marshal(map[string]string{"$file": q.File})
	}
	if q.Inline != "" {
		return json.Marshal(q.Inline)
	}
	return []byte("null"), nil
}

// InlineCount returns the number of inline DataSource dependencies.
func (ds InlineDataSet) InlineCount() int {
	count := 0
	for _, dep := range ds.Dependencies {
		if dep.IsInline() {
			count++
		}
	}
	return count
}

// InlineIndices returns the indices of inline dependencies in the Dependencies slice.
func (ds InlineDataSet) InlineIndices() []int {
	var indices []int
	for i, dep := range ds.Dependencies {
		if dep.IsInline() {
			indices = append(indices, i)
		}
	}
	return indices
}
