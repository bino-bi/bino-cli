package pipeline

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// TestRenderArtefactHTML_ConstantColumnsReachTableAttributes renders a Table
// whose attributes read a constant column: the column is on every row of the
// embedded dataset and the attributes expression reaches the engine unchanged.
func TestRenderArtefactHTML_ConstantColumnsReachTableAttributes(t *testing.T) {
	docs := []config.Document{
		{
			Kind: "DataSet", Name: "sales", File: "sales.yaml",
			Raw: json.RawMessage(`{
				"apiVersion": "bino.bi/v1alpha1",
				"kind": "DataSet",
				"metadata": {"name": "sales"},
				"spec": {
					"query": "SELECT * FROM (VALUES ('Coffee', 1, 10.0), ('Tea', 2, 20.0)) AS t(category, categoryIndex, ac1)",
					"constants": {"unit": "kEUR"}
				}
			}`),
		},
		{
			Kind: "LayoutPage", Name: "page", File: "page.yaml",
			Raw: json.RawMessage(`{
				"apiVersion": "bino.bi/v1alpha1",
				"kind": "LayoutPage",
				"metadata": {"name": "page"},
				"spec": {
					"children": [
						{"kind": "Table", "metadata": {"name": "sales_table"}, "spec": {
							"dataset": "sales",
							"scenarios": ["ac1"],
							"attributes": [{"label": "Unit", "expression": "set(_unit)"}]
						}}
					]
				}
			}`),
		},
	}
	artifact := config.Artifact{
		Document: config.Document{Kind: "ReportArtefact", Name: "report", File: "report.yaml", Raw: json.RawMessage(`{
			"apiVersion": "bino.bi/v1alpha1",
			"kind": "ReportArtefact",
			"metadata": {"name": "report"},
			"spec": {"filename": "report.pdf", "title": "Report", "language": "en"}
		}`)},
		Spec: config.ReportArtefactSpec{
			Format:      config.DefaultArtefactFormat,
			Orientation: config.DefaultArtefactOrientation,
			Language:    "en",
			Filename:    "report.pdf",
			Title:       "Report",
			LayoutPages: config.LayoutPagesOrRefs{{Page: "*"}},
		},
	}
	result, err := RenderArtefactHTML(context.Background(), t.TempDir(), docs, artifact, RenderArtefactOptions{
		EngineVersion: "v1.0.0",
	})
	if err != nil {
		t.Fatalf("render artefact html: %v", err)
	}
	html := string(result.HTML)

	if !strings.Contains(html, `attributes='{&#34;Unit&#34;:&#34;set(_unit)&#34;}'`) {
		t.Fatalf("table attributes not rendered:\n%s", html)
	}

	m := regexp.MustCompile(`<bn-dataset[^>]*name='sales'[^>]*>([^<]*)</bn-dataset>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("dataset element missing:\n%s", html)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(decodeInlinePayload(t, m[1])), &rows); err != nil {
		t.Fatalf("decode dataset payload: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row["_unit"] != "kEUR" {
			t.Errorf("row lacks the constant column: %v", row)
		}
	}
}
