package dataquery

import (
	"fmt"
	"time"
)

const (
	maxMissingListed = 20
	// maxPeriodSpan bounds the periods walked between the first and last date.
	maxPeriodSpan = 1_000_000
	// minWeekdayDates is how many dates a series needs before missing
	// weekends make it a weekday series rather than a daily one.
	minWeekdayDates = 10
)

// findGaps infers the step of sorted, distinct dates and lists the periods
// between the first and the last date that have no row. The step is the
// coarsest one every date lines up with: all on January 1 (or December 31)
// is year, all on a quarter's first (or last) day is quarter, all on the 1st
// (or the last day of the month) is month, all on one weekday is week, no
// Saturday or Sunday in at least minWeekdayDates dates is weekday (Monday to
// Friday, weekends are not missing), else day.
func findGaps(dates []time.Time) *Series {
	s := &Series{From: dates[0].Format(time.DateOnly), To: dates[len(dates)-1].Format(time.DateOnly), Periods: len(dates), Missing: []string{}}
	if len(dates) < 2 {
		return s
	}
	s.Step = inferStep(dates)
	index, label := periodFuncs(s.Step, dates[0])
	present := make(map[int]bool, len(dates))
	for _, d := range dates {
		present[index(d)] = true
	}
	s.Periods = len(present)
	first, last := index(dates[0]), index(dates[len(dates)-1])
	if last-first > maxPeriodSpan {
		return s
	}
	for i := first; i <= last; i++ {
		if present[i] {
			continue
		}
		if len(s.Missing) < maxMissingListed {
			s.Missing = append(s.Missing, label(i))
		} else {
			s.MissingMore++
		}
	}
	return s
}

func inferStep(dates []time.Time) string {
	all := func(f func(time.Time) bool) bool {
		for _, d := range dates {
			if !f(d) {
				return false
			}
		}
		return true
	}
	monthStart := func(d time.Time) bool { return d.Day() == 1 }
	monthEnd := func(d time.Time) bool { return d.AddDate(0, 0, 1).Day() == 1 }
	quarterMonth := func(d time.Time, first bool) bool {
		m := int(d.Month())
		if first {
			return m%3 == 1
		}
		return m%3 == 0
	}
	switch {
	case all(func(d time.Time) bool { return d.Month() == time.January && d.Day() == 1 }),
		all(func(d time.Time) bool { return d.Month() == time.December && d.Day() == 31 }):
		return "year"
	case all(func(d time.Time) bool { return monthStart(d) && quarterMonth(d, true) }),
		all(func(d time.Time) bool { return monthEnd(d) && quarterMonth(d, false) }):
		return "quarter"
	case all(monthStart), all(monthEnd):
		return "month"
	case all(func(d time.Time) bool { return d.Weekday() == dates[0].Weekday() }):
		return "week"
	case len(dates) >= minWeekdayDates && all(func(d time.Time) bool { return d.Weekday() != time.Saturday && d.Weekday() != time.Sunday }):
		return "weekday"
	default:
		return "day"
	}
}

// periodFuncs returns the period number of a date for step and the label of
// a period number. Week and day count days from first, so a week is the
// seven days that start on first's weekday; weekday counts Monday to Friday.
func periodFuncs(step string, first time.Time) (index func(time.Time) int, label func(int) string) {
	// Days come from Unix seconds: time.Duration overflows after 292 years.
	days := func(d time.Time) int { return int(floorDiv(d.Unix()-first.Unix(), 86400)) }
	switch step {
	case "year":
		return func(d time.Time) int { return d.Year() },
			func(i int) string { return fmt.Sprintf("%d", i) }
	case "quarter":
		return func(d time.Time) int { return d.Year()*4 + (int(d.Month())-1)/3 },
			func(i int) string { return fmt.Sprintf("%d-Q%d", i/4, i%4+1) }
	case "month":
		return func(d time.Time) int { return d.Year()*12 + int(d.Month()) - 1 },
			func(i int) string { return fmt.Sprintf("%d-%02d", i/12, i%12+1) }
	case "week":
		return func(d time.Time) int { return days(d) / 7 },
			func(i int) string { return first.AddDate(0, 0, 7*i).Format(time.DateOnly) }
	case "weekday":
		// k counts days from Monday 1969-12-29, so k mod 7 is 0 on a Monday.
		return func(d time.Time) int {
				k := floorDiv(d.Unix(), 86400) + 3
				w, r := floorDiv(k, 7), k-7*floorDiv(k, 7)
				return int(w*5 + min(r, 5))
			}, func(i int) string {
				w := floorDiv(int64(i), 5)
				k := w*7 + int64(i) - 5*w
				return time.Unix((k-3)*86400, 0).UTC().Format(time.DateOnly)
			}
	default:
		return days, func(i int) string { return first.AddDate(0, 0, i).Format(time.DateOnly) }
	}
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}
