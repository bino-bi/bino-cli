package duckdb

import "fmt"

// ShiftMacroName is the name of the session table macro that shifts a scenario
// slot back in time.
const ShiftMacroName = "bino_shift"

// ShiftMacroRevision is bumped whenever the semantics of the bino_shift macro
// change. Dataset results that depend on the macro include it in their cache
// digest, so a macro fix invalidates derived results and nothing else.
const ShiftMacroRevision = 2

// IdentityColumnPattern matches the columns that are NOT part of a row's
// identity: the date column and the sixteen scenario slots. Every other
// column of a dataset (dimensions, their index twins, operation, setname, …)
// identifies the row across periods.
const IdentityColumnPattern = `^(date|(ac|pp|fc|pl)[1-4])$`

// IdentityHelperColumn is a constant column added before the identity key is
// built, so a dataset with no dimension columns still has a (constant) key.
const IdentityHelperColumn = "_bino_one"

// shiftMacroSQL is the definition of bino_shift(src, source, shift, grain, fill).
//
//	src    VARCHAR  name of a view or table in the session (read via query_table)
//	source VARCHAR  the slot to read, e.g. 'ac1'
//	shift  VARCHAR  castable to INTERVAL, e.g. '1 year', '2 month'
//	grain  VARCHAR  a date_trunc specifier: day | week | month | quarter | year
//	fill   BOOLEAN  default true: add a row for an identity that exists one
//	                shift earlier but has no row in a period the data covers
//
// It returns every row and column of src plus one column, shifted: the value
// of source on the row with the same identity whose period equals this row's
// period minus shift, or NULL when there is none. The caller aliases shifted
// to the target slot. Periods are keyed on date_trunc(grain, "date"::DATE) on
// both sides, so month ends line up; the shift must also round-trip, so a
// month shift under grain day leaves the 31st empty instead of matching a
// clamped day. Identity is a struct of every non-slot, non-date column, so no
// per-column partitioning is needed and no user text is interpolated.
//
// With fill, an identity that has a row one shift earlier but none in the
// target period gets a row there: identity columns from the earlier row,
// every slot NULL, date the smallest date string the data uses in that
// period (so only periods present in the data are ever created), and
// shifted filled from the earlier row. assert passes fill := false, because
// it compares supplied rows only.
var shiftMacroSQL = fmt.Sprintf(`CREATE OR REPLACE MACRO %[1]s(src, source, shift, grain, fill := true) AS TABLE
WITH base AS (
  SELECT *, 1 AS %[2]s FROM query_table(src)
), keyed AS (
  SELECT *,
         row(*COLUMNS(c -> NOT regexp_matches(c, '%[3]s'))) AS _bino_key,
         date_trunc(grain, "date"::DATE) AS _bino_period
  FROM base
), periods AS (
  SELECT _bino_period, min("date") AS _bino_date FROM keyed GROUP BY 1
), ident AS (
  SELECT COLUMNS(c -> NOT regexp_matches(c, '%[3]s')) FROM keyed
), filled AS (
  SELECT _bino_i.* EXCLUDE (_bino_period), _bino_per._bino_date AS "date", _bino_per._bino_period
  FROM ident _bino_i
  JOIN periods _bino_per
    ON _bino_per._bino_period = (_bino_i._bino_period + shift::INTERVAL)::DATE
   AND (_bino_per._bino_period - shift::INTERVAL)::DATE = _bino_i._bino_period
  WHERE fill
    AND NOT EXISTS (
      SELECT 1 FROM keyed _bino_cur
      WHERE _bino_cur._bino_key = _bino_i._bino_key AND _bino_cur._bino_period = _bino_per._bino_period
    )
), current AS (
  SELECT * FROM keyed UNION ALL BY NAME SELECT * FROM filled
), prior AS (
  SELECT _bino_key, _bino_period, coalesce(*COLUMNS(c -> c = source)) AS shifted FROM keyed
)
SELECT _bino_a.* EXCLUDE (%[2]s, _bino_key, _bino_period), _bino_p.shifted
FROM current _bino_a
LEFT JOIN prior _bino_p
  ON _bino_p._bino_key = _bino_a._bino_key
 AND _bino_p._bino_period = (_bino_a._bino_period - shift::INTERVAL)::DATE
 AND (_bino_p._bino_period + shift::INTERVAL)::DATE = _bino_a._bino_period`,
	ShiftMacroName, IdentityHelperColumn, IdentityColumnPattern)
