package dataset

import (
	"reflect"
	"strings"
	"testing"
)

func TestFlattenConstants(t *testing.T) {
	tests := []struct {
		name      string
		constants map[string]any
		want      []Constant
		wantErr   string
	}{
		{
			name:      "scalar keeps its type",
			constants: map[string]any{"unit": "kEUR", "factor": 1000.0, "draft": true, "note": nil},
			want: []Constant{
				{Path: "draft", Column: "_draft", Value: true},
				{Path: "factor", Column: "_factor", Value: 1000.0},
				{Path: "note", Column: "_note", Value: nil},
				{Path: "unit", Column: "_unit", Value: "kEUR"},
			},
		},
		{
			name:      "nested object joins keys with underscore",
			constants: map[string]any{"spec": map[string]any{"table": map[string]any{"barColumns": "ac1,pl1"}}},
			want:      []Constant{{Path: "spec.table.barColumns", Column: "_spec_table_barColumns", Value: "ac1,pl1"}},
		},
		{
			name:      "list of scalars becomes a comma string",
			constants: map[string]any{"spec": map[string]any{"any": map[string]any{"scenarios": []any{"ac1", "pl1"}}}},
			want:      []Constant{{Path: "spec.any.scenarios", Column: "_spec_any_scenarios", Value: "ac1,pl1"}},
		},
		{
			name: "list of objects flattens by index",
			constants: map[string]any{"spec": map[string]any{"table": map[string]any{"thereof": []any{
				map[string]any{"rowGroup": "Revenue", "category": "Applications"},
				map[string]any{"rowGroup": "Revenue", "category": "Services", "subCategory": "Consulting"},
			}}}},
			want: []Constant{
				{Path: "spec.table.thereof[0].category", Column: "_spec_table_thereof_0_category", Value: "Applications"},
				{Path: "spec.table.thereof[0].rowGroup", Column: "_spec_table_thereof_0_rowGroup", Value: "Revenue"},
				{Path: "spec.table.thereof[1].category", Column: "_spec_table_thereof_1_category", Value: "Services"},
				{Path: "spec.table.thereof[1].rowGroup", Column: "_spec_table_thereof_1_rowGroup", Value: "Revenue"},
				{Path: "spec.table.thereof[1].subCategory", Column: "_spec_table_thereof_1_subCategory", Value: "Consulting"},
			},
		},
		{
			name:      "mixed list flattens every item by index",
			constants: map[string]any{"k": []any{1.0, map[string]any{"a": 2.0}}},
			want: []Constant{
				{Path: "k[0]", Column: "_k_0", Value: 1.0},
				{Path: "k[1].a", Column: "_k_1_a", Value: 2.0},
			},
		},
		{
			name:      "key with underscore rejected",
			constants: map[string]any{"spec": map[string]any{"table": map[string]any{"bar_columns": "ac1"}}},
			wantErr:   `constants.spec.table.bar_columns: key "bar_columns" must be camelCase`,
		},
		{
			name:      "non-camelCase key rejected",
			constants: map[string]any{"Unit": "kEUR"},
			wantErr:   `constants.Unit: key "Unit" must be camelCase`,
		},
		{
			name:      "empty object yields no columns",
			constants: map[string]any{},
			want:      nil,
		},
		{
			name:      "nil yields no columns",
			constants: nil,
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FlattenConstants(tt.constants)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestStampConstants(t *testing.T) {
	constants := []Constant{
		{Path: "unit", Column: "_unit", Value: "kEUR"},
		{Path: "spec.table.barColumns", Column: "_spec_table_barColumns", Value: "ac1,pl1"},
	}

	t.Run("stamps rows and appends columns", func(t *testing.T) {
		rows := []map[string]any{{"ac1": 1.0}, {"ac1": 2.0}}
		cols, shadowed := StampConstants(rows, []string{"ac1"}, constants)
		if !reflect.DeepEqual(cols, []string{"ac1", "_unit", "_spec_table_barColumns"}) {
			t.Errorf("cols = %v", cols)
		}
		if len(shadowed) != 0 {
			t.Errorf("shadowed = %+v", shadowed)
		}
		for _, row := range rows {
			if row["_unit"] != "kEUR" || row["_spec_table_barColumns"] != "ac1,pl1" {
				t.Errorf("row not stamped: %v", row)
			}
		}
	})

	t.Run("constant wins over a query column and is reported", func(t *testing.T) {
		rows := []map[string]any{{"ac1": 1.0, "_unit": "EUR"}}
		cols, shadowed := StampConstants(rows, []string{"ac1", "_unit"}, constants)
		if !reflect.DeepEqual(cols, []string{"ac1", "_unit", "_spec_table_barColumns"}) {
			t.Errorf("cols = %v", cols)
		}
		if len(shadowed) != 1 || shadowed[0].Column != "_unit" {
			t.Errorf("shadowed = %+v", shadowed)
		}
		if rows[0]["_unit"] != "kEUR" {
			t.Errorf("_unit = %v, want the constant", rows[0]["_unit"])
		}
	})

	t.Run("nil rows lists columns only", func(t *testing.T) {
		cols, _ := StampConstants(nil, []string{"ac1"}, constants)
		if len(cols) != 3 {
			t.Errorf("cols = %v", cols)
		}
	})

	t.Run("no constants is a no-op", func(t *testing.T) {
		cols, shadowed := StampConstants(nil, []string{"ac1"}, nil)
		if !reflect.DeepEqual(cols, []string{"ac1"}) || shadowed != nil {
			t.Errorf("cols = %v, shadowed = %v", cols, shadowed)
		}
	})
}
