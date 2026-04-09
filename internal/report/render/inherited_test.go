package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// TestInheritedKeywordScenarios verifies that scenarios and variances accept
// the inheritance keywords "inherited-page" and "inherited-closest" as scalar
// strings. Before the fix, json.Unmarshal failed because the fields were
// typed as []string which cannot accept a JSON string.
func TestInheritedKeywordScenarios(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		childKind string
		childSpec string
		wantTag   string
	}{
		{
			name:      "ChartStructure inherited-page",
			childKind: "ChartStructure",
			childSpec: `{"dataset": "test", "scenarios": "inherited-page", "variances": "inherited-closest"}`,
			wantTag:   "bn-chart-structure",
		},
		{
			name:      "ChartTime inherited-page",
			childKind: "ChartTime",
			childSpec: `{"dataset": "test", "scenarios": "inherited-page", "variances": "inherited-closest"}`,
			wantTag:   "bn-chart-time",
		},
		{
			name:      "Table inherited-page",
			childKind: "Table",
			childSpec: `{"dataset": "test", "scenarios": "inherited-page", "variances": "inherited-closest"}`,
			wantTag:   "bn-table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layoutPageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
				"apiVersion": "bino.bi/v1",
				"kind": "LayoutPage",
				"metadata": {"name": "page"},
				"spec": {
					"titleScenarios": ["ac1", "pp1"],
					"children": [
						{
							"kind": "`+tt.childKind+`",
							"metadata": {"name": "child"},
							"spec": `+tt.childSpec+`
						}
					]
				}
			}`))

			docs := []config.Document{layoutPageDoc}
			result, _, err := GenerateHTMLFromDocuments(ctx, docs, "en", "", "", ModePreview, "v1.0.0")
			if err != nil {
				t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
			}

			html := string(result.HTML)

			if !strings.Contains(html, "<"+tt.wantTag) {
				t.Fatalf("expected <%s> element in HTML, got:\n%s", tt.wantTag, html)
			}
			if !strings.Contains(html, `scenarios='inherited-page'`) {
				t.Fatalf("expected scenarios='inherited-page' in HTML, got:\n%s", html)
			}
			if !strings.Contains(html, `variances='inherited-closest'`) {
				t.Fatalf("expected variances='inherited-closest' in HTML, got:\n%s", html)
			}
		})
	}
}

// TestScenariosArrayStillWorks verifies that the normal array form of
// scenarios/variances still produces comma-separated HTML attributes
// after the type change from []string to StringOrSlice.
func TestScenariosArrayStillWorks(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		childKind string
		childSpec string
		wantTag   string
	}{
		{
			name:      "ChartStructure array",
			childKind: "ChartStructure",
			childSpec: `{"dataset": "test", "scenarios": ["ac1", "fc1"], "variances": ["dac1_fc1_pos"]}`,
			wantTag:   "bn-chart-structure",
		},
		{
			name:      "ChartTime array",
			childKind: "ChartTime",
			childSpec: `{"dataset": "test", "scenarios": ["ac1", "fc1"], "variances": ["dac1_fc1_pos"]}`,
			wantTag:   "bn-chart-time",
		},
		{
			name:      "Table array",
			childKind: "Table",
			childSpec: `{"dataset": "test", "scenarios": ["ac1", "fc1"], "variances": ["dac1_fc1_pos"]}`,
			wantTag:   "bn-table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layoutPageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
				"apiVersion": "bino.bi/v1",
				"kind": "LayoutPage",
				"metadata": {"name": "page"},
				"spec": {
					"children": [
						{
							"kind": "`+tt.childKind+`",
							"metadata": {"name": "child"},
							"spec": `+tt.childSpec+`
						}
					]
				}
			}`))

			docs := []config.Document{layoutPageDoc}
			result, _, err := GenerateHTMLFromDocuments(ctx, docs, "en", "", "", ModePreview, "v1.0.0")
			if err != nil {
				t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
			}

			html := string(result.HTML)

			if !strings.Contains(html, "<"+tt.wantTag) {
				t.Fatalf("expected <%s> element in HTML, got:\n%s", tt.wantTag, html)
			}
			if !strings.Contains(html, `scenarios='ac1,fc1'`) {
				t.Fatalf("expected scenarios='ac1,fc1' in HTML, got:\n%s", html)
			}
			if !strings.Contains(html, `variances='dac1_fc1_pos'`) {
				t.Fatalf("expected variances='dac1_fc1_pos' in HTML, got:\n%s", html)
			}
		})
	}
}
