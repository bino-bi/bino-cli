// Package dataset — declarative data-prep (filter / groupBy / indexColumns).
//
// buildWrappedQuery compiles the declarative spec into a server-side DuckDB
// query that wraps the (already resolved + inline-ref-rewritten) user query in
// up to three nested layers. The pipeline order is fixed and load-bearing:
//
//	filter (WHERE, innermost, pre-aggregation)
//	  -> groupBy (GROUP BY)
//	    -> indexColumns (outermost pure projection: window / hash / custom)
//
// Filter VALUES become bound parameters (?) on the build path, or inline SQL
// literals on the read-only preview path (WrapQueryForPreview); column names go
// through quoteIdent; indexColumns.expr is emitted raw (intentional parity with
// the query/prql trust model). When none of the three are present, the base
// query is returned byte-identical with args == nil so the cache key and
// behavior are unchanged.
package dataset

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	baseAlias    = "_bn_base"
	groupedAlias = "_bn_grouped"
)

// valueEmitter renders a filter value either as a bound parameter (param mode,
// the build path) or as an inline SQL literal (inline mode, the read-only
// preview path whose SQL client cannot bind parameters). In param mode it
// accumulates the bound values in args, in textual placeholder order, exactly
// as the previous direct `?`-building code did.
type valueEmitter struct {
	inline bool
	args   []any
}

// emit renders one scalar filter value. Param mode appends it to args and
// returns "?"; inline mode returns its SQL literal. Used for every emitted
// filter value, including each element of an in/notIn list.
func (e *valueEmitter) emit(v any) (string, error) {
	if e.inline {
		return sqlLiteral(v)
	}
	e.args = append(e.args, v)
	return "?", nil
}

// sqlLiteral renders a scalar value as a self-contained DuckDB SQL literal for
// inline mode. nil never reaches it (equal/notEqual null compile to IS NULL /
// IS NOT NULL before any value is emitted), so it is rejected as a programming
// error. Integer-valued floats render without a decimal point.
func sqlLiteral(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'", nil
	case bool:
		if x {
			return "TRUE", nil
		}
		return "FALSE", nil
	case int:
		return strconv.FormatInt(int64(x), 10), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case json.Number:
		return x.String(), nil
	case nil:
		return "", fmt.Errorf("internal error: nil value reached sqlLiteral")
	default:
		return "", fmt.Errorf("unsupported filter value type %T for inline SQL literal", v)
	}
}

// quoteIdent double-quotes a SQL identifier, escaping embedded quotes.
// Mirrors internal/report/sqlgen/sqlgen.go (unexported there); duplicated to
// avoid a cross-package API change.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// hasTransforms reports whether any declarative data-prep property is present.
func (s dataSetSpec) hasTransforms() bool {
	return s.Filter != nil || s.GroupBy != nil || len(s.IndexColumns) > 0
}

// buildWrappedQuery compiles filter/groupBy/indexColumns into a wrapped SQL query.
//
// Identity early-return: with no transforms it returns baseQuery unchanged and
// nil args (byte-identical, cache-safe). With any transform present while isPRQL
// is true it errors (the base is still PRQL text and cannot be wrapped as a
// subquery). Args accumulate in textual placeholder order (R6); only the filter
// layer emits parameters today, and it is textually first. The returned warnings
// are the non-fatal semantic findings from ValidateDataSetSpec (e.g. R4: a
// nondeterministic first/last without orderBy).
func buildWrappedQuery(spec dataSetSpec, baseQuery string, isPRQL bool) (sql string, args []any, warnings []string, err error) {
	if !spec.hasTransforms() {
		return baseQuery, nil, nil, nil
	}

	if isPRQL {
		return "", nil, nil, fmt.Errorf("filter/groupBy/indexColumns are not supported with prql; use an SQL query instead")
	}

	// Semantic validation (hard checks) before generating any SQL.
	warnings, err = ValidateDataSetSpec(spec)
	if err != nil {
		return "", nil, warnings, err
	}

	emitter := &valueEmitter{inline: false}
	sql, err = wrapQuery(spec, baseQuery, emitter)
	if err != nil {
		return "", nil, warnings, err
	}
	return sql, emitter.args, warnings, nil
}

