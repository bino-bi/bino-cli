package config

import (
	"encoding/json"
	"testing"
)

func TestUsesOperationSentiment(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"table variances list", `{"kind":"Table","spec":{"variances":["dac1_pl1_pos","dac1_pl1_op"]}}`, true},
		{"comma-separated titleVariances", `{"kind":"LayoutPage","spec":{"titleVariances":"dac1_pl1_pos, drac1_pl1_opinv"}}`, true},
		{"JSON list string", `{"kind":"Table","spec":{"variances":"[\"dac1_pl1_op\"]"}}`, true},
		{"barColumns", `{"kind":"Table","spec":{"barColumns":"ac1,dac1_pl1_op"}}`, true},
		{"scatter axis", `{"kind":"ChartScatter","spec":{"x":{"measure":"drac1_pl1_opinv"},"y":"ac1"}}`, true},
		{"nested in a page", `{"kind":"LayoutPage","spec":{"children":[{"kind":"ChartTime","spec":{"variances":["dac1_pp1_op"]}}]}}`, true},
		{"other suffixes", `{"kind":"Table","spec":{"variances":["dac1_pl1_pos","dac1_pl1_neg","dac1_pl1_neu","dac1_pl1"]}}`, false},
		{"unknown suffix", `{"kind":"Table","spec":{"variances":["dac1_pl1_opx"]}}`, false},
		{"token inside text", `{"kind":"Text","spec":{"value":"see dac1_pl1_op here"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{Kind: "x", Raw: json.RawMessage(tt.raw)}}
			if got := UsesOperationSentiment(docs); got != tt.want {
				t.Fatalf("UsesOperationSentiment = %v, want %v", got, tt.want)
			}
		})
	}
}
