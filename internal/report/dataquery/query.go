package dataquery

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	duckdbdrv "github.com/duckdb/duckdb-go/v2"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasummary"
)

const (
	// DefaultLimit is the number of rows query_data returns when no limit is given.
	DefaultLimit = 100
	// MaxLimit is the largest limit query_data accepts.
	MaxLimit = 1000
	// queryCallTimeout bounds one query_data call, setup included.
	queryCallTimeout = 60 * time.Second
)

// QueryResult is the result of one query_data call.
type QueryResult struct {
	Columns   []datasummary.Column `json:"columns"`
	Rows      [][]any              `json:"rows"`
	Limit     int                  `json:"limit"`
	Truncated bool                 `json:"truncated"`
}

// Query runs one SELECT over the DataSources and DataSets in docs (the
// documents resolved for introspection) and returns at most limit rows. The
// SQL reads DataSources and DataSets by name; a DataSet becomes a view only
// when the SQL names it. Errors from DuckDB come back with its own message.
func Query(ctx context.Context, docs []config.Document, ambiguous config.Ambiguities, sqlText string, limit int) (QueryResult, error) {
	res := QueryResult{Columns: []datasummary.Column{}, Rows: [][]any{}}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return res, fmt.Errorf("limit %d is above the maximum of %d", limit, MaxLimit)
	}
	res.Limit = limit
	sqlText = strings.TrimRight(strings.TrimSpace(sqlText), "; \t\r\n")

	ctx, cancel := context.WithTimeout(ctx, queryCallTimeout)
	defer cancel()
	cat := newCatalog(docs)
	s, err := openSession(ctx)
	if err != nil {
		return res, err
	}
	defer s.Close()

	refs, err := parseTables(ctx, s.conn, sqlText, cat.known, cat.names())
	if err != nil {
		return res, ambiguous.Explain(err)
	}
	wrapped := fmt.Sprintf("SELECT * FROM (\n%s\n) AS _bino_q LIMIT %d", sqlText, limit+1)
	if _, err := parseTables(ctx, s.conn, wrapped, cat.known, cat.names()); err != nil {
		return res, err
	}

	names := make([]string, 0, len(refs))
	for n := range refs {
		names = append(names, n)
	}
	slices.Sort(names)
	targets := make([]*config.Document, 0, len(names))
	for _, n := range names {
		targets = append(targets, cat.entries[n].doc)
	}
	srcs, err := s.prepare(ctx, docs, ambiguous, targets)
	if err != nil {
		return res, err
	}
	for _, t := range targets {
		if t.Kind != "DataSet" {
			continue
		}
		view := fmt.Sprintf("CREATE VIEW %s AS (\n%s\n)", quoteIdent(t.Name), srcs[strings.ToLower(t.Name)])
		if _, err := s.conn.ExecContext(ctx, view); err != nil {
			return res, ambiguous.Explain(fmt.Errorf("DataSet %s: %w", t.Name, err))
		}
	}
	if err := s.lock(ctx); err != nil {
		return res, err
	}
	for _, q := range []string{sqlText, wrapped} {
		if err := requireSelect(ctx, s.conn, q); err != nil {
			return res, ambiguous.Explain(err)
		}
	}

	rows, err := s.conn.QueryContext(ctx, wrapped)
	if err != nil {
		return res, ambiguous.Explain(err)
	}
	defer rows.Close()
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return res, err
	}
	for _, ct := range colTypes {
		res.Columns = append(res.Columns, datasummary.Column{Name: ct.Name(), Type: ct.DatabaseTypeName()})
	}
	for rows.Next() {
		if len(res.Rows) == limit {
			res.Truncated = true
			break
		}
		vals := make([]any, len(colTypes))
		ptrs := make([]any, len(colTypes))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return res, err
		}
		for i, v := range vals {
			vals[i] = normalize(v)
		}
		res.Rows = append(res.Rows, vals)
	}
	return res, rows.Err()
}

// requireSelect prepares q, which binds it without running it, and accepts
// it only when DuckDB reports a SELECT statement. q must be one statement:
// the driver runs every statement before the last one while preparing.
func requireSelect(ctx context.Context, conn *sql.Conn, q string) error {
	return conn.Raw(func(dc any) error {
		c, ok := dc.(*duckdbdrv.Conn)
		if !ok {
			return fmt.Errorf("unexpected driver connection %T", dc)
		}
		ds, err := c.PrepareContext(ctx, q)
		if err != nil {
			return err
		}
		defer ds.Close() //nolint:errcheck // closing a prepared statement that was never run
		stmt, ok := ds.(*duckdbdrv.Stmt)
		if !ok {
			return fmt.Errorf("unexpected driver statement %T", ds)
		}
		typ, err := stmt.StatementType()
		if err != nil {
			return err
		}
		if typ != duckdbdrv.STATEMENT_TYPE_SELECT {
			return errNotSelect
		}
		return nil
	})
}