// WrapQueryForPreview compiles a DataSet's declarative filter/groupBy/index
// transforms into a self-contained SQL string with filter values rendered as
// inline SQL literals (no bound parameters), for the read-only local dev
// preview whose SQL client cannot bind parameters. raw is the document's Raw
// JSON; only the transform fields are read. The no-op identity (no transform)
// returns baseSQL unchanged, and the same PRQL guard as the build path applies
// (any transform with isPRQL is an error). It runs the same hard semantic
// checks as the build but discards warnings.
func WrapQueryForPreview(raw json.RawMessage, baseSQL string, isPRQL bool) (string, error) {
	spec, err := parseDataSetSpec(raw)
	if err != nil {
		return "", err
	}

	if !spec.hasTransforms() {
		return baseSQL, nil
	}
	if isPRQL {
		return "", fmt.Errorf("filter/groupBy/indexColumns are not supported with prql; use an SQL query instead")
	}
	if _, err := ValidateDataSetSpec(spec); err != nil {
		return "", err
	}

	emitter := &valueEmitter{inline: true}
	return wrapQuery(spec, baseSQL, emitter)
}

// wrapQuery is the shared three-layer wrap used by both the build (param mode)
// and preview (inline mode) paths. It assumes spec.hasTransforms() is true and
// the hard semantic checks have already passed; filter values are routed
// through the supplied emitter, which determines param vs inline rendering.
func wrapQuery(spec dataSetSpec, baseQuery string, emitter *valueEmitter) (string, error) {
	// Layer 1: filter -> WHERE over the verbatim base query.
	inner := fmt.Sprintf("(%s) AS %s", baseQuery, baseAlias)
	if spec.Filter != nil {
		pred, err := compileFilter(spec.Filter, baseAlias, emitter)
		if err != nil {
			return "", err
		}
		if pred != "" {
			inner = fmt.Sprintf("SELECT %s.* FROM %s WHERE %s", baseAlias, inner, pred)
		} else {
			inner = fmt.Sprintf("SELECT %s.* FROM %s", baseAlias, inner)
		}
	}

	// Layer 2: groupBy -> GROUP BY. The grouped result is aliased _bn_grouped.
	srcAlias := baseAlias
	if spec.GroupBy != nil {
		selectList, groupClause, err := compileGroupBy(spec.GroupBy, baseAlias)
		if err != nil {
			return "", err
		}
		// When a filter wrapped the base, `inner` is a SELECT statement; wrap it
		// as a subquery so it can be aliased and grouped over.
		if spec.Filter != nil {
			inner = fmt.Sprintf("(%s) AS %s", inner, baseAlias)
		}
		inner = fmt.Sprintf("SELECT %s FROM %s GROUP BY %s", selectList, inner, groupClause)
		srcAlias = groupedAlias
	}

	// Layer 3: indexColumns -> outermost projection.
	if len(spec.IndexColumns) > 0 {
		// `inner` is a SELECT statement whenever a filter or groupBy was applied;
		// wrap it as a subquery aliased with srcAlias so the index exprs can
		// reference its columns.
		if spec.Filter != nil || spec.GroupBy != nil {
			inner = fmt.Sprintf("(%s) AS %s", inner, srcAlias)
		}
		exprs := make([]string, 0, len(spec.IndexColumns))
		for _, ic := range spec.IndexColumns {
			e, err := indexExpr(ic, srcAlias)
			if err != nil {
				return "", err
			}
			exprs = append(exprs, fmt.Sprintf("%s AS %s", e, quoteIdent(ic.Column)))
		}
		inner = fmt.Sprintf("SELECT %s.*, %s FROM %s", srcAlias, strings.Join(exprs, ", "), inner)
	}

	return inner, nil
}

// ValidateSpec runs only the pure semantic checks (op-vs-value coherence, alias
// uniqueness, index-column coherence) on a DataSet document's Raw JSON, for
// lint/editor feedback. A query-only dataset (no transform properties) returns
// no warnings and no error, so it never produces false positives on existing
// datasets.
func ValidateSpec(raw json.RawMessage) (warnings []string, err error) {
	spec, err := parseDataSetSpec(raw)
	if err != nil {
		return nil, err
	}
	return ValidateDataSetSpec(spec)
}

