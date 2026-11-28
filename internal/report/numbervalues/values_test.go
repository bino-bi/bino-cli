package numbervalues

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// fixtureCapture is shaped like the browser capture of a page with a Table
// (pl1, ac1, dac1_pl1_pos, drac1_pl1_pos, with a total row) and a ChartTime
// whose ac1 bars carry value labels.
const fixtureCapture = `{"components":[
 {"id":"bn-chart-time[0]","tag":"bn-chart-time","kind":"ChartTime","page":1,"pageName":"sales",
  "datasets":"monthly","dateInterval":"month","measureScale":"_","measureUnit":"kEUR","items":[
  {"kind":"bar","segment":"base","scenario":"scenario1","value":1199,"point":{"date":"2025-01-01","scenario":"ac1","part":"element"}},
  {"kind":"label","role":"data","text":"1,199","point":{"date":"2025-01-01","scenario":"ac1","part":"label"}},
  {"kind":"bar","segment":"base","scenario":"scenario1","value":1363,"point":{"date":"2025-02-01","scenario":"ac1","part":"element"}},
  {"kind":"label","role":"data","text":"1,363","point":{"date":"2025-02-01","scenario":"ac1","part":"label"}},
  {"kind":"bar","segment":"base","scenario":"scenario2","value":1208,"point":{"date":"2025-01-01","scenario":"pl1","part":"element"}},
  {"kind":"bar","segment":"base","scenario":"scenario2","value":1365,"point":{"date":"2025-02-01","scenario":"pl1","part":"element"}},
  {"kind":"label","role":"category","text":"Jan"}
 ]},
 {"id":"bn-table[0]","tag":"bn-table","kind":"Table","name":"regions","page":1,"pageName":"sales",
  "datasets":"region","measureScale":"_","measureUnit":"kEUR","items":[
  {"kind":"cell","key":"category","value":"North","text":"North","point":{"category":"North","part":"element"}},
  {"kind":"cell","key":"pl1","type":"decimal","value":4920,"text":"4,920","point":{"category":"North","scenario":"pl1","part":"element"}},
  {"kind":"cell","key":"ac1","type":"decimal","value":4943.5,"text":"4,944","point":{"category":"North","scenario":"ac1","part":"element"}},
  {"kind":"cell","key":"dac1_pl1_pos","type":"decimal","value":23.5,"text":"+24","point":{"category":"North","scenario":"dac1_pl1_pos","part":"element"}},
  {"kind":"cell","key":"drac1_pl1_pos","type":"percent","value":0.004776422764227642,"text":"+0.48%","point":{"category":"North","scenario":"drac1_pl1_pos","part":"element"}},
  {"kind":"cell","key":"pl1","type":"decimal","value":0,"text":"0","point":{"category":"East","scenario":"pl1","part":"element"}},
  {"kind":"cell","key":"ac1","type":"decimal","value":10,"text":"10","point":{"category":"East","scenario":"ac1","part":"element"}},
  {"kind":"cell","key":"drac1_pl1_pos","type":"percent","text":"n/a","point":{"category":"East","scenario":"drac1_pl1_pos","part":"element"}},
  {"kind":"cell","key":"ac1","type":"SUM","value":4953.5,"text":"4,954","point":{"row":"total","scenario":"ac1","part":"element"}},
  {"kind":"cell","key":"pl1","type":"SUM","value":4920,"text":"4,920","point":{"row":"total","scenario":"pl1","part":"element"}}
 ]}
]}`

// fixtureRows are the DataSet rows the fixture shows. Monthly rows are keyed
// by month end, as a GROUP BY on a month-end column gives them.
func fixtureRows() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"monthly": json.RawMessage(`[{"date":"2025-01-31","ac1":1199,"pl1":1208},{"date":"2025-02-28","ac1":1363,"pl1":1365}]`),
		"region":  json.RawMessage(`[{"category":"North","categoryIndex":1,"ac1":4943.5,"pl1":4920},{"category":"East","categoryIndex":2,"ac1":10,"pl1":0}]`),
	}
}

