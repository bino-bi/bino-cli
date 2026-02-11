package schema

// DataSourceType is the type of data source.
type DataSourceType string

// DataSourceType constants for all supported data source types.
const (
	DataSourceTypeCSV           DataSourceType = "csv"
	DataSourceTypeParquet       DataSourceType = "parquet"
	DataSourceTypeExcel         DataSourceType = "excel"
	DataSourceTypeJSON          DataSourceType = "json"
	DataSourceTypeInline        DataSourceType = "inline"
	DataSourceTypePostgresQuery DataSourceType = "postgres_query"
	DataSourceTypeMySQLQuery    DataSourceType = "mysql_query"
)

// String returns the string representation of the DataSourceType.
func (t DataSourceType) String() string {
	return string(t)
}

// IsFileType returns true if this is a file-based data source type.
func (t DataSourceType) IsFileType() bool {
	switch t {
	case DataSourceTypeCSV, DataSourceTypeParquet, DataSourceTypeExcel, DataSourceTypeJSON:
		return true
	default:
		return false
	}
}

// IsDatabaseType returns true if this is a database data source type.
func (t DataSourceType) IsDatabaseType() bool {
	switch t {
	case DataSourceTypePostgresQuery, DataSourceTypeMySQLQuery:
		return true
	default:
		return false
	}
}

// DataSourceSpec represents the spec section of a DataSource manifest.
type DataSourceSpec struct {
	// Type is the datasource type (required).
	Type DataSourceType `yaml:"type" json:"type"`

	// Path is the file path for file-based sources (csv, parquet, excel, json).
	// Supports local paths, globs, http(s)://, and s3:// URLs.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Connection holds database connection parameters for postgres_query/mysql_query.
	Connection *ConnectionSpec `yaml:"connection,omitempty" json:"connection,omitempty"`

	// Query is the SQL query for database sources (postgres_query, mysql_query).
	Query string `yaml:"query,omitempty" json:"query,omitempty"`

	// Sheet specifies the Excel sheet name or index (for type "excel").
	Sheet string `yaml:"sheet,omitempty" json:"sheet,omitempty"`

	// Delimiter is the field delimiter character for CSV files.
	// Defaults to comma if not specified.
	Delimiter string `yaml:"delimiter,omitempty" json:"delimiter,omitempty"`

	// Header indicates whether the CSV file has a header row.
	// Defaults to true if not specified.
	Header *bool `yaml:"header,omitempty" json:"header,omitempty"`

	// SkipRows is the number of rows to skip at the beginning of the file.
	SkipRows int `yaml:"skipRows,omitempty" json:"skipRows,omitempty"`

	// Inline contains embedded data content for type "inline".
	Inline any `yaml:"inline,omitempty" json:"inline,omitempty"`

	// Content is an alternative field for embedded data content.
	Content any `yaml:"content,omitempty" json:"content,omitempty"`

	// Thousands is the thousands separator in numeric values (e.g., '.' for European format).
	Thousands string `yaml:"thousands,omitempty" json:"thousands,omitempty"`

	// DecimalSeparator is the decimal point character in numeric values (e.g., ',' for European format).
	DecimalSeparator string `yaml:"decimalSeparator,omitempty" json:"decimalSeparator,omitempty"`

	// ColumnNames provides explicit column names for CSV files.
	// Mutually exclusive with Columns.
	ColumnNames []string `yaml:"columnNames,omitempty" json:"columnNames,omitempty"`

	// DateFormat is a DuckDB strftime format string for parsing date values (e.g., '%d/%m/%Y').
	DateFormat string `yaml:"dateFormat,omitempty" json:"dateFormat,omitempty"`

	// Columns specifies a map of column name to DuckDB type for CSV files (e.g., "amount": "DECIMAL(10,2)").
	// Mutually exclusive with ColumnNames.
	Columns map[string]string `yaml:"columns,omitempty" json:"columns,omitempty"`

	// Ephemeral marks the datasource as non-cacheable.
	// When true, datasets using this source skip caching.
	Ephemeral *bool `yaml:"ephemeral,omitempty" json:"ephemeral,omitempty"`
}

// ConnectionSpec represents database connection configuration.
type ConnectionSpec struct {
	// Host is the database server hostname or IP address.
	Host string `yaml:"host,omitempty" json:"host,omitempty"`

	// Port is the database server port number.
	Port int `yaml:"port,omitempty" json:"port,omitempty"`

	// Database is the name of the database to connect to.
	Database string `yaml:"database,omitempty" json:"database,omitempty"`

	// Schema is the database schema (primarily for PostgreSQL).
	Schema string `yaml:"schema,omitempty" json:"schema,omitempty"`

	// User is the username for database authentication.
	User string `yaml:"user,omitempty" json:"user,omitempty"`

	// Secret is the name of a ConnectionSecret manifest containing credentials.
	Secret string `yaml:"secret,omitempty" json:"secret,omitempty"`
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
	Inline *DataSourceSpec
}

// IsRef returns true if this is a string reference to a named DataSource.
func (d DataSourceRef) IsRef() bool {
	return d.Ref != ""
}

// IsInline returns true if this is an inline DataSource definition.
func (d DataSourceRef) IsInline() bool {
	return d.Inline != nil
}

// IsEmpty returns true if the reference has no value.
func (d DataSourceRef) IsEmpty() bool {
	return d.Ref == "" && d.Inline == nil
}
