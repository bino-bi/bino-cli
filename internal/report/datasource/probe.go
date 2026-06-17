package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// ProbeRequest describes a not-yet-registered data source to introspect.
type ProbeRequest struct {
	// SpecJSON is the DataSource spec object, e.g. {"type":"csv","path":"data.csv"}.
	SpecJSON json.RawMessage
	// BaseDir resolves relative file paths in the spec (typically the directory
	// of the manifest that will eventually hold the DataSource).
	BaseDir string
	// Docs are sibling documents used to resolve ConnectionSecret references for
	// database sources. May be nil for file sources.
	Docs []config.Document
	// Sheet optionally overrides the Excel sheet to read.
	Sheet string
	// Limit caps the number of sample rows returned (default 100 when <= 0).
	Limit int
}

// ProbeColumn is a single introspected column with its DuckDB type.
type ProbeColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// DetectedCSV reports CSV options auto-detected by DuckDB's sniff_csv, so the
// wizard can pre-fill the delimiter/header/skip fields.
type DetectedCSV struct {
	Delimiter string `json:"delimiter,omitempty"`
	HasHeader *bool  `json:"hasHeader,omitempty"`
	SkipRows  *int   `json:"skipRows,omitempty"`
}

// ProbeResult is the introspection of a draft data source.
type ProbeResult struct {
	Columns     []ProbeColumn    `json:"columns"`
	Sheets      []string         `json:"sheets,omitempty"`
	SampleRows  []map[string]any `json:"sampleRows"`
	Truncated   bool             `json:"truncated"`
	DetectedCSV *DetectedCSV     `json:"detectedCsv,omitempty"`
}

// Probe introspects a draft data source described by req, returning its columns
// (with types), a sample of rows, and source-specific extras (Excel sheet names,
// detected CSV options). It is read-only: it builds the same source SELECT the
// engine would use and wraps it, never creating views or mutating the session.
// The caller owns the session lifecycle (the daemon and lsp-helper each pass a
// fresh ephemeral session so introspection never pollutes the shared session).
func Probe(ctx context.Context, session *duckdb.Session, req ProbeRequest) (*ProbeResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	spec, err := parseProbeSpec(req.SpecJSON)
	if err != nil {
		return nil, err
	}
	spec.Name = "_probe"
	spec.BaseDir = req.BaseDir
	if req.Sheet != "" {
		spec.Sheet = req.Sheet
	}

	if spec.Type == sourceTypeInline {
		return nil, fmt.Errorf("inline sources cannot be introspected")
	}

	result := &ProbeResult{
		Columns:    []ProbeColumn{},
		SampleRows: []map[string]any{},
	}

	// Excel sheet names are read directly from the workbook zip (the excel
	// extension exposes no sheet-listing function).
	if spec.Type == sourceTypeExcel {
		if path, ok := localFilePath(spec); ok {
			if sheets, err := sheetNames(path); err == nil {
				result.Sheets = sheets
			}
		}
	}

	db := session.DB()

	if ext := extensionForSource(spec.Type); ext != "" {
		if err := session.InstallAndLoadExtensions(ctx, []string{ext}); err != nil {
			return nil, fmt.Errorf("load %s extension: %w", ext, err)
		}
	}

	// Database sources need credentials + an ATTACH before the source query runs.
	if attachName, attachSQL := buildAttachSQL(spec); attachSQL != "" {
		if _, _, err := LoadSecrets(ctx, db, req.Docs); err != nil {
			return nil, fmt.Errorf("load secrets: %w", err)
		}
		if _, err := db.ExecContext(ctx, attachSQL); err != nil { // codeql[go/sql-injection] ATTACH string is SQL-escaped; connects to the developer's own DB on a local ephemeral session
			return nil, fmt.Errorf("attach %s: %w", attachName, err)
		}
	}

	base, err := buildViewSourceSQL(spec)
	if err != nil {
		return nil, err
	}

	cols, rows, truncated, err := sampleAndDescribe(ctx, db, base, limit)
	if err != nil {
		return nil, err
	}
	result.Columns = cols
	result.SampleRows = rows
	result.Truncated = truncated

	if spec.Type == sourceTypeCSV {
		if path, ok := localFilePath(spec); ok {
			result.DetectedCSV = sniffCSV(ctx, db, path)
		}
	}

	return result, nil
}

