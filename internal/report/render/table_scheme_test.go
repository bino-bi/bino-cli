package render

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

func TestTableCalculationScheme_RenderedAttributes(t *testing.T) {
	html := renderTablePage(t, `{
		"dataset": "test",
		"type": "optnototal",
		"runningSum": true,
		"thereofPosition": "above",
		"varianceWaterfall": true
	}`)

	for _, want := range []string{`running-sum='true'`, `thereof-position='above'`, `variance-waterfall='true'`} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %s in HTML, got:\n%s", want, html)
		}
	}
}

func TestTableCalculationScheme_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test", "type": "optnototal"}`)

	for _, attr := range []string{"running-sum=", "thereof-position=", "variance-waterfall="} {
		if strings.Contains(html, attr) {
			t.Fatalf("expected no %s attribute in HTML, got:\n%s", attr, html)
		}
	}
}

func TestTableCalculationScheme_EngineGate(t *testing.T) {
	table := `{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test", %s}}]}`
	for field, prop := range map[string]string{
		`"runningSum": true`:         "runningSum",
		`"thereofPosition": "above"`: "thereofPosition",
		`"varianceWaterfall": true`:  "varianceWaterfall",
	} {
		docs := []config.Document{pageDoc(fmt.Sprintf(table, field))}
		_, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.39")
		if err == nil || !strings.Contains(err.Error(), "v1.0.0-next.40") || !strings.Contains(err.Error(), prop) ||
			!strings.Contains(err.Error(), "table-calculation-scheme-engine-version") {
			t.Fatalf("%s: err = %v", prop, err)
		}
		if _, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.40"); err != nil {
			t.Fatalf("%s on the new engine: %v", prop, err)
		}
	}
	plain := []config.Document{pageDoc(fmt.Sprintf(table, `"runningSum": false, "thereofPosition": "below", "varianceWaterfall": false`))}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), plain, "en", "", "", ModeBuild, "v1.0.0-next.39"); err != nil {
		t.Fatalf("defaults: %v", err)
	}
}

// A field that only a dataset default turns on reaches the gate too.
func TestTableCalculationScheme_EngineGateDatasetDefault(t *testing.T) {
	docs := []config.Document{pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test"}}]}`)}
	results := []dataset.Result{{Name: "test", Data: json.RawMessage(`[]`), Defaults: dataset.Defaults{"table": {"runningSum": json.RawMessage(`true`)}}}}
	err := checkTableCalculationSchemeEngine(docs, results, "v1.0.0-next.39")
	if err == nil || !strings.Contains(err.Error(), "runningSum") {
		t.Fatalf("err = %v", err)
	}
	if err := checkTableCalculationSchemeEngine(docs, results, "v1.0.0-next.40"); err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := checkTableCalculationSchemeEngine(docs, []dataset.Result{{Name: "test"}}, "v1.0.0-next.39"); err != nil {
		t.Fatalf("no default: %v", err)
	}
}
