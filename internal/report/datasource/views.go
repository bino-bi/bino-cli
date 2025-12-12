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

// RegisterViews creates DuckDB views for all non-inline DataSource documents in the session.
// Each DataSource becomes a view named by metadata.name, so subsequent queries
// can simply `SELECT * FROM <name>`.
//
// Inline sources are skipped - they don't need views since their data is
// available directly as JSON.
//
// This function:
//   - Installs required extensions (postgres, mysql) based on source types
//   - Attaches external databases (postgres, mysql) using ATTACH with appropriate secrets
//   - Creates `CREATE OR REPLACE VIEW "<name>" AS <sourceSQL>` for each DataSource
//   - Returns diagnostics for individual failures without aborting the entire operation
func RegisterViews(ctx context.Context, session *duckdb.Session, docs []config.Document) ([]Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var (
		diags      []Diagnostic
		extensions = map[string]struct{}{}
		views      []viewDef
	)

	// Collect all non-inline DataSource documents and determine required extensions
	for _, doc := range docs {
		if doc.Kind != "DataSource" {
			continue
		}

		spec, err := parseSpec(doc.Raw)
		if err != nil {
			diags = append(diags, diagnostic(doc.Name, "spec", err))
			continue
		}

		// Skip inline sources - they don't need views
		if spec.Type == sourceTypeInline {
			continue
		}

		baseDir := filepath.Dir(doc.File)
		spec.Name = doc.Name
		spec.BaseDir = baseDir

		// Track required extensions
		if ext := extensionForSource(spec.Type); ext != "" {
			extensions[ext] = struct{}{}
		}

		views = append(views, viewDef{
			name: doc.Name,
			spec: spec,
		})
	}

	if len(views) == 0 {
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
		var additionalExts []string
		for _, ext := range secretExts {
			if _, loaded := extensions[ext]; !loaded {
				additionalExts = append(additionalExts, ext)
			}
		}
		if len(additionalExts) > 0 {
			if err := session.InstallAndLoadExtensions(ctx, additionalExts); err != nil {
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

	return diags, nil
}

// viewDef holds the information needed to create a DuckDB view.
type viewDef struct {
	name string
	spec sourceSpec
}

// createView executes CREATE OR REPLACE VIEW for a single DataSource.
func createView(ctx context.Context, db *sql.DB, session *duckdb.Session, v viewDef) error {
	sourceSQL, err := buildViewSourceSQL(v.spec)
	if err != nil {
		return err
	}

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
		return buildFileSourceSQL(spec, "read_csv_auto")
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

// buildFileSourceSQL creates a query for file-based sources (excel, csv, parquet).
func buildFileSourceSQL(spec sourceSpec, readerFn string) (string, error) {
	path := strings.TrimSpace(spec.Path)
	if path == "" {
		return "", fmt.Errorf("path is required for %s datasources", spec.Type)
	}

	// URLs (http, https, s3) are passed directly to DuckDB
	if pathutil.IsURL(path) {
		return fmt.Sprintf("SELECT * FROM %s('%s')", readerFn, escapeSQLString(path)), nil
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
				return fmt.Sprintf("SELECT * FROM %s('%s')", readerFn, escapeSQLString(matches[0])), nil
			}
			// For multiple files, UNION ALL them together
			var parts []string
			for _, m := range matches {
				parts = append(parts, fmt.Sprintf("SELECT * FROM %s('%s')", readerFn, escapeSQLString(m)))
			}
			return strings.Join(parts, " UNION ALL "), nil
		}
		// Other readers (csv, parquet) support globs natively
		return fmt.Sprintf("SELECT * FROM %s('%s')", readerFn, escapeSQLString(resolved)), nil
	}

	// Check file exists
	if _, err := os.Stat(resolved); err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT * FROM %s('%s')", readerFn, escapeSQLString(resolved)), nil
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