func buildFixture(t *testing.T) *File {
	t.Helper()
	file, err := Build([]byte(fixtureCapture))
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func text(s string) *string { return &s }

func TestBuild_TableCellsAndChartLabels(t *testing.T) {
	file := buildFixture(t)
	if len(file.Components) != 2 {
		t.Fatalf("got %d components, want 2", len(file.Components))
	}

	chart := file.Components[0]
	if chart.Kind != "ChartTime" || chart.Page != 1 || chart.MeasureUnit != "kEUR" || !reflect.DeepEqual(chart.Datasets, []string{"monthly"}) {
		t.Errorf("chart identity: %+v", chart)
	}
	wantChart := []Value{
		{Key: "ac1", Point: map[string]string{"date": "2025-01-01"}, Value: float64(1199), Text: text("1,199")},
		{Key: "ac1", Point: map[string]string{"date": "2025-02-01"}, Value: float64(1363), Text: text("1,363")},
		{Key: "pl1", Point: map[string]string{"date": "2025-01-01"}, Value: float64(1208)},
		{Key: "pl1", Point: map[string]string{"date": "2025-02-01"}, Value: float64(1365)},
	}
	if !reflect.DeepEqual(chart.Values, wantChart) {
		t.Errorf("chart values:\n got %s\nwant %s", mustJSON(chart.Values), mustJSON(wantChart))
	}

	table := file.Components[1]
	if table.Kind != "Table" || table.Name != "regions" {
		t.Errorf("table identity: %+v", table)
	}
	if len(table.Values) != 9 {
		t.Fatalf("table has %d values, want 9 (the category column is no number): %s", len(table.Values), mustJSON(table.Values))
	}
	wantFirst := Value{Key: "pl1", Point: map[string]string{"category": "North"}, Value: float64(4920), Text: text("4,920")}
	if !reflect.DeepEqual(table.Values[0], wantFirst) {
		t.Errorf("first table value %s, want %s", mustJSON(table.Values[0]), mustJSON(wantFirst))
	}
	for _, key := range []string{"ac1", "pl1", "dac1_pl1_pos", "drac1_pl1_pos"} {
		if !hasKey(table.Values, key) {
			t.Errorf("table values have no %s", key)
		}
	}
	if file.Count() != 13 {
		t.Errorf("Count() = %d, want 13", file.Count())
	}
}

func TestVerify_NoDifference(t *testing.T) {
	file := buildFixture(t)
	Verify(file, fixtureRows())
	if file.Differences == nil || len(*file.Differences) != 0 {
		t.Fatalf("differences = %s, want an empty list", mustJSON(file.Differences))
	}
	want := Summary{Values: 13, Matched: 13}
	if !reflect.DeepEqual(*file.Check, want) {
		t.Errorf("check = %s, want %s", mustJSON(file.Check), mustJSON(want))
	}
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"differences":[]`) {
		t.Errorf("an empty differences list must be written: %s", data)
	}
}

func TestVerify_ChangedRowIsADifference(t *testing.T) {
	file := buildFixture(t)
	rows := fixtureRows()
	// the data changed after the report was drawn: North actual 4943.5 -> 5000
	rows["region"] = json.RawMessage(`[{"category":"North","ac1":5000,"pl1":4920},{"category":"East","ac1":10,"pl1":0}]`)
	Verify(file, rows)

	got := map[string]Difference{}
	for _, d := range *file.Differences {
		got[d.Key+"/"+d.Point["category"]+d.Point["row"]] = d
	}
	// the cell itself, both variances computed from it and the total
	for _, want := range []string{"ac1/North", "dac1_pl1_pos/North", "drac1_pl1_pos/North", "ac1/total"} {
		if _, ok := got[want]; !ok {
			t.Errorf("no difference for %s; got %s", want, mustJSON(file.Differences))
		}
	}
	if len(got) != 4 || file.Check.Differences != 4 || file.Check.Matched != 9 {
		t.Errorf("check = %s, differences = %s", mustJSON(file.Check), mustJSON(file.Differences))
	}
	d := got["ac1/North"]
	if d.Component != "bn-table[0]" || d.Shown != float64(4943.5) || d.Expected != float64(5000) || d.Text == nil || *d.Text != "4,944" {
		t.Errorf("difference = %s", mustJSON(d))
	}
	if d.Reason == "" {
		t.Error("a difference needs a reason")
	}
}

func TestVerify_Unmatched(t *testing.T) {
	capture := `{"components":[
	 {"id":"bn-table[0]","kind":"Table","datasets":"region","items":[
	  {"kind":"cell","key":"ac1","value":7,"point":{"category":"South","part":"element"}},
	  {"kind":"cell","key":"ac1","value":7},
	  {"kind":"cell","key":"fc1","value":7,"point":{"category":"North","part":"element"}}
	 ]},
	 {"id":"bn-table[1]","kind":"Table","datasets":"region","filter":"category = 'North'","items":[
	  {"kind":"cell","key":"ac1","value":4943.5,"point":{"category":"North","part":"element"}}
	 ]},
	 {"id":"bn-table[2]","kind":"Table","datasets":"missing","items":[
	  {"kind":"cell","key":"ac1","value":1,"point":{"category":"North","part":"element"}}
	 ]}
	]}`
	file, err := Build([]byte(capture))
	if err != nil {
		t.Fatal(err)
	}
	Verify(file, fixtureRows())
	want := Summary{Values: 5, Unmatched: 5, UnmatchedReasons: map[string]int{
		reasonNoRow:    1,
		reasonNoPoint:  1,
		reasonNoColumn: 1,
		reasonFilter:   1,
		reasonNoRows:   1,
	}}
	if !reflect.DeepEqual(*file.Check, want) {
		t.Errorf("check = %s, want %s", mustJSON(file.Check), mustJSON(want))
	}
	if len(*file.Differences) != 0 {
		t.Errorf("unmatched values are no differences: %s", mustJSON(file.Differences))
	}
}

func TestVerify_RelativeVarianceAgainstZero(t *testing.T) {
	// East plans 0: the engine shows n/a. A number there is a difference.
	capture := `{"components":[{"id":"t","kind":"Table","datasets":"region","items":[
	  {"kind":"cell","key":"drac1_pl1_pos","value":0.5,"text":"+50%","point":{"category":"East"}}]}]}`
	file, err := Build([]byte(capture))
	if err != nil {
		t.Fatal(err)
	}
	Verify(file, fixtureRows())
	if file.Check.Differences != 1 || (*file.Differences)[0].Expected != nil {
		t.Errorf("check = %s, differences = %s", mustJSON(file.Check), mustJSON(file.Differences))
	}
}

func TestDateBucket(t *testing.T) {
	cases := []struct{ interval, in, want string }{
		{"month", "2025-01-31", "2025-01-01"},
		{"month", "2025-01-31T00:00:00Z", "2025-01-01"},
		{"quarter", "2025-05-17", "2025-04-01"},
		{"year", "2025-05-17 00:00:00", "2025-01-01"},
		{"week", "2025-01-05", "2024-12-30"}, // a Sunday belongs to the week of the Monday before
		{"day", "2025-01-05T13:00:00", "2025-01-05"},
		{"hour", "2025-01-05T13:45:00", "2025-01-05T13:00:00"},
	}
	for _, c := range cases {
		got, ok := dateBucket(c.interval)(c.in)
		if !ok || got != c.want {
			t.Errorf("dateBucket(%q)(%q) = %q, %v; want %q", c.interval, c.in, got, ok, c.want)
		}
	}
	for _, unknown := range []string{"", "auto", "setname", "none"} {
		if dateBucket(unknown) != nil {
			t.Errorf("dateBucket(%q) is not nil", unknown)
		}
	}
}

// Without the interval the engine used, a date point is never compared: a
// monthly bucket over daily rows would look like a difference.
func TestVerify_UnknownIntervalIsUnmatched(t *testing.T) {
	capture := `{"components":[{"id":"c","kind":"ChartTime","datasets":"daily","dateInterval":"auto","items":[
	  {"kind":"bar","value":3,"point":{"date":"2025-01-01","scenario":"ac1","part":"element"}}]}]}`
	file, err := Build([]byte(capture))
	if err != nil {
		t.Fatal(err)
	}
	Verify(file, map[string]json.RawMessage{"daily": json.RawMessage(`[{"date":"2025-01-01","ac1":1},{"date":"2025-01-02","ac1":2}]`)})
	want := Summary{Values: 1, Unmatched: 1, UnmatchedReasons: map[string]int{reasonInterval: 1}}
	if !reflect.DeepEqual(*file.Check, want) {
		t.Errorf("check = %s, want %s", mustJSON(file.Check), mustJSON(want))
	}
}

// Axis ticks, titles and other text of a chart carry no data-point key and
// are no shown values.
func TestBuild_SkipsTextWithoutDataPoint(t *testing.T) {
	capture := `{"components":[{"id":"bn-chart-scatter[0]","kind":"ChartScatter","items":[
	  {"kind":"label","role":"data","text":"100"},
	  {"kind":"label","role":"axisLabel","text":"2025","point":{"category":"x","part":"element"}},
	  {"kind":"bar","value":7},
	  {"kind":"point","text":"5"},
	  {"kind":"label","role":"data","text":"42","point":{"category":"A","scenario":"ac1","part":"label"}}]}]}`
	file, err := Build([]byte(capture))
	if err != nil {
		t.Fatal(err)
	}
	want := []Value{{Key: "ac1", Point: map[string]string{"category": "A"}, Text: text("42")}}
	if got := file.Components[0].Values; !reflect.DeepEqual(got, want) {
		t.Errorf("values = %s, want %s", mustJSON(got), mustJSON(want))
	}
}

func hasKey(values []Value, key string) bool {
	for _, v := range values {
		if v.Key == key {
			return true
		}
	}
	return false
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(data)
}

func TestVerify_RowValuesAsTheEngineKeysThem(t *testing.T) {
	// a numeric category comes back as 2024.0 from the database, the engine keys it "2024"
	capture := `{"components":[
	 {"id":"a","kind":"Table","datasets":"years","items":[{"kind":"cell","key":"ac1","value":5,"point":{"category":"2024"}}]},
	 {"id":"b","kind":"Table","datasets":"years,region","items":[{"kind":"cell","key":"ac1","value":5,"point":{"category":"2024"}}]},
	 {"id":"c","kind":"ChartTime","datasets":"dates","dateInterval":"month","items":[{"kind":"bar","value":3,"point":{"date":"2025-01-01","scenario":"ac1"}}]}
	]}`
	file, err := Build([]byte(capture))
	if err != nil {
		t.Fatal(err)
	}
	rows := fixtureRows()
	rows["years"] = json.RawMessage(`[{"category":2024.0,"ac1":5}]`)
	rows["dates"] = json.RawMessage(`[{"date":"2025-01-31","ac1":3},{"date":"someday","ac1":1}]`)
	Verify(file, rows)
	want := Summary{Values: 3, Matched: 1, Unmatched: 2, UnmatchedReasons: map[string]int{reasonDatasets: 1, reasonDate: 1}}
	if !reflect.DeepEqual(*file.Check, want) {
		t.Errorf("check = %s, want %s", mustJSON(file.Check), mustJSON(want))
	}
}
