package render

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

const spanEngine = "v1.0.0-next.46"

var spanCellPattern = regexp.MustCompile(`<div slot='(\d+-\d+)'([^>]*)>\s*<(bn-[a-z-]+)([^>]*)>`)

// spanCell is one cell of a rendered grid: the attributes of the slotted
// child and of the component in it.
type spanCell struct {
	slot      string
	cell      map[string]string
	component map[string]string
}

func spanCells(html string) []spanCell {
	matches := spanCellPattern.FindAllStringSubmatch(html, -1)
	out := make([]spanCell, 0, len(matches))
	for _, m := range matches {
		out = append(out, spanCell{slot: m[1], cell: attrsOf(m[2]), component: attrsOf(m[4])})
	}
	return out
}

func renderSpan(t *testing.T, smSpec string, extra ...config.Document) (string, []string) {
	t.Helper()
	res, _, err := renderSmallMultipleWith(smSpec, spanEngine, extra...)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(res.HTML), res.Warnings
}

const spanHead = `"dataset": "profit", "unitScaling": 10, "scale": "0.5", ` + inlineBase

func slotsOf(cells []spanCell) []string {
	slots := make([]string, len(cells))
	for i, c := range cells {
		slots[i] = c.slot
	}
	return slots
}

func TestPlacePanels(t *testing.T) {
	p := func(rowSpan, columnSpan int) panel { return panel{rowSpan: rowSpan, columnSpan: columnSpan} }
	tests := []struct {
		name     string
		panels   []panel
		columns  int
		want     [][2]int
		wantRows int
	}{
		{"no span", []panel{p(1, 1), p(1, 1), p(1, 1), p(1, 1), p(1, 1)}, 2, [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {2, 0}}, 3},
		{"row span at the end of a row", []panel{p(1, 1), p(1, 1), p(2, 1), p(1, 1), p(1, 1), p(1, 1)}, 3,
			[][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {2, 0}}, 3},
		{"row span at the start of a row", []panel{p(2, 1), p(1, 1), p(1, 1), p(1, 1)}, 2, [][2]int{{0, 0}, {0, 1}, {1, 1}, {2, 0}}, 3},
		{"column span moves to the next row", []panel{p(1, 1), p(1, 1), p(1, 2), p(1, 1)}, 3, [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 2}}, 2},
		{"a skipped position stays empty", []panel{p(1, 2), p(1, 2), p(1, 1)}, 3, [][2]int{{0, 0}, {1, 0}, {1, 2}}, 2},
		{"rows count the covered positions", []panel{p(3, 1)}, 1, [][2]int{{0, 0}}, 3},
		{"block", []panel{p(2, 2), p(1, 1), p(1, 1), p(1, 1)}, 3, [][2]int{{0, 0}, {0, 2}, {1, 2}, {2, 0}}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := placePanels(tt.panels, tt.columns)
			if rows != tt.wantRows {
				t.Errorf("rows = %d, want %d", rows, tt.wantRows)
			}
			for i, p := range tt.panels {
				if got := [2]int{p.row, p.column}; got != tt.want[i] {
					t.Errorf("panel %d at %v, want %v", i, got, tt.want[i])
				}
			}
		})
	}
}

func TestSmallMultipleColumns(t *testing.T) {
	plain := panel{rowSpan: 1, columnSpan: 1}
	if got := smallMultipleColumns(0, []panel{plain, plain, plain, plain, plain}); got != 3 {
		t.Errorf("5 panels: %d columns, want 3", got)
	}
	if got := smallMultipleColumns(0, []panel{plain, plain, plain, plain, plain, {rowSpan: 2, columnSpan: 2}}); got != 3 {
		t.Errorf("9 positions: %d columns, want 3", got)
	}
	if got := smallMultipleColumns(0, []panel{{rowSpan: 1, columnSpan: 4}}); got != 4 {
		t.Errorf("one wide panel: %d columns, want 4", got)
	}
	if got := smallMultipleColumns(2, []panel{plain, plain, plain, plain, plain}); got != 2 {
		t.Errorf("columns field: %d, want 2", got)
	}
}

// Without the new fields a SmallMultiple renders as before: no attribute on
// the cells and the same slots.
func TestSmallMultiple_NoSpanAttributesByDefault(t *testing.T) {
	html, _ := renderSpan(t, `{`+spanHead+`, "panels": {"by": "rowGroup"}}`)
	for _, absent := range []string{"row-span", "column-span", "cell-background", "axis-title-position", "data-bino-annotation-target"} {
		if strings.Contains(html, absent) {
			t.Errorf("unexpected %s in:\n%s", absent, html)
		}
	}
	if got := strings.Join(slotsOf(spanCells(html)), " "); got != "0-0 0-1 1-0 1-1" {
		t.Errorf("slots = %s", got)
	}
	if !strings.Contains(html, "<div slot='0-0'><bn-chart-time") {
		t.Errorf("the cell element changed:\n%s", html)
	}
}

