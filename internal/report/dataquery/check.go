package dataquery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// errNotSelect is returned for any statement other than one SELECT.
var errNotSelect = errors.New("only one SELECT statement is allowed; INSERT, UPDATE, DELETE, CREATE, COPY, ATTACH, SET, PRAGMA and other statements are refused")

// parseTables parses sqlText with DuckDB's own parser (json_serialize_sql,
// which runs nothing) and returns the lower-cased names of the tables it
// reads. It refuses anything but one SELECT, every table function, DESCRIBE,
// SUMMARIZE and SHOW, qualified names, and every table that known does not
// name.
func parseTables(ctx context.Context, conn *sql.Conn, sqlText string, known func(string) bool, tables []string) (map[string]bool, error) {
	var raw string
	if err := conn.QueryRowContext(ctx, "SELECT CAST(json_serialize_sql(?) AS VARCHAR)", sqlText).Scan(&raw); err != nil {
		return nil, fmt.Errorf("parse SQL: %w", err)
	}
	var parsed struct {
		Error        bool   `json:"error"`
		ErrorType    string `json:"error_type"`
		ErrorMessage string `json:"error_message"`
		Statements   []any  `json:"statements"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse SQL: %w", err)
	}
	if parsed.Error {
		if parsed.ErrorType == "parser" {
			return nil, fmt.Errorf("Parser Error: %s", parsed.ErrorMessage) //nolint:staticcheck // DuckDB's own error prefix
		}
		return nil, errNotSelect
	}
	switch len(parsed.Statements) {
	case 0:
		return nil, errors.New("sql is empty")
	case 1:
	default:
		return nil, fmt.Errorf("%d statements found; %w", len(parsed.Statements), errNotSelect)
	}
	w := walker{known: known, tables: tables, refs: map[string]bool{}}
	if err := w.walk(parsed.Statements[0], nil); err != nil {
		return nil, err
	}
	return w.refs, nil
}

type walker struct {
	known  func(string) bool
	tables []string
	refs   map[string]bool
}

// walk visits every node of the serialized statement. scope holds the CTE
// names visible at this point; a CTE is only visible after its definition,
// and in its own body only when it is recursive, so a CTE cannot hide a real
// table of the same name.
func (w *walker) walk(node any, scope map[string]bool) error {
	switch n := node.(type) {
	case []any:
		for _, item := range n {
			if err := w.walk(item, scope); err != nil {
				return err
			}
		}
	case map[string]any:
		if cm, ok := n["cte_map"].(map[string]any); ok {
			entries, _ := cm["map"].([]any)
			for _, e := range entries {
				entry, _ := e.(map[string]any)
				name, _ := entry["key"].(string)
				if err := w.walk(entry["value"], scope); err != nil {
					return err
				}
				scope = with(scope, name)
			}
		}
		if _, isExpr := n["class"]; !isExpr {
			typ, _ := n["type"].(string)
			switch typ {
			case "TABLE_FUNCTION":
				fn, _ := n["function"].(map[string]any)
				name, _ := fn["function_name"].(string)
				return fmt.Errorf("table function %s() is not allowed: the SQL may only read the project's DataSources and DataSets by name", name)
			case "SHOW_REF":
				return errors.New("DESCRIBE, SUMMARIZE and SHOW are not allowed; use profile_data or get_columns")
			case "BASE_TABLE":
				if err := w.table(n, scope); err != nil {
					return err
				}
			case "RECURSIVE_CTE_NODE":
				name, _ := n["cte_name"].(string)
				scope = with(scope, name)
			}
		}
		keys := make([]string, 0, len(n))
		for k := range n {
			if k != "cte_map" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := w.walk(n[k], scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *walker) table(n map[string]any, scope map[string]bool) error {
	name, _ := n["table_name"].(string)
	catalog, _ := n["catalog_name"].(string)
	schema, _ := n["schema_name"].(string)
	if catalog != "" || schema != "" {
		return fmt.Errorf("qualified table name %q is not allowed: use the DataSource or DataSet name alone", strings.Join(nonEmpty(catalog, schema, name), "."))
	}
	lower := strings.ToLower(name)
	if scope[lower] {
		return nil
	}
	if !w.known(lower) {
		return fmt.Errorf("unknown table %q: the SQL may only read the project's DataSources and DataSets by name (%s)", name, strings.Join(w.tables, ", "))
	}
	w.refs[lower] = true
	return nil
}

func with(scope map[string]bool, name string) map[string]bool {
	out := make(map[string]bool, len(scope)+1)
	for k := range scope {
		out[k] = true
	}
	out[strings.ToLower(name)] = true
	return out
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
