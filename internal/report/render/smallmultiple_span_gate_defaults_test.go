package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// The gate also sees a chart field that only a dataset default sets.
func TestSpanEngineGate_DatasetDefault(t *testing.T) {
	chart := pageDoc(`{"children": [{"kind": "ChartTime", "metadata": {"name": "c"}, "spec": {"dataset": "sales"}}]}`)
	table := pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "sales"}}]}`)
	render := func(page config.Document, token, field, value, engineVersion string) error {
		results := []dataset.Result{{Name: "sales", Data: json.RawMessage(`[{"ac1":1}]`),
			Defaults: dataset.Defaults{token: {field: json.RawMessage(value)}}}}
		_, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, results, "en", "", "", ModeBuild, nil, nil, engineVersion, nil, nil, "", "", "")
		return err
	}
	err := render(chart, "charttime", "scenarioSwitch", "true", "v1.0.0-next.45")
	if err == nil || !strings.Contains(err.Error(), "small-multiple-span-engine-version: ChartTime scenarioSwitch") {
		t.Errorf("kind default on the old engine: err = %v", err)
	}
	err = render(chart, dataset.AnyToken, "dataFormatDigitsDecimal", "0", "v1.0.0-next.45")
	if err == nil || !strings.Contains(err.Error(), "ChartTime dataFormatDigitsDecimal") {
		t.Errorf("any default on the old engine: err = %v", err)
	}
	if err := render(chart, "charttime", "scenarioSwitch", "true", spanEngine); err != nil {
		t.Errorf("new engine: %v", err)
	}
	// A Table had the digits before: a project with tables only keeps its engine.
	if err := render(table, dataset.AnyToken, "dataFormatDigitsDecimal", "0", "v1.0.0-next.45"); err != nil {
		t.Errorf("any default with a table only: %v", err)
	}
}
