// Package datasource provides data collection from various sources (files, S3, databases).
package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// ViewsOptions configures view registration behavior.
type ViewsOptions struct {
	// TempDir is the directory for temporary files (e.g., inline CSV files).
	// If empty, inline sources will be skipped.
	TempDir string
}

// RegisterViews creates DuckDB views for all DataSource documents in the session.
// Each DataSource becomes a view named by metadata.name, so subsequent queries
// can simply `SELECT * FROM <name>`.
//
// Inline sources are materialized as CSV files in opts.TempDir and registered
// as views using read_csv_auto(). If opts is nil or TempDir is empty, inline
// sources are skipped.
//
// This function:
//   - Installs required extensions (postgres, mysql) based on source types
//   - Attaches external databases (postgres, mysql) using ATTACH with appropriate secrets
//   - Creates `CREATE OR REPLACE VIEW "<name>" AS <sourceSQL>` for each DataSource
//   - Returns diagnostics for individual failures without aborting the entire operation
func RegisterViews(ctx context.Context, session *duckdb.Session, docs []config.Document, opts *ViewsOptions) ([]Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var (
		diags       []Diagnostic
		extensions  = map[string]struct{}{}
		views       []viewDef
		inlineViews []inlineViewDef
	)

	// Determine if we can handle inline sources
	tempDir := ""
	if opts != nil {
		tempDir = opts.TempDir
	}

	// Collect all DataSource documents and determine required extensions
	for _, doc := range docs {
		if doc.Kind != "DataSource" {
			continue
		}

		spec, err := parseSpec(doc.Raw)
		if err != nil {
			diags = append(diags, diagnostic(doc.Name, "spec", err))
			continue
		}

		baseDir := filepath.Dir(doc.File)
		spec.Name = doc.Name
		spec.BaseDir = baseDir

		// Handle inline sources separately
		if spec.Type == sourceTypeInline {
			// Skip inline sources if no tempDir is provided
			if tempDir == "" {
				continue
			}
			payload, err := spec.inlinePayload()
			if err != nil {
				diags = append(diags, diagnostic(doc.Name, "inline", err))
				continue
			}
			inlineViews = append(inlineViews, inlineViewDef{
				name:    doc.Name,
				payload: payload,
			})
			continue
		}

		// Track required extensions
		if ext := extensionForSource(spec.Type); ext != "" {
			extensions[ext] = struct{}{}
		}

		views = append(views, viewDef{
			name: doc.Name,
			spec: spec,
		})
	}

	if len(views) == 0 && len(inlineViews) == 0 {
		return diags, nil
	}

	// Install required extensions
	if len(extensions) > 0 {
		requested := make([]string, 0, len(extensions))
		for name := range extensions {
			requested = append(requested, name)
		}
		sort.Strings(requested)
		if err := session.InstallAndLoadExtensions(ctx, requested); err != nil {
			return diags, err
		}
	}

	db := session.DB()

	// Load secrets from ConnectionSecret documents before attaching databases
	secretExts, secretDiags, err := LoadSecrets(ctx, db, docs)
	if err != nil {
		return append(diags, secretDiags...), err
	}
	diags = append(diags, secretDiags...)

	// Install extensions required by secrets (if any weren't already loaded)
	if len(secretExts) > 0 {
		var standardExts, communityExts []string
		for _, ext := range secretExts {
			if _, loaded := extensions[ext]; !loaded {
				if IsCommunityExtension(ext) {
					communityExts = append(communityExts, ext)
				} else {
					standardExts = append(standardExts, ext)
				}
			}
		}
		if len(standardExts) > 0 {
			if err := session.InstallAndLoadExtensions(ctx, standardExts); err != nil {
				return diags, err
			}
		}
		if len(communityExts) > 0 {
			if err := session.InstallAndLoadCommunityExtensions(ctx, communityExts); err != nil {
				return diags, err
			}
		}
	}

	// Attach external databases (postgres, mysql) before creating views
	attachedDBs := make(map[string]struct{})
	for _, v := range views {
		if err := ctx.Err(); err != nil {
			return diags, err
		}

		attachName, attachSQL := buildAttachSQL(v.spec)
		if attachSQL == "" {
			continue
		}

		// Skip if already attached
		if _, exists := attachedDBs[attachName]; exists {
			continue
		}

		session.LogQuery(attachSQL)
		attachStart := time.Now()
		_, execErr := db.ExecContext(ctx, attachSQL)
		attachDuration := time.Since(attachStart)

		// Emit query execution metadata for ATTACH statement
		meta := duckdb.QueryExecMeta{
			Query:      attachSQL,
			QueryType:  "attach",
			Datasource: v.name,
			StartTime:  attachStart,
			DurationMs: attachDuration.Milliseconds(),
			RowCount:   0,
		}
		if execErr != nil {
			meta.Error = execErr.Error()
		}
		session.LogQueryExec(meta)

		if execErr != nil {
			diags = append(diags, diagnostic(v.name, "datasource", execErr))
			continue
		}
		attachedDBs[attachName] = struct{}{}
	}

	// Create views for each DataSource
	for _, v := range views {
		if err := ctx.Err(); err != nil {
			return diags, err
		}

		if err := createView(ctx, db, session, v); err != nil {
			diags = append(diags, diagnostic(v.name, "create view", err))
			continue
		}
	}

	// Create views for inline DataSources
	for _, iv := range inlineViews {
		if err := ctx.Err(); err != nil {
			return diags, err
		}

		if err := createInlineView(ctx, db, session, iv, tempDir); err != nil {
			diags = append(diags, diagnostic(iv.name, "create view", err))
			continue
		}
	}

	return diags, nil
}

