package duckdb

import (
	"context"
	"strings"
	"testing"
)

func openTestSession(t *testing.T) (context.Context, *Session) {
	t.Helper()
	ctx := context.Background()
	s, err := OpenSession(ctx, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return ctx, s
}

func mustExec(ctx context.Context, t *testing.T, s *Session, sql string) {
	t.Helper()
	if _, err := s.DB().ExecContext(ctx, sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// shiftedByDate runs bino_shift and returns date -> shifted (nil for NULL),
// optionally restricted with an extra WHERE clause.
func shiftedByDate(ctx context.Context, t *testing.T, s *Session, call string, where string) map[string]*float64 {
	t.Helper()
	q := "SELECT \"date\", shifted FROM " + call
	if where != "" {
		q += " WHERE " + where
	}
	rows, err := s.DB().QueryContext(ctx, q)
	if err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	defer rows.Close()
	out := map[string]*float64{}
	for rows.Next() {
		var date string
		var shifted *float64
		if err := rows.Scan(&date, &shifted); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[date] = shifted
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func wantValue(t *testing.T, got map[string]*float64, date string, want float64) {
	t.Helper()
	v, ok := got[date]
	if !ok {
		t.Fatalf("row %s missing from result", date)
	}
	if v == nil {
		t.Fatalf("%s: shifted = NULL, want %v", date, want)
	}
	if *v != want {
		t.Errorf("%s: shifted = %v, want %v", date, *v, want)
	}
}

func wantNull(t *testing.T, got map[string]*float64, date string) {
	t.Helper()
	v, ok := got[date]
	if !ok {
		t.Fatalf("row %s missing from result", date)
	}
	if v != nil {
		t.Errorf("%s: shifted = %v, want NULL", date, *v)
	}
}

func TestShiftMacroSQLUsesIdentityConstants(t *testing.T) {
	if !strings.Contains(shiftMacroSQL, IdentityColumnPattern) {
		t.Errorf("macro SQL does not contain IdentityColumnPattern %q", IdentityColumnPattern)
	}
	if !strings.Contains(shiftMacroSQL, IdentityHelperColumn) {
		t.Errorf("macro SQL does not contain IdentityHelperColumn %q", IdentityHelperColumn)
	}
}

func TestBinoShift_GapReturnsNullNotPreviousRow(t *testing.T) {
	ctx, s := openTestSession(t)
	// Monthly actuals; 2020-02 is missing. lag() would hand 2021-02 the value
	// of 2020-01; the shift must return NULL instead.
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', '2020-01-31', 10.0),
		('A', '2020-03-31', 30.0),
		('A', '2021-01-31', 110.0),
		('A', '2021-02-28', 120.0),
		('A', '2021-03-31', 130.0)) v(category, "date", ac1)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 year', 'month')", "")
	wantValue(t, got, "2021-01-31", 10)
	wantNull(t, got, "2021-02-28")
	wantValue(t, got, "2021-03-31", 30)
	wantNull(t, got, "2020-01-31")
	if len(got) != 5 {
		t.Errorf("row count = %d, want 5 (no fan-out, no drop)", len(got))
	}
}

func TestBinoShift_MonthEndsMatchUnderMonthGrain(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('2019-02-28', 1.0),
		('2019-03-31', 2.0),
		('2019-04-30', 3.0),
		('2020-02-29', 10.0),
		('2020-03-31', 20.0),
		('2020-04-30', 30.0)) v("date", ac1)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 year', 'month')", "")
	wantValue(t, got, "2020-02-29", 1)
	wantValue(t, got, "2020-03-31", 2)
	wantValue(t, got, "2020-04-30", 3)

	// One month back: 2020-04-30 -> 2020-03 (raw date arithmetic would give
	// 2020-03-30 and miss the month-end row).
	got = shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 month', 'month')", "")
	wantValue(t, got, "2020-04-30", 20)
	wantValue(t, got, "2020-03-31", 10)
	wantNull(t, got, "2020-02-29")
}

func TestBinoShift_DayGrainMonthShiftLeavesThe31stNull(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS
		SELECT strftime(d, '%Y-%m-%d') AS "date", day(d)::DOUBLE AS ac1
		FROM range(DATE '2020-02-01', DATE '2020-04-01', INTERVAL 1 DAY) r(d)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 month', 'day')", "")
	wantValue(t, got, "2020-03-15", 15)
	wantValue(t, got, "2020-03-29", 29)
	// 2020-03-30 and -31 have no same-day counterpart in February: NULL, not
	// the clamped 2020-02-29.
	wantNull(t, got, "2020-03-30")
	wantNull(t, got, "2020-03-31")
}

func TestBinoShift_NoDimensionColumns(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('2019-06-30', 5.0),
		('2020-06-30', 6.0)) v("date", ac1)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 year', 'month')", "")
	wantValue(t, got, "2020-06-30", 5)
	wantNull(t, got, "2019-06-30")

	// The helper column must not leak into the output.
	rows, err := s.DB().QueryContext(ctx, "SELECT * FROM bino_shift('t', 'ac1', '1 year', 'month')")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if strings.Join(cols, ",") != "date,ac1,shifted" {
		t.Errorf("columns = %v, want [date ac1 shifted]", cols)
	}
}

func TestBinoShift_TwoDimensionsPartition(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('North', 'A', '2019-01-31', 1.0),
		('North', 'B', '2019-01-31', 2.0),
		('South', 'A', '2019-01-31', 3.0),
		('North', 'A', '2020-01-31', 10.0),
		('North', 'B', '2020-01-31', 20.0),
		('South', 'A', '2020-01-31', 30.0),
		('South', 'B', '2020-01-31', 40.0)) v(rowGroup, category, "date", ac1)`)
	call := "bino_shift('t', 'ac1', '1 year', 'month')"
	na := shiftedByDate(ctx, t, s, call, "rowGroup = 'North' AND category = 'A'")
	wantValue(t, na, "2020-01-31", 1)
	nb := shiftedByDate(ctx, t, s, call, "rowGroup = 'North' AND category = 'B'")
	wantValue(t, nb, "2020-01-31", 2)
	sa := shiftedByDate(ctx, t, s, call, "rowGroup = 'South' AND category = 'A'")
	wantValue(t, sa, "2020-01-31", 3)
	sb := shiftedByDate(ctx, t, s, call, "rowGroup = 'South' AND category = 'B'")
	wantNull(t, sb, "2020-01-31")
}

func TestBinoShift_WeekGrainTruncatesToISOMonday(t *testing.T) {
	ctx, s := openTestSession(t)
	// 2024-01-03 is a Wednesday, 2024-01-12 a Friday of the following week.
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('2024-01-03', 1.0),
		('2024-01-12', 2.0),
		('2024-01-15', 3.0)) v("date", ac1)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 week', 'week')", "")
	wantValue(t, got, "2024-01-12", 1)
	wantValue(t, got, "2024-01-15", 2)
	wantNull(t, got, "2024-01-03")
}

func TestBinoShift_SourceSelectsNamedSlot(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('2019-01-31', 1.0, 100.0),
		('2020-01-31', 2.0, 200.0)) v("date", ac1, pl1)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'pl1', '1 year', 'month')", "")
	wantValue(t, got, "2020-01-31", 100)
	got = shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 year', 'month')", "")
	wantValue(t, got, "2020-01-31", 1)
}

func TestBinoShift_NullDimensionsMatchEachOther(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		(NULL, '2019-01-31', 1.0),
		(NULL, '2020-01-31', 2.0)) v(category, "date", ac1)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 year', 'month')", "")
	wantValue(t, got, "2020-01-31", 1)
}

func TestBinoShift_TimestampWithOffsetUsesCalendarDay(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('2019-01-31T23:30:00+01:00', 1.0),
		('2020-01-31T00:15:00-05:00', 2.0)) v("date", ac1)`)
	got := shiftedByDate(ctx, t, s, "bino_shift('t', 'ac1', '1 year', 'month')", "")
	wantValue(t, got, "2020-01-31T00:15:00-05:00", 1)
}

