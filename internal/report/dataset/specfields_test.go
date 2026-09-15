package dataset

import (
	"fmt"
	"testing"
)

func TestSpecField(t *testing.T) {
	tests := []struct {
		kind, field string
		want        FieldType
		ok          bool
	}{
		{"Table", "barColumns", FieldList, true},
		{"Table", "scenarios", FieldList, true},
		{"Table", "thereof", FieldObject, true},
		{"Table", "attributes", FieldObject, true},
		{"Table", "measureUnit", FieldString, true},
		{"Table", "type", FieldString, true},
		{"Table", "grouped", FieldBool, true},
		{"Table", "percentageScaling", FieldNumber, true},
		{"Table", "limit", FieldNumber, true},
		{"Table", "scale", FieldString, true},
		{"ChartStructure", "level", FieldString, true},
		{"ChartStructure", "stack", FieldObject, true},
		{"Table", "barColumnz", "", false},
		{"Tree", "scenarios", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.kind+"."+tt.field, func(t *testing.T) {
			got, ok := SpecField(tt.kind, tt.field)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("SpecField(%s, %s) = %q, %v; want %q, %v", tt.kind, tt.field, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestAnyKindHasField(t *testing.T) {
	if ft, ok := AnyKindHasField("scenarios"); !ok || ft != FieldList {
		t.Errorf("scenarios = %q, %v", ft, ok)
	}
	if ft, ok := AnyKindHasField("barColumns"); !ok || ft != FieldList {
		t.Errorf("barColumns = %q, %v", ft, ok)
	}
	if _, ok := AnyKindHasField("nope"); ok {
		t.Error("nope should be unknown")
	}
}

func TestFieldTypeAt(t *testing.T) {
	tests := []struct {
		kind string
		path []string
		want FieldType
		ok   bool
	}{
		{"Table", []string{"columnthereof", "0", "subGroups"}, FieldList, true},
		{"Table", []string{"thereof", "0", "rowGroup"}, FieldString, true},
		{"ChartStructure", []string{"stack", "by"}, FieldString, true},
		{"Table", []string{"thereof", "0", "categorie"}, "", false},
		{"Table", []string{"thereof", "0"}, "", false},
	}
	for _, tt := range tests {
		got, ok := FieldTypeAt(tt.kind, tt.path)
		if ok != tt.ok || got != tt.want {
			t.Errorf("FieldTypeAt(%s, %v) = %q, %v; want %q, %v", tt.kind, tt.path, got, ok, tt.want, tt.ok)
		}
	}
}

func TestObjectKeys(t *testing.T) {
	keys, required := ObjectKeys("Table", []string{"thereof", "0"})
	if fmt.Sprint(keys) != "[category rowGroup subCategory]" || len(required) != 0 {
		t.Errorf("thereof keys = %v required = %v", keys, required)
	}
	keys, required = ObjectKeys("ChartStructure", []string{"stack"})
	if fmt.Sprint(keys) != "[by mode order]" || fmt.Sprint(required) != "[by]" {
		t.Errorf("stack keys = %v required = %v", keys, required)
	}
	if !IsListOfObjects("Table", "thereof") || IsListOfObjects("ChartStructure", "stack") || IsListOfObjects("Table", "barColumns") {
		t.Error("IsListOfObjects")
	}
	if !ObjectAcceptsString("ChartScatter", "x") || ObjectAcceptsString("Table", "thereof") || ObjectAcceptsString("ChartStructure", "stack") {
		t.Error("ObjectAcceptsString")
	}
	if kind, ok := AnyKindWithField("labels"); !ok || kind != "ChartBubble" {
		t.Errorf("AnyKindWithField(labels) = %s, %v", kind, ok)
	}
}
