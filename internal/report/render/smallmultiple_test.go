package render

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
)

const profitRows = `[
	{"rowGroup":"Cologne","rowGroupIndex":2,"date":"2024-01-01","ac1":5},
	{"rowGroup":"Berlin","rowGroupIndex":1,"date":"2024-01-01","ac1":30},
	{"rowGroup":"O'Hare","rowGroupIndex":4,"date":"2024-01-01","ac1":-10},
	{"rowGroup":"Munich","rowGroupIndex":3,"date":"2024-01-01","ac1":20},
	{"rowGroup":"Berlin","rowGroupIndex":1,"date":"2025-01-01","ac1":1}
]`

type renderedSM struct {
	html     string
	grid     string
	cells    []map[string]string
	warnings []string
}

var (
	smAttrPattern = regexp.MustCompile(`([a-z-]+)='([^']*)'`)
	cellPattern   = regexp.MustCompile(`<div slot='([^']*)'><bn-chart-time([^>]*)>`)
)

func attrsOf(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range smAttrPattern.FindAllStringSubmatch(tag, -1) {
		out[m[1]] = html.UnescapeString(m[2])
	}
	return out
}

func renderSmallMultipleWith(smSpec, engineVersion string, extra ...config.Document) (Result, []datasource.Diagnostic, error) {
	page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": ` + smSpec + `}]}`)
	docs := append([]config.Document{page}, extra...)
	results := []dataset.Result{
		{Name: "profit", Data: json.RawMessage(profitRows)},
		{Name: "profit_avg", Data: json.RawMessage(`[]`)},
	}
	return GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, results, "en", "", "", ModeBuild, nil, nil, engineVersion, nil, nil, "", "", "")
}

func renderSmallMultiple(t *testing.T, smSpec string, extra ...config.Document) renderedSM {
	t.Helper()
	res, _, err := renderSmallMultipleWith(smSpec, "v1.0.0-next.33", extra...)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := renderedSM{html: string(res.HTML), warnings: res.Warnings}
	if strings.Contains(out.html, "<bn-grid") {
		out.grid = openTag(t, out.html, "bn-grid")
	}
	for _, m := range cellPattern.FindAllStringSubmatch(out.html, -1) {
		cell := attrsOf(m[2])
		cell["slot"] = m[1]
		out.cells = append(out.cells, cell)
	}
	return out
}

const inlineBase = `"base": {"kind": "ChartTime", "spec": {"chartTitle": "Profit ${panel.label}"}}`

func TestSmallMultiple_ExpandsToBaselineGrid(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": "profit_scale", "scale": "0.5",
		`+inlineBase+`, "panels": {"by": "rowGroup"}}`)
	grid := attrsOf(out.grid)
	for k, want := range map[string]string{
		"cell-height": "equal", "cell-width": "equal",
		"cell-vertical-align": "baseline", "cell-horizontal-align": "baseline",
		"show-row-headers": "false", "show-column-headers": "false", "show-borders": "false",
		"row-headers": `["r0","r1"]`, "column-headers": `["c0","c1"]`,
	} {
		if grid[k] != want {
			t.Errorf("bn-grid %s = %q, want %q", k, grid[k], want)
		}
	}
	if len(out.cells) != 4 {
		t.Fatalf("cells = %d, want 4:\n%s", len(out.cells), out.html)
	}
	wantOrder := []string{"Berlin", "Cologne", "Munich", "O'Hare"} // by rowGroupIndex
	wantSlots := []string{"0-0", "0-1", "1-0", "1-1"}
	var captions map[string]string
	if err := json.Unmarshal([]byte(grid["cell-captions"]), &captions); err != nil {
		t.Fatalf("cell-captions %q: %v", grid["cell-captions"], err)
	}
	for i, cell := range out.cells {
		label := wantOrder[i]
		if cell["slot"] != wantSlots[i] {
			t.Errorf("cell %d slot = %q, want %q", i, cell["slot"], wantSlots[i])
		}
		wantFilter := "[rowGroup] = '" + strings.ReplaceAll(label, "'", "''") + "'"
		if cell["filter"] != wantFilter {
			t.Errorf("cell %d filter = %q, want %q", i, cell["filter"], wantFilter)
		}
		if cell["datasets"] != "profit" || cell["unit-scaling"] != "profit_scale" || cell["scale"] != "0.5" {
			t.Errorf("cell %d shared fields = %+v", i, cell)
		}
		if cell["chart-title"] != "Profit "+label {
			t.Errorf("cell %d title = %q", i, cell["chart-title"])
		}
		if captions[wantSlots[i]] != label {
			t.Errorf("caption %s = %q, want %q", wantSlots[i], captions[wantSlots[i]], label)
		}
	}
}

func TestSmallMultiple_QuotesLabelsInJSONAndFilter(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "none",
		"base": {"kind": "ChartTime", "spec": {"chartTitle": "${panel.label}"}},
		"panels": {"items": [{"label": "say \"hi\" \\ back", "filter": "rowGroup = 'Berlin'"}]}}`)
	if len(out.cells) != 1 {
		t.Fatalf("cells = %d", len(out.cells))
	}
	if got := out.cells[0]["chart-title"]; got != `say "hi" \ back` {
		t.Errorf("title = %q", got)
	}
	if got := out.cells[0]["filter"]; got != "rowGroup = 'Berlin'" {
		t.Errorf("filter = %q", got)
	}
}

