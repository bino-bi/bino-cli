package dataset

import (
	"fmt"
	"strings"
)

// ValidateDataSetSpec performs the pure semantic checks the JSON schema can't
// express (op-vs-value coherence, alias uniqueness, index-column coherence).
// It returns non-fatal warnings and the first hard error, if any.
//
// The hard checks are also enforced inside buildWrappedQuery / the compilers, so
// broken SQL is never generated even if this is not called; this entry point
// exists for pre-build editor feedback (lint/validate) and to surface warnings.
func ValidateDataSetSpec(spec dataSetSpec) ([]string, error) {
	var warnings []string

	if spec.Filter != nil {
		if err := validateFilterGroup(spec.Filter); err != nil {
			return warnings, err
		}
	}

	// Names that survive into the output: grouped columns + aggregate aliases.
	// Used to detect index-column collisions when groupBy is present (R1).
	outputNames := map[string]bool{}

	if spec.GroupBy != nil {
		if len(spec.GroupBy.Columns) == 0 {
			return warnings, fmt.Errorf("groupBy requires at least one column")
		}
		if len(spec.GroupBy.Aggregates) == 0 {
			return warnings, fmt.Errorf("groupBy requires at least one aggregate")
		}
		for _, c := range spec.GroupBy.Columns {
			if c == "" {
				return warnings, fmt.Errorf("groupBy column must not be empty")
			}
			outputNames[c] = true
		}
		w, err := validateAggregates(spec.GroupBy.Aggregates, outputNames)
		warnings = append(warnings, w...)
		if err != nil {
			return warnings, err
		}
	}

	if len(spec.IndexColumns) > 0 {
		w, err := validateIndexColumns(spec.IndexColumns, spec.GroupBy != nil, outputNames)
		warnings = append(warnings, w...)
		if err != nil {
			return warnings, err
		}
	}

	return warnings, nil
}

// validateFilterGroup recursively validates op-vs-value coherence.
func validateFilterGroup(g *filterGroup) error {
	if g == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(g.Op)) {
	case "", "and", "or":
	default:
		return fmt.Errorf("filter group op must be 'and' or 'or', got %q", g.Op)
	}
	for _, node := range g.Conditions {
		switch {
		case node.Group != nil:
			if err := validateFilterGroup(node.Group); err != nil {
				return err
			}
		case node.Leaf != nil:
			if err := validateCondition(*node.Leaf); err != nil {
				return err
			}
		default:
			return fmt.Errorf("filter node has neither a condition nor a nested group")
		}
	}
	return nil
}

// validateCondition checks one leaf condition's op/value coherence.
func validateCondition(c filterCondition) error {
	if c.Column == "" {
		return fmt.Errorf("filter condition requires a column")
	}
	switch c.Op {
	case "equal", "notEqual":
		if c.Value == nil {
			return nil // becomes IS NULL / IS NOT NULL
		}
		return requireScalar(c)
	case "gt", "gte", "lt", "lte":
		if c.Value == nil {
			return fmt.Errorf("filter op %q on column %q requires a non-null scalar value", c.Op, c.Column)
		}
		return requireScalar(c)
	case "in", "notIn":
		_, err := requireList(c)
		return err
	case "regex":
		if _, ok := c.Value.(string); !ok {
			return fmt.Errorf("filter op 'regex' on column %q requires a string value", c.Column)
		}
		return nil
	default:
		return fmt.Errorf("unknown filter op %q on column %q", c.Op, c.Column)
	}
}

// validateAggregates checks each aggregate's fn/column coherence and that every
// alias is unique across aggregates and the (already-populated) group columns.
func validateAggregates(aggs []aggregate, outputNames map[string]bool) ([]string, error) {
	var warnings []string
	for _, a := range aggs {
		if a.As == "" {
			return warnings, fmt.Errorf("aggregate on column %q requires an 'as' output name", a.Column)
		}
		if outputNames[a.As] {
			return warnings, fmt.Errorf("aggregate output name %q collides with another output column", a.As)
		}
		switch a.Fn {
		case "sum", "avg", "min", "max", "countDistinct", "first", "last":
			if a.Column == "" || a.Column == "*" {
				return warnings, fmt.Errorf("aggregate %q (as %q) requires a column", a.Fn, a.As)
			}
		case "count":
			// count(*) is allowed.
		default:
			return warnings, fmt.Errorf("unknown aggregate fn %q (as %q)", a.Fn, a.As)
		}
		if (a.Fn == "first" || a.Fn == "last") && a.OrderBy == "" {
			warnings = append(warnings, fmt.Sprintf("aggregate %q (as %q) has no orderBy; result is nondeterministic", a.Fn, a.As))
		}
		outputNames[a.As] = true
	}
	return warnings, nil
}

// validateIndexColumns checks each index column's fn/expr coherence and output
// name uniqueness. When groupBy is present, index names must not collide with a
// grouped/aggregate name (R1). It also warns on suspicious index column names.
func validateIndexColumns(cols []indexColumn, hasGroupBy bool, outputNames map[string]bool) ([]string, error) {
	var warnings []string
	seen := map[string]bool{}
	for _, ic := range cols {
		if ic.Column == "" {
			return warnings, fmt.Errorf("indexColumn requires a column name")
		}
		if seen[ic.Column] {
			return warnings, fmt.Errorf("duplicate indexColumn output name %q", ic.Column)
		}
		if hasGroupBy && outputNames[ic.Column] {
			return warnings, fmt.Errorf("indexColumn output name %q collides with a grouped or aggregate column", ic.Column)
		}
		seen[ic.Column] = true

		hasFn := ic.Fn != ""
		hasExpr := ic.Expr != ""
		if hasFn == hasExpr {
			return warnings, fmt.Errorf("indexColumn %q requires exactly one of 'fn' or 'expr'", ic.Column)
		}
		if hasFn {
			switch ic.Fn {
			case "hash":
				if ic.Of == "" {
					return warnings, fmt.Errorf("indexColumn %q with fn 'hash' requires 'of'", ic.Column)
				}
			case "rowNumber", "rank", "denseRank":
				if ic.Over == "" {
					return warnings, fmt.Errorf("indexColumn %q with fn %q requires 'over'", ic.Column, ic.Fn)
				}
			default:
				return warnings, fmt.Errorf("indexColumn %q has unknown fn %q", ic.Column, ic.Fn)
			}
		}

		if !knownIndexColumns[ic.Column] && !strings.HasPrefix(ic.Column, "_") {
			warnings = append(warnings, fmt.Sprintf("indexColumn %q is not a known *Index column and is not underscore-prefixed; check for a typo", ic.Column))
		}
	}
	return warnings, nil
}

// knownIndexColumns is the set of canonical numeric *Index columns (the partner
// columns of the dimension pairs), derived from the dataset schema so it can
// never drift from StandardColumns().
var knownIndexColumns = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range StandardColumns() {
		if c.Pair != "" && c.Kind == ColumnNumber {
			m[c.Name] = true
		}
	}
	return m
}()
