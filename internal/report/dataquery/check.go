package dataquery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// plainIdent is the only form a CTE name may take, so a CTE can never carry
// the name of a file.
var plainIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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
				if !plainIdent.MatchString(name) {
					return fmt.Errorf("CTE name %q is not allowed: use letters, digits and underscores only", name)
				}
				if err := w.walk(entry["value"], scope); err != nil {
					return err
				}
				scope = with(scope, name)
			}
		}
		if n["class"] == "FUNCTION" {
			if err := checkGenerator(n); err != nil {
				return err
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
				// DuckDB binds the anchor (left) without the CTE in scope: a
				// name there is a real table or a file, never the CTE itself.
				if err := w.walk(n["left"], scope); err != nil {
					return err
				}
				name, _ := n["cte_name"].(string)
				scope = with(scope, name)
			}
		}
		keys := make([]string, 0, len(n))
		recursive := n["type"] == "RECURSIVE_CTE_NODE"
		for k := range n {
			if k != "cte_map" && (!recursive || k != "left") {
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

const (
	// maxGeneratedItems bounds the lists range, generate_series, list_resize
	// and repeat build: DuckDB builds such a list in one vector, outside its
	// memory limit and without a point where it can be interrupted.
	maxGeneratedItems = 10_000_000
	// maxGeneratedBytes bounds the strings repeat, lpad and rpad build.
	maxGeneratedBytes = 1 << 20
)

// checkGenerator refuses calls that build a list or a string whose size an
// argument sets, unless that argument is a constant (literals, casts and
// + - * / of them) and the result stays under the limits. The size of a
// computed value cannot be judged before it runs, so it is refused.
func checkGenerator(fn map[string]any) error {
	name, _ := fn["function_name"].(string)
	args, _ := fn["arguments"].([]any)
	needConst := func(what string) error {
		return fmt.Errorf("%s() is refused: its %s must be a constant (a literal, a cast of one, or + - * / of them)", name, what)
	}
	switch strings.ToLower(name) {
	case "range", "generate_series":
		nums := make([]float64, len(args))
		for i, a := range args {
			f, ok := fold(a)
			if !ok {
				return needConst("arguments")
			}
			nums[i] = f
		}
		if items, ok := rangeItems(nums); ok && items > maxGeneratedItems {
			return fmt.Errorf("%s() with %.0f items is refused: at most %d are allowed", name, items, maxGeneratedItems)
		}
	case "list_resize", "array_resize":
		if len(args) < 2 {
			return nil
		}
		n, ok := fold(args[1])
		if !ok {
			return needConst("size")
		}
		if n > maxGeneratedItems {
			return fmt.Errorf("%s() to %.0f items is refused: at most %d are allowed", name, n, maxGeneratedItems)
		}
	case "lpad", "rpad":
		if len(args) < 2 {
			return nil
		}
		n, ok := fold(args[1])
		if !ok {
			return needConst("length")
		}
		if n > maxGeneratedBytes {
			return fmt.Errorf("%s() to %.0f bytes is refused: at most %d are allowed", name, n, maxGeneratedBytes)
		}
	case "repeat":
		if len(args) != 2 {
			return nil
		}
		if _, ok := fold(args[1]); !ok {
			return needConst("count")
		}
		size, items, ok := built(fn)
		if !ok {
			return fmt.Errorf("repeat() is refused: it may only repeat a constant text or list, or another repeat, lpad or rpad of one")
		}
		if size > maxGeneratedBytes || items > maxGeneratedItems {
			return fmt.Errorf("repeat() building %.0f bytes or items is refused: at most %d bytes of text or %d list items are allowed", max(size, items), maxGeneratedBytes, maxGeneratedItems)
		}
	}
	return nil
}

// rangeItems is the number of items range(stop), range(start, stop) or
// range(start, stop, step) yields.
func rangeItems(nums []float64) (float64, bool) {
	start, step := 0.0, 1.0
	var stop float64
	switch len(nums) {
	case 1:
		stop = nums[0]
	case 2:
		start, stop = nums[0], nums[1]
	case 3:
		start, stop, step = nums[0], nums[1], nums[2]
	default:
		return 0, false
	}
	if step == 0 {
		return 0, false
	}
	return (stop - start) / step, true
}

// built returns the bytes of text or the list items e builds, when e is a
// constant text or list, or repeat, lpad or rpad of one with constant sizes.
func built(e any) (size, items float64, ok bool) {
	m := unwrap(e)
	switch m["class"] {
	case "CONSTANT":
		lit, _ := m["literal"].(map[string]any)
		text, _ := lit["text"].(string)
		return float64(len(text)), 0, true
	case "CAST":
		return built(m["child"])
	case "FUNCTION":
		name, _ := m["function_name"].(string)
		args, _ := m["arguments"].([]any)
		switch strings.ToLower(name) {
		case "list_value":
			for _, a := range args {
				if _, _, ok := built(a); !ok {
					return 0, 0, false
				}
			}
			return 0, float64(len(args)), true
		case "repeat":
			if len(args) != 2 {
				return 0, 0, false
			}
			n, okN := fold(args[1])
			s, it, okS := built(args[0])
			return s * n, it * n, okN && okS
		case "lpad", "rpad":
			if len(args) < 2 {
				return 0, 0, false
			}
			n, okN := fold(args[1])
			return n, 0, okN
		}
	}
	return 0, 0, false
}

// fold evaluates a constant number: a literal, a cast of one, a unary minus
// or + - * / of constants.
func fold(e any) (float64, bool) {
	m := unwrap(e)
	switch m["class"] {
	case "CONSTANT":
		lit, _ := m["literal"].(map[string]any)
		text, _ := lit["text"].(string)
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		return f, err == nil
	case "CAST":
		return fold(m["child"])
	case "FUNCTION":
		if op, _ := m["is_operator"].(bool); !op {
			return 0, false
		}
		name, _ := m["function_name"].(string)
		args, _ := m["arguments"].([]any)
		vals := make([]float64, len(args))
		for i, a := range args {
			v, ok := fold(a)
			if !ok {
				return 0, false
			}
			vals[i] = v
		}
		switch {
		case len(vals) == 1 && name == "-":
			return -vals[0], true
		case len(vals) == 1 && name == "+":
			return vals[0], true
		case len(vals) != 2:
			return 0, false
		}
		switch name {
		case "+":
			return vals[0] + vals[1], true
		case "-":
			return vals[0] - vals[1], true
		case "*":
			return vals[0] * vals[1], true
		case "/", "//":
			if vals[1] == 0 {
				return 0, false
			}
			return vals[0] / vals[1], true
		}
	}
	return 0, false
}

// unwrap returns the expression of a serialized function argument.
func unwrap(a any) map[string]any {
	m, _ := a.(map[string]any)
	if e, ok := m["expression"].(map[string]any); ok {
		return e
	}
	return m
}
