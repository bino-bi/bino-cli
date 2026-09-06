package dataset

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"bino.bi/bino/pkg/duckdb"
)

// runDeclarationChecks verifies the derive/assert declarations of a compiled
// dataset against every row of its base view, after Setup ran and before the
// final query. Every failure is an error: a declared expectation is not a
// sampled type check, so these fail the build regardless of --data-validation.
//
//   - a derived slot must not already be in the query result
//   - an asserted slot, every from slot and "date" must be in the result
//   - no two rows may share an identity within one period (per declared grain)
//   - a supplied (asserted) slot must equal the shifted value wherever one exists
func runDeclarationChecks(ctx context.Context, db *sql.DB, c Compiled) error {
	columns, err := viewColumns(ctx, db, c.View)
	if err != nil {
		return err
	}
	if !columns["date"] {
		return fmt.Errorf(`column "date" is missing from the query result`)
	}
	for _, slot := range sortedSlots(c.Derive) {
		if columns[slot] {
			return fmt.Errorf("slot %s is already in the query result; use assert: for a supplied slot", slot)
		}
		if from := c.Derive[slot].From; !columns[from] {
			return fmt.Errorf("slot %s (from of %s) is missing from the query result", from, slot)
		}
	}
	for _, slot := range sortedSlots(c.Assert) {
		if !columns[slot] {
			return fmt.Errorf("slot %s is asserted but missing from the query result", slot)
		}
		if from := c.Assert[slot].From; !columns[from] {
			return fmt.Errorf("slot %s (from of %s) is missing from the query result", from, slot)
		}
	}

	for _, grain := range declaredGrains(c) {
		if err := checkDuplicates(ctx, db, c.View, grain); err != nil {
			return err
		}
	}

	for _, slot := range sortedSlots(c.Assert) {
		if err := checkAssert(ctx, db, c.View, slot, c.Assert[slot]); err != nil {
			return err
		}
	}
	return nil
}

func viewColumns(ctx context.Context, db *sql.DB, view string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %q LIMIT 0", view))
	if err != nil {
		return nil, fmt.Errorf("describe %s: %w", view, err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	columns := make(map[string]bool, len(names))
	for _, n := range names {
		columns[n] = true
	}
	return columns, nil
}

func declaredGrains(c Compiled) []string {
	seen := map[string]bool{}
	for _, m := range []map[string]ShiftDeclaration{c.Derive, c.Assert} {
		for _, d := range m {
			seen[d.Grain] = true
		}
	}
	grains := make([]string, 0, len(seen))
	for g := range seen {
		grains = append(grains, g)
	}
	sort.Strings(grains)
	return grains
}

// checkDuplicates reports the first pair of rows sharing an identity within a period.
func checkDuplicates(ctx context.Context, db *sql.DB, view, grain string) error {
	query := fmt.Sprintf(`WITH _bino_b AS (SELECT *, 1 AS %s FROM %q),
_bino_k AS (SELECT *, row(*COLUMNS(c -> NOT regexp_matches(c, '%s'))) AS _bino_key, date_trunc('%s', "date"::DATE) AS _bino_period FROM _bino_b)
SELECT * EXCLUDE (_bino_key) FROM _bino_k QUALIFY count(*) OVER (PARTITION BY _bino_key, _bino_period) > 1 ORDER BY ALL LIMIT 1`,
		duckdb.IdentityHelperColumn, view, duckdb.IdentityColumnPattern, grain)
	row, err := firstRow(ctx, db, query)
	if err != nil {
		return fmt.Errorf("duplicate check (grain %s): %w", grain, err)
	}
	if row == nil {
		return nil
	}
	return fmt.Errorf("duplicate rows for identity %s in period %s (grain %s)",
		renderIdentity(row), renderPeriod(row["_bino_period"]), grain)
}

// checkAssert compares a supplied slot to the shifted value on every row
// where a shifted value exists. Tolerance is 1e-9 relative to the larger
// magnitude (at least 1); a NULL on exactly one side is a mismatch.
func checkAssert(ctx context.Context, db *sql.DB, view, slot string, d ShiftDeclaration) error {
	source := fmt.Sprintf("%s('%s', '%s', '%s', '%s')", duckdb.ShiftMacroName, view, d.From, d.Shift, d.Grain)
	mismatch := fmt.Sprintf(`shifted IS NOT NULL AND (%q::DOUBLE IS NULL OR abs(%q::DOUBLE - shifted::DOUBLE) > 1e-9 * greatest(abs(%q::DOUBLE), abs(shifted::DOUBLE), 1))`,
		slot, slot, slot)

	var count int
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", source, mismatch)).Scan(&count); err != nil {
		return fmt.Errorf("assert %s: %w", slot, err)
	}
	if count == 0 {
		return nil
	}
	row, err := firstRow(ctx, db, fmt.Sprintf(`SELECT *, date_trunc('%s', "date"::DATE) AS _bino_period FROM %s WHERE %s ORDER BY ALL LIMIT 1`, d.Grain, source, mismatch))
	if err != nil {
		return fmt.Errorf("assert %s: %w", slot, err)
	}
	return fmt.Errorf("assert %s: %d row(s) differ from %s shifted by %s (grain %s); first at identity %s, period %s",
		slot, count, d.From, d.Shift, d.Grain, renderIdentity(row), renderPeriod(row["_bino_period"]))
}

func firstRow(ctx context.Context, db *sql.DB, query string) (map[string]any, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		return nil, rows.Err()
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	row := make(map[string]any, len(cols))
	for i, c := range cols {
		row[c] = values[i]
	}
	return row, nil
}

// renderIdentity prints the identity columns of a row as {a=1, b=2}: every
// column except date, the scenario slots and the helper columns.
func renderIdentity(row map[string]any) string {
	keys := make([]string, 0, len(row))
	for k := range row {
		switch {
		case k == "date", k == "shifted", slotNames[k],
			k == duckdb.IdentityHelperColumn, k == "_bino_period":
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, row[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func renderPeriod(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.Format("2006-01-02")
	}
	return fmt.Sprintf("%v", v)
}

// allNullDerivedSlots warns for every derived slot that is NULL on all rows of
// a non-empty result: the query window has no prior period to read from. It
// works on the serialized rows, so it applies to cached results as well.
func allNullDerivedSlots(name string, data json.RawMessage, derive map[string]ShiftDeclaration) []Warning {
	if len(derive) == 0 {
		return nil
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return nil
	}
	var warnings []Warning
	for _, slot := range sortedSlots(derive) {
		allNull := true
		for _, row := range rows {
			if v, ok := row[slot]; ok && string(v) != "null" {
				allNull = false
				break
			}
		}
		if allNull {
			warnings = append(warnings, Warning{
				DataSet: name,
				Message: fmt.Sprintf("%s derived from %s is null on every row — the query window has no prior period", slot, derive[slot].From),
			})
		}
	}
	return warnings
}