func TestBinoShift_VisibleOnEveryPooledConnection(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT '2020-01-31' AS "date", 1.0 AS ac1`)
	// Hold one connection open so the second query must use another one.
	c1, err := s.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer c1.Close()
	c2, err := s.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer c2.Close()
	var n int
	if err := c1.QueryRowContext(ctx, "SELECT count(*) FROM bino_shift('t', 'ac1', '1 year', 'month')").Scan(&n); err != nil {
		t.Fatalf("conn 1: %v", err)
	}
	if err := c2.QueryRowContext(ctx, "SELECT count(*) FROM bino_shift('t', 'ac1', '1 year', 'month')").Scan(&n); err != nil {
		t.Fatalf("conn 2: %v", err)
	}
}

// A category that appears for the first time keeps its row; the slot is NULL
// because there is no prior row of the same identity.
func TestBinoShift_NewIdentityKeepsRowWithNullShifted(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', 1, '2024-01-31', 100.0),
		('A', 1, '2024-02-29', 110.0),
		('C', 3, '2024-02-29', 50.0)
	) v(category, "categoryIndex", "date", ac1)`)
	call := "bino_shift('t', 'ac1', '1 month', 'month')"
	a := shiftedByDate(ctx, t, s, call, "category = 'A'")
	wantValue(t, a, "2024-02-29", 100)
	c := shiftedByDate(ctx, t, s, call, "category = 'C'")
	wantNull(t, c, "2024-02-29")
}

