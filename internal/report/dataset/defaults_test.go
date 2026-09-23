package dataset

import (
	"encoding/json"
	"fmt"
	"testing"
)

func rowsJSON(t *testing.T, rows ...map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func value(t *testing.T, d Defaults, token, field string) string {
	t.Helper()
	v, ok := d[token][field]
	if !ok {
		t.Fatalf("%s.%s missing in %v", token, field, d)
	}
	return string(v)
}

func TestFoldDefaults_ListMergesDistinctInRowOrder(t *testing.T) {
	d, warnings := FoldDefaults("sales", rowsJSON(t,
		map[string]any{"ac1": 1, "_spec_table_barColumns": "pl1"},
		map[string]any{"ac1": 2, "_spec_table_barColumns": nil},
		map[string]any{"ac1": 3, "_spec_table_barColumns": ""},
		map[string]any{"ac1": 4, "_spec_table_barColumns": "ac1, pl1"},
		map[string]any{"ac1": 5, "_spec_table_barColumns": `["fc1"]`},
	))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if got := value(t, d, "table", "barColumns"); got != `["pl1","ac1","fc1"]` {
		t.Errorf("barColumns = %s", got)
	}
}

func TestFoldDefaults_ScalarsAndTypes(t *testing.T) {
	d, warnings := FoldDefaults("sales", rowsJSON(t,
		map[string]any{"_spec_table_measureUnit": "kEUR", "_spec_table_grouped": "1", "_spec_table_dataFormatDigitsDecimal": "1", "_spec_table_unitScaling": "auto", "_spec_table_limit": 2, "_spec_table_thereof": `[{"rowGroup":"Revenue"}]`, "_spec_any_scenarios": "ac1,pl1"},
		map[string]any{"_spec_table_measureUnit": " kEUR ", "_spec_table_grouped": true, "_spec_table_dataFormatDigitsDecimal": 1, "_spec_table_limit": 2.0, "_spec_table_thereof": nil, "_spec_any_scenarios": ""},
	))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	want := map[[2]string]string{
		{"table", "measureUnit"}:             `"kEUR"`,
		{"table", "grouped"}:                 `true`,
		{"table", "dataFormatDigitsDecimal"}: `1`,
		{"table", "unitScaling"}:             `"auto"`,
		{"table", "limit"}:                   `2`,
		{"table", "thereof"}:                 `[{"rowGroup":"Revenue"}]`,
		{"any", "scenarios"}:                 `["ac1","pl1"]`,
	}
	for k, w := range want {
		if got := value(t, d, k[0], k[1]); got != w {
			t.Errorf("%s.%s = %s, want %s", k[0], k[1], got, w)
		}
	}
}

func TestFoldDefaults_Warnings(t *testing.T) {
	tests := []struct {
		name string
		rows []map[string]any
		want string
	}{
		{
			name: "scalar clash",
			rows: []map[string]any{{"_spec_table_measureUnit": "kEUR"}, {"_spec_table_measureUnit": "EUR"}},
			want: "measureUnit: 2 distinct values (kEUR, EUR) in dataset sales",
		},
		{
			name: "unknown token",
			rows: []map[string]any{{"_spec_tabel_barColumns": "ac1"}},
			want: `_spec_tabel_barColumns: unknown kind token "tabel"; expected one of chartbubble, chartbullet, chartscatter, chartstructure, charttime, table, text, any`,
		},
		{
			name: "unknown field",
			rows: []map[string]any{{"_spec_table_barColumnz": "ac1"}},
			want: `_spec_table_barColumnz: Table has no spec field "barColumnz"`,
		},
		{
			name: "any field on no kind",
			rows: []map[string]any{{"_spec_any_nope": "x"}},
			want: `_spec_any_nope: no kind has a spec field "nope"`,
		},
		{
			name: "bad number",
			rows: []map[string]any{{"_spec_table_dataFormatDigitsDecimal": "lots"}},
			want: `_spec_table_dataFormatDigitsDecimal: cannot parse "lots" as number`,
		},
		{
			name: "bad boolean",
			rows: []map[string]any{{"_spec_table_grouped": "maybe"}},
			want: `_spec_table_grouped: cannot parse "maybe" as boolean`,
		},
		{
			name: "marker on a row without dimensions",
			rows: []map[string]any{{"_spec_table_thereof": "category"}},
			want: `_spec_table_thereof: row marked "category" has no rowGroup value`,
		},
		{
			name: "dataset binding",
			rows: []map[string]any{{"_spec_table_dataset": "other"}},
			want: "_spec_table_dataset: the dataset binding cannot be set from data",
		},
		{
			name: "missing field part",
			rows: []map[string]any{{"_spec_table": "x"}},
			want: "_spec_table: expected _spec_<kind>_<field>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, warnings := FoldDefaults("sales", rowsJSON(t, tt.rows...))
			if len(warnings) != 1 || warnings[0].DataSet != "sales" || warnings[0].Message != tt.want {
				t.Fatalf("warnings = %v, want %q", warnings, tt.want)
			}
			if len(d) != 0 {
				t.Errorf("field should be dropped, got %v", d)
			}
		})
	}
}

