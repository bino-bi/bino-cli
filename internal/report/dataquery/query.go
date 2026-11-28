package dataquery

import (
	"context"
	"database/sql"
	"encoding/json"
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
)

// queryCallTimeout bounds one query_data call, setup included. When it ends,
// the driver interrupts the running statement. A var so tests can shorten it.
var queryCallTimeout = 60 * time.Second

// QueryResult is the result of one query_data call.
type QueryResult struct {
	Columns   []datasummary.Column `json:"columns"`
	Rows      [][]any              `json:"rows"`
	Limit     int                  `json:"limit"`
	Truncated bool                 `json:"truncated"`
	// Notes says when rows or cells were cut to keep the result small.
	Notes []string `json:"notes,omitempty"`
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
		return res, explain(ambiguous, err)
	}
	wrapped := fmt.Sprintf("SELECT * FROM (\n%s\n) AS _bino_q LIMIT %d", sqlText, limit+1) //nolint:gosec // G201: sqlText is checked above to be one SELECT over known tables, and again after wrapping
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
			return res, explain(ambiguous, fmt.Errorf("DataSet %s: %w", t.Name, err))
		}
	}
	if err := s.lock(ctx); err != nil {
		return res, err
	}
	for _, q := range []string{sqlText, wrapped} {
		if err := requireSelect(ctx, s.conn, q); err != nil {
			return res, explain(ambiguous, err)
		}
	}

	if err := checkPlan(ctx, s.conn, wrapped, targets, srcs); err != nil {
		return res, explain(ambiguous, err)
	}

	rows, err := s.conn.QueryContext(ctx, wrapped)
	if err != nil {
		return res, explain(ambiguous, err)
	}
	defer rows.Close()
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return res, err
	}
	for _, ct := range colTypes[:min(len(colTypes), maxColumns)] {
		res.Columns = append(res.Columns, datasummary.Column{Name: ct.Name(), Type: ct.DatabaseTypeName()})
	}
	if len(colTypes) > maxColumns {
		res.Notes = append(res.Notes, fmt.Sprintf("only the first %d of %d columns are returned", maxColumns, len(colTypes)))
	}
	size, cutCells := 0, 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return res, err
		}
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
		row := vals[:len(res.Columns)]
		full := false
		for i, v := range row {
			if full {
				row[i] = cutMark
				cutCells++
				continue
			}
			var cut bool
			var n int
			row[i], n, cut = capCell(normalizeTyped(v, res.Columns[i].Type))
			size += n
			if cut {
				cutCells++
			}
			full = size > maxResultBytes
		}
		res.Rows = append(res.Rows, row)
		if full {
			res.Truncated = true
			res.Notes = append(res.Notes, fmt.Sprintf("rows stop after %d: the result is larger than %d bytes; select fewer or narrower columns", len(res.Rows), maxResultBytes))
			break
		}
	}
	if cutCells > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("%d cells were cut to at most %d bytes and end in %q", cutCells, maxCellBytes, cutMark))
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

// checkPlan compares the scans in DuckDB's bound plan of q with the scans of
// the targets on their own: q may not read through a function (a file
// reader, a catalog function) that none of the targets reads through. This
// catches any name the SQL check let through but DuckDB resolved to
// something else.
func checkPlan(ctx context.Context, conn *sql.Conn, q string, targets []*config.Document, srcs map[string]string) error {
	allowed := map[string]bool{}
	for _, t := range targets {
		fns, err := planFunctions(ctx, conn, "SELECT * FROM (\n"+srcs[strings.ToLower(t.Name)]+"\n) AS _bino_b")
		if err != nil {
			return err
		}
		for fn := range fns {
			allowed[fn] = true
		}
	}
	fns, err := planFunctions(ctx, conn, q)
	if err != nil {
		return err
	}
	for fn := range fns {
		if !allowed[fn] {
			return fmt.Errorf("the query reads through %s, which none of the DataSources and DataSets it names does: the SQL may only read the project's DataSources and DataSets by name", strings.ToLower(fn))
		}
	}
	return nil
}

// planFunctions returns the table functions in the plan of q. EXPLAIN binds
// and plans q without running it.
func planFunctions(ctx context.Context, conn *sql.Conn, q string) (map[string]bool, error) {
	var key, plan string
	if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+q).Scan(&key, &plan); err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal([]byte(plan), &tree); err != nil {
		return nil, fmt.Errorf("read query plan: %w", err)
	}
	out := map[string]bool{}
	var visit func(any)
	visit = func(n any) {
		switch v := n.(type) {
		case []any:
			for _, c := range v {
				visit(c)
			}
		case map[string]any:
			if info, ok := v["extra_info"].(map[string]any); ok {
				if fn, ok := info["Function"].(string); ok {
					out[fn] = true
				}
			}
			visit(v["children"])
		}
	}
	visit(tree)
	return out, nil
}
