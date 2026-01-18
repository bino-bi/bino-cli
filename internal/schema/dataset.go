package schema

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

// HasInline returns true if the query is specified inline.
func (q QueryField) HasInline() bool {
	return q.Inline != ""
}

// DataSetSpec represents the spec section of a DataSet manifest.
// A DataSet transforms data from DataSources or other DataSets using SQL or PRQL queries.
type DataSetSpec struct {
	// Query is an SQL query string or $file reference.
	// Mutually exclusive with Prql and Source.
	Query *QueryField `yaml:"query,omitempty" json:"query,omitempty"`

	// Prql is a PRQL query string or $file reference.
	// Mutually exclusive with Query and Source.
	Prql *QueryField `yaml:"prql,omitempty" json:"prql,omitempty"`

	// Source is a direct pass-through reference to a DataSource.
	// When set, the DataSet becomes an alias without transformation.
	// Mutually exclusive with Query and Prql.
	Source *DataSourceRef `yaml:"source,omitempty" json:"source,omitempty"`

	// Dependencies lists the DataSources referenced by the query.
	// Each dependency can be a string reference or inline DataSource.
	// Inline DataSources are referenced in queries via @inline(N) syntax,
	// where N is the zero-based index in this array.
	Dependencies []DataSourceRef `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// HasQuery returns true if a query is specified.
func (ds DataSetSpec) HasQuery() bool {
	return ds.Query != nil && !ds.Query.IsEmpty()
}

// HasPrql returns true if a PRQL query is specified.
func (ds DataSetSpec) HasPrql() bool {
	return ds.Prql != nil && !ds.Prql.IsEmpty()
}

// HasSource returns true if a direct source pass-through is specified.
func (ds DataSetSpec) HasSource() bool {
	return ds.Source != nil && !ds.Source.IsEmpty()
}

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
	Inline *DataSetSpec
}

// IsRef returns true if this is a string reference to a named DataSet.
func (d DatasetRef) IsRef() bool {
	return d.Ref != ""
}

// IsInline returns true if this is an inline DataSet definition.
func (d DatasetRef) IsInline() bool {
	return d.Inline != nil
}

// IsEmpty returns true if the reference has no value.
func (d DatasetRef) IsEmpty() bool {
	return d.Ref == "" && d.Inline == nil
}
