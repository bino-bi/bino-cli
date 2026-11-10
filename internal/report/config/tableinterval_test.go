package config

import (
	"encoding/json"
	"testing"
)

func TestUsesCategoryIntervalPosition(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"table with position", `{"kind":"Table","spec":{"interval":"setname","categoryIntervalPosition":2}}`, true},
		{"nested table in a card", `{"kind":"LayoutPage","spec":{"children":[{"kind":"LayoutCard","spec":{"children":[{"kind":"Table","spec":{"categoryIntervalPosition":1}}]}}]}}`, true},
		{"tree node", `{"kind":"Tree","spec":{"nodes":[{"id":"a","kind":"Table","spec":{"categoryIntervalPosition":3}}]}}`, true},
		{"zero is the default layout", `{"kind":"Table","spec":{"interval":"setname","categoryIntervalPosition":0}}`, false},
		{"not set", `{"kind":"Table","spec":{"interval":"setname"}}`, false},
		{"other kind", `{"kind":"ChartStructure","spec":{"categoryIntervalPosition":2}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{Kind: "x", Raw: json.RawMessage(tt.raw)}}
			if got := UsesCategoryIntervalPosition(docs); got != tt.want {
				t.Fatalf("UsesCategoryIntervalPosition = %v, want %v", got, tt.want)
			}
		})
	}
}
