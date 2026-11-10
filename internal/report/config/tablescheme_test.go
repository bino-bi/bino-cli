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
			if got := TableCalculationSchemeProps(docs); !slices.Equal(got, tt.want) {
				t.Fatalf("TableCalculationSchemeProps = %v, want %v", got, tt.want)
			}
		})
	}
}
