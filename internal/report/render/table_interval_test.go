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

func TestTableInterval_RenderedAttribute(t *testing.T) {
	html := renderTablePage(t, `{
		"dataset": "test",
		"interval": "year"
	}`)

	want := `interval='year'`
	if !strings.Contains(html, want) {
		t.Fatalf("expected %s in HTML, got:\n%s", want, html)
	}
}

func TestTableInterval_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test"}`)

	if strings.Contains(html, "interval=") {
		t.Fatalf("expected no interval attribute in HTML, got:\n%s", html)
	}
}

func TestTableCategoryIntervalPosition_RenderedAttribute(t *testing.T) {
	html := renderTablePage(t, `{
		"dataset": "test",
		"interval": "setname",
		"categoryIntervalPosition": 2
	}`)

	want := `category-interval-position='2'`
	if !strings.Contains(html, want) {
		t.Fatalf("expected %s in HTML, got:\n%s", want, html)
	}
}

func TestTableCategoryIntervalPosition_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test", "interval": "setname"}`)

	if strings.Contains(html, "category-interval-position=") {
		t.Fatalf("expected no category-interval-position attribute in HTML, got:\n%s", html)
	}
}

func TestTableCategoryIntervalPosition_EngineGate(t *testing.T) {
	table := `{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": {"dataset": "test", "interval": "setname", "categoryIntervalPosition": %s}}]}`
	docs := []config.Document{pageDoc(fmt.Sprintf(table, "1"))}
	_, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.38")
	if err == nil || !strings.Contains(err.Error(), "v1.0.0-next.39") || !strings.Contains(err.Error(), "table-category-interval-position-engine-version") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.39"); err != nil {
		t.Fatalf("new engine: %v", err)
	}
	plain := []config.Document{pageDoc(fmt.Sprintf(table, "0"))}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), plain, "en", "", "", ModeBuild, "v1.0.0-next.38"); err != nil {
		t.Fatalf("position 0: %v", err)
	}
}

func TestTableCategoryIntervalPosition_TooLargeWarning(t *testing.T) {
	rows := json.RawMessage(`[{"setname":"Jan","ac1":1},{"setname":"Feb","ac1":2},{"setname":"YTD","ac1":3}]`)
	render := func(spec string) []string {
		t.Helper()
		docs := []config.Document{pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "t"}, "spec": ` + spec + `}]}`)}
		results := []dataset.Result{{Name: "sales", Data: rows}}
		result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, results, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.39", nil, nil, "", "", "")
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		return result.Warnings
	}

	warnings := render(`{"dataset": "sales", "interval": "setname", "categoryIntervalPosition": 8}`)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "table-category-interval-position-too-large") ||
		!strings.Contains(warnings[0], `Table "t"`) || !strings.Contains(warnings[0], "only 3 interval group(s)") {
		t.Fatalf("warnings = %v", warnings)
	}
	for name, spec := range map[string]string{
		"equal to the group count": `{"dataset": "sales", "interval": "setname", "categoryIntervalPosition": 3}`,
		"date interval":            `{"dataset": "sales", "interval": "month", "categoryIntervalPosition": 8}`,
		"columnthereof wins":       `{"dataset": "sales", "interval": "setname", "categoryIntervalPosition": 8, "columnthereof": [{"scenario": "ac1", "name": "DE"}]}`,
		"rows not known":           `{"dataset": "$source", "interval": "setname", "categoryIntervalPosition": 8}`,
	} {
		if warnings := render(spec); len(warnings) != 0 {
			t.Errorf("%s: warnings = %v, want none", name, warnings)
		}
	}
}