// A category that existed only in the prior period gets a row in the current
// period: identity from the earlier row, source NULL, shifted filled.
func TestBinoShift_PriorOnlyIdentityGetsRowWithNullSource(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', 1, '2024-01-31', 100.0),
		('B', 2, '2024-01-31', 563.0),
		('A', 1, '2024-02-29', 110.0)
	) v(category, "categoryIndex", "date", ac1)`)
	call := "bino_shift('t', 'ac1', '1 month', 'month')"
	if got := rowCount(ctx, t, s, "SELECT count(*) FROM "+call); got != 4 {
		t.Errorf("row count = %d, want 4 (3 source rows + the filled B row)", got)
	}
	b := shiftedByDate(ctx, t, s, call, "category = 'B'")
	wantNull(t, b, "2024-01-31")
	wantValue(t, b, "2024-02-29", 563)
}

// Rows are only filled into periods the data covers: nothing is created after
// the last period, and fill := false adds no rows at all.
func TestBinoShift_FillOnlyWithinExistingPeriods(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', 1, '2024-01-31', 100.0),
		('A', 1, '2024-02-29', 110.0)
	) v(category, "categoryIndex", "date", ac1)`)
	if got := rowCount(ctx, t, s, "SELECT count(*) FROM bino_shift('t', 'ac1', '1 month', 'month')"); got != 2 {
		t.Errorf("row count = %d, want 2: no March row after the data ends", got)
	}
	if got := rowCount(ctx, t, s, "SELECT count(*) FROM bino_shift('t', 'ac1', '1 month', 'month') WHERE \"date\" > '2024-02-29'"); got != 0 {
		t.Errorf("%d row(s) after the last period", got)
	}
}

func TestBinoShift_FillFalseAddsNoRows(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', 1, '2024-01-31', 100.0),
		('B', 2, '2024-01-31', 563.0),
		('A', 1, '2024-02-29', 110.0)
	) v(category, "categoryIndex", "date", ac1)`)
	if got := rowCount(ctx, t, s, "SELECT count(*) FROM bino_shift('t', 'ac1', '1 month', 'month', fill := false)"); got != 3 {
		t.Errorf("row count = %d, want 3 (the source rows)", got)
	}
}

// The filled row borrows the date string the data uses in the target period,
// so its format matches and it lands in the period other rows use.
func TestBinoShift_FilledRowUsesPeriodDate(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', 1, '2024-01-31', 100.0),
		('B', 2, '2024-01-31', 563.0),
		('A', 1, '2024-02-29', 110.0)
	) v(category, "categoryIndex", "date", ac1)`)
	var date string
	var ac1, shifted *float64
	err := s.DB().QueryRowContext(ctx, `SELECT "date", ac1, shifted FROM bino_shift('t', 'ac1', '1 month', 'month') WHERE category = 'B' AND "date" <> '2024-01-31'`).Scan(&date, &ac1, &shifted)
	if err != nil {
		t.Fatalf("filled row: %v", err)
	}
	if date != "2024-02-29" {
		t.Errorf("date = %q, want the period's date 2024-02-29", date)
	}
	if ac1 != nil {
		t.Errorf("ac1 = %v, want NULL", *ac1)
	}
	if shifted == nil || *shifted != 563 {
		t.Errorf("shifted = %v, want 563", shifted)
	}
}

