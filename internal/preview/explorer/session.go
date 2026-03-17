// Package explorer provides a persistent DuckDB session for interactive data
// exploration in preview mode. Unlike the ephemeral sessions used during
// report rendering, this session survives across refresh cycles so users
// can run ad-hoc SQL queries against registered datasource views.
package explorer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/pkg/duckdb"
)

// Session owns a long-lived DuckDB connection for data exploration.
// HTTP query handlers use WithDB (read lock, concurrent).
// The refresh cycle calls Refresh (write lock, exclusive).
type Session struct {
	mu      sync.RWMutex
	session *duckdb.Session
	tempDir string
	logger  logx.Logger
	docs    []config.Document
}

// NewSession opens a persistent in-memory DuckDB session for exploration.
func NewSession(ctx context.Context, logger logx.Logger) (*Session, error) {
	tmpDir, err := os.MkdirTemp("", "bino-explorer-*")
	if err != nil {
		return nil, fmt.Errorf("explorer: create temp dir: %w", err)
	}

	sess, err := duckdb.OpenSession(ctx, duckdb.Options{})
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("explorer: open duckdb: %w", err)
	}

	// Install standard and community extensions
	if err := sess.InstallAndLoadExtensions(ctx, duckdb.DefaultExtensions()); err != nil {
		sess.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("explorer: install extensions: %w", err)
	}
	if err := sess.InstallAndLoadCommunityExtensions(ctx, duckdb.CommunityExtensions()); err != nil {
		logger.Warnf("explorer: community extensions: %v", err)
	}

	return &Session{
		session: sess,
		tempDir: tmpDir,
		logger:  logger,
	}, nil
}

// Refresh drops all user views, detaches external databases, and
// re-registers views from the provided documents. Called on each
// file-watcher refresh. Errors are non-fatal and logged.
func (s *Session) Refresh(ctx context.Context, docs []config.Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	db := s.session.DB()

	// Drop all user views
	if err := dropUserViews(ctx, db); err != nil {
		return fmt.Errorf("explorer: drop views: %w", err)
	}

	// Detach all user databases
	if err := detachUserDatabases(ctx, db); err != nil {
		return fmt.Errorf("explorer: detach databases: %w", err)
	}

	// Clean temp dir for inline sources
	entries, _ := os.ReadDir(s.tempDir)
	for _, e := range entries {
		os.Remove(fmt.Sprintf("%s/%s", s.tempDir, e.Name()))
	}

	// Re-register views from documents
	diags, err := datasource.RegisterViews(ctx, s.session, docs, &datasource.ViewsOptions{
		TempDir: s.tempDir,
	})
	for _, d := range diags {
		s.logger.Warnf("explorer: %v", d)
	}
	if err != nil {
		return fmt.Errorf("explorer: register views: %w", err)
	}

	s.docs = docs
	return nil
}

// WithDB acquires a read lock and provides the underlying *sql.DB to the
// callback. Multiple callers can hold a read lock concurrently.
func (s *Session) WithDB(fn func(*sql.DB) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fn(s.session.DB())
}

// Docs returns the latest set of documents used in the last Refresh call.
func (s *Session) Docs() []config.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.docs
}

// Close releases the DuckDB session and removes temp files.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	os.RemoveAll(s.tempDir)
	if s.session != nil {
		return s.session.Close()
	}
	return nil
}

// dropUserViews drops all non-internal views from the DuckDB session.
func dropUserViews(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT view_name FROM duckdb_views() WHERE NOT internal")
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, name := range names {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP VIEW IF EXISTS %q`, name)); err != nil {
			return err
		}
	}
	return nil
}

// detachUserDatabases detaches all non-system databases from the session.
func detachUserDatabases(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT database_name FROM duckdb_databases() WHERE NOT internal AND database_name != 'memory'")
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, name := range names {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DETACH %s", name)); err != nil {
			// Log but continue — some databases may fail to detach
			continue
		}
	}
	return nil
}