// viewDef holds the information needed to create a DuckDB view.
type viewDef struct {
	name string
	spec sourceSpec
}

// inlineViewDef holds the information needed to create a DuckDB view from inline data.
type inlineViewDef struct {
	name    string
	payload json.RawMessage
}

// createInlineView creates a DuckDB view from inline data by writing to a temp CSV file.
func createInlineView(ctx context.Context, db *sql.DB, session *duckdb.Session, iv inlineViewDef, tempDir string) error {
	viewSQL, err := buildInlineViewSQL(iv.name, tempDir, iv.payload)
	if err != nil {
		return err
	}

	session.LogQuery(viewSQL)
	viewStart := time.Now()
	_, execErr := db.ExecContext(ctx, viewSQL)
	viewDuration := time.Since(viewStart)

	// Emit query execution metadata for CREATE VIEW statement
	meta := duckdb.QueryExecMeta{
		Query:      viewSQL,
		QueryType:  "create_view",
		Datasource: iv.name,
		StartTime:  viewStart,
		DurationMs: viewDuration.Milliseconds(),
		RowCount:   0,
	}
	if execErr != nil {
		meta.Error = execErr.Error()
	}
	session.LogQueryExec(meta)

	if execErr != nil {
		return fmt.Errorf("execute CREATE VIEW: %w", execErr)
	}

	return nil
}

// createView executes CREATE OR REPLACE VIEW for a single DataSource.
func createView(ctx context.Context, db *sql.DB, session *duckdb.Session, v viewDef) error {
	sourceSQL, err := buildViewSourceSQL(v.spec)
	if err != nil {
		return err
	}

	// Append USING SAMPLE clause if configured
	sourceSQL += buildSampleClause(v.spec.Sample)

	// Use quoted identifier for the view name to handle special characters
	viewSQL := fmt.Sprintf("CREATE OR REPLACE VIEW \"%s\" AS %s", v.name, sourceSQL)

	session.LogQuery(viewSQL)
	viewStart := time.Now()
	_, execErr := db.ExecContext(ctx, viewSQL)
	viewDuration := time.Since(viewStart)

	// Emit query execution metadata for CREATE VIEW statement
	meta := duckdb.QueryExecMeta{
		Query:      viewSQL,
		QueryType:  "create_view",
		Datasource: v.name,
		StartTime:  viewStart,
		DurationMs: viewDuration.Milliseconds(),
		RowCount:   0,
	}
	if execErr != nil {
		meta.Error = execErr.Error()
	}
	session.LogQueryExec(meta)

	if execErr != nil {
		return fmt.Errorf("execute CREATE VIEW: %w", execErr)
	}

	return nil
}

