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
	defaults := func(binding string) map[string]any {
		if binding != "pnl" {
			return nil
		}
		return map[string]any{"runningSum": true, "thereofPosition": "above"}
	}
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