func TestSmallMultiple_FiltersAreANDed(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", "filter": "date >= '2024-01-01'",
		"base": {"kind": "ChartTime", "spec": {"filter": "ac1 > 0"}},
		"panels": {"by": "rowGroup", "limit": 1}}`)
	if got := out.cells[0]["filter"]; got != "(date >= '2024-01-01') AND (ac1 > 0) AND ([rowGroup] = 'Berlin')" {
		t.Errorf("filter = %q", got)
	}
}

func TestSmallMultiple_Order(t *testing.T) {
	tests := []struct {
		order string
		want  []string
	}{
		{"dataset", []string{"Berlin", "Cologne", "Munich", "O'Hare"}},
		{"label", []string{"Berlin", "Cologne", "Munich", "O'Hare"}},
		{"label desc", []string{"O'Hare", "Munich", "Cologne", "Berlin"}},
		{"ac1 desc", []string{"Berlin", "Munich", "Cologne", "O'Hare"}}, // 31, 20, 5, -10
		{"ac1", []string{"O'Hare", "Cologne", "Munich", "Berlin"}},
	}
	for _, tt := range tests {
		t.Run(tt.order, func(t *testing.T) {
			out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
				"panels": {"by": "rowGroup", "order": "`+tt.order+`"}}`)
			var got []string
			for _, cell := range out.cells {
				got = append(got, strings.TrimPrefix(cell["chart-title"], "Profit "))
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSmallMultiple_LimitDropsAndItemsAppend(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"columns": 4,
		"panels": {"by": "rowGroup", "limit": 2, "items": [
			{"label": "Others", "filter": "rowGroup NOT IN ('Berlin', 'Cologne')"},
			{"label": "Average", "dataset": "profit_avg", "spec": {"chartTitle": "Avg"}}
		]}}`)
	if len(out.cells) != 4 {
		t.Fatalf("cells = %d", len(out.cells))
	}
	if got := attrsOf(out.grid)["column-headers"]; got != `["c0","c1","c2","c3"]` {
		t.Errorf("columns = %s", got)
	}
	if out.cells[2]["filter"] != "rowGroup NOT IN ('Berlin', 'Cologne')" || out.cells[2]["chart-title"] != "Profit Others" {
		t.Errorf("items[0] = %+v", out.cells[2])
	}
	if out.cells[3]["datasets"] != "profit_avg" || out.cells[3]["chart-title"] != "Avg" || out.cells[3]["filter"] != "" {
		t.Errorf("items[1] = %+v", out.cells[3])
	}
	joined := strings.Join(out.warnings, "\n")
	if !strings.Contains(joined, "limit 2 dropped 2 panel(s)") || !strings.Contains(joined, `dataset "profit_avg" has no rows`) {
		t.Errorf("warnings = %v", out.warnings)
	}
}

func TestSmallMultiple_RefBaseWithParam(t *testing.T) {
	chart := makeTestDoc("ChartTime", "trend", json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"trend",
		"params":[{"name":"LOCATION","type":"string"}]},
		"spec":{"dataset":"other","chartTitle":"Trend ${LOCATION}","unitScaling":"auto"}}`))
	chart.Params = []config.LayoutPageParamSpec{{Name: "LOCATION", Type: "string"}}
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5",
		"base": {"kind": "ChartTime", "ref": "trend"},
		"panels": {"by": "rowGroup", "param": "LOCATION", "limit": 2}}`, chart)
	if len(out.cells) != 2 {
		t.Fatalf("cells = %d", len(out.cells))
	}
	for i, want := range []string{"Berlin", "Cologne"} {
		cell := out.cells[i]
		if cell["chart-title"] != "Trend "+want || cell["datasets"] != "profit" || cell["unit-scaling"] != "10" {
			t.Errorf("cell %d = %+v", i, cell)
		}
	}
}

func TestSmallMultiple_CaptionOffAndCustom(t *testing.T) {
	off := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"caption": "", "panels": {"by": "rowGroup"}}`)
	if _, ok := attrsOf(off.grid)["cell-captions"]; ok {
		t.Errorf("caption \"\" must write no captions: %s", off.grid)
	}
	custom := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"caption": "${panel.index}. ${panel.label}", "panels": {"by": "rowGroup", "limit": 1}}`)
	if got := attrsOf(custom.grid)["cell-captions"]; got != `{"0-0":"1. Berlin"}` {
		t.Errorf("captions = %s", got)
	}
}

func TestSmallMultiple_MissingColumnWarns(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"panels": {"by": "rowgroup"}}`)
	joined := strings.Join(out.warnings, "\n")
	if !strings.Contains(joined, `column "rowgroup" is not in the rows`) || !strings.Contains(joined, "no panels") {
		t.Errorf("warnings = %v", out.warnings)
	}
}

