package lint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// refParamsPage builds a LayoutPage document with a single ref child.
func refParamsPage(t *testing.T, child map[string]any) Document {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"apiVersion": "bino.bi/v1",
		"kind":       "LayoutPage",
		"metadata":   map[string]any{"name": "mainPage"},
		"spec":       map[string]any{"children": []any{child}},
	})
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	return Document{File: "/test/page.yaml", Position: 1, Kind: "LayoutPage", Name: "mainPage", Raw: raw}
}

func TestRefParams(t *testing.T) {
	predef := Document{
		File:     "/test/predef.yaml",
		Position: 1,
		Kind:     "Text",
		Name:     "@acme/commentary",
		Params: []config.LayoutPageParamSpec{
			{Name: "REGION", Type: "select", Options: &config.LayoutPageParamOptions{
				Items: []config.LayoutPageParamOptionItem{{Value: "EU"}, {Value: "US"}},
			}},
			{Name: "YEAR", Required: true},
		},
		Raw: rawDoc("Text", "@acme/commentary", map[string]any{"value": "${REGION} ${YEAR}"}),
	}

	tests := []struct {
		name        string
		child       map[string]any
		wantMessage string // substring expected in a finding; empty means no findings
	}{
		{
			name: "valid params",
			child: map[string]any{
				"kind": "Text", "ref": "@acme/commentary",
				"params": map[string]any{"REGION": "EU", "YEAR": "2024"},
			},
		},
		{
			name: "unknown param warned",
			child: map[string]any{
				"kind": "Text", "ref": "@acme/commentary",
				"params": map[string]any{"REGION": "EU", "YEAR": "2024", "BOGUS": "x"},
			},
			wantMessage: `unknown param "BOGUS"`,
		},
		{
			name: "invalid select value",
			child: map[string]any{
				"kind": "Text", "ref": "@acme/commentary",
				"params": map[string]any{"REGION": "APAC", "YEAR": "2024"},
			},
			wantMessage: `value "APAC" is not a valid option`,
		},
		{
			name: "missing required param",
			child: map[string]any{
				"kind": "Text", "ref": "@acme/commentary",
				"params": map[string]any{"REGION": "EU"},
			},
			wantMessage: `missing required param "YEAR"`,
		},
		{
			name: "missing target skipped silently",
			child: map[string]any{
				"kind": "Text", "ref": "@acme/other",
				"params": map[string]any{"REGION": "EU"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{predef, refParamsPage(t, tt.child)}
			findings := refParams.Check(context.Background(), docs)
			if tt.wantMessage == "" {
				if len(findings) != 0 {
					t.Fatalf("expected no findings, got: %+v", findings)
				}
				return
			}
			for _, f := range findings {
				if strings.Contains(f.Message, tt.wantMessage) {
					return
				}
			}
			t.Fatalf("expected finding containing %q, got: %+v", tt.wantMessage, findings)
		})
	}
}

func TestRefParams_DefinitionSide(t *testing.T) {
	predef := Document{
		File:     "/test/predef.yaml",
		Position: 1,
		Kind:     "Table",
		Name:     "@acme/revenue",
		Params: []config.LayoutPageParamSpec{
			{Name: "REGION"},
			{Name: "REGION"}, // duplicate
		},
		Raw: rawDoc("Table", "@acme/revenue", map[string]any{"dataset": "revenue"}),
	}

	findings := refParams.Check(context.Background(), []Document{predef})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, `duplicate param name "REGION"`) {
		t.Fatalf("expected duplicate param finding, got: %+v", findings)
	}
}
