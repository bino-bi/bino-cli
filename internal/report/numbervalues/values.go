// Package numbervalues turns what the tables and charts of a rendered report
// show into data: the values file `bino build --number-values` writes next to
// the PDF, and the check of those values against the DataSet rows that
// `--verify-numbers` adds.
//
// The browser side reads each component's full layout detail (see
// internal/chrome/number_values.go). Every shown number carries a data-point
// key there: the scenario or variance column and the category, date or row
// group it belongs to. That key is what joins a value to a DataSet row.
package numbervalues

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Version is the format version of the values file.
const Version = 1

// Capture is the browser's answer, one entry per table or chart.
type Capture struct {
	Components []CapturedComponent `json:"components"`
}

// CapturedComponent is one table or chart as the page reports it.
type CapturedComponent struct {
	ID           string `json:"id"`
	Tag          string `json:"tag"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Ref          string `json:"ref"`
	Page         int    `json:"page"`
	PageName     string `json:"pageName"`
	Datasets     string `json:"datasets"`
	Filter       string `json:"filter"`
	DateInterval string `json:"dateInterval"`
	MeasureScale string `json:"measureScale"`
	MeasureUnit  string `json:"measureUnit"`
	Error        string `json:"error"`
	Items        []Item `json:"items"`
}

// Item is one element of the layout detail: a table cell, a bar, a label or a mark.
type Item struct {
	Kind     string            `json:"kind"`
	Key      string            `json:"key"`
	Type     string            `json:"type"`
	Role     string            `json:"role"`
	Segment  string            `json:"segment"`
	Scenario string            `json:"scenario"`
	Series   string            `json:"series"`
	Value    any               `json:"value"`
	Text     *string           `json:"text"`
	Point    map[string]string `json:"point"`
}

// File is the values file.
type File struct {
	Version    int         `json:"version"`
	Components []Component `json:"components"`
	// Check and Differences are set by Verify: Differences is then always
	// present, so an empty list means "checked, nothing differs".
	Check       *Summary      `json:"check,omitempty"`
	Differences *[]Difference `json:"differences,omitempty"`
}

// Component is what one table or chart shows.
type Component struct {
	// ID is the engine's component id, the same as in the layout state file.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Ref  string `json:"ref,omitempty"`
	// Page is the 1-based LayoutPage of the report, PageName its metadata.name.
	Page         int      `json:"page,omitempty"`
	PageName     string   `json:"pageName,omitempty"`
	Datasets     []string `json:"datasets,omitempty"`
	MeasureScale string   `json:"measureScale,omitempty"`
	MeasureUnit  string   `json:"measureUnit,omitempty"`
	Error        string   `json:"error,omitempty"`
	Values       []Value  `json:"values"`

	filter       string
	dateInterval string
}

// Value is one shown number. Value is the raw number the engine computed
// (or a string such as "n/a"), Text what is printed: scaled and formatted.
type Value struct {
	Key   string            `json:"key,omitempty"`
	Point map[string]string `json:"point,omitempty"`
	Value any               `json:"value,omitempty"`
	Text  *string           `json:"text,omitempty"`
}

var (
	slotPattern     = regexp.MustCompile(`^(ac|pp|fc|pl)[1-4]$`)
	variancePattern = regexp.MustCompile(`^(dr|d)((?:ac|pp|fc|pl)[1-4])_((?:ac|pp|fc|pl)[1-4])_(pos|neg|neu|opinv|op)$`)
)

// isNumberKey reports whether a column key is a scenario slot or a variance.
func isNumberKey(key string) bool {
	return slotPattern.MatchString(key) || variancePattern.MatchString(key)
}

// Build turns a capture into the values file.
func Build(raw []byte) (*File, error) {
	var capture Capture
	if err := json.Unmarshal(raw, &capture); err != nil {
		return nil, fmt.Errorf("read number values: %w", err)
	}
	file := &File{Version: Version, Components: make([]Component, 0, len(capture.Components))}
	for _, c := range capture.Components {
		file.Components = append(file.Components, buildComponent(c))
	}
	return file, nil
}

func buildComponent(c CapturedComponent) Component {
	kind := c.Kind
	if kind == "" {
		kind = c.Tag
	}
	out := Component{
		ID:           c.ID,
		Kind:         kind,
		Name:         c.Name,
		Ref:          c.Ref,
		Page:         c.Page,
		PageName:     c.PageName,
		Datasets:     splitList(c.Datasets),
		MeasureScale: c.MeasureScale,
		MeasureUnit:  c.MeasureUnit,
		Error:        c.Error,
		Values:       []Value{},
		filter:       strings.TrimSpace(c.Filter),
		dateInterval: c.DateInterval,
	}

	// A bar and its value label are two elements of one data point: the bar
	// has the raw value, the label the printed text. They share the key.
	merged := map[string]int{}
	for _, item := range c.Items {
		v, ok := itemValue(item)
		if !ok {
			continue
		}
		if item.Kind != "cell" && v.Point != nil {
			id := pointID(v.Key, v.Point)
			if i, seen := merged[id]; seen {
				mergeValue(&out.Values[i], v)
				continue
			}
			merged[id] = len(out.Values)
		}
		out.Values = append(out.Values, v)
	}
	return out
}

// itemValue keeps an element that shows a number: a cell of a scenario or
// variance column or with a numeric value, a bar, a data label, a mark.
func itemValue(item Item) (Value, bool) {
	key := item.Key
	if key == "" {
		key = item.Point["scenario"]
	}
	point := cleanPoint(item.Point, key)
	v := Value{Key: key, Point: point, Value: item.Value, Text: item.Text}
	switch item.Kind {
	case "cell":
		if _, number := item.Value.(float64); !number && !isNumberKey(key) {
			return Value{}, false
		}
	case "label":
		// a value label has a data-point key with part label (or total);
		// axis ticks and titles have none
		if part := item.Point["part"]; part != "label" && part != "total" {
			return Value{}, false
		}
	case "bar", "point":
		if item.Point == nil {
			return Value{}, false
		}
	default:
		return Value{}, false
	}
	return v, true
}

// cleanPoint drops the parts of a data-point key the value already says:
// its scenario (the value's key) and which element of the data point it is,
// keeping part "total", which is a value of its own.
func cleanPoint(point map[string]string, key string) map[string]string {
	if len(point) == 0 {
		return nil
	}
	out := make(map[string]string, len(point))
	for k, v := range point {
		if (k == "part" && v != "total") || (k == "scenario" && v == key) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeValue(into *Value, from Value) {
	if into.Value == nil {
		into.Value = from.Value
	}
	if into.Text == nil {
		into.Text = from.Text
	}
}

func pointID(key string, point map[string]string) string {
	keys := make([]string, 0, len(point))
	for k := range point {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(key)
	for _, k := range keys {
		b.WriteString("\x00" + k + "=" + point[k])
	}
	return b.String()
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Count is the number of captured values.
func (f *File) Count() int {
	n := 0
	for _, c := range f.Components {
		n += len(c.Values)
	}
	return n
}
