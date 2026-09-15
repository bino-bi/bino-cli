package dataset

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Constant columns
//
// A DataSet may declare spec.constants: values that are the same on every
// row. Every key becomes a `_`-prefixed column, materialized in Go after the
// query has run so one path serves query, prql and source datasets alike.
//
// Column names inside the `_` namespace that start with `_spec_`
// (`_spec_<kind>_<field>`, `_spec_any_<field>`) are dataset defaults (see
// defaults.go) and must not be used as custom passthrough columns. A list of
// objects under constants.spec produces the index-column form the fold reads
// back (`_spec_table_thereof_0_rowGroup`).

// constantKeyPattern is the key rule: camelCase without underscores, because
// `_` is the path separator of the flattened column name.
var constantKeyPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// Constant is one flattened spec.constants entry.
type Constant struct {
	// Path is the key path inside spec.constants, dotted, with [i] for a
	// list index: "spec.table.thereof[0].rowGroup".
	Path string
	// Column is the row column the value is written to: "_spec_table_thereof_0_rowGroup".
	Column string
	// Value is the cell value, as decoded from the manifest.
	Value any
}

// FlattenConstants turns spec.constants into the columns it produces, sorted
// by column name.
//
//   - a scalar (string, number, bool, null) is one column and keeps its type
//   - a list of scalars is one column holding the comma-joined string
//   - an object flattens by key, a list that contains an object or a list
//     flattens by zero-based index: [1, {a: 2}] -> _k_0 = 1, _k_1_a = 2
//
// Keys must match ^[a-z][A-Za-z0-9]*$; any other key is an error.
func FlattenConstants(constants map[string]any) ([]Constant, error) {
	var out []Constant
	if err := flattenObject(constants, "", "", &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Column < out[j].Column })
	return out, nil
}

func flattenObject(obj map[string]any, path, column string, out *[]Constant) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !constantKeyPattern.MatchString(k) {
			where := k
			if path != "" {
				where = path + "." + k
			}
			return fmt.Errorf("constants.%s: key %q must be camelCase without underscores (^[a-z][A-Za-z0-9]*$)", where, k)
		}
		p := k
		if path != "" {
			p = path + "." + k
		}
		if err := flattenValue(obj[k], p, column+"_"+k, out); err != nil {
			return err
		}
	}
	return nil
}

func flattenValue(v any, path, column string, out *[]Constant) error {
	switch val := v.(type) {
	case map[string]any:
		return flattenObject(val, path, column, out)
	case []any:
		if isScalarList(val) {
			*out = append(*out, Constant{Path: path, Column: column, Value: joinScalars(val)})
			return nil
		}
		for i, item := range val {
			if err := flattenValue(item, fmt.Sprintf("%s[%d]", path, i), column+"_"+strconv.Itoa(i), out); err != nil {
				return err
			}
		}
		return nil
	case map[any]any:
		return fmt.Errorf("constants.%s: keys must be strings", path)
	default:
		*out = append(*out, Constant{Path: path, Column: column, Value: v})
		return nil
	}
}

func isScalarList(list []any) bool {
	for _, item := range list {
		switch item.(type) {
		case map[string]any, map[any]any, []any:
			return false
		}
	}
	return true
}

func joinScalars(list []any) string {
	parts := make([]string, len(list))
	for i, item := range list {
		parts[i] = valueToString(item)
	}
	return strings.Join(parts, ",")
}

// StampConstants writes every constant onto every row and appends the new
// column names to columns. rows may be nil for callers that only list
// columns. It returns the constants whose column the rows already had: the
// constant has overwritten the query's value.
func StampConstants(rows []map[string]any, columns []string, constants []Constant) (cols []string, shadowed []Constant) {
	if len(constants) == 0 {
		return columns, nil
	}
	have := make(map[string]bool, len(columns))
	for _, c := range columns {
		have[c] = true
	}
	cols = columns
	for _, c := range constants {
		if have[c.Column] {
			shadowed = append(shadowed, c)
		} else {
			cols = append(cols, c.Column)
			have[c.Column] = true
		}
		for _, row := range rows {
			row[c.Column] = c.Value
		}
	}
	return cols, shadowed
}