// buildViewSourceSQL builds the source SELECT query for a DataSource.
// This is the query that becomes the view definition.
// Note: inline sources are not supported here - they are handled separately.
func buildViewSourceSQL(spec sourceSpec) (string, error) {
	switch spec.Type {
	case sourceTypeExcel:
		return buildFileSourceSQL(spec, "read_xlsx")
	case sourceTypeCSV:
		return buildCSVSourceSQL(spec)
	case sourceTypeParquet:
		return buildFileSourceSQL(spec, "read_parquet")
	case sourceTypePostgresQuery:
		return buildPostgresSourceSQL(spec)
	case sourceTypeMySQLQuery:
		return buildMySQLSourceSQL(spec)
	case sourceTypeInline:
		return "", fmt.Errorf("inline sources should not be registered as views")
	default:
		return "", fmt.Errorf("unsupported datasource type: %s", spec.Type)
	}
}

// buildFileSourceSQL creates a query for file-based sources (excel, parquet).
func buildFileSourceSQL(spec sourceSpec, readerFn string) (string, error) {
	return buildFileSourceSQLWithParams(spec, readerFn, "")
}

// buildFileSourceSQLWithParams creates a query for file-based sources with optional extra DuckDB parameters.
func buildFileSourceSQLWithParams(spec sourceSpec, readerFn string, extraParams string) (string, error) {
	path := strings.TrimSpace(spec.Path)
	if path == "" {
		return "", fmt.Errorf("path is required for %s datasources", spec.Type)
	}

	formatCall := func(fn, resolvedPath string) string {
		if extraParams != "" {
			return fmt.Sprintf("SELECT * FROM %s('%s', %s)", fn, escapeSQLString(resolvedPath), extraParams)
		}
		return fmt.Sprintf("SELECT * FROM %s('%s')", fn, escapeSQLString(resolvedPath))
	}

	// URLs (http, https, s3) are passed directly to DuckDB
	if pathutil.IsURL(path) {
		return formatCall(readerFn, path), nil
	}

	// Resolve local path relative to the manifest file
	resolved, err := pathutil.Resolve(spec.BaseDir, path)
	if err != nil {
		return "", err
	}

	// Check if path is a directory - auto-generate glob pattern
	info, statErr := os.Stat(resolved)
	if statErr == nil && info.IsDir() {
		resolved = filepath.ToSlash(filepath.Join(resolved, defaultGlobForType(spec.Type)))
	}

	// Handle glob patterns
	if pathutil.HasGlob(resolved) {
		// read_xlsx doesn't support globs, so we need to expand and UNION ALL
		if readerFn == "read_xlsx" {
			matches, err := filepath.Glob(resolved)
			if err != nil {
				return "", fmt.Errorf("expand glob %s: %w", resolved, err)
			}
			if len(matches) == 0 {
				return "", fmt.Errorf("no files match pattern %s", resolved)
			}
			// For a single file, just read it directly
			if len(matches) == 1 {
				return formatCall(readerFn, matches[0]), nil
			}
			// For multiple files, UNION ALL them together
			var parts []string
			for _, m := range matches {
				parts = append(parts, formatCall(readerFn, m))
			}
			return strings.Join(parts, " UNION ALL "), nil
		}
		// Other readers (csv, parquet) support globs natively
		return formatCall(readerFn, resolved), nil
	}

	// Check file exists
	if _, err := os.Stat(resolved); err != nil {
		return "", err
	}

	return formatCall(readerFn, resolved), nil
}

// buildCSVSourceSQL creates a query for CSV datasources, using read_csv with explicit
// parameters when any CSV reader options are set, or read_csv_auto otherwise.
func buildCSVSourceSQL(spec sourceSpec) (string, error) {
	params := buildCSVParams(spec)
	if params == "" {
		return buildFileSourceSQLWithParams(spec, "read_csv_auto", "")
	}
	return buildFileSourceSQLWithParams(spec, "read_csv", params)
}

