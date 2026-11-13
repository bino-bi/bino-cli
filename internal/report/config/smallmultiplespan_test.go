package config

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestSmallMultipleSpanFeatures(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"plain grid", `{"kind":"Grid","spec":{"children":[{"row":0,"column":0,"kind":"Text","rowSpan":1,"columnSpan":1,"background":""}]}}`, nil},
		{"grid span and background", `{"kind":"Grid","spec":{"children":[{"row":0,"column":0,"kind":"Text","rowSpan":2,"background":"red"},{"row":0,"column":1,"kind":"Text","columnSpan":2}]}}`,
			[]string{"Grid background", "Grid columnSpan", "Grid rowSpan"}},
		{"plain small multiple", `{"kind":"SmallMultiple","spec":{"axisTitles":"each","base":{"kind":"ChartTime","spec":{"scenarios":["ac1"]}},"panels":{"by":"g","items":[{"label":"a"}]}}}`, nil},
		{"small multiple override", `{"kind":"SmallMultiple","spec":{"base":{"kind":"Table","spec":{}},"panels":{"by":"g","overrides":[{"value":"a","rowSpan":2}]}}}`,
			[]string{"SmallMultiple rowSpan"}},
		{"small multiple item", `{"kind":"SmallMultiple","spec":{"base":{"kind":"Table","spec":{}},"panels":{"items":[{"label":"a","columnSpan":2,"background":"#eee"}]}}}`,
			[]string{"SmallMultiple background", "SmallMultiple columnSpan"}},
		{"small multiple axis titles", `{"kind":"SmallMultiple","spec":{"axisTitles":"rowEnd","base":{"kind":"ChartTime","ref":"t"},"panels":{"by":"g"}}}`,
			[]string{"SmallMultiple axisTitles"}},
		{"small multiple base and item spec", `{"kind":"SmallMultiple","spec":{"base":{"kind":"ChartTime","spec":{"scenarios":[]}},"panels":{"items":[{"label":"a","spec":{"scenarioSwitch":true}}]}}}`,
			[]string{"ChartTime scenarioSwitch", "ChartTime scenarios: []"}},
		{"item spec of a table base", `{"kind":"SmallMultiple","spec":{"base":{"kind":"Table","spec":{}},"panels":{"items":[{"label":"a","spec":{"scenarios":[]}}]}}}`, nil},
		{"page title id", `{"kind":"LayoutPage","spec":{"titleId":"13A","children":[]}}`, []string{"LayoutPage titleId"}},
		{"empty page title id", `{"kind":"LayoutPage","spec":{"titleId":"","children":[]}}`, nil},
		{"time chart at its defaults", `{"kind":"ChartTime","spec":{"dataFormatDigitsDecimal":1,"dataFormatDigitsPercent":2,"axisLabelsStep":1,"scenarioSwitch":false,"axisTitlePosition":"left","scenarios":["ac1"]}}`, nil},
		{"time chart, every field", `{"kind":"ChartTime","spec":{"dataFormatDigitsDecimal":0,"dataFormatDigitsPercent":0,"axisLabelsStep":2,"scenarioSwitch":true,"axisTitlePosition":"none","scenarios":[]}}`,
			[]string{"ChartTime axisLabelsStep", "ChartTime axisTitlePosition", "ChartTime dataFormatDigitsDecimal", "ChartTime dataFormatDigitsPercent", "ChartTime scenarioSwitch", "ChartTime scenarios: []"}},
		{"structure chart inline in a card", `{"kind":"LayoutPage","spec":{"children":[{"kind":"LayoutCard","spec":{"children":[{"kind":"ChartStructure","spec":{"scenarios":[],"dataFormatDigitsDecimal":2}}]}}]}}`,
			[]string{"ChartStructure dataFormatDigitsDecimal", "ChartStructure scenarios: []"}},
		{"table with an empty scenario list", `{"kind":"Table","spec":{"scenarios":[],"dataFormatDigitsDecimal":0}}`, nil},
		{"style keys", `{"kind":"ComponentStyle","spec":{"content":{"bn-grid":{"cellBackground":"#eee"},"bn-layout-card":{"titleFontSize":16}}}}`,
			[]string{"ComponentStyle bn-grid.cellBackground", "ComponentStyle bn-layout-card.titleFontSize"}},
		{"style keys in a JSON string", `{"kind":"ComponentStyle","spec":{"content":"{\"bn-grid\":{\"cellBackground\":\"#eee\"}}"}}`,
			[]string{"ComponentStyle bn-grid.cellBackground"}},
		{"other style keys", `{"kind":"ComponentStyle","spec":{"content":{"bn-grid":{"captionFontSize":11}}}}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{Kind: "x", Raw: json.RawMessage(tt.raw)}}
			if got := SmallMultipleSpanFeatures(docs); !slices.Equal(got, tt.want) {
				t.Fatalf("features = %v, want %v", got, tt.want)
			}
		})
	}
}
