package numbervalues

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Summary counts the outcome of Verify. Every captured value is counted once.
type Summary struct {
	Values      int `json:"values"`
	Matched     int `json:"matched"`
	Unmatched   int `json:"unmatched"`
	Differences int `json:"differences"`
	// UnmatchedReasons says why values could not be compared, with how often.
	UnmatchedReasons map[string]int `json:"unmatchedReasons,omitempty"`
}

// Difference is a shown value that does not match the DataSet rows.
type Difference struct {
	// Component is the component id, as in the components list.
	Component string            `json:"component"`
	Key       string            `json:"key"`
	Point     map[string]string `json:"point,omitempty"`
	// Shown is the raw value the engine drew, Text what it printed.
	Shown    any     `json:"shown"`
	Text     *string `json:"text,omitempty"`
	Expected any     `json:"expected"`
	Reason   string  `json:"reason"`
}

// Unmatched reasons, as constants so the summary groups them.
const (
	reasonNoRows    = "dataset rows not available"
	reasonFilter    = "component has a filter"
	reasonNotNumber = "not a scenario or variance column"
	reasonNoPoint   = "no data point key"
	reasonNoRow     = "no DataSet row for the data point"
	reasonNoColumn  = "scenario column not in the DataSet"
	reasonNoRaw     = "no raw value"
	reasonOperation = "rows with operation '-'"
	reasonAmbiguous = "several values with the same data point"
	reasonDate      = "date not readable"
	reasonInterval  = "date interval unknown"
	reasonDatasets  = "component reads several datasets"
)

// tolerance is relative to the expected value: the engine sums in
// JavaScript, this check in Go, so the last bits can differ.
const tolerance = 1e-9

// Verify compares every shown value of a scenario slot or variance with the
// DataSet rows of its component and fills file.Check and file.Differences.
// datasets maps a dataset name to its rows as a JSON array of objects.
func Verify(file *File, datasets map[string]json.RawMessage) {
	summary := &Summary{UnmatchedReasons: map[string]int{}}
	differences := []Difference{}
	decoded := map[string][]map[string]any{}

	for _, c := range file.Components {
		rows, ok := componentRows(c, datasets, decoded)
		seen := map[string]int{}
		for _, v := range c.Values {
			seen[pointID(v.Key, v.Point)]++
		}
		for _, v := range c.Values {
			summary.Values++
			reason := ""
			switch {
			case !ok:
				reason = reasonNoRows
			case len(c.Datasets) > 1:
				// how the engine joins several datasets is not checked here
				reason = reasonDatasets
			case c.filter != "":
				reason = reasonFilter
			case v.Point != nil && seen[pointID(v.Key, v.Point)] > 1:
				reason = reasonAmbiguous
			}
			var diff *Difference
			if reason == "" {
				diff, reason = check(c, v, rows)
			}
			switch {
			case reason != "":
				summary.Unmatched++
				summary.UnmatchedReasons[reason]++
			case diff != nil:
				summary.Differences++
				differences = append(differences, *diff)
			default:
				summary.Matched++
			}
		}
	}
	if len(summary.UnmatchedReasons) == 0 {
		summary.UnmatchedReasons = nil
	}
	file.Check = summary
	file.Differences = &differences
}

// componentRows joins the rows of the component's datasets.
func componentRows(c Component, datasets map[string]json.RawMessage, decoded map[string][]map[string]any) ([]map[string]any, bool) {
	if len(c.Datasets) == 0 {
		return nil, false
	}
	var rows []map[string]any
	for _, name := range c.Datasets {
		if _, done := decoded[name]; !done {
			raw, ok := datasets[name]
			if !ok {
				return nil, false
			}
			var parsed []map[string]any
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&parsed); err != nil {
				return nil, false
			}
			decoded[name] = parsed
		}
		rows = append(rows, decoded[name]...)
	}
	return rows, true
}

// pointDims are the data-point fields that select DataSet rows.
var pointDims = []string{"rowGroup", "category", "subCategory", "date"}

// check compares one value. It returns a difference, or an unmatched
// reason, or neither for a match.
func check(c Component, v Value, rows []map[string]any) (diff *Difference, unmatched string) {
	if !isNumberKey(v.Key) {
		return nil, reasonNotNumber
	}
	dims := map[string]string{}
	for _, dim := range pointDims {
		if value, ok := v.Point[dim]; ok {
			dims[dim] = value
		}
	}
	if len(dims) == 0 && v.Point["row"] != "total" {
		return nil, reasonNoPoint
	}

	matched, reason := selectRows(rows, dims, c.dateInterval)
	if reason != "" {
		return nil, reason
	}
	for _, row := range matched {
		if op, _ := row["operation"].(string); strings.TrimSpace(op) == "-" {
			return nil, reasonOperation
		}
	}

	expected, how, reason := expectedValue(v.Key, matched)
	if reason != "" {
		return nil, reason
	}

	shown, shownIsNumber := toFloat(v.Value)
	switch {
	case v.Value == nil && v.Text == nil:
		return nil, reasonNoRaw
	case expected == nil:
		// no value in the data, or a relative variance against 0: nothing to draw
		if v.Value == nil || !shownIsNumber {
			return nil, ""
		}
	case v.Value == nil:
		return nil, reasonNoRaw
	case shownIsNumber && closeEnough(shown, *expected):
		return nil, ""
	}
	diff = &Difference{Component: c.ID, Key: v.Key, Point: v.Point, Shown: v.Value, Text: v.Text, Reason: how}
	if expected != nil {
		diff.Expected = *expected
	}
	return diff, ""
}