func TestFoldDefaults_SkipsAndEmpty(t *testing.T) {
	d, warnings := FoldDefaults("sales", rowsJSON(t,
		map[string]any{"_spec_table_thereof_0_rowGroup": "Revenue", "_unit": "kEUR", "_spec_table_measureUnit": nil},
	))
	if len(warnings) != 0 || len(d) != 1 || value(t, d, "table", "thereof") != `[{"rowGroup":"Revenue"}]` {
		t.Fatalf("index form folds, empty cells are silent: %v %v", d, warnings)
	}
	if d, w := FoldDefaults("sales", json.RawMessage(`[]`)); d != nil || w != nil {
		t.Fatalf("zero rows: %v %v", d, w)
	}
	if d, w := FoldDefaults("sales", json.RawMessage(`{"_spec_table_measureUnit":"x"}`)); d != nil || w != nil {
		t.Fatalf("non-array data: %v %v", d, w)
	}
}

// dimRows are the dimension rows of the object-field tests; extra sets the
// `_spec_` cells of each row in order (nil = none).
func dimRows(t *testing.T, col string, cells ...any) json.RawMessage {
	t.Helper()
	base := []map[string]any{
		{"rowGroup": "Revenue", "category": "Applications", "subCategory": "Applications-A", "ac1": 120},
		{"rowGroup": "Revenue", "category": "Applications", "subCategory": "Applications-B", "ac1": 80},
		{"rowGroup": "Revenue", "category": "Services", "subCategory": "Consulting", "ac1": 40},
	}
	for i, c := range cells {
		if c != nil {
			base[i][col] = c
		}
	}
	return rowsJSON(t, base...)
}

func TestFoldDefaults_RowMarker(t *testing.T) {
	t.Run("category level on both Applications rows yields one entry", func(t *testing.T) {
		d, w := FoldDefaults("sales", dimRows(t, "_spec_table_thereof", "category", "category", nil))
		if len(w) != 0 {
			t.Fatalf("warnings: %v", w)
		}
		if got := value(t, d, "table", "thereof"); got != `[{"category":"Applications","rowGroup":"Revenue"}]` {
			t.Errorf("thereof = %s", got)
		}
	})
	t.Run("subCategory level carries all three keys", func(t *testing.T) {
		d, w := FoldDefaults("sales", dimRows(t, "_spec_table_thereof", nil, nil, "subCategory"))
		if len(w) != 0 {
			t.Fatalf("warnings: %v", w)
		}
		if got := value(t, d, "table", "thereof"); got != `[{"category":"Services","rowGroup":"Revenue","subCategory":"Consulting"}]` {
			t.Errorf("thereof = %s", got)
		}
	})
	t.Run("both levels in one column give two entries in row order", func(t *testing.T) {
		d, w := FoldDefaults("sales", dimRows(t, "_spec_table_thereof", "category", nil, "subCategory"))
		if len(w) != 0 {
			t.Fatalf("warnings: %v", w)
		}
		want := `[{"category":"Applications","rowGroup":"Revenue"},{"category":"Services","rowGroup":"Revenue","subCategory":"Consulting"}]`
		if got := value(t, d, "table", "thereof"); got != want {
			t.Errorf("thereof = %s", got)
		}
	})
	t.Run("partof supports the category level only", func(t *testing.T) {
		d, w := FoldDefaults("sales", dimRows(t, "_spec_table_partof", nil, nil, "subCategory"))
		if len(w) != 1 || w[0].Message != `_spec_table_partof: level "subCategory" is not supported (keys: category, rowGroup)` {
			t.Fatalf("warnings = %v", w)
		}
		if len(d) != 0 {
			t.Errorf("dropped field expected, got %v", d)
		}
		d, w = FoldDefaults("sales", dimRows(t, "_spec_table_partof", "category", nil, nil))
		if len(w) != 0 || value(t, d, "table", "partof") != `[{"category":"Applications","rowGroup":"Revenue"}]` {
			t.Errorf("partof = %v %v", d, w)
		}
	})
	t.Run("a cell that is no level", func(t *testing.T) {
		_, w := FoldDefaults("sales", dimRows(t, "_spec_table_thereof", "drill", nil, nil))
		if len(w) != 1 || w[0].Message != `_spec_table_thereof: "drill" is neither a level (rowGroup, category, subCategory) nor JSON` {
			t.Fatalf("warnings = %v", w)
		}
	})
}