func TestSmallMultiple_RefusesAutoScale(t *testing.T) {
	for _, smSpec := range []string{
		`{"dataset": "profit", "unitScaling": 10, ` + inlineBase + `, "panels": {"by": "rowGroup"}}`,
		`{"dataset": "profit", "unitScaling": 10, "scale": "auto", ` + inlineBase + `, "panels": {"by": "rowGroup"}}`,
		`{"dataset": "profit", "unitScaling": "auto", "scale": "0.5", ` + inlineBase + `, "panels": {"by": "rowGroup"}}`,
		`{"dataset": "profit", "scale": "0.5", ` + inlineBase + `, "panels": {"by": "rowGroup"}}`,
	} {
		page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": ` + smSpec + `}]}`)
		_, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, nil, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.32", nil, nil, "", "", "")
		if err == nil || !strings.Contains(err.Error(), "all panels must share one scale") {
			t.Errorf("%s: err = %v", smSpec, err)
		}
	}
}

func TestSmallMultiple_DatasourceWithByFails(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"dataset": "$src",
		"unitScaling": 10, "scale": "0.5", ` + inlineBase + `, "panels": {"by": "rowGroup"}}}]}`)
	_, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, nil, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.32", nil, nil, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "panels.by needs a DataSet") {
		t.Fatalf("err = %v", err)
	}
}

func TestSmallMultiple_EngineGate(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"dataset": "profit",
		"unitScaling": 10, "scale": "0.5", ` + inlineBase + `, "panels": {"by": "rowGroup"}}}]}`)
	_, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{page}, "en", "", "", ModeBuild, "v1.0.0-next.30")
	if err == nil || !strings.Contains(err.Error(), "grid-cell-layout-engine-version") {
		t.Fatalf("err = %v", err)
	}
}

func TestSmallMultiple_ParamValueIsEscaped(t *testing.T) {
	t.Setenv("SM_SECRET", "leaked")
	rows := `[{"rowGroup":"Store \"A\" \\ ${SM_SECRET}"}]`
	chart := makeTestDoc("ChartTime", "trend", json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"trend"},
		"spec":{"dataset":"other","chartTitle":"Trend ${LOCATION}"}}`))
	chart.Params = []config.LayoutPageParamSpec{{Name: "LOCATION", Type: "string"}}
	page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"dataset": "odd",
		"unitScaling": 10, "scale": "0.5", "base": {"kind": "ChartTime", "ref": "trend"},
		"panels": {"by": "rowGroup", "param": "LOCATION"}}}]}`)
	res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page, chart},
		[]dataset.Result{{Name: "odd", Data: json.RawMessage(rows)}}, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.32", nil, nil, "", "", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	m := cellPattern.FindStringSubmatch(string(res.HTML))
	if m == nil {
		t.Fatalf("no cell:\n%s", res.HTML)
	}
	if got := attrsOf(m[2])["chart-title"]; got != `Trend Store "A" \ ${SM_SECRET}` {
		t.Fatalf("title = %q", got)
	}
}