// selectRows returns the rows whose dimensions equal the point's. A date
// matches by the bucket of the component's date interval.
func selectRows(rows []map[string]any, dims map[string]string, interval string) (matched []map[string]any, unmatched string) {
	bucket := dateBucket(interval)
	if want, ok := dims["date"]; ok {
		if bucket == nil {
			// a period of unknown length: matching exact dates could report false differences
			return nil, reasonInterval
		}
		b, ok := bucket(want)
		if !ok {
			return nil, reasonDate
		}
		dims["date"] = b
	}
	for _, row := range rows {
		ok, readable := rowMatches(row, dims, bucket)
		if !readable {
			// a row that cannot be compared could belong to the point: no partial sums
			return nil, reasonDate
		}
		if ok {
			matched = append(matched, row)
		}
	}
	if len(matched) == 0 {
		return nil, reasonNoRow
	}
	return matched, ""
}

// rowMatches reports whether the row has the point's dimensions. readable
// is false for a row whose date cannot be read.
func rowMatches(row map[string]any, dims map[string]string, bucket func(string) (string, bool)) (match, readable bool) {
	match = true
	for dim, want := range dims {
		got, ok := row[dim]
		if !ok || got == nil {
			match = false
			continue
		}
		value := scalarString(got)
		if dim == "date" {
			if value, ok = bucket(value); !ok {
				return false, false
			}
		}
		if value != want {
			match = false
		}
	}
	return match, true
}

// expectedValue sums a scenario column over the rows, or recomputes a
// variance from the sums of its two scenarios the way the engine does.
// how describes the computation, for a difference.
func expectedValue(key string, rows []map[string]any) (expected *float64, how, reason string) {
	if slotPattern.MatchString(key) {
		sum, ok, present := sumColumn(rows, key)
		if !present {
			return nil, "", reasonNoColumn
		}
		how = fmt.Sprintf("sum of %s over %d DataSet row(s)", key, len(rows))
		if !ok {
			return nil, how + " is empty", ""
		}
		return &sum, how, ""
	}

	parts := variancePattern.FindStringSubmatch(key)
	kind, base, delta := parts[1], parts[2], parts[3]
	a, okA, presentA := sumColumn(rows, base)
	b, okB, presentB := sumColumn(rows, delta)
	if !presentA || !presentB {
		return nil, "", reasonNoColumn
	}
	if kind == "d" {
		how = fmt.Sprintf("%s - %s over %d DataSet row(s)", base, delta, len(rows))
	} else {
		how = fmt.Sprintf("(%s - %s) / |%s| over %d DataSet row(s)", base, delta, delta, len(rows))
	}
	if !okA || !okB {
		return nil, how + ": a scenario is empty", ""
	}
	difference := a - b
	if kind == "d" {
		return &difference, how, ""
	}
	// the engine's rule: a reference of 0, also up to float rounding, has no ratio ("n/a")
	if math.Abs(b) <= math.Abs(difference)*1e-9 {
		return nil, how + ": the reference is 0, expected n/a", ""
	}
	ratio := difference / math.Abs(b)
	return &ratio, how, ""
}

// sumColumn sums a numeric column. ok is false when no row has a value,
// present is false when no row has the column at all.
func sumColumn(rows []map[string]any, column string) (sum float64, ok, present bool) {
	for _, row := range rows {
		raw, has := row[column]
		if !has {
			continue
		}
		present = true
		if f, isNumber := toFloat(raw); isNumber {
			sum += f
			ok = true
		}
	}
	return sum, ok, present
}

func closeEnough(shown, expected float64) bool {
	return math.Abs(shown-expected) <= tolerance*math.Max(1, math.Abs(expected))
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

// scalarString is a row value as the engine writes it into a data-point key.
func scalarString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		// as JavaScript's String(): 2024.0 is "2024"
		if f, err := s.Float64(); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return s.String()
	case bool:
		return strconv.FormatBool(s)
	}
	return fmt.Sprint(v)
}

// dateLayouts are the forms a date reaches the rows in.
var dateLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02",
}

func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// formatPointDate writes a date the way the engine keys a data point:
// YYYY-MM-DD, with the time only when it is not midnight.
func formatPointDate(t time.Time) string {
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02T15:04:05")
}

// dateBucket maps a date to the start of its bucket for a date interval,
// written like the data-point key of the bucket. Weeks start on Monday
// (ISO), as in the engine. Nil for an interval that is not a known length
// (blank, auto, setname).
func dateBucket(interval string) func(string) (string, bool) {
	var start func(time.Time) time.Time
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "year":
		start = func(t time.Time) time.Time { return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location()) }
	case "quarter":
		start = func(t time.Time) time.Time {
			return time.Date(t.Year(), time.Month((int(t.Month())-1)/3*3+1), 1, 0, 0, 0, 0, t.Location())
		}
	case "month":
		start = func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()) }
	case "week":
		start = func(t time.Time) time.Time {
			offset := (int(t.Weekday()) + 6) % 7
			return time.Date(t.Year(), t.Month(), t.Day()-offset, 0, 0, 0, 0, t.Location())
		}
	case "day":
		start = func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	case "hour":
		start = func(t time.Time) time.Time { return t.Truncate(time.Hour) }
	case "minute":
		start = func(t time.Time) time.Time { return t.Truncate(time.Minute) }
	case "second":
		start = func(t time.Time) time.Time { return t.Truncate(time.Second) }
	default:
		return nil
	}
	return func(s string) (string, bool) {
		t, ok := parseDate(s)
		if !ok {
			return "", false
		}
		return formatPointDate(start(t)), true
	}
}