// compileFilter recursively compiles a filter group into a parenthesized SQL
// predicate. Filter values are routed through emitter (bound parameters in
// param mode, inline literals in inline mode). An empty group (no usable
// conditions) yields an empty predicate so the WHERE is omitted.
func compileFilter(g *filterGroup, alias string, emitter *valueEmitter) (pred string, err error) {
	if g == nil || len(g.Conditions) == 0 {
		return "", nil
	}

	var joiner string
	switch strings.ToLower(strings.TrimSpace(g.Op)) {
	case "", "and":
		joiner = " AND "
	case "or":
		joiner = " OR "
	default:
		return "", fmt.Errorf("filter group op must be 'and' or 'or', got %q", g.Op)
	}

	var parts []string
	for _, node := range g.Conditions {
		switch {
		case node.Group != nil:
			sub, subErr := compileFilter(node.Group, alias, emitter)
			if subErr != nil {
				return "", subErr
			}
			if sub != "" {
				parts = append(parts, sub)
			}
		case node.Leaf != nil:
			cond, condErr := compileCondition(*node.Leaf, alias, emitter)
			if condErr != nil {
				return "", condErr
			}
			parts = append(parts, cond)
		default:
			return "", fmt.Errorf("filter node has neither a condition nor a nested group")
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, joiner) + ")", nil
}

// compileCondition compiles a single leaf condition into SQL, routing any value
// through emitter (bound parameter or inline literal per its mode).
func compileCondition(c filterCondition, alias string, emitter *valueEmitter) (pred string, err error) {
	if c.Column == "" {
		return "", fmt.Errorf("filter condition requires a column")
	}
	col := alias + "." + quoteIdent(c.Column)

	switch c.Op {
	case "equal", "notEqual":
		if c.Value == nil {
			if c.Op == "equal" {
				return col + " IS NULL", nil
			}
			return col + " IS NOT NULL", nil
		}
		if err := requireScalar(c); err != nil {
			return "", err
		}
		val, err := emitter.emit(c.Value)
		if err != nil {
			return "", err
		}
		if c.Op == "equal" {
			return col + " = " + val, nil
		}
		return col + " <> " + val, nil
	case "gt", "gte", "lt", "lte":
		if c.Value == nil {
			return "", fmt.Errorf("filter op %q on column %q requires a non-null scalar value", c.Op, c.Column)
		}
		if err := requireScalar(c); err != nil {
			return "", err
		}
		val, err := emitter.emit(c.Value)
		if err != nil {
			return "", err
		}
		return col + " " + comparisonOp(c.Op) + " " + val, nil
	case "in", "notIn":
		list, err := requireList(c)
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			// DuckDB has no IN () — emit the equivalent constant.
			if c.Op == "in" {
				return "FALSE", nil
			}
			return "TRUE", nil
		}
		rendered := make([]string, 0, len(list))
		for _, v := range list {
			val, err := emitter.emit(v)
			if err != nil {
				return "", err
			}
			rendered = append(rendered, val)
		}
		placeholders := strings.Join(rendered, ", ")
		if c.Op == "in" {
			return fmt.Sprintf("%s IN (%s)", col, placeholders), nil
		}
		return fmt.Sprintf("%s NOT IN (%s)", col, placeholders), nil
	case "regex":
		s, ok := c.Value.(string)
		if !ok {
			return "", fmt.Errorf("filter op 'regex' on column %q requires a string value", c.Column)
		}
		val, err := emitter.emit(s)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("regexp_matches(%s, %s)", col, val), nil
	default:
		return "", fmt.Errorf("unknown filter op %q on column %q", c.Op, c.Column)
	}
}

// comparisonOp maps a comparison operator name to its SQL operator.
func comparisonOp(op string) string {
	switch op {
	case "gt":
		return ">"
	case "gte":
		return ">="
	case "lt":
		return "<"
	case "lte":
		return "<="
	}
	return ""
}

// requireScalar rejects array values for scalar operators.
func requireScalar(c filterCondition) error {
	if _, ok := c.Value.([]any); ok {
		return fmt.Errorf("filter op %q on column %q requires a scalar value, got an array", c.Op, c.Column)
	}
	return nil
}

// requireList validates an in/notIn value as a non-null-element array.
func requireList(c filterCondition) ([]any, error) {
	list, ok := c.Value.([]any)
	if !ok {
		return nil, fmt.Errorf("filter op %q on column %q requires an array value", c.Op, c.Column)
	}
	for _, v := range list {
		if v == nil {
			// NOT IN with NULL silently drops all rows; IN with NULL is misleading.
			return nil, fmt.Errorf("filter op %q on column %q must not contain null list elements", c.Op, c.Column)
		}
	}
	return list, nil
}

// compileGroupBy compiles a GROUP BY layer into its SELECT list and GROUP BY
// clause. Non-grouped, non-aggregated columns are dropped (true GROUP BY).
func compileGroupBy(g *groupBy, alias string) (selectList, groupByClause string, err error) {
	if g == nil || len(g.Columns) == 0 {
		return "", "", fmt.Errorf("groupBy requires at least one column")
	}
	if len(g.Aggregates) == 0 {
		return "", "", fmt.Errorf("groupBy requires at least one aggregate")
	}

	cols := make([]string, 0, len(g.Columns))
	groupCols := make([]string, 0, len(g.Columns))
	for _, c := range g.Columns {
		if c == "" {
			return "", "", fmt.Errorf("groupBy column must not be empty")
		}
		cols = append(cols, alias+"."+quoteIdent(c))
		groupCols = append(groupCols, alias+"."+quoteIdent(c))
	}

	selectParts := append([]string{}, cols...)
	for _, a := range g.Aggregates {
		expr, err := aggregateExpr(a, alias)
		if err != nil {
			return "", "", err
		}
		selectParts = append(selectParts, fmt.Sprintf("%s AS %s", expr, quoteIdent(a.As)))
	}

	return strings.Join(selectParts, ", "), strings.Join(groupCols, ", "), nil
}

