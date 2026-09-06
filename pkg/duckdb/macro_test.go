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
