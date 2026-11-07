package config

import (
	"encoding/json"
	"testing"
)

func TestUsesGridCellLayout(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"standalone grid with equal rows", `{"kind":"Grid","spec":{"cellHeight":"equal","children":[]}}`, true},
		{"standalone grid with defaults", `{"kind":"Grid","spec":{"cellHeight":"auto","cellWidth":"auto","cellVerticalAlign":"top","cellHorizontalAlign":"left","children":[]}}`, false},
		{"plain grid", `{"kind":"Grid","spec":{"cellGap":"4px","children":[{"row":0,"column":0,"kind":"Text"}]}}`, false},
		{"child caption", `{"kind":"Grid","spec":{"children":[{"row":0,"column":0,"kind":"Text","caption":"North"}]}}`, true},
		{"empty caption", `{"kind":"Grid","spec":{"children":[{"row":0,"column":0,"kind":"Text","caption":""}]}}`, false},
		{"grid inside a card inside a page", `{"kind":"LayoutPage","spec":{"children":[{"kind":"LayoutCard","spec":{"children":[{"kind":"Grid","spec":{"cellVerticalAlign":"baseline"}}]}}]}}`, true},
		{"field name outside a grid", `{"kind":"Table","spec":{"cellHeight":"equal"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{Kind: "x", Raw: json.RawMessage(tt.raw)}}
			if got := UsesGridCellLayout(docs); got != tt.want {
				t.Fatalf("UsesGridCellLayout = %v, want %v", got, tt.want)
			}
		})
	}
}
