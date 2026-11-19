package annotation

import (
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/schema"
)

func TestDecodeRowsKeepsNumbersAsWritten(t *testing.T) {
	rows, err := DecodeRows([]byte(`[{"category": 2024, "ac1": 1.50}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got := text(rows[0]["category"]); got != "2024" {
		t.Errorf("category = %q, want 2024", got)
	}
}

func TestCheckPoint(t *testing.T) {
	yearly := Rows{
		{"date": "2024-03-15", "ac1": 5, "pl1": nil},
		{"date": "2025-01-01T00:00:00Z", "ac1": 6, "pl1": 7},
	}
	byProduct := Rows{
		{"rowGroup": "Revenue", "category": "Software", "subCategory": "Cloud", "ac1": 5, "pl1": nil},
		{"rowGroup": "Revenue", "category": "Services", "ac1": nil, "pl1": 1},
	}
	tests := []struct {
		name  string
		kind  string
		point *schema.AnnotationPoint
		rows  Rows
		want  string // part of the reason, "" means no finding
	}{
		{"no point", schema.KindText, nil, nil, ""},
		{"empty point", schema.KindText, &schema.AnnotationPoint{}, nil, ""},
		{"a text has no data points", schema.KindText, &schema.AnnotationPoint{Category: "x"}, nil, "no data points"},
		{"a tree has no data points", schema.KindTree, &schema.AnnotationPoint{Part: "element"}, nil, "no data points"},

		{"unknown data is judged by fields only", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01"}, nil, ""},
		{"a time chart has no row group", schema.KindChartTime, &schema.AnnotationPoint{RowGroup: "x"}, nil, "has no rowGroup"},
		{"a time chart has a category: the series of a stack", schema.KindChartTime, &schema.AnnotationPoint{Category: "x"}, nil, ""},
		{"a scatter chart has no scenario", schema.KindChartScatter, &schema.AnnotationPoint{Category: "x", Scenario: "ac1"}, nil, "has no scenario"},
		{"a table has no label part", schema.KindTable, &schema.AnnotationPoint{Category: "x", Part: "label"}, nil, "no part"},

		{"exact date", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-03-15", Scenario: "ac1"}, yearly, ""},
		{"first day of the year", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01", Scenario: "ac1"}, yearly, ""},
		{"first day of the quarter", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01"}, yearly, ""},
		{"first day of the month", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-03-01"}, yearly, ""},
		{"first day of the week", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-03-11"}, yearly, ""},
		{"timestamp in the row", schema.KindChartTime, &schema.AnnotationPoint{Date: "2025-01-01", Scenario: "pl1"}, yearly, ""},
		{"time in the point", schema.KindChartTime, &schema.AnnotationPoint{Date: "2025-01-01T00:00:00"}, yearly, ""},
		{"other year", schema.KindChartTime, &schema.AnnotationPoint{Date: "2023-01-01"}, yearly, "no row"},
		{"later month of the year is no period start", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-02-01"}, yearly, "no row"},
		{"null scenario", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01", Scenario: "pl1"}, yearly, "has no value"},
		{"missing scenario column", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01", Scenario: "fc1"}, yearly, "has no value"},

		{"dimensions", schema.KindChartStructure, &schema.AnnotationPoint{Category: "Software", Scenario: "ac1"}, byProduct, ""},
		{"unknown value", schema.KindChartStructure, &schema.AnnotationPoint{Category: "Nothing"}, byProduct, "no row"},
		{"variance needs both slots", schema.KindChartStructure, &schema.AnnotationPoint{Category: "Software", Scenario: "dac1_pl1_pos"}, byProduct, "has no value"},
		{"variance without sentiment", schema.KindChartStructure, &schema.AnnotationPoint{Category: "Software", Scenario: "dac1_ac1"}, byProduct, ""},
		{"empty dataset", schema.KindChartStructure, &schema.AnnotationPoint{Category: "Software"}, Rows{}, "no row"},

		{"whole row", schema.KindTable, &schema.AnnotationPoint{Category: "Services"}, byProduct, ""},
		{"table attribute column", schema.KindTable, &schema.AnnotationPoint{Category: "Services", Scenario: "Owner"}, byProduct, ""},
		{"total row has no values", schema.KindTable, &schema.AnnotationPoint{Row: "total"}, byProduct, ""},
		{"subtotal checks its parent", schema.KindTable, &schema.AnnotationPoint{Row: "subtotal", RowGroup: "Revenue"}, byProduct, ""},
		{"subtotal of an unknown parent", schema.KindTable, &schema.AnnotationPoint{Row: "subtotal", RowGroup: "Costs"}, byProduct, "no row"},
		{"variance with its slots on two rows", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01", Scenario: "dac1_pl1_pos"}, Rows{{"date": "2024-01-05", "ac1": 1}, {"date": "2024-01-20", "pl1": 2}}, ""},
		{"family on a stack total", schema.KindChartTime, &schema.AnnotationPoint{Date: "2025-01-01", Scenario: "pl", Part: "total"}, yearly, ""},
		{"family without a value", schema.KindChartTime, &schema.AnnotationPoint{Date: "2025-01-01", Scenario: "fc", Part: "total"}, yearly, "has no value"},
		{"family is only a stack total", schema.KindChartTime, &schema.AnnotationPoint{Date: "2025-01-01", Scenario: "ac"}, yearly, "has no value"},
		{"row date as year and month", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01"}, Rows{{"date": "2024-01"}}, ""},
		{"row date as epoch milliseconds", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01"}, Rows{{"date": 1704067200000}}, ""},
		{"row without a date", schema.KindChartTime, &schema.AnnotationPoint{Date: "2024-01-01"}, Rows{{"ac1": 1}}, "no row"},
		{"bullet measure column", schema.KindChartBullet, &schema.AnnotationPoint{Category: "Software", Scenario: "fc2"}, byProduct, "has no value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckPoint(tt.kind, tt.point, tt.rows)
			if tt.want == "" && got != "" {
				t.Fatalf("got finding %q, want none", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("got %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestBinding(t *testing.T) {
	tests := map[string]string{
		`{"dataset": "a"}`:        "a",
		`{"dataset": ["$a"]}`:     "$a",
		`{"dataset": ["a", "b"]}`: "",
		`{}`:                      "",
		`{"dataset": {"bad": 1}}`: "",
		`not json`:                "",
	}
	for in, want := range tests {
		if got := Binding([]byte(in)); got != want {
			t.Errorf("Binding(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestHasContent(t *testing.T) {
	if HasContent(schema.AnnotationSpec{Title: "  "}) {
		t.Error("a blank title is no content")
	}
	if !HasContent(schema.AnnotationSpec{Text: "x"}) {
		t.Error("a text is content")
	}
}

func TestVarianceToken_OperationSentiment(t *testing.T) {
	for _, token := range []string{"dac1_pl1_op", "drac1_pl1_opinv"} {
		if m := varianceToken.FindStringSubmatch(token); m == nil || m[0] != token || m[1] != "ac1" || m[2] != "pl1" {
			t.Errorf("%s: match = %q", token, m)
		}
	}
	if varianceToken.MatchString("dac1_pl1_opx") {
		t.Error("dac1_pl1_opx must not match")
	}
}

func TestSumsRows(t *testing.T) {
	rows := Rows{
		{"category": "Spain", "rowGroup": "Europe", "ac1": 1},
		{"category": "France", "rowGroup": "Europe", "ac2": 2},
		{"category": "USA", "rowGroup": nil, "ac1": 3},
	}
	point := func(category, scenario string) *schema.AnnotationPoint {
		return &schema.AnnotationPoint{Category: category, Scenario: scenario}
	}

	if got := SumsOf(json.RawMessage(`{"dataset":"d"}`)); got != (Sums{}) {
		t.Errorf("no sums: got %+v", got)
	}
	if reason := CheckPoint(schema.KindChartStructure, point("Europe", "ac1"), Sums{}.Rows(rows)); reason == "" {
		t.Error("a chart without sums has no row Europe")
	}

	all := SumsOf(json.RawMessage(`{"sums":"all","sumTitle":"World"}`))
	if all != (Sums{Groups: true, Total: "World"}) {
		t.Fatalf("all: got %+v", all)
	}
	for _, p := range []*schema.AnnotationPoint{point("Europe", "ac1"), point("Europe", "ac2"), point("World", "ac1"), point("Spain", "ac1")} {
		if reason := CheckPoint(schema.KindChartStructure, p, all.Rows(rows)); reason != "" {
			t.Errorf("%s: %s", Describe(p), reason)
		}
	}
	if reason := CheckPoint(schema.KindChartStructure, point("Asia", ""), all.Rows(rows)); reason == "" {
		t.Error("there is no group Asia")
	}

	total := SumsOf(json.RawMessage(`{"sums":"total"}`))
	if total != (Sums{Total: "SUM_TOTAL"}) {
		t.Errorf("total without a title: got %+v", total)
	}
	if reason := CheckPoint(schema.KindChartStructure, point("Europe", ""), total.Rows(rows)); reason == "" {
		t.Error("sums: total adds no group row")
	}
	if got := all.Rows(nil); got != nil {
		t.Errorf("unknown rows stay unknown, got %v", got)
	}
	// a chart without rows draws no sum row
	if got := all.Rows(Rows{}); len(got) != 0 {
		t.Errorf("empty rows: got %v", got)
	}

	// the sum row has the level column of the chart, and group sums exist at level category only
	byGroup := SumsOf(json.RawMessage(`{"sums":"all","sumTitle":"World","level":"rowGroup"}`))
	if byGroup != (Sums{Groups: true, Total: "World", Level: "rowGroup"}) {
		t.Fatalf("level rowGroup: got %+v", byGroup)
	}
	if reason := CheckPoint(schema.KindChartStructure, &schema.AnnotationPoint{RowGroup: "World"}, byGroup.Rows(rows)); reason != "" {
		t.Errorf("rowGroup=World: %s", reason)
	}
	if reason := CheckPoint(schema.KindChartStructure, point("World", ""), byGroup.Rows(rows)); reason == "" {
		t.Error("at level rowGroup the sum row is no category")
	}
	if got := len(byGroup.Rows(rows)); got != len(rows)+1 {
		t.Errorf("level rowGroup: %d rows, want the rows and one total", got)
	}

	// without a level the chart takes the lowest dimension the rows fill
	deep := Rows{{"category": "Spain", "subCategory": "Madrid", "rowGroup": "Europe", "ac1": 1}}
	auto := SumsOf(json.RawMessage(`{"sums":"all","sumTitle":"World","level":"auto"}`))
	if reason := CheckPoint(schema.KindChartStructure, &schema.AnnotationPoint{SubCategory: "World"}, auto.Rows(deep)); reason != "" {
		t.Errorf("detected level subCategory: %s", reason)
	}
	if reason := CheckPoint(schema.KindChartStructure, point("Europe", ""), auto.Rows(deep)); reason == "" {
		t.Error("no group sum at the detected level subCategory")
	}
	if reason := CheckPoint(schema.KindChartStructure, point("World", ""), auto.Rows(rows)); reason != "" {
		t.Errorf("detected level category: %s", reason)
	}
}

func TestSumsLevelAndDefaults(t *testing.T) {
	defaults := map[string]json.RawMessage{"sums": json.RawMessage(`"total"`), "sumTitle": json.RawMessage(`"World"`)}
	// a component without a spec of its own still gets the defaults
	for _, spec := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		if got := SumsOf(spec, defaults); got != (Sums{Total: "World"}) {
			t.Errorf("SumsOf(%q) = %+v", spec, got)
		}
	}
	if got := SumsOf(json.RawMessage(`not json`), defaults); got != (Sums{}) {
		t.Errorf("a broken spec has no sum rows, got %+v", got)
	}

	total := Sums{Total: "World"}
	last := func(rows Rows) map[string]any { out := total.Rows(rows); return out[len(out)-1] }
	// the level is the lowest dimension the rows fill, and category when they fill none
	if row := last(Rows{{"ac1": 1}}); row["category"] != "World" {
		t.Errorf("no dimension: %v", row)
	}
	if row := last(Rows{{"rowGroup": "Europe", "ac1": 1}}); row["rowGroup"] != "World" {
		t.Errorf("only rowGroup: %v", row)
	}
	if row := last(Rows{{"rowGroup": "Europe", "category": "Spain", "ac1": 1}}); row["category"] != "World" {
		t.Errorf("rowGroup and category: %v", row)
	}

	// a panel shows a part of the dataset, but the level comes from all of it
	all := Rows{
		{"region": "North", "category": "Spain", "ac1": 1},
		{"region": "South", "category": "USA", "subCategory": "East", "ac1": 2},
	}
	north := PanelRows(all, "region", "North")
	if row := last(north); row["category"] != "World" {
		t.Errorf("level from the panel rows alone: %v", row)
	}
	fixed := total.WithLevelOf(all).Rows(north)
	if row := fixed[len(fixed)-1]; row["subCategory"] != "World" {
		t.Errorf("level from the whole dataset: %v", row)
	}
	// unknown rows leave the level open, a level of the spec stays
	if got := total.WithLevelOf(nil); got.Level != "" {
		t.Errorf("unknown rows: %+v", got)
	}
	if got := (Sums{Total: "World", Level: "rowGroup"}).WithLevelOf(all); got.Level != "rowGroup" {
		t.Errorf("the level of the spec stays: %+v", got)
	}
}
