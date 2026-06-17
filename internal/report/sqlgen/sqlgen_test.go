package sqlgen

import (
	"strings"
	"testing"
)

// normalize collapses all whitespace runs to single spaces so that the pretty
// (aligned, multi-line) form can be compared against the compact form.
func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestCleanAlias(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"valid identifier preserved", "id", "id"},
		{"camelCase preserved", "categoryIndex", "categoryIndex"},
		{"underscores preserved", "row_group_index", "row_group_index"},
		{"spaces", "Full Name", "full_name"},
		{"punctuation collapsed", "Amount (€)", "amount"},
		{"multiple separators collapse", "a -- b", "a_b"},
		{"leading digit prefixed", "1st place", "_1st_place"},
		{"pure digits prefixed", "2024", "_2024"},
		{"trims edges", "  spaced  ", "spaced"},
		{"empty falls back", "***", "column"},
		{"slashes", "qty/order", "qty_order"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanAlias(tt.in); got != tt.want {
				t.Errorf("CleanAlias(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveAliasesDedup(t *testing.T) {
	cols := []Column{
		{Name: "qty"},
		{Name: "qty"},
		{Name: "qty%"}, // cleans to "qty"
		{Name: "amount"},
	}
	got := resolveAliases(cols)
	want := []string{"qty", "qty_2", "qty_3", "amount"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("alias[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestTypedSelectCompact(t *testing.T) {
	cols := []Column{
		{Name: "id", Type: "BIGINT"},
		{Name: "Full Name", Type: "VARCHAR"},
		{Name: "amount", Type: "VARCHAR", TargetType: "DECIMAL(18,2)"},
	}
	got, aliases := TypedSelect("sales_2024", cols, Options{})
	want := `SELECT "id", "Full Name" AS full_name, "amount"::DECIMAL(18,2) AS amount FROM "sales_2024"`
	if got != want {
		t.Errorf("compact mismatch:\n got: %s\nwant: %s", got, want)
	}
	wantAliases := []string{"id", "full_name", "amount"}
	for i := range wantAliases {
		if aliases[i] != wantAliases[i] {
			t.Errorf("alias[%d] = %q, want %q", i, aliases[i], wantAliases[i])
		}
	}
}

func TestTypedSelectExprColumns(t *testing.T) {
	// The schema-driven mapper maps source columns, constants, and expressions
	// onto fixed dataset column names (the alias).
	cols := []Column{
		{Name: "amount", Type: "VARCHAR", TargetType: "DOUBLE", Alias: "ac1"},
		{Alias: "operation", Expr: "'+'"},
		{Alias: "ac2", Expr: `sum("amount")`},
	}
	got, aliases := TypedSelect("sales_src", cols, Options{})
	want := `SELECT "amount"::DOUBLE AS ac1, '+' AS operation, sum("amount") AS ac2 FROM "sales_src"`
	if got != want {
		t.Errorf("expr mismatch:\n got: %s\nwant: %s", got, want)
	}
	wantAliases := []string{"ac1", "operation", "ac2"}
	for i := range wantAliases {
		if aliases[i] != wantAliases[i] {
			t.Errorf("alias[%d] = %q, want %q", i, aliases[i], wantAliases[i])
		}
	}
}

func TestTypedSelectPretty(t *testing.T) {
	cols := []Column{
		{Name: "id", Type: "BIGINT"},
		{Name: "Full Name", Type: "VARCHAR"},
		{Name: "amount", Type: "VARCHAR", TargetType: "DECIMAL(18,2)"},
	}
	compact, _ := TypedSelect("sales_2024", cols, Options{})
	pretty, _ := TypedSelect("sales_2024", cols, Options{Pretty: true})
	if !strings.Contains(pretty, "\n") {
		t.Fatalf("pretty output is not multi-line:\n%s", pretty)
	}
	if normalize(pretty) != compact {
		t.Errorf("pretty normalizes to a different statement:\n got: %s\nwant: %s", normalize(pretty), compact)
	}
}

func TestTypedSelectCastModes(t *testing.T) {
	cols := []Column{
		{Name: "n", Type: "DECIMAL(10,2)", TargetType: "DECIMAL(10,2)"}, // same -> no cast in ambiguous
		{Name: "s", Type: "VARCHAR", TargetType: "DATE"},                // differs -> cast
		{Name: "x", Type: "BIGINT"},                                     // no target
	}

	t.Run("ambiguous only", func(t *testing.T) {
		got, _ := TypedSelect("t", cols, Options{CastMode: CastAmbiguousOnly})
		want := `SELECT "n", "s"::DATE AS s, "x" FROM "t"`
		if got != want {
			t.Errorf("got: %s want: %s", got, want)
		}
	})

	t.Run("never", func(t *testing.T) {
		got, _ := TypedSelect("t", cols, Options{CastMode: CastNever})
		want := `SELECT "n", "s", "x" FROM "t"`
		if got != want {
			t.Errorf("got: %s want: %s", got, want)
		}
	})

	t.Run("always", func(t *testing.T) {
		got, _ := TypedSelect("t", cols, Options{CastMode: CastAlways})
		want := `SELECT "n"::DECIMAL(10,2) AS n, "s"::DATE AS s, "x"::BIGINT AS x FROM "t"`
		if got != want {
			t.Errorf("got: %s want: %s", got, want)
		}
	})
}

func TestTypedSelectQuotesEmbeddedQuote(t *testing.T) {
	cols := []Column{{Name: `we"ird`, Type: "VARCHAR"}}
	got, _ := TypedSelect("t", cols, Options{})
	want := `SELECT "we""ird" AS we_ird FROM "t"`
	if got != want {
		t.Errorf("got: %s want: %s", got, want)
	}
}

func TestTypedSelectExplicitAlias(t *testing.T) {
	cols := []Column{{Name: "categoryIndex", Type: "BIGINT", Alias: "cat_idx"}}
	got, _ := TypedSelect("t", cols, Options{})
	want := `SELECT "categoryIndex" AS cat_idx FROM "t"`
	if got != want {
		t.Errorf("got: %s want: %s", got, want)
	}
}