func TestSmallMultiple_OverridesAndItemSpan(t *testing.T) {
	html, warnings := renderSpan(t, `{`+spanHead+`, "columns": 3, "panels": {"by": "rowGroup",
		"overrides": [
			{"value": "Munich", "rowSpan": 2, "background": "#fff3c4"},
			{"value": "Nowhere", "columnSpan": 2}
		],
		"items": [{"label": "Total", "columnSpan": 2, "background": "rgb(1, 2, 3)"}]}}`)
	cells := spanCells(html)
	// Berlin, Cologne, Munich over two rows, O'Hare, then Total over two columns.
	if got := strings.Join(slotsOf(cells), " "); got != "0-0 0-1 0-2 1-0 2-0" {
		t.Fatalf("slots = %s", got)
	}
	munich, total := cells[2].cell, cells[4].cell
	if munich["row-span"] != "2" || munich["column-span"] != "" || munich["cell-background"] != "#fff3c4" {
		t.Errorf("Munich cell = %v", munich)
	}
	if total["column-span"] != "2" || total["row-span"] != "" || total["cell-background"] != "rgb(1, 2, 3)" {
		t.Errorf("Total cell = %v", total)
	}
	if len(cells[0].cell) != 0 {
		t.Errorf("Berlin cell has attributes: %v", cells[0].cell)
	}
	grid := attrsOf(openTag(t, html, "bn-grid"))
	if grid["row-headers"] != `["r0","r1","r2"]` || grid["column-headers"] != `["c0","c1","c2"]` {
		t.Errorf("headers = %s x %s", grid["row-headers"], grid["column-headers"])
	}
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w, `panels.overrides: no panel has the value "Nowhere"`)
	}
	if !found {
		t.Errorf("no warning for the unmatched override: %v", warnings)
	}
}