func TestFoldDefaults_ColumnMarker(t *testing.T) {
	rows := rowsJSON(t,
		map[string]any{"category": "A", "columnGroup": "DE", "columnSubGroup": "Berlin", "ac1": 1, "_spec_table_columnthereof": "ac1"},
		map[string]any{"category": "A", "columnGroup": "DE", "columnSubGroup": "Hamburg", "ac1": 2, "_spec_table_columnthereof": "ac1"},
		map[string]any{"category": "A", "columnGroup": "FR", "columnSubGroup": "Paris", "ac1": 3},
	)
	d, w := FoldDefaults("sales", rows)
	if len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if got := value(t, d, "table", "columnthereof"); got != `[{"name":"DE","scenario":"ac1","subGroups":["Berlin","Hamburg"]}]` {
		t.Errorf("columnthereof = %s", got)
	}
}

func TestFoldDefaults_IndexForm(t *testing.T) {
	t.Run("two entries, indices sorted numerically", func(t *testing.T) {
		d, w := FoldDefaults("sales", rowsJSON(t, map[string]any{
			"ac1":                                   1,
			"_spec_table_thereof_0_rowGroup":        "Revenue",
			"_spec_table_thereof_0_category":        "Applications",
			"_spec_table_thereof_1_rowGroup":        "Revenue",
			"_spec_table_thereof_1_category":        "Services",
			"_spec_table_thereof_1_subCategory":     "Consulting",
			"_spec_table_columnthereof_0_name":      "DE",
			"_spec_table_columnthereof_0_scenario":  "ac1",
			"_spec_table_columnthereof_0_subGroups": "Berlin,Hamburg",
		}))
		if len(w) != 0 {
			t.Fatalf("warnings: %v", w)
		}
		want := `[{"category":"Applications","rowGroup":"Revenue"},{"category":"Services","rowGroup":"Revenue","subCategory":"Consulting"}]`
		if got := value(t, d, "table", "thereof"); got != want {
			t.Errorf("thereof = %s", got)
		}
		if got := value(t, d, "table", "columnthereof"); got != `[{"name":"DE","scenario":"ac1","subGroups":["Berlin","Hamburg"]}]` {
			t.Errorf("columnthereof = %s", got)
		}
	})
	t.Run("ten entries keep their order", func(t *testing.T) {
		row := map[string]any{}
		for i := 0; i < 11; i++ {
			row[fmt.Sprintf("_spec_table_thereof_%d_rowGroup", i)] = fmt.Sprintf("G%d", i)
		}
		d, w := FoldDefaults("sales", rowsJSON(t, row))
		if len(w) != 0 {
			t.Fatalf("warnings: %v", w)
		}
		var items []map[string]string
		if err := json.Unmarshal(d["table"]["thereof"], &items); err != nil || len(items) != 11 || items[10]["rowGroup"] != "G10" || items[2]["rowGroup"] != "G2" {
			t.Errorf("thereof = %s (%v)", d["table"]["thereof"], err)
		}
	})
	t.Run("gap in the indices", func(t *testing.T) {
		d, w := FoldDefaults("sales", rowsJSON(t, map[string]any{
			"_spec_table_thereof_0_rowGroup": "Revenue",
			"_spec_table_thereof_2_rowGroup": "Costs",
		}))
		if len(w) != 1 || w[0].Message != "thereof: index columns must be contiguous from 0 (missing 1) in dataset sales" {
			t.Fatalf("warnings = %v", w)
		}
		if len(d) != 0 {
			t.Errorf("dropped field expected, got %v", d)
		}
	})
	t.Run("plain object field and required key", func(t *testing.T) {
		d, w := FoldDefaults("sales", rowsJSON(t, map[string]any{"_spec_chartstructure_stack_by": "scenarios", "_spec_chartstructure_stack_mode": "relative"}))
		if len(w) != 0 || value(t, d, "chartstructure", "stack") != `{"by":"scenarios","mode":"relative"}` {
			t.Errorf("stack = %v %v", d, w)
		}
		_, w = FoldDefaults("sales", rowsJSON(t, map[string]any{"_spec_chartstructure_stack_mode": "relative"}))
		if len(w) != 1 || w[0].Message != `stack: missing required key "by" in dataset sales` {
			t.Errorf("warnings = %v", w)
		}
	})
	t.Run("unknown key, index tail, non-object field", func(t *testing.T) {
		cases := map[string]string{
			"_spec_table_thereof_0_categorie": `_spec_table_thereof_0_categorie: thereof item has no key "categorie"`,
			"_spec_table_thereof_0":           "_spec_table_thereof_0: expected a key after index 0",
			"_spec_table_measureUnit_0":       "_spec_table_measureUnit_0: measureUnit is not an object field",
		}
		for col, want := range cases {
			_, w := FoldDefaults("sales", rowsJSON(t, map[string]any{col: "x"}))
			if len(w) != 1 || w[0].Message != want {
				t.Errorf("%s: warnings = %v, want %q", col, w, want)
			}
		}
	})
}