// aggregateExpr compiles a single aggregate expression (without its alias).
func aggregateExpr(a aggregate, alias string) (string, error) {
	if a.As == "" {
		return "", fmt.Errorf("aggregate on column %q requires an 'as' output name", a.Column)
	}
	colExpr := alias + "." + quoteIdent(a.Column)

	switch a.Fn {
	case "sum", "avg", "min", "max":
		if a.Column == "" || a.Column == "*" {
			return "", fmt.Errorf("aggregate %q (as %q) requires a column", a.Fn, a.As)
		}
		return fmt.Sprintf("%s(%s)", a.Fn, colExpr), nil
	case "count":
		if a.Column == "" || a.Column == "*" {
			return "count(*)", nil
		}
		return fmt.Sprintf("count(%s)", colExpr), nil
	case "countDistinct":
		if a.Column == "" || a.Column == "*" {
			return "", fmt.Errorf("aggregate 'countDistinct' (as %q) requires a column", a.As)
		}
		return fmt.Sprintf("count(DISTINCT %s)", colExpr), nil
	case "first", "last":
		if a.Column == "" || a.Column == "*" {
			return "", fmt.Errorf("aggregate %q (as %q) requires a column", a.Fn, a.As)
		}
		if a.OrderBy != "" {
			order := alias + "." + quoteIdent(a.OrderBy)
			if a.OrderDesc {
				order += " DESC"
			}
			return fmt.Sprintf("%s(%s ORDER BY %s)", a.Fn, colExpr, order), nil
		}
		return fmt.Sprintf("%s(%s)", a.Fn, colExpr), nil
	default:
		return "", fmt.Errorf("unknown aggregate fn %q (as %q)", a.Fn, a.As)
	}
}

// indexExpr compiles a single index column into its SQL expression (without the
// output alias). srcAlias is the alias of the layer the expression reads from.
func indexExpr(ic indexColumn, srcAlias string) (string, error) {
	if ic.Column == "" {
		return "", fmt.Errorf("indexColumn requires a column name")
	}

	hasFn := ic.Fn != ""
	hasExpr := ic.Expr != ""
	if hasFn == hasExpr {
		return "", fmt.Errorf("indexColumn %q requires exactly one of 'fn' or 'expr'", ic.Column)
	}

	if hasExpr {
		return ic.Expr, nil
	}

	switch ic.Fn {
	case "hash":
		if ic.Of == "" {
			return "", fmt.Errorf("indexColumn %q with fn 'hash' requires 'of'", ic.Column)
		}
		return fmt.Sprintf("hash(%s.%s)", srcAlias, quoteIdent(ic.Of)), nil
	case "rowNumber", "rank", "denseRank":
		if ic.Over == "" {
			return "", fmt.Errorf("indexColumn %q with fn %q requires 'over'", ic.Column, ic.Fn)
		}
		var b strings.Builder
		b.WriteString(windowFn(ic.Fn))
		b.WriteString("() OVER (")
		if len(ic.Partition) > 0 {
			parts := make([]string, 0, len(ic.Partition))
			for _, p := range ic.Partition {
				if p == "" {
					return "", fmt.Errorf("indexColumn %q partition column must not be empty", ic.Column)
				}
				parts = append(parts, srcAlias+"."+quoteIdent(p))
			}
			b.WriteString("PARTITION BY ")
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString(" ")
		}
		b.WriteString("ORDER BY ")
		b.WriteString(srcAlias)
		b.WriteString(".")
		b.WriteString(quoteIdent(ic.Over))
		if ic.OverDesc {
			b.WriteString(" DESC")
		}
		b.WriteString(")")
		return b.String(), nil
	default:
		return "", fmt.Errorf("indexColumn %q has unknown fn %q", ic.Column, ic.Fn)
	}
}

// windowFn maps an index window-function name to its SQL function name.
func windowFn(fn string) string {
	switch fn {
	case "rowNumber":
		return "row_number"
	case "rank":
		return "rank"
	case "denseRank":
		return "dense_rank"
	}
	return ""
}
