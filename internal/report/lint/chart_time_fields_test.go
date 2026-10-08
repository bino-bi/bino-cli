package lint

import "testing"

func TestChartTimeFieldNoEffect(t *testing.T) {
	removed := map[string]any{
		"dataset":        "revenue",
		"level":          "category",
		"order":          "ac1",
		"orderDirection": "desc",
		"limit":          0,
	}

	tests := []struct {
		name      string
		doc       Document
		wantPaths []string
	}{
		{
			name:      "standalone ChartTime",
			doc:       componentDoc("ChartTime", "trend", removed),
			wantPaths: []string{"spec.level", "spec.limit", "spec.order", "spec.orderDirection"},
		},
		{
			name: "inline child of a page",
			doc: componentDoc("LayoutPage", "page", map[string]any{
				"children": []any{
					map[string]any{"kind": "ChartTime", "spec": map[string]any{"dataset": "revenue", "order": "inherited-page"}},
				},
			}),
			wantPaths: []string{"spec.children.0.spec.order"},
		},
		{
			name: "ChartTime without the fields",
			doc:  componentDoc("ChartTime", "trend", map[string]any{"dataset": "revenue", "maxBars": 12}),
		},
		{
			// The same fields do something on the other kinds.
			name: "ChartStructure",
			doc:  componentDoc("ChartStructure", "regions", removed),
		},
		{
			name: "Table",
			doc:  componentDoc("Table", "regions", removed),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runRule(t, chartTimeFieldNoEffect, []Document{tt.doc})
			got := make([]string, 0, len(findings))
			for _, f := range findings {
				if f.Severity != "" {
					t.Errorf("Severity = %q, want the default (warning)", f.Severity)
				}
				got = append(got, f.Path)
			}
			assertPaths(t, got, tt.wantPaths)
		})
	}
}
