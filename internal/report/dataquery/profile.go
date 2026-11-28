package dataquery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasummary"
)

const (
	// DefaultSampleRows is how many rows profile_data summarizes at most
	// when no sample_rows is given.
	DefaultSampleRows = 100_000
	// MaxSampleRows is the largest sample_rows profile_data accepts.
	MaxSampleRows = 1_000_000
	// topValuesMaxDistinct is the most distinct values a column may have to
	// get its top values listed.
	topValuesMaxDistinct = 20
	topValuesCount       = 5
	// maxDistinctDates bounds the distinct dates read for the gap check.
	maxDistinctDates   = 100_000
	profileCallTimeout = 120 * time.Second
	sampleTable        = "_bino_sample"
)

// Profile describes the data of one DataSource or DataSet.
type Profile struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	RowCount int64  `json:"rowCount"`
	// Sampled reports that the column statistics come from a random sample
	// of SampleRows rows, because the data has more rows than that.
	Sampled    bool            `json:"sampled"`
	SampleRows int             `json:"sampleRows"`
	Columns    []ColumnProfile `json:"columns"`
	Warnings   []string        `json:"warnings"`
}

// ColumnProfile describes one column. The statistics refer to the
// summarized rows: all rows, or the sample when the profile is sampled.
type ColumnProfile struct {
	Name           string       `json:"name"`
	Type           string       `json:"type"`
	Min            any          `json:"min"`
	Max            any          `json:"max"`
	ApproxDistinct int64        `json:"approxDistinct"`
	NullCount      int64        `json:"nullCount"`
	TopValues      []ValueCount `json:"topValues,omitempty"`
	// Series is set for DATE and TIMESTAMP columns and is computed over all rows.
	Series *Series `json:"series,omitempty"`
}

// ValueCount is one value and how often it occurs.
type ValueCount struct {
	Value any   `json:"value"`
	Count int64 `json:"count"`
}

// Series is the date range of a column and the periods missing in it.
type Series struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Step is the inferred period: day, weekday, week, month, quarter or year. Empty
	// when the column has fewer than two distinct dates.
	Step    string `json:"step,omitempty"`
	Periods int    `json:"periods"`
	// Missing lists the first missing periods, MissingMore counts the rest.
	Missing     []string `json:"missing"`
	MissingMore int      `json:"missingMore"`
}

// ProfileData profiles target, one DataSource or DataSet from docs. When the
// data has more than sampleRows rows, the column statistics are computed on
// a random sample of that size.
func ProfileData(ctx context.Context, docs []config.Document, ambiguous config.Ambiguities, target *config.Document, sampleRows int) (Profile, error) {
	p := Profile{Name: target.Name, Kind: target.Kind, Columns: []ColumnProfile{}, Warnings: []string{}}
	if sampleRows <= 0 {
		sampleRows = DefaultSampleRows
	}
	if sampleRows > MaxSampleRows {
		return p, fmt.Errorf("sample_rows %d is above the maximum of %d", sampleRows, MaxSampleRows)
	}

	ctx, cancel := context.WithTimeout(ctx, profileCallTimeout)
	defer cancel()
	s, err := openSession(ctx)
	if err != nil {
		return p, err
	}
	defer s.Close()
	srcs, err := s.prepare(ctx, docs, ambiguous, []*config.Document{target})
	if err != nil {
		return p, err
	}
	if err := s.lock(ctx); err != nil {
		return p, err
	}
	full := "(\n" + srcs[strings.ToLower(target.Name)] + "\n) AS _bino_p"

	if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM "+full).Scan(&p.RowCount); err != nil {
		return p, explain(ambiguous, err)
	}
	src := full
	if p.RowCount > int64(sampleRows) {
		create := fmt.Sprintf("CREATE TEMP TABLE %s AS SELECT * FROM %s USING SAMPLE reservoir(%d ROWS) REPEATABLE (1)", sampleTable, full, sampleRows)
		if _, err := s.conn.ExecContext(ctx, create); err != nil {
			return p, explain(ambiguous, err)
		}
		src = sampleTable
		p.Sampled = true
		p.SampleRows = sampleRows
	}

	cols, rows, err := datasummary.Summarize(ctx, s.conn, datasummary.QuerySQL("SELECT * FROM "+src), normalize)
	if err != nil {
		return p, explain(ambiguous, err)
	}
	idx := map[string]int{}
	for i, c := range cols {
		idx[c.Name] = i
	}
	for _, r := range rows {
		cp := ColumnProfile{
			Name:           fmt.Sprint(r[idx["column_name"]]),
			Type:           fmt.Sprint(r[idx["column_type"]]),
			Min:            r[idx["min"]],
			Max:            r[idx["max"]],
			ApproxDistinct: toInt64(r[idx["approx_unique"]]),
		}
		p.Columns = append(p.Columns, cp)
	}
	if err := s.nullCounts(ctx, src, p.Columns); err != nil {
		return p, explain(ambiguous, err)
	}

	for i := range p.Columns {
		c := &p.Columns[i]
		if c.ApproxDistinct > 0 && c.ApproxDistinct <= topValuesMaxDistinct {
			if c.TopValues, err = s.topValues(ctx, src, c.Name, c.Type); err != nil {
				return p, explain(ambiguous, err)
			}
		}
		if isDateType(c.Type) {
			series, warn, err := s.series(ctx, full, c.Name)
			if err != nil {
				return p, explain(ambiguous, err)
			}
			c.Series = series
			if warn != "" {
				p.Warnings = append(p.Warnings, warn)
			}
		}
	}
	warns, err := s.textWarnings(ctx, src, p.Columns)
	if err != nil {
		return p, explain(ambiguous, err)
	}
	p.Warnings = append(p.Warnings, warns...)
	return p, nil
}

