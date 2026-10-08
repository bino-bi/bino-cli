package lint

import "testing"

func TestStyleFillpatternDeprecated(t *testing.T) {
	bars := func(pattern string) map[string]any {
		return map[string]any{
			"bn-chart-time": map[string]any{
				"barStyles": map[string]any{
					"ac": map[string]any{"fill": "#333333"},
					"fc": map[string]any{"fill": "#ffffff", "fillpattern": pattern},
				},
			},
		}
	}

	tests := []struct {
		name      string
		doc       Document
		wantPaths []string
	}{
		{
			name:      "fc in object content",
			doc:       componentDoc("ComponentStyle", "theme", map[string]any{"content": bars("fc")}),
			wantPaths: []string{"spec.content.bn-chart-time.barStyles.fc.fillpattern"},
		},
		{
			name: "fc twice in object content",
			doc: componentDoc("ComponentStyle", "theme", map[string]any{"content": map[string]any{
				"bn-chart-structure": bars("fc")["bn-chart-time"],
				"bn-chart-time":      bars("fc")["bn-chart-time"],
			}}),
			wantPaths: []string{
				"spec.content.bn-chart-structure.barStyles.fc.fillpattern",
				"spec.content.bn-chart-time.barStyles.fc.fillpattern",
			},
		},
		{
			// A string has one position, so two hits give one finding.
			name: "fc twice in JSON string content",
			doc: componentDoc("ComponentStyle", "theme", map[string]any{
				"content": `{"bn-chart-time":{"barStyles":{"fc":{"fillpattern":"fc"}}},` +
					`"bn-chart-structure":{"barStyles":{"fc":{"fillpattern":"fc"}}}}`,
			}),
			wantPaths: []string{"spec.content"},
		},
		{
			name: "hatched",
			doc:  componentDoc("ComponentStyle", "theme", map[string]any{"content": bars("hatched")}),
		},
		{
			name: "string content that is not JSON",
			doc:  componentDoc("ComponentStyle", "theme", map[string]any{"content": "color: red;"}),
		},
		{
			// Only a ComponentStyle carries a theme.
			name: "other kind",
			doc:  componentDoc("RuleSet", "rules", map[string]any{"content": bars("fc")}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runRule(t, styleFillpatternDeprecated, []Document{tt.doc})
			got := make([]string, 0, len(findings))
			for _, f := range findings {
				if f.Severity != "info" {
					t.Errorf("Severity = %q, want info", f.Severity)
				}
				got = append(got, f.Path)
			}
			assertPaths(t, got, tt.wantPaths)
		})
	}
}
