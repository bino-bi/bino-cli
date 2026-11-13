package lint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSmallMultipleSpan(t *testing.T) {
	tests := []struct {
		name string
		spec string
		path string
	}{
		{"fits", `{"columns":2,"panels":{"by":"g","overrides":[{"value":"a","columnSpan":2}],"items":[{"label":"b","columnSpan":1}]}}`, ""},
		{"no columns field", `{"panels":{"by":"g","overrides":[{"value":"a","columnSpan":5}]}}`, ""},
		{"override too wide", `{"columns":2,"panels":{"by":"g","overrides":[{"value":"a","columnSpan":3}]}}`, "panels.overrides[0].columnSpan"},
		{"item too wide", `{"columns":2,"panels":{"items":[{"label":"a"},{"label":"b","columnSpan":3}]}}`, "panels.items[1].columnSpan"},
		{"rowSpan has no limit", `{"columns":1,"panels":{"items":[{"label":"a","rowSpan":4}]}}`, ""},
	}
	for _, tt := range tests {
		for _, kind := range []string{"SmallMultiple", "LayoutPage"} {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				findings := smallMultipleSpan.Check(context.Background(), []Document{smDoc(kind, tt.spec)})
				if tt.path == "" {
					wantFindings(t, findings, "", 0, "")
					return
				}
				wantFindings(t, findings, "small-multiple-span", 1, "error")
				if !strings.HasSuffix(findings[0].Path, tt.path) {
					t.Errorf("path = %s, want suffix %s", findings[0].Path, tt.path)
				}
			})
		}
	}
}

func TestSmallMultipleAxisTitles(t *testing.T) {
	tests := []struct {
		name  string
		spec  string
		fires bool
	}{
		{"time chart", `{"axisTitles":"rowEnd","base":{"kind":"ChartTime"}}`, false},
		{"each on a table", `{"axisTitles":"each","base":{"kind":"Table"}}`, false},
		{"not set", `{"base":{"kind":"ChartStructure"}}`, false},
		{"structure chart", `{"axisTitles":"rowEnd","base":{"kind":"ChartStructure"}}`, true},
		{"table", `{"axisTitles":"rowEnd","base":{"kind":"Table"}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := smallMultipleAxisTitles.Check(context.Background(), []Document{smDoc("LayoutPage", tt.spec)})
			if !tt.fires {
				wantFindings(t, findings, "", 0, "")
				return
			}
			wantFindings(t, findings, "small-multiple-axis-titles", 1, "error")
			if !strings.HasSuffix(findings[0].Path, "spec.axisTitles") {
				t.Errorf("path = %s", findings[0].Path)
			}
		})
	}
}

func TestGridCellSpan(t *testing.T) {
	grid := func(children string) string {
		return `{"rowHeaders":["a","b"],"columnHeaders":["x","y"],"children":[` + children + `]}`
	}
	standalone := func(spec string) Document {
		return Document{Kind: "Grid", Name: "g", File: "g.yaml", Position: 1, Raw: json.RawMessage(`{"kind":"Grid","spec":` + spec + `}`)}
	}
	inline := func(spec string) Document {
		return Document{Kind: "LayoutPage", Name: "p", File: "p.yaml", Position: 1,
			Raw: json.RawMessage(`{"kind":"LayoutPage","spec":{"children":[{"kind":"Grid","spec":` + spec + `}]}}`)}
	}

	fits := grid(`{"row":0,"column":0,"kind":"Text","rowSpan":2},{"row":0,"column":1,"kind":"Text"}`)
	wantFindings(t, gridCellSpan.Check(context.Background(), []Document{standalone(fits), inline(fits)}), "", 0, "")

	covered := grid(`{"row":0,"column":0,"kind":"Text","columnSpan":2},{"row":0,"column":1,"kind":"Text"}`)
	findings := gridCellSpan.Check(context.Background(), []Document{standalone(covered)})
	wantFindings(t, findings, "grid-cell-span", 1, "error")
	if findings[0].Path != "spec.children[1]" {
		t.Errorf("path = %s", findings[0].Path)
	}

	beyond := grid(`{"row":1,"column":1,"kind":"Text","rowSpan":2,"columnSpan":2}`)
	findings = gridCellSpan.Check(context.Background(), []Document{inline(beyond)})
	wantFindings(t, findings, "grid-cell-span", 2, "error")
	if findings[0].Path != "spec.children[0].spec.children[0].rowSpan" || findings[1].Path != "spec.children[0].spec.children[0].columnSpan" {
		t.Errorf("paths = %s, %s", findings[0].Path, findings[1].Path)
	}
}

func TestSmallMultipleOverrideFindings(t *testing.T) {
	rows := func(binding string) json.RawMessage {
		if binding == "profit" {
			return json.RawMessage(`[{"rowGroup":"Berlin","year":2024},{"rowGroup":"Munich","year":2025}]`)
		}
		return nil
	}
	sm := func(dataset, by, overrides string) Document {
		return smDoc("SmallMultiple", `{"dataset":"`+dataset+`","base":{"kind":"ChartTime"},"panels":{"by":"`+by+`","overrides":[`+overrides+`]}}`)
	}

	wantFindings(t, SmallMultipleOverrideFindings([]Document{sm("profit", "rowGroup", `{"value":"Berlin","rowSpan":2}`)}, rows), "", 0, "")
	wantFindings(t, SmallMultipleOverrideFindings([]Document{sm("profit", "year", `{"value":2025,"rowSpan":2}`)}, rows), "", 0, "")

	findings := SmallMultipleOverrideFindings([]Document{sm("profit", "rowGroup", `{"value":"Berlin"},{"value":"Hamburg","rowSpan":2}`)}, rows)
	wantFindings(t, findings, "small-multiple-override-unmatched", 1, "warning")
	if findings[0].Path != "spec.panels.overrides[1].value" || !strings.Contains(findings[0].Message, `"Hamburg"`) {
		t.Errorf("finding = %+v", findings[0])
	}

	// rows that are not known, or without the column, prove nothing
	wantFindings(t, SmallMultipleOverrideFindings([]Document{sm("other", "rowGroup", `{"value":"Hamburg"}`)}, rows), "", 0, "")
	wantFindings(t, SmallMultipleOverrideFindings([]Document{sm("profit", "region", `{"value":"Hamburg"}`)}, rows), "", 0, "")
}