// nullCounts counts the nulls of every column exactly, in one scan of src.
func (s *session) nullCounts(ctx context.Context, src string, cols []ColumnProfile) error {
	if len(cols) == 0 {
		return nil
	}
	exprs := make([]string, len(cols))
	ptrs := make([]any, len(cols))
	for i := range cols {
		exprs[i] = fmt.Sprintf("count(*) - count(%s)", quoteIdent(cols[i].Name))
		ptrs[i] = &cols[i].NullCount
	}
	return s.conn.QueryRowContext(ctx, "SELECT "+strings.Join(exprs, ", ")+" FROM "+src).Scan(ptrs...)
}

func (s *session) topValues(ctx context.Context, src, col, typ string) ([]ValueCount, error) {
	q := fmt.Sprintf("SELECT %s, count(*) FROM %s GROUP BY 1 ORDER BY 2 DESC, 1 NULLS LAST LIMIT %d", quoteIdent(col), src, topValuesCount)
	rows, err := s.conn.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ValueCount
	for rows.Next() {
		var vc ValueCount
		if err := rows.Scan(&vc.Value, &vc.Count); err != nil {
			return nil, err
		}
		vc.Value = normalizeTyped(vc.Value, typ)
		out = append(out, vc)
	}
	return out, rows.Err()
}

// series reads the distinct dates of col over all rows and finds the gaps.
func (s *session) series(ctx context.Context, full, col string) (*Series, string, error) {
	q := fmt.Sprintf("SELECT DISTINCT CAST(%[1]s AS DATE) FROM %[2]s WHERE %[1]s IS NOT NULL ORDER BY 1 LIMIT %[3]d", quoteIdent(col), full, maxDistinctDates+1)
	rows, err := s.conn.QueryContext(ctx, q)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var dates []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, "", err
		}
		dates = append(dates, d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(dates) == 0 {
		return nil, "", nil
	}
	if len(dates) > maxDistinctDates {
		return nil, fmt.Sprintf("column %q has more than %d distinct dates; gaps were not checked", col, maxDistinctDates), nil
	}
	return findGaps(dates), "", nil
}

// textWarnings flags VARCHAR columns whose values all read as numbers or as
// dates: often a CSV option or a column type is missing.
func (s *session) textWarnings(ctx context.Context, src string, cols []ColumnProfile) ([]string, error) {
	var text []string
	var exprs []string
	for _, c := range cols {
		if c.Type != "VARCHAR" {
			continue
		}
		id := quoteIdent(c.Name)
		text = append(text, c.Name)
		exprs = append(exprs, fmt.Sprintf("count(%[1]s), count(TRY_CAST(%[1]s AS DOUBLE)), count(TRY_CAST(%[1]s AS TIMESTAMP))", id))
	}
	if len(text) == 0 {
		return nil, nil
	}
	vals := make([]int64, 3*len(text))
	ptrs := make([]any, len(vals))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := s.conn.QueryRowContext(ctx, "SELECT "+strings.Join(exprs, ", ")+" FROM "+src).Scan(ptrs...); err != nil {
		return nil, err
	}
	var out []string
	for i, name := range text {
		n, num, date := vals[3*i], vals[3*i+1], vals[3*i+2]
		switch {
		case n == 0:
		case num == n:
			out = append(out, fmt.Sprintf("column %q is VARCHAR, but all %d non-null values read as numbers", name, n))
		case date == n:
			out = append(out, fmt.Sprintf("column %q is VARCHAR, but all %d non-null values read as dates", name, n))
		}
	}
	return out, nil
}

func isDateType(t string) bool {
	return t == "DATE" || strings.HasPrefix(t, "TIMESTAMP")
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case uint64:
		return int64(n) //nolint:gosec // counts fit in int64
	case float64:
		return int64(n)
	}
	var f float64
	if _, err := fmt.Sscan(fmt.Sprint(v), &f); err == nil {
		return int64(f)
	}
	return 0
}
