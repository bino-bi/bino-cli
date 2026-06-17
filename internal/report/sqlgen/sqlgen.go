// Package sqlgen generates column-aware SQL SELECT statements from introspected
// schema information. It is stdlib-only so it can be shared by the CLI, the
// daemon, and the lsp-helper without dragging in cobra or the report engine.
package sqlgen

import (
	"fmt"
	"regexp"
	"strings"
)

// CastMode controls when TypedSelect emits an explicit ::TYPE cast for a column.
type CastMode int

const (
	// CastAmbiguousOnly (the default) emits a cast only when a column carries an
	// explicit TargetType that differs from its introspected Type. This keeps
	// strongly-typed sources (databases, typed parquet) free of redundant casts
	// that could silently lose precision, while still honoring user-requested
	// type changes made in the wizard column/type table.
	CastAmbiguousOnly CastMode = iota
	// CastNever never emits a cast (pure projection + aliasing).
	CastNever
	// CastAlways casts every column to its TargetType (or its introspected Type
	// when no TargetType is set), making the resulting schema fully explicit.
	CastAlways
)

// Column describes a single output column for TypedSelect.
type Column struct {
	// Name is the physical source column name as returned by introspection.
	Name string `json:"name"`
	// Alias is the desired output column name. When empty, a safe alias is
	// derived from Name via CleanAlias.
	Alias string `json:"alias,omitempty"`
	// Type is the introspected DuckDB type of the source column (informational;
	// used to decide whether a TargetType cast is redundant).
	Type string `json:"type,omitempty"`
	// TargetType is the type the user wants this column cast to. When set and
	// the CastMode calls for it, the column is emitted as Name::TargetType.
	TargetType string `json:"targetType,omitempty"`
	// Expr, when non-empty, is a raw SQL expression emitted verbatim as the
	// column's source (e.g. a constant '+' or an expression sum("amount")).
	// It takes precedence over Name/TargetType — no quoting or cast is applied —
	// and is always aliased. Used by the schema-driven mapper for constant and
	// expression columns.
	Expr string `json:"expr,omitempty"`
}

// ParseCastMode maps a string (as sent by the wizard/CLI) to a CastMode.
// Unknown values fall back to the default, CastAmbiguousOnly.
func ParseCastMode(s string) CastMode {
	switch s {
	case "never":
		return CastNever
	case "always":
		return CastAlways
	default:
		return CastAmbiguousOnly
	}
}

// Options tunes TypedSelect output.
type Options struct {
	// CastMode selects when explicit casts are emitted. The zero value is
	// CastAmbiguousOnly.
	CastMode CastMode
	// Pretty produces a multi-line, aligned SELECT. When false the statement is
	// emitted on a single line.
	Pretty bool
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// TypedSelect builds a column-aware SELECT statement against source.
// It returns the SQL string and the final, de-duplicated output aliases in
// column order. Physical column and source names are always double-quoted for
// safety; aliases are emitted unquoted because CleanAlias guarantees a valid
// identifier. An AS clause is only emitted when the output name differs from the
// physical name or a cast is applied, keeping the output uncluttered.
func TypedSelect(source string, cols []Column, opts Options) (sql string, aliases []string) {
	aliases = resolveAliases(cols)

	type item struct {
		expr     string
		alias    string
		hasAlias bool
	}
	items := make([]item, len(cols))
	maxExpr := 0
	for i, col := range cols {
		var expr string
		hasAlias := false
		if raw := strings.TrimSpace(col.Expr); raw != "" {
			// Raw expression (constant or expression column): emit verbatim, always aliased.
			expr = raw
			hasAlias = true
		} else {
			expr = quoteIdent(col.Name)
			castType, doCast := castFor(col, opts.CastMode)
			if doCast {
				expr += "::" + castType
			}
			hasAlias = doCast || aliases[i] != col.Name
		}
		alias := aliases[i]
		items[i] = item{expr: expr, alias: alias, hasAlias: hasAlias}
		if hasAlias && len(expr) > maxExpr {
			maxExpr = len(expr)
		}
	}

	from := "FROM " + quoteIdent(source)

	if !opts.Pretty {
		parts := make([]string, len(items))
		for i, it := range items {
			if it.hasAlias {
				parts[i] = it.expr + " AS " + it.alias
			} else {
				parts[i] = it.expr
			}
		}
		return "SELECT " + strings.Join(parts, ", ") + " " + from, aliases
	}

	var b strings.Builder
	b.WriteString("SELECT\n")
	for i, it := range items {
		b.WriteString("  ")
		if it.hasAlias {
			b.WriteString(it.expr)
			b.WriteString(strings.Repeat(" ", maxExpr-len(it.expr)+1))
			b.WriteString("AS ")
			b.WriteString(it.alias)
		} else {
			b.WriteString(it.expr)
		}
		if i < len(items)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(from)
	return b.String(), aliases
}

// resolveAliases computes the final output alias for each column, preserving
// order and disambiguating collisions with numeric suffixes (_2, _3, ...).
func resolveAliases(cols []Column) []string {
	out := make([]string, len(cols))
	used := map[string]bool{}
	for i, col := range cols {
		base := col.Alias
		if base == "" {
			base = CleanAlias(col.Name)
		}
		alias := base
		for n := 2; used[alias]; n++ {
			alias = fmt.Sprintf("%s_%d", base, n)
		}
		used[alias] = true
		out[i] = alias
	}
	return out
}

// CleanAlias returns a safe, unquoted SQL identifier for a column name. Names
// that are already valid identifiers are returned unchanged (so camelCase names
// like "categoryIndex" survive); messier names are normalised to lower_snake_case.
func CleanAlias(name string) string {
	s := strings.TrimSpace(name)
	if identRe.MatchString(s) {
		return s
	}
	s = strings.ToLower(s)
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
			prevUnderscore = r == '_'
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	cleaned := strings.Trim(b.String(), "_")
	if cleaned == "" {
		return "column"
	}
	if cleaned[0] >= '0' && cleaned[0] <= '9' {
		cleaned = "_" + cleaned
	}
	return cleaned
}

// castFor reports the cast type to apply for a column under mode, if any.
func castFor(col Column, mode CastMode) (castType string, do bool) {
	switch mode {
	case CastNever:
		return "", false
	case CastAlways:
		t := col.TargetType
		if t == "" {
			t = col.Type
		}
		if t == "" {
			return "", false
		}
		return t, true
	default: // CastAmbiguousOnly
		if col.TargetType != "" && !typeEqual(col.TargetType, col.Type) {
			return col.TargetType, true
		}
		return "", false
	}
}

// typeEqual compares two DuckDB type strings ignoring case and internal spacing.
func typeEqual(a, b string) bool {
	return normalizeType(a) == normalizeType(b)
}

func normalizeType(t string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(t), " ", ""))
}

// quoteIdent double-quotes a SQL identifier, escaping embedded quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
