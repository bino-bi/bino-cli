package spec

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestTableCalculationSchemeProps(t *testing.T) {
	cases := []struct {
		name string
		spec map[string]any
		want []string
	}{
		{"nothing set", map[string]any{"type": "optnototal"}, nil},
		{"defaults", map[string]any{"runningSum": false, "thereofPosition": "below", "varianceWaterfall": false}, nil},
		{"running sum", map[string]any{"runningSum": true}, []string{"runningSum"}},
		{"thereof above", map[string]any{"thereofPosition": " Above "}, []string{"thereofPosition"}},
		{"waterfall", map[string]any{"varianceWaterfall": true}, []string{"varianceWaterfall"}},
		{"all, fixed order", map[string]any{"varianceWaterfall": true, "thereofPosition": "above", "runningSum": true},
			[]string{"runningSum", "thereofPosition", "varianceWaterfall"}},
		{"wrong types", map[string]any{"runningSum": "true", "thereofPosition": true, "varianceWaterfall": 1.0}, nil},
	}
	for _, c := range cases {
		if got := TableCalculationSchemeProps(c.spec); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRawTableCalculationSchemeProps(t *testing.T) {
	fields := map[string]json.RawMessage{
		"runningSum":      json.RawMessage(`true`),
		"thereofPosition": json.RawMessage(`"below"`),
		"type":            json.RawMessage(`"optnototal"`),
	}
	if got := RawTableCalculationSchemeProps(fields); !slices.Equal(got, []string{"runningSum"}) {
		t.Errorf("got %v", got)
	}
	if got := RawTableCalculationSchemeProps(nil); got != nil {
		t.Errorf("nil fields: got %v", got)
	}
}

func TestMergeTableCalculationSchemeProps(t *testing.T) {
	got := MergeTableCalculationSchemeProps([]string{"varianceWaterfall"}, nil, []string{"runningSum", "varianceWaterfall"})
	if !slices.Equal(got, []string{"runningSum", "varianceWaterfall"}) {
		t.Errorf("got %v", got)
	}
	if got := MergeTableCalculationSchemeProps(nil, nil); got != nil {
		t.Errorf("empty: got %v", got)
	}
}

func TestHasAbsoluteVarianceColumn(t *testing.T) {
	cases := []struct {
		cols StringOrSlice
		want bool
	}{
		{nil, false},
		{StringOrSlice{"ac1", "pp1"}, false},
		{StringOrSlice{"drac1_pp1_pos"}, false},
		{StringOrSlice{"ac1", "dac1_pp1_pos"}, true},
		{StringOrSlice{" DAC1_PP1_POS "}, true},
		{StringOrSlice{"drac1_pp1_pos,dac1_pp1_pos"}, true},
		{StringOrSlice{`["drac1_pp1_pos", "dac1_pl1_neg"]`}, true},
		{StringOrSlice{`["drac1_pp1_pos", "ac1"]`}, false},
	}
	for _, c := range cases {
		if got := HasAbsoluteVarianceColumn(c.cols); got != c.want {
			t.Errorf("HasAbsoluteVarianceColumn(%v) = %v, want %v", c.cols, got, c.want)
		}
	}
}
