package spec

import (
	"encoding/json"
	"testing"
)

func TestMeasureList_UnmarshalJSON_String(t *testing.T) {
	input := `"[{\"name\": \"Bruttoumsatz\", \"unit\": \"mEUR\"}]"`

	var m MeasureList
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m) != 1 {
		t.Fatalf("expected 1 measure, got %d", len(m))
	}

	if m[0].Name != "Bruttoumsatz" {
		t.Errorf("expected name 'Bruttoumsatz', got %q", m[0].Name)
	}

	if m[0].Unit != "mEUR" {
		t.Errorf("expected unit 'mEUR', got %q", m[0].Unit)
	}
}

func TestMeasureList_UnmarshalJSON_Array(t *testing.T) {
	input := `[{"name": "Balance Sheet Items", "unit": "mUSD"}, {"name": "Profit and Loss", "unit": "mEUR"}]`

	var m MeasureList
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m) != 2 {
		t.Fatalf("expected 2 measures, got %d", len(m))
	}

	if m[0].Name != "Balance Sheet Items" {
		t.Errorf("expected name 'Balance Sheet Items', got %q", m[0].Name)
	}

	if m[0].Unit != "mUSD" {
		t.Errorf("expected unit 'mUSD', got %q", m[0].Unit)
	}

	if m[1].Name != "Profit and Loss" {
		t.Errorf("expected name 'Profit and Loss', got %q", m[1].Name)
	}

	if m[1].Unit != "mEUR" {
		t.Errorf("expected unit 'mEUR', got %q", m[1].Unit)
	}
}

func TestMeasureList_UnmarshalJSON_Null(t *testing.T) {
	input := `null`

	var m MeasureList
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m != nil {
		t.Errorf("expected nil, got %v", m)
	}
}

func TestMeasureList_UnmarshalJSON_EmptyString(t *testing.T) {
	input := `""`

	var m MeasureList
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m != nil {
		t.Errorf("expected nil, got %v", m)
	}
}

func TestMeasureList_String(t *testing.T) {
	m := MeasureList{
		{Name: "Bruttoumsatz", Unit: "mEUR"},
		{Name: "Nettoumsatz", Unit: "mEUR"},
	}

	got := m.String()
	expected := `[{"name":"Bruttoumsatz","unit":"mEUR"},{"name":"Nettoumsatz","unit":"mEUR"}]`

	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestMeasureList_String_Empty(t *testing.T) {
	var m MeasureList

	got := m.String()
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestMeasureList_Empty(t *testing.T) {
	var m MeasureList
	if !m.Empty() {
		t.Error("expected Empty() to return true for nil list")
	}

	m = MeasureList{}
	if !m.Empty() {
		t.Error("expected Empty() to return true for empty list")
	}

	m = MeasureList{{Name: "test", Unit: "EUR"}}
	if m.Empty() {
		t.Error("expected Empty() to return false for non-empty list")
	}
}