func TestSmallMultiple_LabelInBaseFilterIsSQLQuoted(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5",
		"base": {"kind": "ChartTime", "spec": {"filter": "note <> '${panel.label}'"}},
		"panels": {"by": "rowGroup", "order": "label desc", "limit": 1}}`)
	if got := out.cells[0]["filter"]; got != "(note <> 'O''Hare') AND ([rowGroup] = 'O''Hare')" {
		t.Fatalf("filter = %q", got)
	}
}

func TestSmallMultiple_ItemWithoutFilterDropsDatasetDefaultFilter(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"dataset": "profit",
		"unitScaling": 10, "scale": "0.5", ` + inlineBase + `, "panels": {"items": [{"label": "All"}]}}}]}`)
	results := []dataset.Result{{Name: "profit", Data: json.RawMessage(profitRows),
		Defaults: dataset.Defaults{dataset.AnyToken: {"filter": json.RawMessage(`"rowGroup = 'Berlin'"`)}}}}
	res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, results, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.32", nil, nil, "", "", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	m := cellPattern.FindStringSubmatch(string(res.HTML))
	if m == nil || attrsOf(m[2])["filter"] != "" {
		t.Fatalf("the default filter must not narrow one panel: %v", m)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), `dataset "profit" sets a default filter`) {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestSmallMultiple_ParamInRefFilterIsSQLQuoted(t *testing.T) {
	chart := makeTestDoc("ChartTime", "trend", json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"trend"},
		"spec":{"dataset":"other","filter":"note = '${LOCATION}'"}}`))
	chart.Params = []config.LayoutPageParamSpec{{Name: "LOCATION", Type: "string"}}
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5",
		"base": {"kind": "ChartTime", "ref": "trend"},
		"panels": {"by": "rowGroup", "param": "LOCATION", "order": "label desc", "limit": 1}}`, chart)
	if got := out.cells[0]["filter"]; got != "(note = 'O''Hare') AND ([rowGroup] = 'O''Hare')" {
		t.Fatalf("filter = %q", got)
	}
}

func TestSmallMultiple_ItemSpecCannotLeaveTheSharedScale(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"panels": {"items": [{"label": "x", "spec": {"unitScaling": "auto", "scale": "auto", "chartTitle": "own"}}]}}`)
	cell := out.cells[0]
	if cell["unit-scaling"] != "10" || cell["scale"] != "0.5" || cell["chart-title"] != "own" {
		t.Fatalf("cell = %+v", cell)
	}
}

func TestSmallMultiple_NumbersSortAsNumbers(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"dataset": "months",
		"unitScaling": 10, "scale": "0.5", "base": {"kind": "ChartTime", "spec": {"chartTitle": "${panel.label}"}},
		"panels": {"by": "month"}}}]}`)
	rows := `[{"month":10},{"month":2},{"month":1},{"month":11},{"month":3}]`
	res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page},
		[]dataset.Result{{Name: "months", Data: json.RawMessage(rows)}}, "en", "", "", ModeBuild, nil, nil, "v1.0.0-next.32", nil, nil, "", "", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	matches := cellPattern.FindAllStringSubmatch(string(res.HTML), -1)
	got := make([]string, 0, len(matches))
	for _, m := range matches {
		got = append(got, attrsOf(m[2])["chart-title"])
	}
	if strings.Join(got, ",") != "1,2,3,10,11" {
		t.Fatalf("order = %v", got)
	}
}

func TestSmallMultiple_WarnsThatFiltersDoNotChooseThePanels(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", "filter": "ac1 > 0",
		`+inlineBase+`, "panels": {"by": "rowGroup"}}`)
	if !strings.Contains(strings.Join(out.warnings, "\n"), "filters only apply inside each panel") {
		t.Fatalf("warnings = %v", out.warnings)
	}
}

var cardCellPattern = regexp.MustCompile(`(?s)<div slot='([^']*)'><bn-layout-card([^>]*)>.*?<bn-chart-time([^>]*)>`)

type cardCell struct {
	slot        string
	card, chart map[string]string
}

func cardCells(html string) []cardCell {
	var out []cardCell
	for _, m := range cardCellPattern.FindAllStringSubmatch(html, -1) {
		out = append(out, cardCell{slot: m[1], card: attrsOf(m[2]), chart: attrsOf(m[3])})
	}
	return out
}

