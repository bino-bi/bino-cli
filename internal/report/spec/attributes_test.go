package spec

import (
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/schema"
)

func TestAttributesList_UnmarshalJSON_Array_PreservesOrder(t *testing.T) {
	// Labels chosen so alphabetical order differs from insertion order.
	input := `[{"label": "Zebra", "expression": "sum(ac1)"}, {"label": "Alpha", "expression": "set(_leiter)"}]`

	var a AttributesList
	if err := json.Unmarshal([]byte(input), &a); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(a.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(a.Items))
	}
	if a.Items[0].Label != "Zebra" || a.Items[0].Expression != "sum(ac1)" {
		t.Errorf("expected first item Zebra/sum(ac1), got %q/%q", a.Items[0].Label, a.Items[0].Expression)
	}
	if a.Items[1].Label != "Alpha" || a.Items[1].Expression != "set(_leiter)" {
		t.Errorf("expected second item Alpha/set(_leiter), got %q/%q", a.Items[1].Label, a.Items[1].Expression)
	}
	if a.Raw != "" {
		t.Errorf("expected empty Raw, got %q", a.Raw)
	}
}

func TestAttributesList_UnmarshalJSON_String_KeptVerbatim(t *testing.T) {
	inner := `{"Zebra": "sum(ac1)", "Alpha": "set(_x)"}`
	input, err := json.Marshal(inner)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	var a AttributesList
	if err := json.Unmarshal(input, &a); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a.Raw != inner {
		t.Errorf("expected Raw kept verbatim %q, got %q", inner, a.Raw)
	}
	if a.Items != nil {
		t.Errorf("expected nil Items, got %v", a.Items)
	}
}

func TestAttributesList_UnmarshalJSON_Tolerant(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"null", `null`},
		{"empty string", `""`},
		{"string not JSON", `"not json"`},
		{"string JSON array", `"[1,2]"`},
		{"string JSON scalar", `"42"`},
		// A YAML map arrives here as a JSON object after the loader's
		// map[string]any round-trip; it must be dropped (order is lost).
		{"direct object", `{"A": "sum(ac1)"}`},
		{"number", `42`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a AttributesList
			if err := json.Unmarshal([]byte(tt.input), &a); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Items != nil || a.Raw != "" {
				t.Errorf("expected zero value, got Items=%v Raw=%q", a.Items, a.Raw)
			}
		})
	}
}

func TestAttributesList_String_OrderAndEscaping(t *testing.T) {
	a := AttributesList{Items: []schema.AttributeItem{
		{Label: `Umsatz "gesamt"`, Expression: "sum(ac1)"},
		{Label: "Verkaufsleiter", Expression: "set(_leiter)"},
	}}

	want := `{"Umsatz \"gesamt\"":"sum(ac1)","Verkaufsleiter":"set(_leiter)"}`
	if got := a.String(); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestAttributesList_String_RawPassthrough(t *testing.T) {
	raw := `{"Zebra": "sum(ac1)", "Alpha": "set(_x)"}`
	a := AttributesList{Raw: raw}

	if got := a.String(); got != raw {
		t.Errorf("expected raw passthrough %q, got %q", raw, got)
	}
}

func TestAttributesList_String_Empty(t *testing.T) {
	var a AttributesList
	if got := a.String(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestAttributesList_MarshalJSON_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"array form", `[{"label":"Zebra","expression":"sum(ac1)"},{"label":"Alpha","expression":"set(_leiter)"}]`},
		{"string form", `"{\"Zebra\": \"sum(ac1)\"}"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a AttributesList
			if err := json.Unmarshal([]byte(tt.input), &a); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			data, err := json.Marshal(a)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var b AttributesList
			if err := json.Unmarshal(data, &b); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}

			if a.String() != b.String() {
				t.Errorf("round trip changed value: %q != %q", a.String(), b.String())
			}
		})
	}
}
