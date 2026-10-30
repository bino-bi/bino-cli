// Package annotation holds what the renderer and the linter share about
// Annotation documents: parsing, and whether a point can exist in the data.
package annotation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/schema"
)

// Parse reads the spec of an Annotation document.
func Parse(raw json.RawMessage) (schema.AnnotationSpec, error) {
	var payload struct {
		Spec schema.AnnotationSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return schema.AnnotationSpec{}, err
	}
	return payload.Spec, nil
}

// HasContent reports whether an annotation has something to list. Only those
// get a number.
func HasContent(s schema.AnnotationSpec) bool {
	return strings.TrimSpace(s.Title) != "" || strings.TrimSpace(s.Text) != ""
}

// Key identifies a component on a page.
func Key(kind, name string) string { return kind + ":" + name }

// Rows are the rows of one dataset.
type Rows []map[string]any

// DecodeRows reads a JSON array of rows. Numbers stay as written, so a category
// of 2024 compares equal to the point value "2024".
func DecodeRows(raw json.RawMessage) (Rows, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	rows := Rows{}
	if err := dec.Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// FirstDataset returns the first dataset a component spec binds, "$name" for a
// DataSource, or "" when it binds none.
func FirstDataset(componentSpec json.RawMessage) string {
	var p struct {
		Dataset spec.DatasetList `json:"dataset"`
	}
	if json.Unmarshal(componentSpec, &p) != nil {
		return ""
	}
	if names := p.Dataset.Strings(); len(names) > 0 {
		return names[0]
	}
	return ""
}

// pointFields are the point fields the engine reports per kind. A kind that is
// missing here has no data points at all.
var pointFields = map[string][]string{
	schema.KindChartTime:      {"date", "scenario"},
	schema.KindChartStructure: {"rowGroup", "category", "subCategory", "scenario"},
	schema.KindChartScatter:   {"rowGroup", "category", "subCategory"},
	schema.KindChartBubble:    {"rowGroup", "category", "subCategory"},
	schema.KindChartBullet:    {"rowGroup", "category", "subCategory", "scenario"},
	schema.KindTable:          {"rowGroup", "category", "subCategory", "date", "scenario", "row"},
}

// CheckPoint returns why a point cannot exist in a component, or "" when it can.
// Nil rows mean the data is not known, so only the fields of the point are
// judged. A row with the values is no proof of a single rendered element: the
// engine has the last word, and a component's filter is not looked at.
func CheckPoint(kind string, p *schema.AnnotationPoint, rows Rows) string {
	if p == nil || *p == (schema.AnnotationPoint{}) {
		return ""
	}
	if reason := checkFields(kind, p); reason != "" {
		return reason
	}
	if rows == nil || p.Row == "total" {
		return ""
	}
	withValues := false
	for _, row := range rows {
		if !matchesDimensions(row, p) {
			continue
		}
		withValues = true
		if scenarioHasValue(kind, row, p.Scenario) {
			return ""
		}
	}
	if withValues {
		return fmt.Sprintf("scenario %q has no value on the rows with %s", p.Scenario, Describe(p))
	}
	return fmt.Sprintf("no row of the dataset has %s", Describe(p))
}

func checkFields(kind string, p *schema.AnnotationPoint) string {
	allowed, ok := pointFields[kind]
	if !ok {
		return kind + " has no data points, use a target without point"
	}
	for _, f := range setFields(p) {
		if !contains(allowed, f) {
			return fmt.Sprintf("a data point of %s has no %s, only %s", kind, f, strings.Join(allowed, ", "))
		}
	}
	if kind == schema.KindTable && p.Part != "" && p.Part != "element" {
		return fmt.Sprintf("a data point of Table has no part %q, only element", p.Part)
	}
	return ""
}

// Describe renders the set fields of a point for a message.
func Describe(p *schema.AnnotationPoint) string {
	fields := setFields(p)
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f+"="+fieldValue(p, f))
	}
	return strings.Join(parts, ", ")
}

func setFields(p *schema.AnnotationPoint) []string {
	var fields []string
	for _, f := range []string{"rowGroup", "category", "subCategory", "date", "scenario", "row"} {
		if fieldValue(p, f) != "" {
			fields = append(fields, f)
		}
	}
	return fields
}

func fieldValue(p *schema.AnnotationPoint, field string) string {
	switch field {
	case "rowGroup":
		return p.RowGroup
	case "category":
		return p.Category
	case "subCategory":
		return p.SubCategory
	case "date":
		return p.Date
	case "scenario":
		return p.Scenario
	case "row":
		return p.Row
	}
	return ""
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func matchesDimensions(row map[string]any, p *schema.AnnotationPoint) bool {
	for _, f := range []string{"rowGroup", "category", "subCategory"} {
		if want := fieldValue(p, f); want != "" && text(row[f]) != want {
			return false
		}
	}
	return p.Date == "" || dateInPeriod(text(row["date"]), p.Date)
}

// dateInPeriod reports whether want can be the first day of a period around the
// row's date. The interval of a chart can be left to the engine, so every
// period counts. Too generous at worst, and then the engine draws nothing.
func dateInPeriod(rowDate, want string) bool {
	if len(rowDate) < 10 || len(want) < 10 {
		return false
	}
	day, err := time.Parse(time.DateOnly, rowDate[:10])
	if err != nil {
		return false
	}
	start, err := time.Parse(time.DateOnly, want[:10])
	if err != nil {
		return false
	}
	// a week starts on a day that depends on the locale
	if age := day.Sub(start); age >= 0 && age < 7*24*time.Hour {
		return true
	}
	if start.Year() != day.Year() || start.Day() != 1 {
		return false
	}
	return start.Month() == day.Month() || start.Month() == time.January ||
		start.Month() == time.Month((int(day.Month())-1)/3*3+1)
}

var (
	slotToken     = regexp.MustCompile(`^(?:ac|pp|fc|pl)[1-4]$`)
	varianceToken = regexp.MustCompile(`^(?:dr|d)((?:ac|pp|fc|pl)[1-4])_((?:ac|pp|fc|pl)[1-4])(?:_(?:pos|neg|neu))?$`)
)

func scenarioHasValue(kind string, row map[string]any, scenario string) bool {
	switch {
	case scenario == "":
		return true
	case slotToken.MatchString(scenario):
		return row[scenario] != nil
	}
	if m := varianceToken.FindStringSubmatch(scenario); m != nil {
		return row[m[1]] != nil && row[m[2]] != nil
	}
	// A table column can be an attribute, which is computed and not in the rows.
	return kind == schema.KindTable || row[scenario] != nil
}

func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
