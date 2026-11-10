package config

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestTableCalculationSchemeProps(t *testing.T) {
	tests := []struct {
		name string
		raw  []string
		want []string
	}{
		{"running sum", []string{`{"kind":"Table","spec":{"runningSum":true}}`}, []string{"runningSum"}},
		{"nested table in a card", []string{`{"kind":"LayoutPage","spec":{"children":[{"kind":"LayoutCard","spec":{"children":[{"kind":"Table","spec":{"thereofPosition":"above"}}]}}]}}`}, []string{"thereofPosition"}},
		{"tree node", []string{`{"kind":"Tree","spec":{"nodes":[{"id":"a","kind":"Table","spec":{"varianceWaterfall":true}}]}}`}, []string{"varianceWaterfall"}},
		{"two documents, fixed order", []string{`{"kind":"Table","spec":{"varianceWaterfall":true}}`, `{"kind":"Table","spec":{"runningSum":true}}`}, []string{"runningSum", "varianceWaterfall"}},
		{"defaults", []string{`{"kind":"Table","spec":{"runningSum":false,"thereofPosition":"below","varianceWaterfall":false}}`}, nil},
		{"not set", []string{`{"kind":"Table","spec":{"type":"optnototal"}}`}, nil},
		{"other kind", []string{`{"kind":"ChartStructure","spec":{"runningSum":true}}`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var docs []Document
			for _, raw := range tt.raw {
				docs = append(docs, Document{Kind: "x", Raw: json.RawMessage(raw)})
			}
			if got := TableCalculationSchemeProps(docs, nil); !slices.Equal(got, tt.want) {
				t.Fatalf("TableCalculationSchemeProps = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTableCalculationSchemeProps_Resolution(t *testing.T) {
	defaults := map[string]map[string]any{"pnl": {"runningSum": true, "thereofPosition": "above"}}
	base := Document{Kind: "Table", Name: "base", Raw: json.RawMessage(`{"kind":"Table","metadata":{"name":"base"},"spec":{"dataset":"pnl","runningSum":false,"varianceWaterfall":true}}`)}
	child := func(node string) Document {
		return Document{Kind: "LayoutPage", Raw: json.RawMessage(`{"kind":"LayoutPage","spec":{"children":[` + node + `]}}`)}
	}
	tests := []struct {
		name string
		docs []Document
		want []string
	}{
		{"default of the primary dataset", []Document{child(`{"kind":"Table","spec":{"dataset":["pnl","other"]}}`)}, []string{"runningSum", "thereofPosition"}},
		{"own value wins over the default", []Document{child(`{"kind":"Table","spec":{"dataset":"pnl","runningSum":false,"thereofPosition":"below"}}`)}, nil},
		{"null leaves the default", []Document{child(`{"kind":"Table","spec":{"dataset":"pnl","runningSum":null,"thereofPosition":"below"}}`)}, []string{"runningSum"}},
		{"not the primary dataset", []Document{child(`{"kind":"Table","spec":{"dataset":["other","pnl"]}}`)}, nil},
		{"DataSource binding", []Document{child(`{"kind":"Table","spec":{"dataset":"$pnl"}}`)}, nil},
		{"other kind", []Document{child(`{"kind":"ChartStructure","spec":{"dataset":"pnl"}}`)}, nil},
		{"referenced Table", []Document{base}, []string{"thereofPosition", "varianceWaterfall"}},
		{"child overrides the referenced Table", []Document{child(`{"kind":"Table","ref":"base","spec":{"dataset":"other","varianceWaterfall":false}}`)}, nil},
		{"child inherits from the referenced Table", []Document{child(`{"kind":"Table","ref":"base","spec":{"dataset":"other"}}`), base}, []string{"thereofPosition", "varianceWaterfall"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TableCalculationSchemeProps(tt.docs, defaults); !slices.Equal(got, tt.want) {
				t.Fatalf("TableCalculationSchemeProps = %v, want %v", got, tt.want)
			}
		})
	}
}

// A primary dataset that is only known at render time counts the defaults of
// every dataset. A value in the manifest still wins.
func TestTableCalculationSchemeProps_UnresolvedDataset(t *testing.T) {
	defaults := map[string]map[string]any{
		"pnl":   {"runningSum": true},
		"other": {"varianceWaterfall": true},
	}
	page := func(node string) Document {
		return Document{Kind: "LayoutPage", Raw: json.RawMessage(`{"kind":"LayoutPage","spec":{"children":[` + node + `]}}`)}
	}
	base := func(fields string) Document {
		return Document{Kind: "Table", Name: "base", Raw: json.RawMessage(`{"kind":"Table","metadata":{"name":"base"},"spec":{"dataset":"${DS}"` + fields + `}}`)}
	}
	multiple := func(spec string) Document {
		return page(`{"kind":"SmallMultiple","spec":{` + spec + `}}`)
	}
	tests := []struct {
		name string
		docs []Document
		want []string
	}{
		{"placeholder dataset", []Document{page(`{"kind":"Table","ref":"base","params":{"DS":"pnl"}}`), base("")}, []string{"runningSum", "varianceWaterfall"}},
		{"placeholder dataset, own values win", []Document{page(`{"kind":"Table","ref":"base","params":{"DS":"pnl"}}`), base(`,"runningSum":false,"varianceWaterfall":false`)}, nil},
		{"SmallMultiple dataset", []Document{multiple(`"dataset":"pnl","base":{"kind":"Table","spec":{"varianceWaterfall":false}}`)}, []string{"runningSum"}},
		{"SmallMultiple item dataset", []Document{multiple(`"base":{"kind":"Table","spec":{"dataset":"none"}},"panels":{"items":[{"label":"a","dataset":"other"}]}`)}, []string{"runningSum", "varianceWaterfall"}},
		{"SmallMultiple base ref", []Document{multiple(`"dataset":"pnl","base":{"kind":"Table","ref":"base"}`), base(`,"runningSum":false`)}, []string{"varianceWaterfall"}},
		{"SmallMultiple base turns all off", []Document{multiple(`"dataset":"pnl","base":{"kind":"Table","spec":{"runningSum":false,"varianceWaterfall":false}}`)}, nil},
		{"SmallMultiple item spec", []Document{multiple(`"base":{"kind":"Table","spec":{}},"panels":{"items":[{"label":"a","spec":{"thereofPosition":"above"}}]}`)}, []string{"runningSum", "thereofPosition", "varianceWaterfall"}},
		{"SmallMultiple chart base", []Document{multiple(`"dataset":"pnl","base":{"kind":"ChartTime","spec":{}}`)}, nil},
		{"no data", []Document{multiple(`"dataset":"pnl","base":{"kind":"Table","spec":{}}`)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := defaults
			if tt.name == "no data" {
				d = nil
			}
			if got := TableCalculationSchemeProps(tt.docs, d); !slices.Equal(got, tt.want) {
				t.Fatalf("TableCalculationSchemeProps = %v, want %v", got, tt.want)
			}
		})
	}
}