// buildCSVParams builds the DuckDB parameter string from CSV reader options on the spec.
// Returns an empty string if no CSV options are set.
func buildCSVParams(spec sourceSpec) string {
	var parts []string

	if spec.Delimiter != "" {
		parts = append(parts, fmt.Sprintf("delim = '%s'", escapeSQLString(spec.Delimiter)))
	}
	if spec.Header != nil {
		parts = append(parts, fmt.Sprintf("header = %t", *spec.Header))
	}
	if spec.SkipRows > 0 {
		parts = append(parts, fmt.Sprintf("skip = %d", spec.SkipRows))
	}
	if spec.Thousands != "" {
		parts = append(parts, fmt.Sprintf("thousands = '%s'", escapeSQLString(spec.Thousands)))
	}
	if spec.DecimalSeparator != "" {
		parts = append(parts, fmt.Sprintf("decimal_separator = '%s'", escapeSQLString(spec.DecimalSeparator)))
	}
	if spec.DateFormat != "" {
		parts = append(parts, fmt.Sprintf("dateformat = '%s'", escapeSQLString(spec.DateFormat)))
	}
	if len(spec.Columns) > 0 {
		parts = append(parts, fmt.Sprintf("columns = %s", formatDuckDBStruct(spec.Columns)))
	}
	if len(spec.ColumnNames) > 0 {
		parts = append(parts, fmt.Sprintf("names = %s", formatDuckDBList(spec.ColumnNames)))
	}

	return strings.Join(parts, ", ")
}

// formatDuckDBStruct formats a map as a DuckDB struct literal: {'key1': 'val1', 'key2': 'val2'}.
// Keys are sorted for deterministic output.
func formatDuckDBStruct(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("'%s': '%s'", escapeSQLString(k), escapeSQLString(m[k])))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// formatDuckDBList formats a string slice as a DuckDB list literal: ['a', 'b', 'c'].
func formatDuckDBList(items []string) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("'%s'", escapeSQLString(item)))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// buildPostgresSourceSQL creates a query for postgres_query datasources.
// According to DuckDB documentation, postgres_query takes (attached_database, query).
// The database must be attached first using ATTACH with TYPE postgres.
func buildPostgresSourceSQL(spec sourceSpec) (string, error) {
	if spec.Connection == nil {
		return "", fmt.Errorf("connection settings are required")
	}
	query := strings.TrimSpace(spec.Query)
	if query == "" {
		return "", fmt.Errorf("query is required for postgres_query datasources")
	}

	// Construct the ATTACH name based on the datasource name
	attachName := postgresAttachName(spec.Name)

	return fmt.Sprintf(
		"SELECT * FROM postgres_query('%s', '%s')",
		escapeSQLString(attachName),
		escapeSQLString(query),
	), nil
}

// buildMySQLSourceSQL creates a query for mysql_query datasources.
// According to DuckDB documentation, mysql_query takes (attached_database, query).
// The database must be attached first using ATTACH with TYPE mysql.
func buildMySQLSourceSQL(spec sourceSpec) (string, error) {
	if spec.Connection == nil {
		return "", fmt.Errorf("connection settings are required")
	}
	query := strings.TrimSpace(spec.Query)
	if query == "" {
		return "", fmt.Errorf("query is required for mysql_query datasources")
	}

	// Construct the ATTACH name based on the datasource name
	attachName := mysqlAttachName(spec.Name)

	return fmt.Sprintf(
		"SELECT * FROM mysql_query('%s', '%s')",
		escapeSQLString(attachName),
		escapeSQLString(query),
	), nil
}

// buildAttachSQL returns the ATTACH statement for external database sources.
// Returns (attachName, attachSQL). If the source doesn't need ATTACH, returns ("", "").
func buildAttachSQL(spec sourceSpec) (string, string) {
	switch spec.Type {
	case sourceTypePostgresQuery:
		return buildPostgresAttachSQL(spec)
	case sourceTypeMySQLQuery:
		return buildMySQLAttachSQL(spec)
	default:
		return "", ""
	}
}

// postgresAttachName returns the name used for attaching a PostgreSQL database.
func postgresAttachName(sourceName string) string {
	return fmt.Sprintf("_pg_%s", sourceName)
}