func TestSmallMultiple_CardWrapsEveryPanel(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"card": {"titleImage": "logo_${panel.value}", "footerText": "Source: ${panel.label} \"ctl\""},
		"panels": {"by": "rowGroup"}}`)
	cells := cardCells(out.html)
	if len(cells) != 4 {
		t.Fatalf("card cells = %d, want 4:\n%s", len(cells), out.html)
	}
	if _, ok := attrsOf(out.grid)["cell-captions"]; ok {
		t.Errorf("a card replaces the captions: %s", out.grid)
	}
	ohare := cells[3]
	if ohare.slot != "1-1" || ohare.card["title-business-unit"] != "O'Hare" || ohare.card["card-show-border"] != "false" {
		t.Errorf("card = %+v", ohare.card)
	}
	if ohare.card["title-image"] != "logo_O'Hare" || ohare.card["footer-text"] != `Source: O'Hare "ctl"` {
		t.Errorf("card placeholders = %+v", ohare.card)
	}
	if _, ok := ohare.card["card-fit-to-content"]; ok {
		t.Errorf("equal rows need no fit-to-content: %+v", ohare.card)
	}
	if ohare.chart["filter"] != "[rowGroup] = 'O''Hare'" || ohare.chart["unit-scaling"] != "10" || ohare.chart["chart-title"] != "Profit O'Hare" {
		t.Errorf("chart = %+v", ohare.chart)
	}
}

func TestSmallMultiple_ItemCardOverridesCard(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"card": {"titleImage": "logo", "cardShowBorder": true}, "cellHeight": "auto",
		"panels": {"by": "rowGroup", "limit": 1, "items": [{"label": "Average", "dataset": "profit_avg",
			"card": {"titleImage": "", "titleBusinessUnit": "All ${panel.index}"}}]}}`)
	cells := cardCells(out.html)
	if len(cells) != 2 {
		t.Fatalf("card cells = %d:\n%s", len(cells), out.html)
	}
	if c := cells[0].card; c["title-image"] != "logo" || c["title-business-unit"] != "Berlin" || c["card-show-border"] != "true" || c["card-fit-to-content"] != "true" {
		t.Errorf("first card = %+v", c)
	}
	if c := cells[1].card; c["title-image"] != "" || c["title-business-unit"] != "All 2" {
		t.Errorf("item card = %+v", c)
	}
}

func TestSmallMultiple_CardKeepsAnExplicitCaption(t *testing.T) {
	out := renderSmallMultiple(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", `+inlineBase+`,
		"card": {}, "caption": "${panel.index}", "panels": {"by": "rowGroup", "limit": 1}}`)
	if got := attrsOf(out.grid)["cell-captions"]; got != `{"0-0":"1"}` {
		t.Errorf("captions = %s", got)
	}
}

func TestSmallMultiple_CardErrors(t *testing.T) {
	for smSpec, want := range map[string]string{
		`{"dataset": "profit", "unitScaling": 10, "scale": "0.5", ` + inlineBase + `, "card": {"cardLayout": "2x2"},
			"panels": {"by": "rowGroup"}}`: "cardLayout is set by the SmallMultiple",
		`{"dataset": "profit", "unitScaling": 10, "scale": "0.5", ` + inlineBase + `,
			"panels": {"items": [{"label": "A", "card": {"footerText": "x"}}]}}`: "panels.items[].card needs card",
	} {
		_, _, err := renderSmallMultipleWith(smSpec, "v1.0.0-next.33")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", smSpec, err, want)
		}
	}
}

func TestSmallMultiple_CardEngineGate(t *testing.T) {
	smSpec := `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", ` + inlineBase + `, "card": {}, "panels": {"by": "rowGroup"}}`
	_, _, err := renderSmallMultipleWith(smSpec, "v1.0.0-next.32")
	if err == nil || !strings.Contains(err.Error(), "SmallMultiple card needs template engine v1.0.0-next.33") {
		t.Fatalf("err = %v", err)
	}
	plain := `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", ` + inlineBase + `, "panels": {"by": "rowGroup"}}`
	if _, _, err := renderSmallMultipleWith(plain, "v1.0.0-next.32"); err != nil {
		t.Fatalf("a SmallMultiple without card still works on next.32: %v", err)
	}
}
