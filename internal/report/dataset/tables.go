package dataset

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// ReadTables returns the tables a DataSet's SQL query reads, taken from the
// query as it runs ($file resolved, @inline(N) rewritten), minus the names of
// its CTEs. ok is false when the reads cannot be known: a source or PRQL
// dataset, a query that cannot be resolved, or SQL DuckDB cannot serialize
// (a syntax error, or a statement other than SELECT such as PIVOT).
//
// The query is parsed on conn with json_serialize_sql, which does not bind, so
// the tables need not exist. duckdb.GetTableNames is not used: on DuckDB v1.4.4
// it returns no names at all for a query with a JOIN ... USING clause.
//
// A qualified name counts only in the default catalog and schema, where the
// DataSource views live. bino_shift and query_table calls with a literal first
// argument count as reads of that table. Names are sorted and case-insensitively
// unique.
func ReadTables(ctx context.Context, conn *sql.Conn, doc config.Document) ([]string, bool) {
	spec, err := parseDataSetSpec(doc.Raw)
	if err != nil || spec.Source != "" || !spec.Prql.IsEmpty() {
		return nil, false
	}
	query, err := spec.Query.ResolveQuery(filepath.Dir(doc.File))
	if err != nil || query == "" {
		return nil, false
	}
	if HasInlineRefs(query) {
		if query, err = RewriteInlineRefs(query, spec.Dependencies); err != nil {
			return nil, false
		}
	}

	var serialized string
	if err := conn.QueryRowContext(ctx, "SELECT json_serialize_sql(?::VARCHAR)::VARCHAR", query).Scan(&serialized); err != nil {
		return nil, false
	}
	var tree map[string]any
	if err := json.Unmarshal([]byte(serialized), &tree); err != nil || tree["error"] != false {
		return nil, false
	}

	var refs tableRefs
	refs.walk(tree)
	return refs.tables(), true
}

// tableRefs collects the table references of a serialized statement tree.
type tableRefs struct {
	unqualified []string // may name a CTE
	qualified   []string // default catalog and schema only
	ctes        []string
}

func (r *tableRefs) walk(node any) {
	switch n := node.(type) {
	case []any:
		for _, v := range n {
			r.walk(v)
		}
	case map[string]any:
		switch n["type"] {
		case "BASE_TABLE":
			r.addBaseTable(n)
		case "TABLE_FUNCTION":
			r.addTableFunction(n)
		}
		if cteMap, ok := n["cte_map"].(map[string]any); ok {
			entries, _ := cteMap["map"].([]any)
			for _, e := range entries {
				entry, _ := e.(map[string]any)
				if key, ok := entry["key"].(string); ok {
					r.ctes = append(r.ctes, key)
				}
			}
		}
		for _, v := range n {
			r.walk(v)
		}
	}
}

func (r *tableRefs) addBaseTable(n map[string]any) {
	name, _ := n["table_name"].(string)
	schema, _ := n["schema_name"].(string)
	catalog, _ := n["catalog_name"].(string)
	switch {
	case name == "":
	case schema == "" && catalog == "":
		r.unqualified = append(r.unqualified, name)
	case (schema == "" || strings.EqualFold(schema, "main")) && (catalog == "" || strings.EqualFold(catalog, "memory")):
		r.qualified = append(r.qualified, name)
	}
}

func (r *tableRefs) addTableFunction(n map[string]any) {
	fn, _ := n["function"].(map[string]any)
	name, _ := fn["function_name"].(string)
	if !strings.EqualFold(name, duckdb.ShiftMacroName) && !strings.EqualFold(name, "query_table") {
		return
	}
	args, _ := fn["children"].([]any)
	if len(args) == 0 {
		return
	}
	arg, _ := args[0].(map[string]any)
	value, _ := arg["value"].(map[string]any)
	if table, ok := value["value"].(string); ok && arg["type"] == "VALUE_CONSTANT" {
		r.unqualified = append(r.unqualified, table)
	}
}

func (r *tableRefs) tables() []string {
	names := slices.Clone(r.qualified)
	for _, name := range r.unqualified {
		if !slices.ContainsFunc(r.ctes, func(cte string) bool { return strings.EqualFold(cte, name) }) {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(strings.Compare(strings.ToLower(a), strings.ToLower(b)), strings.Compare(a, b))
	})
	return slices.CompactFunc(names, strings.EqualFold)
}