// mysqlAttachName returns the name used for attaching a MySQL database.
func mysqlAttachName(sourceName string) string {
	return fmt.Sprintf("_mysql_%s", sourceName)
}

// buildPostgresAttachSQL builds ATTACH statement for PostgreSQL.
// Per DuckDB docs: ATTACH 'connStr' AS name (TYPE postgres, SECRET secretName)
func buildPostgresAttachSQL(spec sourceSpec) (string, string) {
	if spec.Connection == nil {
		return "", ""
	}

	attachName := postgresAttachName(spec.Name)
	connStr := buildPostgresConnection(*spec.Connection)

	if secretName := strings.TrimSpace(spec.Connection.Secret); secretName != "" {
		// Use ATTACH with SECRET for authentication
		// Quote the secret name to handle names with special characters like hyphens
		return attachName, fmt.Sprintf(
			"ATTACH '%s' AS %s (TYPE postgres, SECRET \"%s\")",
			escapeSQLString(connStr),
			attachName,
			secretName,
		)
	}

	// Without a secret, use connection string directly
	return attachName, fmt.Sprintf(
		"ATTACH '%s' AS %s (TYPE postgres)",
		escapeSQLString(connStr),
		attachName,
	)
}

// buildMySQLAttachSQL builds ATTACH statement for MySQL.
// Per DuckDB docs: ATTACH 'connStr' AS name (TYPE mysql, SECRET secretName)
func buildMySQLAttachSQL(spec sourceSpec) (string, string) {
	if spec.Connection == nil {
		return "", ""
	}

	attachName := mysqlAttachName(spec.Name)
	connStr := buildMySQLConnection(*spec.Connection)

	if secretName := strings.TrimSpace(spec.Connection.Secret); secretName != "" {
		// Use ATTACH with SECRET for authentication
		// Quote the secret name to handle names with special characters like hyphens
		return attachName, fmt.Sprintf(
			"ATTACH '%s' AS %s (TYPE mysql, SECRET \"%s\")",
			escapeSQLString(connStr),
			attachName,
			secretName,
		)
	}

	// Without a secret, use connection string directly
	return attachName, fmt.Sprintf(
		"ATTACH '%s' AS %s (TYPE mysql)",
		escapeSQLString(connStr),
		attachName,
	)
}

// QueryView executes SELECT * FROM "<name>" and returns the result as JSON.
// Use this after RegisterViews to fetch data for a DataSource.
// If session is provided, query execution metadata will be logged.
func QueryView(ctx context.Context, db *sql.DB, session *duckdb.Session, name string) (json.RawMessage, error) {
	query := fmt.Sprintf("SELECT * FROM \"%s\"", name)

	queryStart := time.Now()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		// Emit metadata for failed query
		if session != nil {
			meta := duckdb.QueryExecMeta{
				Query:      query,
				QueryType:  "datasource_query",
				Datasource: name,
				StartTime:  queryStart,
				DurationMs: time.Since(queryStart).Milliseconds(),
				RowCount:   0,
				Error:      err.Error(),
			}
			session.LogQueryExec(meta)
		}
		return nil, fmt.Errorf("query view %s: %w", name, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}

	var result []map[string]any
	for rows.Next() {
		scanTargets := make([]any, len(columns))
		holders := make([]any, len(columns))
		for i := range scanTargets {
			holders[i] = &scanTargets[i]
		}
		if err := rows.Scan(holders...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		row := make(map[string]any, len(columns))
		for idx, col := range columns {
			row[col] = normalizeValue(scanTargets[idx])
		}
		result = append(result, row)
	}

	queryDuration := time.Since(queryStart)

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	// Emit query execution metadata
	if session != nil {
		meta := duckdb.QueryExecMeta{
			Query:      query,
			QueryType:  "datasource_query",
			Datasource: name,
			StartTime:  queryStart,
			DurationMs: queryDuration.Milliseconds(),
			RowCount:   len(result),
			Columns:    columns,
		}
		session.LogQueryExec(meta)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode rows: %w", err)
	}

	return json.RawMessage(data), nil
}
