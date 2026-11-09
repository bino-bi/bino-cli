package config

import (
	"encoding/json"
	"testing"
)

func TestUsesTableOrderMeasure(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"table variance", `{"kind":"Table","spec":{"order":"dac1_pp1"}}`, true},
		{"table variance with sentiment in a list", `{"kind":"Table","spec":{"order":"rowgroupindex, drac1_pl1_pos"}}`, true},
		{"table scenario bucket", `{"kind":"Table","spec":{"order":"ac1@_November"}}`, true},
		{"page titleOrder", `{"kind":"LayoutPage","spec":{"titleOrder":"dac1_pp1_neg"}}`, true},
		{"nested table in a card", `{"kind":"LayoutPage","spec":{"children":[{"kind":"LayoutCard","spec":{"children":[{"kind":"Table","spec":{"order":"dac1_pp1@Q1"}}]}}]}}`, true},
		{"dimensions and scenarios", `{"kind":"Table","spec":{"order":"rowgroupindex,category,ac1"}}`, false},
		{"keywords", `{"kind":"Table","spec":{"order":"inherited-closest"}}`, false},
		{"chart order is no table order", `{"kind":"ChartStructure","spec":{"order":"dac1_pp1"}}`, false},
		{"variance elsewhere", `{"kind":"Table","spec":{"order":"ac1","variances":["dac1_pp1_pos"]}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{Kind: "x", Raw: json.RawMessage(tt.raw)}}
			if got := UsesTableOrderMeasure(docs); got != tt.want {
				t.Fatalf("UsesTableOrderMeasure = %v, want %v", got, tt.want)
			}
		})
	}
}