func TestSmallMultiple_OverrideMatchesNumberValue(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "SmallMultiple", "metadata": {"name": "sm"}, "spec": {"dataset": "years",
		"unitScaling": 10, "scale": "0.5", ` + inlineBase + `,
		"panels": {"by": "year", "overrides": [{"value": 2025, "background": "red"}]}}}]}`)
	results := []dataset.Result{{Name: "years", Data: json.RawMessage(`[{"year":2024,"ac1":1},{"year":2025,"ac1":2}]`)}}
	res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, results, "en", "", "", ModeBuild, nil, nil, spanEngine, nil, nil, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cells := spanCells(string(res.HTML))
	if len(cells) != 2 || cells[0].cell["cell-background"] != "" || cells[1].cell["cell-background"] != "red" {
		t.Errorf("cells = %+v", cells)
	}
}

func TestSmallMultiple_ColumnSpanLargerThanColumnsFails(t *testing.T) {
	_, _, err := renderSmallMultipleWith(`{`+spanHead+`, "columns": 2, "panels": {"by": "rowGroup",
		"overrides": [{"value": "Berlin", "columnSpan": 3}]}}`, spanEngine)
	if err == nil || !strings.Contains(err.Error(), `panel "Berlin": columnSpan 3 is larger than the 2 column(s)`) {
		t.Fatalf("err = %v", err)
	}
}

func TestSmallMultiple_AxisTitlesRowEnd(t *testing.T) {
	html, _ := renderSpan(t, `{`+spanHead+`, "columns": 3, "axisTitles": "rowEnd", "panels": {"by": "rowGroup",
		"overrides": [{"value": "Munich", "rowSpan": 2}],
		"items": [
			{"label": "Keeps", "spec": {"axisTitlePosition": "left"}},
			{"label": "Last"}
		]}}`)
	// Row 0: Berlin, Cologne, Munich (two rows). Row 1: O'Hare, Keeps. Row 2: Last.
	want := map[string]string{"0-0": "none", "0-1": "none", "0-2": "right", "1-0": "none", "1-1": "left", "2-0": "right"}
	cells := spanCells(html)
	if len(cells) != len(want) {
		t.Fatalf("slots = %v", slotsOf(cells))
	}
	for _, c := range cells {
		if got := c.component["axis-title-position"]; got != want[c.slot] {
			t.Errorf("slot %s: axis-title-position = %q, want %q", c.slot, got, want[c.slot])
		}
	}
}

func TestSmallMultiple_AxisTitlesRowEndReplacesTheBase(t *testing.T) {
	html, _ := renderSpan(t, `{"dataset": "profit", "unitScaling": 10, "scale": "0.5", "axisTitles": "rowEnd", "columns": 2,
		"base": {"kind": "ChartTime", "spec": {"axisTitlePosition": "left"}}, "panels": {"by": "rowGroup", "limit": 2}}`)
	cells := spanCells(html)
	if len(cells) != 2 || cells[0].component["axis-title-position"] != "none" || cells[1].component["axis-title-position"] != "right" {
		t.Errorf("cells = %+v", cells)
	}
}

func TestSmallMultiple_AxisTitlesRowEndNeedsChartTime(t *testing.T) {
	_, _, err := renderSmallMultipleWith(`{"dataset": "profit", "unitScaling": 10, "scale": "0.5", "axisTitles": "rowEnd",
		"base": {"kind": "ChartStructure", "spec": {}}, "panels": {"by": "rowGroup"}}`, spanEngine)
	if err == nil || !strings.Contains(err.Error(), "axisTitles rowEnd needs a ChartTime base, not ChartStructure") {
		t.Fatalf("err = %v", err)
	}
}

// The point check of a panel looks at the rows of that panel only: Cologne has
// no value in 2025, Berlin has.
// A target without panel is the SmallMultiple as a whole, like a Grid.
func gridPage(children string) config.Document {
	return pageDoc(`{"children": [{"kind": "Grid", "metadata": {"name": "g"}, "spec": {
		"rowHeaders": ["a", "b"], "columnHeaders": [{"label": "X", "id": "x"}, {"label": "Y", "id": "y"}, {"label": "Z", "id": "z"}],
		"children": [` + children + `]}}]}`)
}

func gridText(row, column, value, extra string) string {
	return `{"row": ` + row + `, "column": "` + column + `", "kind": "Text", "spec": {"value": "` + value + `"}` + extra + `}`
}

func renderGridPage(t *testing.T, children string) (string, []string) {
	t.Helper()
	res, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{gridPage(children)}, "en", "", "", ModeBuild, spanEngine)
	if err != nil {
		t.Fatal(err)
	}
	return string(res.HTML), res.Warnings
}

func TestGrid_CellSpanAndBackground(t *testing.T) {
	html, warnings := renderGridPage(t, gridText("0", "x", "wide", `, "rowSpan": 2, "columnSpan": 2, "background": "#eee"`)+","+
		gridText("0", "z", "plain", `, "rowSpan": 1, "columnSpan": 1`)+","+
		gridText("1", "y", "hidden", ""))
	if !strings.Contains(html, "<div slot='0-x' row-span='2' column-span='2' cell-background='#eee'><bn-text") {
		t.Errorf("span attributes missing:\n%s", html)
	}
	if !strings.Contains(html, "<div slot='0-z'><bn-text") {
		t.Errorf("a span of 1 must write nothing:\n%s", html)
	}
	if strings.Contains(html, "slot='1-y'") || strings.Contains(html, "hidden") {
		t.Errorf("a child in a covered position must not be written:\n%s", html)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "row 1, column y is not shown") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestGrid_NoCellAttributesByDefault(t *testing.T) {
	html, _ := renderGridPage(t, gridText("0", "x", "a", "")+","+gridText("1", "y", "b", ""))
	for _, absent := range []string{"row-span", "column-span", "cell-background"} {
		if strings.Contains(html, absent) {
			t.Errorf("unexpected %s in:\n%s", absent, html)
		}
	}
}

func TestGridSpanProblems(t *testing.T) {
	spec := func(children string) json.RawMessage {
		return json.RawMessage(`{"rowHeaders": ["a", "b"], "columnHeaders": ["x", "y", "z"], "children": [` + children + `]}`)
	}
	cell := func(row, column, extra string) string {
		return `{"row": ` + row + `, "column": ` + column + `, "kind": "Text"` + extra + `}`
	}
	tests := []struct {
		name     string
		children string
		want     []GridSpanProblem
	}{
		{"no span", cell("0", "0", "") + "," + cell("1", "1", ""), nil},
		{"span that fits", cell("0", "0", `, "rowSpan": 2, "columnSpan": 2`) + "," + cell("0", "2", "") + "," + cell("1", "2", ""), nil},
		{"covered child", cell("0", "0", `, "columnSpan": 2`) + "," + cell("0", "1", ""),
			[]GridSpanProblem{{Child: 1, Message: "the span of the child in row 0, column 0 covers this position: the child is not shown"}}},
		{"covered child written first", cell("1", "0", "") + "," + cell("0", "0", `, "rowSpan": 2`),
			[]GridSpanProblem{{Child: 0, Message: "the span of the child in row 0, column 0 covers this position: the child is not shown"}}},
		{"beyond the last row", cell("1", "0", `, "rowSpan": 2`),
			[]GridSpanProblem{{Child: 0, Field: "rowSpan", Message: "rowSpan 2 goes beyond the last row: only 1 row(s) from row 1 on"}}},
		{"beyond the last column", cell("0", "1", `, "columnSpan": 3`),
			[]GridSpanProblem{{Child: 0, Field: "columnSpan", Message: "columnSpan 3 goes beyond the last column: only 2 column(s) from column 1 on"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GridSpanProblems(spec(tt.children))
			if len(got) != len(tt.want) {
				t.Fatalf("problems = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("problem %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