// parseProbeSpec parses a bare spec object (no manifest envelope) into a sourceSpec.
func parseProbeSpec(specJSON json.RawMessage) (sourceSpec, error) {
	if len(specJSON) == 0 {
		return sourceSpec{}, fmt.Errorf("spec is required")
	}
	wrapped := make([]byte, 0, len(specJSON)+9)
	wrapped = append(wrapped, []byte(`{"spec":`)...)
	wrapped = append(wrapped, specJSON...)
	wrapped = append(wrapped, '}')
	return parseSpec(wrapped)
}

// sampleAndDescribe runs the source SELECT once with LIMIT n+1, deriving the
// typed columns from the result schema and scanning up to limit sample rows.
func sampleAndDescribe(ctx context.Context, db *sql.DB, base string, limit int) (cols []ProbeColumn, sample []map[string]any, truncated bool, err error) {
	query := fmt.Sprintf("SELECT * FROM (%s) AS _probe LIMIT %d", base, limit+1) //nolint:gosec // G201: base comes from buildViewSourceSQL (internal, escaped), not user free-text
	rows, err := db.QueryContext(ctx, query)                                     // codeql[go/sql-injection] source SELECT is built from an escaped, developer-supplied spec on a local ephemeral session
	if err != nil {
		return nil, nil, false, fmt.Errorf("introspect source: %w", err)
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, false, fmt.Errorf("column types: %w", err)
	}
	cols = make([]ProbeColumn, len(colTypes))
	for i, ct := range colTypes {
		cols[i] = ProbeColumn{Name: ct.Name(), Type: ct.DatabaseTypeName()}
	}

	sample = []map[string]any{}
	values := make([]any, len(colTypes))
	ptrs := make([]any, len(colTypes))
	for i := range values {
		ptrs[i] = &values[i]
	}
	count := 0
	for rows.Next() {
		count++
		if count > limit {
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, false, fmt.Errorf("scan row: %w", err)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c.Name] = normalizeValue(values[i])
		}
		sample = append(sample, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("read rows: %w", err)
	}
	return cols, sample, count > limit, nil
}

// sniffCSV runs DuckDB's sniff_csv to surface auto-detected CSV options.
// Best-effort: returns nil if sniffing fails.
func sniffCSV(ctx context.Context, db *sql.DB, path string) *DetectedCSV {
	query := fmt.Sprintf("SELECT Delimiter, HasHeader, SkipRows FROM sniff_csv('%s')", escapeSQLString(path)) //nolint:gosec // G201: path is SQL-string-escaped
	row := db.QueryRowContext(ctx, query)                                                                     // codeql[go/sql-injection] path is SQL-escaped; sniffs a local developer-selected file
	var (
		delim    sql.NullString
		header   sql.NullBool
		skipRows sql.NullInt64
	)
	if err := row.Scan(&delim, &header, &skipRows); err != nil {
		return nil
	}
	out := &DetectedCSV{}
	if delim.Valid {
		out.Delimiter = delim.String
	}
	if header.Valid {
		h := header.Bool
		out.HasHeader = &h
	}
	if skipRows.Valid {
		s := int(skipRows.Int64)
		out.SkipRows = &s
	}
	return out
}

// localFilePath resolves the spec's path to a single local file, returning
// (path, true) only when it is a concrete local file (not a URL, glob or directory).
func localFilePath(spec sourceSpec) (string, bool) {
	path := strings.TrimSpace(spec.Path)
	if path == "" || pathutil.IsURL(path) {
		return "", false
	}
	resolved, err := pathutil.Resolve(spec.BaseDir, path)
	if err != nil || pathutil.HasGlob(resolved) {
		return "", false
	}
	info, err := os.Stat(resolved) // codeql[go/path-injection] intentional stat of a developer-selected local file path (local introspection tool)
	if err != nil || info.IsDir() {
		return "", false
	}
	return resolved, true
}
