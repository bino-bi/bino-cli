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

	// Filter is a declarative row filter compiled to a server-side SQL WHERE
	// clause over the query result. Not supported together with Prql.
	Filter *FilterGroup `yaml:"filter,omitempty" json:"filter,omitempty"`

	// GroupBy is a declarative server-side GROUP BY compiled to SQL.
	// Not supported together with Prql.
	GroupBy *GroupBy `yaml:"groupBy,omitempty" json:"groupBy,omitempty"`

	// IndexColumns lists computed index/order columns projected onto the result.
	// Not supported together with Prql.
	IndexColumns []IndexColumn `yaml:"indexColumns,omitempty" json:"indexColumns,omitempty"`
}

// FilterGroup is a logical group of filter conditions combined with AND or OR.
// Groups nest via FilterNode to express arbitrary boolean trees.
//
// FilterGroup intentionally has no custom (un)marshaler: decoding recurses into
// its FilterNode children, each of which discriminates group-vs-leaf and
// terminates at leaves.
type FilterGroup struct {
	// Op joins the conditions: "and" (default) or "or".
	Op string `yaml:"op,omitempty" json:"op,omitempty"`
	// Conditions are the members of this group (leaf conditions or nested groups).
	Conditions []FilterNode `yaml:"conditions" json:"conditions"`
}

// FilterNode is a union of a nested group or a leaf condition; exactly one is set.
// The (un)marshalers in marshal.go discriminate structurally on the "conditions" key.
type FilterNode struct {
	// Group is set when this node is a nested logical group.
	Group *FilterGroup
	// Leaf is set when this node is a single column condition.
	Leaf *FilterCondition
}

// FilterCondition is a single filter predicate on one column.
type FilterCondition struct {
	// Column to filter on (identifier-quoted in the generated SQL).
	Column string `yaml:"column" json:"column"`
	// Op is the comparison operator.
	Op string `yaml:"op" json:"op"`
	// Value is the comparison operand: scalar, []any, or nil. Bound as parameter(s).
	Value any `yaml:"value,omitempty" json:"value,omitempty"`
}

// GroupBy is a server-side GROUP BY definition; columns and aggregates become the
// only output columns.
type GroupBy struct {
	// Columns to group by; they survive into the output verbatim.
	Columns []string `yaml:"columns" json:"columns"`
	// Aggregates are the per-group aggregate expressions.
	Aggregates []Aggregate `yaml:"aggregates,omitempty" json:"aggregates,omitempty"`
}

// Aggregate is an aggregate expression over one column, output under an alias.
type Aggregate struct {
	// Column to aggregate ("*" with Fn "count" yields count(*)).
	Column string `yaml:"column" json:"column"`
	// Fn is the aggregate function.
	Fn string `yaml:"fn" json:"fn"`
	// As is the output column name (unique across aggregates and group-by columns).
	As string `yaml:"as" json:"as"`
	// OrderBy, for "first"/"last", names the column to order by for determinism.
	OrderBy string `yaml:"orderBy,omitempty" json:"orderBy,omitempty"`
	// OrderDesc, for "first"/"last", orders descending instead of ascending.
	OrderDesc bool `yaml:"orderDesc,omitempty" json:"orderDesc,omitempty"`
}

// IndexColumn is a computed index/order column. Exactly one of Fn or Expr is set.
type IndexColumn struct {
	// Column is the output column name (e.g. categoryIndex).
	Column string `yaml:"column" json:"column"`
	// Fn is a predefined function: hash | rowNumber | rank | denseRank.
	Fn string `yaml:"fn,omitempty" json:"fn,omitempty"`
	// Of, for "hash", names the column to hash.
	Of string `yaml:"of,omitempty" json:"of,omitempty"`
	// Over, for window functions, names the ORDER BY column.
	Over string `yaml:"over,omitempty" json:"over,omitempty"`
	// OverDesc, for window functions, orders descending.
	OverDesc bool `yaml:"overDesc,omitempty" json:"overDesc,omitempty"`
	// Partition, for window functions, lists optional PARTITION BY columns.
	Partition []string `yaml:"partition,omitempty" json:"partition,omitempty"`
	// Expr is a raw SQL expression emitted verbatim. Mutually exclusive with Fn.
	Expr string `yaml:"expr,omitempty" json:"expr,omitempty"`
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