// Under grain day a month shift must round-trip, so a prior row on the 31st
// does not fill a day that does not exist in the target month.
func TestBinoShift_FilledRowsRoundTripUnderDayGrain(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', 1, '2024-01-30', 100.0),
		('B', 2, '2024-01-31', 563.0),
		('A', 1, '2024-02-29', 110.0)
	) v(category, "categoryIndex", "date", ac1)`)
	call := "bino_shift('t', 'ac1', '1 month', 'day')"
	if got := rowCount(ctx, t, s, "SELECT count(*) FROM "+call+" WHERE category = 'B'"); got != 1 {
		t.Errorf("B has %d row(s), want 1: Jan 31 + 1 month does not round-trip", got)
	}
}

// Two layers: the row layer one adds is a current row for layer two.
func TestBinoShift_TwoLayersFillInBothOrders(t *testing.T) {
	ctx, s := openTestSession(t)
	// B: February last year and January this year, nothing in February this year.
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('B', 2, '2023-02-28', 500.0),
		('B', 2, '2024-01-31', 563.0),
		('A', 1, '2024-02-29', 110.0)
	) v(category, "categoryIndex", "date", ac1)`)
	mustExec(ctx, t, s, `CREATE VIEW m AS SELECT * EXCLUDE (shifted), shifted AS pp1 FROM bino_shift('t', 'ac1', '1 month', 'month')`)
	mustExec(ctx, t, s, `CREATE VIEW my AS SELECT * EXCLUDE (shifted), shifted AS pp2 FROM bino_shift('m', 'ac1', '1 year', 'month')`)
	mustExec(ctx, t, s, `CREATE VIEW y AS SELECT * EXCLUDE (shifted), shifted AS pp2 FROM bino_shift('t', 'ac1', '1 year', 'month')`)
	mustExec(ctx, t, s, `CREATE VIEW ym AS SELECT * EXCLUDE (shifted), shifted AS pp1 FROM bino_shift('y', 'ac1', '1 month', 'month')`)
	for _, view := range []string{"my", "ym"} {
		var pp1, pp2 *float64
		err := s.DB().QueryRowContext(ctx, `SELECT pp1, pp2 FROM `+view+` WHERE category = 'B' AND "date" = '2024-02-29'`).Scan(&pp1, &pp2)
		if err != nil {
			t.Fatalf("%s: filled B row: %v", view, err)
		}
		if pp1 == nil || *pp1 != 563 || pp2 == nil || *pp2 != 500 {
			t.Errorf("%s: pp1 = %v, pp2 = %v; want 563 and 500", view, pp1, pp2)
		}
	}
}

func rowCount(ctx context.Context, t *testing.T, s *Session, query string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRowContext(ctx, query).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// The index twin is part of the identity: the same category with a different
// categoryIndex in the prior period does not match.
func TestBinoShift_IndexTwinIsPartOfIdentity(t *testing.T) {
	ctx, s := openTestSession(t)
	mustExec(ctx, t, s, `CREATE TABLE t AS SELECT * FROM (VALUES
		('A', 1, '2024-01-31', 100.0),
		('A', 2, '2024-02-29', 110.0)
	) v(category, "categoryIndex", "date", ac1)`)
	call := "bino_shift('t', 'ac1', '1 month', 'month')"
	// A/2 is a new identity: nothing to read.
	wantNull(t, shiftedByDate(ctx, t, s, call, `"categoryIndex" = 2`), "2024-02-29")
	// A/1 exists only in January: it is filled into February with its value.
	wantValue(t, shiftedByDate(ctx, t, s, call, `"categoryIndex" = 1`), "2024-02-29", 100)
}