func TestFoldDefaults_JSONForm(t *testing.T) {
	t.Run("JSON string cell and native array cell", func(t *testing.T) {
		d, w := FoldDefaults("sales", rowsJSON(t,
			map[string]any{"_spec_table_thereof": ` [{"rowGroup": "Revenue", "category": "Applications"}] `},
			map[string]any{"_spec_table_thereof": []any{map[string]any{"rowGroup": "Revenue", "category": "Applications"}}},
		))
		if len(w) != 0 || value(t, d, "table", "thereof") != `[{"category":"Applications","rowGroup":"Revenue"}]` {
			t.Errorf("thereof = %v %v", d, w)
		}
	})
	t.Run("invalid JSON", func(t *testing.T) {
		d, w := FoldDefaults("sales", rowsJSON(t, map[string]any{"_spec_table_thereof": `[{"rowGroup": }`}))
		if len(w) != 1 || w[0].Message != "_spec_table_thereof: invalid JSON" || len(d) != 0 {
			t.Errorf("got %v %v", d, w)
		}
	})
	t.Run("object cell with item keys is wrapped, attributes object stays a string", func(t *testing.T) {
		d, w := FoldDefaults("sales", rowsJSON(t, map[string]any{
			"_spec_table_thereof":    `{"rowGroup":"Revenue"}`,
			"_spec_table_attributes": `{"Unit":"set(_unit)"}`,
		}))
		if len(w) != 0 {
			t.Fatalf("warnings: %v", w)
		}
		if got := value(t, d, "table", "thereof"); got != `[{"rowGroup":"Revenue"}]` {
			t.Errorf("thereof = %s", got)
		}
		if got := value(t, d, "table", "attributes"); got != `"{\"Unit\":\"set(_unit)\"}"` {
			t.Errorf("attributes = %s", got)
		}
	})
	t.Run("string-or-object field takes a bare token", func(t *testing.T) {
		d, w := FoldDefaults("sales", rowsJSON(t, map[string]any{"_spec_chartscatter_x": "ac1", "_spec_chartscatter_y": `{"measure":"pl1","label":"Plan"}`}))
		if len(w) != 0 || value(t, d, "chartscatter", "x") != `"ac1"` || value(t, d, "chartscatter", "y") != `{"label":"Plan","measure":"pl1"}` {
			t.Errorf("got %v %v", d, w)
		}
	})
}

func TestFoldDefaults_MixedForms(t *testing.T) {
	t.Run("marker and index columns", func(t *testing.T) {
		rows := []map[string]any{
			{"rowGroup": "Revenue", "category": "Applications", "_spec_table_thereof": "category", "_spec_table_thereof_0_rowGroup": "Revenue"},
		}
		d, w := FoldDefaults("sales", rowsJSON(t, rows...))
		if len(w) != 1 || w[0].Message != "thereof: two forms (marker, index) in dataset sales" || len(d) != 0 {
			t.Errorf("got %v %v", d, w)
		}
	})
	t.Run("JSON and marker cells in one column", func(t *testing.T) {
		_, w := FoldDefaults("sales", dimRows(t, "_spec_table_thereof", "category", `[{"rowGroup":"Revenue"}]`, nil))
		if len(w) != 1 || w[0].Message != "thereof: two forms (marker, json) in dataset sales" {
			t.Errorf("warnings = %v", w)
		}
	})
}

func TestMarkerLevelsAreDimensions(t *testing.T) {
	dims := map[string]bool{}
	for _, c := range StandardColumns() {
		if c.Group == "Dimensions" && c.Kind == ColumnString {
			dims[c.Name] = true
		}
	}
	for _, l := range append(append([]string{}, rowLevels...), columnLevels...) {
		if !dims[l] {
			t.Errorf("%s is not a string dimension column", l)
		}
	}
}
