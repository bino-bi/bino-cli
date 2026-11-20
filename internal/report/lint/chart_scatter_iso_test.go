package lint

import "testing"

func TestChartScatterIsoAxisBelowZero(t *testing.T) {
	iso := map[string]any{"values": []any{1, 2}}
	axis := func(lower any) map[string]any {
		return map[string]any{"measure": "ac1", "min": lower}
	}

	tests := []struct {
		name      string
		doc       Document
		wantPaths []string
	}{
		{
			name:      "both axes below 0",
			doc:       componentDoc("ChartScatter", "s", map[string]any{"dataset": "d", "x": axis(-5), "y": axis(-0.5), "iso": iso}),
			wantPaths: []string{"spec.x.min", "spec.y.min"},
		},
		{
			name: "axes from 0 and above",
			doc:  componentDoc("ChartScatter", "s", map[string]any{"dataset": "d", "x": axis(0), "y": axis(2), "iso": iso}),
		},
		{
			name: "no iso-lines",
			doc:  componentDoc("ChartScatter", "s", map[string]any{"dataset": "d", "x": axis(-5), "y": axis(-5)}),
		},
		{
			name: "bare measure tokens",
			doc:  componentDoc("ChartScatter", "s", map[string]any{"dataset": "d", "x": "ac1", "y": "ac2", "iso": iso}),
		},
		{
			name: "inline child of a page",
			doc: componentDoc("LayoutPage", "page", map[string]any{
				"children": []any{
					map[string]any{"kind": "ChartScatter", "spec": map[string]any{"dataset": "d", "x": axis(-1), "y": "ac2", "iso": iso}},
				},
			}),
			wantPaths: []string{"spec.children.0.spec.x.min"},
		},
		{
			// a bubble chart has no iso-lines
			name: "ChartBubble",
			doc:  componentDoc("ChartBubble", "b", map[string]any{"dataset": "d", "x": axis(-5), "y": axis(-5), "iso": iso}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runRule(t, chartScatterIsoAxisBelowZero, []Document{tt.doc})
			got := make([]string, 0, len(findings))
			for _, f := range findings {
				got = append(got, f.Path)
			}
			assertPaths(t, got, tt.wantPaths)
		})
	}
}
