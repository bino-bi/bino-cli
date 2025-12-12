package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

var standardExtensions = []string{"excel", "postgres", "mysql", "httpfs"}
var communityExtensions = []string{"prql"}

// DefaultExtensions returns the built-in set of DuckDB extensions required by the CLI.
func DefaultExtensions() []string {
	return append([]string(nil), standardExtensions...)
}

// CommunityExtensions returns DuckDB community extensions installed via FROM community.
func CommunityExtensions() []string {
	return append([]string(nil), communityExtensions...)
}

// QueryLogger is called for each SQL query executed via the session.
// Use this to log queries to the terminal or a build log.
type QueryLogger func(query string)

// QueryExecMeta captures detailed metadata about a query execution.
type QueryExecMeta struct {
	// Query is the SQL query text.
	Query string
	// QueryType classifies the query: "dataset_query", "datasource_query", "attach", "create_view".
	QueryType string
	// Dataset is the dataset name if applicable.
	Dataset string
	// Datasource is the datasource name if applicable.
	Datasource string
	// StartTime is when the query started.
	StartTime time.Time
	// DurationMs is the execution duration in milliseconds.
	DurationMs int64
	// RowCount is the number of rows returned.
	RowCount int
	// Columns contains the column names.
	Columns []string
	// Rows contains row data as strings for CSV building.
	Rows [][]string
	// Error is the error message if the query failed.
	Error string
}

// QueryExecLogger is called for each query execution with detailed metadata.
type QueryExecLogger func(meta QueryExecMeta)

// Session owns an in-memory DuckDB connection configured for CLI pipelines.
type Session struct {
	db              *sql.DB
	cacheDir        string
	queryLogger     QueryLogger
	queryExecLogger QueryExecLogger
}

// Options capture how a DuckDB session should be created.
type Options struct {
	// Path controls the DuckDB database location. Leave empty for in-memory mode.
	Path string
	// CacheDir stores downloaded extensions to avoid repeated fetches.
	CacheDir string
	// QueryLogger is called for each SQL query executed. May be nil.
	QueryLogger QueryLogger
	// QueryExecLogger is called for each query execution with detailed metadata. May be nil.
	QueryExecLogger QueryExecLogger
}

// DefaultOptions returns sensible defaults for the CLI (in-memory DB + user cache).
func DefaultOptions() (Options, error) {
	cache, err := defaultCacheDir()
	if err != nil {
		return Options{}, err
	}

	return Options{CacheDir: cache}, nil
}

// OpenSession bootstraps DuckDB, configures extension caching, and validates connectivity.
func OpenSession(ctx context.Context, opts Options) (*Session, error) {
	if opts.CacheDir == "" {
		cache, err := defaultCacheDir()
		if err != nil {
			return nil, err
		}
		opts.CacheDir = cache
	}

	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	connStr := opts.Path
	// Empty connection string is DuckDB's in-memory mode.
	db, err := sql.Open("duckdb", connStr)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}

	s := &Session{db: db, cacheDir: opts.CacheDir, queryLogger: opts.QueryLogger, queryExecLogger: opts.QueryExecLogger}
	if err := s.configureExtensionDirectory(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func defaultCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("detect home dir: %w", err)
	}

	return filepath.Join(home, ".bn", "cache", "duckdb", "extensions"), nil
}

func (s *Session) configureExtensionDirectory(ctx context.Context) error {
	escaped := strings.ReplaceAll(s.cacheDir, "'", "''")
	query := fmt.Sprintf("SET extension_directory='%s';", escaped)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("set extension directory: %w", err)
	}
	return nil
}

// Close releases the underlying DuckDB connection.
func (s *Session) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the raw sql.DB for downstream components that need advanced access.
func (s *Session) DB() *sql.DB {
	return s.db
}

// LogQuery invokes the QueryLogger if set. This is a no-op when QueryLogger is nil.
func (s *Session) LogQuery(query string) {
	if s != nil && s.queryLogger != nil {
		s.queryLogger(query)
	}
}

// LogQueryExec invokes the QueryExecLogger if set. This is a no-op when QueryExecLogger is nil.
func (s *Session) LogQueryExec(meta QueryExecMeta) {
	if s != nil && s.queryExecLogger != nil {
		s.queryExecLogger(meta)
	}
}

var extensionNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// InstallAndLoadExtensions downloads (if needed) and loads the provided extensions.
func (s *Session) InstallAndLoadExtensions(ctx context.Context, names []string) error {
	if s == nil || s.db == nil {
		return errors.New("duckdb session is not initialized")
	}

	for _, name := range names {
		if !extensionNamePattern.MatchString(name) {
			return fmt.Errorf("invalid DuckDB extension name: %s", name)
		}

		install := fmt.Sprintf("INSTALL %s;", name)
		if _, err := s.db.ExecContext(ctx, install); err != nil {
			return fmt.Errorf("install extension %s: %w", name, err)
		}

		load := fmt.Sprintf("LOAD %s;", name)
		if _, err := s.db.ExecContext(ctx, load); err != nil {
			return fmt.Errorf("load extension %s: %w", name, err)
		}
	}

	return nil
}

// InstallAndLoadCommunityExtensions downloads and loads extensions from the DuckDB community repository.
// These extensions require the "FROM community" syntax for installation.
func (s *Session) InstallAndLoadCommunityExtensions(ctx context.Context, names []string) error {
	if s == nil || s.db == nil {
		return errors.New("duckdb session is not initialized")
	}

	for _, name := range names {
		if !extensionNamePattern.MatchString(name) {
			return fmt.Errorf("invalid DuckDB extension name: %s", name)
		}

		install := fmt.Sprintf("INSTALL %s FROM community;", name)
		if _, err := s.db.ExecContext(ctx, install); err != nil {
			return fmt.Errorf("install community extension %s: %w", name, err)
		}

		load := fmt.Sprintf("LOAD %s;", name)
		if _, err := s.db.ExecContext(ctx, load); err != nil {
			return fmt.Errorf("load community extension %s: %w", name, err)
		}
	}

	return nil
}
