// Package datasummary runs DuckDB SUMMARIZE statements. The preview's data
// explorer and the MCP profile_data tool share it.
package datasummary

import (
	"context"
	"database/sql"
	"fmt"
)

// Column is one result column and its DuckDB type.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Querier is what Summarize needs: a *sql.DB or a *sql.Conn.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// TableSQL summarizes a table or view by name.
func TableSQL(name string) string {
	return fmt.Sprintf("SUMMARIZE SELECT * FROM %q", name)
}

// QuerySQL summarizes the result of a query.
func QuerySQL(query string) string {
	return fmt.Sprintf("SUMMARIZE (%s)", query)
}

// Summarize runs summarizeSQL and returns its columns and rows. Every value is
// passed through normalize, so callers choose their own JSON shape.
func Summarize(ctx context.Context, db Querier, summarizeSQL string, normalize func(any) any) ([]Column, [][]any, error) {
	rows, err := db.QueryContext(ctx, summarizeSQL)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	cols := make([]Column, len(colTypes))
	for i, ct := range colTypes {
		cols[i] = Column{Name: ct.Name(), Type: ct.DatabaseTypeName()}
	}

	out := make([][]any, 0)
	for rows.Next() {
		vals := make([]any, len(colTypes))
		ptrs := make([]any, len(colTypes))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		normalized := make([]any, len(vals))
		for i, v := range vals {
			normalized[i] = normalize(v)
		}
		out = append(out, normalized)
	}
	return cols, out, rows.Err()
}
