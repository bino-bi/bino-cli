package render

import (
	"context"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// An `any` default reaches every kind that has the field. Charts have the
// digits fields now, so they get the default too; a table default stays on
// tables.
func TestDatasetDefaults_AnyDigitsReachCharts(t *testing.T) {
	page := pageDoc(`{"children": [
		{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "sales"}},
		{"kind": "ChartTime", "metadata": {"name": "ct"}, "spec": {"dataset": "sales"}},
		{"kind": "ChartStructure", "metadata": {"name": "cs"}, "spec": {"dataset": "sales"}}]}`)
	render := func(token string) string {
		results := []dataset.Result{{Name: "sales", Data: json.RawMessage(`[{"ac1":1}]`),
			Defaults: dataset.Defaults{token: {"dataFormatDigitsDecimal": json.RawMessage(`0`)}}}}
		res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, results, "en", "", "", ModeBuild, nil, nil, spanEngine, nil, nil, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return string(res.HTML)
	}
	html := render(dataset.AnyToken)
	for _, tag := range []string{"bn-table", "bn-chart-time", "bn-chart-structure"} {
		if attrsOf(openTag(t, html, tag))["data-format-digits-decimal"] != "0" {
			t.Errorf("any default: %s has no digits", tag)
		}
	}
	html = render("table")
	if attrsOf(openTag(t, html, "bn-table"))["data-format-digits-decimal"] != "0" {
		t.Errorf("table default: the table has no digits")
	}
	for _, tag := range []string{"bn-chart-time", "bn-chart-structure"} {
		if _, ok := attrsOf(openTag(t, html, tag))["data-format-digits-decimal"]; ok {
			t.Errorf("table default: %s must not get the digits", tag)
		}
	}
}
