package datasource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"bino.bi/bino/internal/pathutil"
)

// Supported DataSource types.
const (
	sourceTypeInline        = "inline"
	sourceTypeExcel         = "excel"
	sourceTypeCSV           = "csv"
	sourceTypeParquet       = "parquet"
	sourceTypePostgresQuery = "postgres_query"
	sourceTypeMySQLQuery    = "mysql_query"
)

// sourceSpec represents the parsed specification for a DataSource manifest.
// The type field determines which other fields are required:
//   - inline: requires Content or Inline block
//   - excel, csv, parquet: requires Path (local, http(s)://, s3://)
//   - postgres_query, mysql_query: requires Connection and Query
//
// Each DataSource is registered as a DuckDB view named by metadata.name.
type sourceSpec struct {
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
	// The query result is used to populate the DuckDB view.
	Query string `json:"query"`

	// CSV reader options
	Delimiter        string            `json:"delimiter,omitempty"`
	Header           *bool             `json:"header,omitempty"`
	SkipRows         int               `json:"skipRows,omitempty"`
	Thousands        string            `json:"thousands,omitempty"`
	DecimalSeparator string            `json:"decimalSeparator,omitempty"`
	ColumnNames      []string          `json:"columnNames,omitempty"`
	DateFormat       string            `json:"dateFormat,omitempty"`
	Columns          map[string]string `json:"columns,omitempty"`

	// Ephemeral controls whether the datasource should be refetched on every build.
	// If nil (not set), ephemeral status is auto-detected based on source type:
	//   - postgres_query, mysql_query: always ephemeral (data may change)
	//   - http/https/s3 URLs: always ephemeral (remote content may change)
	//   - Local files inside workdir: not ephemeral (file watcher detects changes)
	//   - Local files outside workdir: ephemeral (can't detect changes)
	// Set explicitly to false to cache remote/database sources.
	Ephemeral *bool `json:"ephemeral,omitempty"`

	// Internal fields set by the loader.
	Name    string `json:"-"`
	BaseDir string `json:"-"`
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

func parseSpec(raw json.RawMessage) (sourceSpec, error) {
	var payload struct {
		Spec sourceSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return sourceSpec{}, fmt.Errorf("decode spec: %w", err)
	}
	payload.Spec.Type = strings.ToLower(strings.TrimSpace(payload.Spec.Type))
	if payload.Spec.Type == "" {
		return sourceSpec{}, fmt.Errorf("spec.type is required")
	}
	return payload.Spec, nil
}

func (s sourceSpec) inlinePayload() (json.RawMessage, error) {
	content, err := s.inlineContent()
	if err != nil {
		return nil, err
	}
	canonical, err := normalizeInlineArray(content)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func (s sourceSpec) inlineContent() (json.RawMessage, error) {
	switch {
	case s.Inline != nil && len(s.Inline.Content) > 0:
		return s.Inline.Content, nil
	case len(s.Content) > 0:
		return s.Content, nil
	default:
		return nil, fmt.Errorf("inline content is missing")
	}
}

func normalizeInlineArray(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("inline content cannot be empty")
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, fmt.Errorf("decode inline JSON string: %w", err)
		}
		trimmed = bytes.TrimSpace([]byte(text))
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("inline JSON string cannot be empty")
		}
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(trimmed, &arr); err != nil {
		return nil, fmt.Errorf("inline content must be a JSON array: %w", err)
	}
	data, err := json.Marshal(arr)
	if err != nil {
		return nil, fmt.Errorf("encode inline rows: %w", err)
	}
	return json.RawMessage(data), nil
}

// IsEphemeral determines whether this datasource should be treated as ephemeral
// (refetched on every build). The workdir parameter is used to determine if
// local files are inside the watchable directory tree.
//
// Auto-detection rules when Ephemeral is not explicitly set:
//   - postgres_query, mysql_query: always ephemeral (database data may change)
//   - http/https/s3 URLs: always ephemeral (remote content may change)
//   - Local files inside workdir: not ephemeral (file watcher detects changes)
//   - Local files outside workdir: ephemeral (changes cannot be detected)
//   - inline: not ephemeral (content is embedded in the manifest)
func (s sourceSpec) IsEphemeral(workdir string) bool {
	// If explicitly set, use that value
	if s.Ephemeral != nil {
		return *s.Ephemeral
	}

	sourceType := strings.ToLower(strings.TrimSpace(s.Type))

	// Database sources are always ephemeral by default
	switch sourceType {
	case "postgres_query", "mysql_query":
		return true
	case "inline":
		return false
	}

	// For file-based sources, check if path is a URL or outside workdir
	path := strings.TrimSpace(s.Path)
	if path == "" {
		return false
	}

	// URLs (http, https, s3) are always ephemeral
	if pathutil.IsURL(path) {
		return true
	}

	// Local file: check if it's inside the workdir (watchable)
	// If workdir is empty, treat as ephemeral to be safe
	if workdir == "" {
		return true
	}

	// Resolve the path relative to the datasource's base directory
	resolved, err := pathutil.Resolve(s.BaseDir, path)
	if err != nil {
		// Can't resolve path, treat as ephemeral to be safe
		return true
	}

	// Check if resolved path is inside workdir
	rel, err := filepath.Rel(workdir, resolved)
	if err != nil {
		return true
	}

	// If relative path starts with "..", it's outside workdir
	if strings.HasPrefix(rel, "..") {
		return true
	}

	// Local file inside workdir - not ephemeral (file watcher handles it)
	return false
}
