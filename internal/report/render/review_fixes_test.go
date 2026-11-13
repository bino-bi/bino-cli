package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// A wide child that renders nothing takes no position: the child it would
// cover is shown.
func TestGrid_SkippedSpanCoversNothing(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "Grid", "metadata": {"name": "g"}, "spec": {
		"rowHeaders": ["a"], "columnHeaders": ["x", "y"],
		"children": [
			{"row": 0, "column": 0, "columnSpan": 2, "kind": "Text", "ref": "missing", "optional": true},
			{"row": 0, "column": 1, "kind": "Text", "spec": {"value": "fallback"}}
		]}}]}`)
	res, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{page}, "en", "", "", ModeBuild, spanEngine)
	if err != nil {
		t.Fatal(err)
	}
	html := string(res.HTML)
	if !strings.Contains(html, "<div slot='0-1'><bn-text") || strings.Contains(html, "slot='0-0'") {
		t.Errorf("the fallback child must be the only cell:\n%s", html)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "is not shown") {
			t.Errorf("unexpected warning: %s", w)
		}
	}
}

func TestGridSpanProblems_CoveringChildThatMayBeSkipped(t *testing.T) {
	spec := func(wide string) json.RawMessage {
		return json.RawMessage(`{"rowHeaders": ["a"], "columnHeaders": ["x", "y"], "children": [
			{"row": 0, "column": 0, "columnSpan": 2, "kind": "Text"` + wide + `},
			{"row": 0, "column": 1, "kind": "Text"}]}`)
	}
	if got := GridSpanProblems(spec("")); len(got) != 1 {
		t.Errorf("plain covering child: problems = %+v", got)
	}
	if got := GridSpanProblems(spec(`, "ref": "wide", "optional": true`)); len(got) != 0 {
		t.Errorf("optional covering child: problems = %+v", got)
	}
	if got := GridSpanProblems(spec(`, "metadata": {"constraints": ["mode==build"]}`)); len(got) != 0 {
		t.Errorf("covering child with constraints: problems = %+v", got)
	}
}

func TestSmallMultiple_DuplicateOverrideIsRejected(t *testing.T) {
	html, warnings := renderSpan(t, `{`+spanHead+`, "columns": 3, "panels": {"by": "rowGroup", "overrides": [
		{"value": "Berlin", "columnSpan": 2},
		{"value": "Berlin", "background": "red"}]}}`)
	berlin := spanCells(html)[0].cell
	if berlin["column-span"] != "2" || berlin["cell-background"] != "" {
		t.Errorf("only the first entry counts, got %v", berlin)
	}
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w, `panels.overrides: the value "Berlin" has more than one entry`)
	}
	if !found {
		t.Errorf("warnings = %v", warnings)
	}
}

// An override value is compared with the panel label as written.
func TestSmallMultiple_OverrideValueIsNotTrimmed(t *testing.T) {
	render := func(value string) ([]spanCell, []string) {
		page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"dataset": "odd",
			"unitScaling": 10, "scale": "0.5", ` + inlineBase + `,
			"panels": {"by": "rowGroup", "overrides": [{"value": ` + value + `, "background": "blue"}]}}}]}`)
		results := []dataset.Result{{Name: "odd", Data: json.RawMessage(`[{"rowGroup":" A ","ac1":1},{"rowGroup":"B","ac1":2}]`)}}
		res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, results, "en", "", "", ModeBuild, nil, nil, spanEngine, nil, nil, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return spanCells(string(res.HTML)), res.Warnings
	}
	cells, warnings := render(`" A "`)
	if len(cells) != 2 || cells[0].cell["cell-background"] != "blue" || len(warnings) != 0 {
		t.Errorf("exact value: cells = %+v, warnings = %v", cells, warnings)
	}
	cells, warnings = render(`"A"`)
	if cells[0].cell["cell-background"] != "" || len(warnings) != 1 {
		t.Errorf("trimmed value must not match: cells = %+v, warnings = %v", cells, warnings)
	}
	if got := PanelValueText(json.RawMessage(` 2024 `)); got != "2024" {
		t.Errorf("number value = %q", got)
	}
}

func TestSmallMultiple_AnnotationOnAmbiguousPanel(t *testing.T) {
	html, warnings := renderSpan(t, `{`+spanHead+`, "panels": {"by": "rowGroup", "items": [{"label": "Berlin", "filter": "1=1"}]}}`,
		panelAnnotation("berlin", "Berlin", ""))
	if strings.Count(html, "data-bino-annotation-target=") != 2 {
		t.Errorf("both panels with the label are targets:\n%s", html)
	}
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w, `annotation-panel-ambiguous: SmallMultiple "sm" has 2 panels with the label "Berlin"`)
	}
	if !found {
		t.Errorf("warnings = %v", warnings)
	}
}
