package annotation

import (
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
		{"a time chart has no category", schema.KindChartTime, &schema.AnnotationPoint{Category: "x"}, nil, "has no category"},
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

func TestFirstDataset(t *testing.T) {
	tests := map[string]string{
		`{"dataset": "a"}`:         "a",
		`{"dataset": ["$a", "b"]}`: "$a",
		`{}`:                       "",
		`{"dataset": {"bad": 1}}`:  "",
		`not json`:                 "",
	}
	for in, want := range tests {
		if got := FirstDataset([]byte(in)); got != want {
			t.Errorf("FirstDataset(%s) = %q, want %q", in, got, want)
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
