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
		{"cell background at its default", `{"kind":"ComponentStyle","spec":{"content":{"bn-grid":{"cellBackground":" Transparent "}}}}`, nil},
		{"empty cell background", `{"kind":"ComponentStyle","spec":{"content":{"bn-grid":{"cellBackground":""}}}}`, nil},
		{"other style keys", `{"kind":"ComponentStyle","spec":{"content":{"bn-grid":{"captionFontSize":11}}}}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{Kind: "x", Raw: json.RawMessage(tt.raw)}}
			if got := SmallMultipleSpanFeatures(docs, nil); !slices.Equal(got, tt.want) {
				t.Fatalf("features = %v, want %v", got, tt.want)
			}
		})
	}
}

// A ref child that only overrides the items has no base of its own: the kind
// of the base comes from the referenced SmallMultiple.
func TestSmallMultipleSpanFeatures_RefOverride(t *testing.T) {
	override := Document{Kind: "LayoutPage", Name: "p", Raw: json.RawMessage(`{"kind":"LayoutPage","spec":{"children":[
		{"kind":"SmallMultiple","ref":"sm","spec":{"panels":{"items":[{"label":"a","spec":{"scenarioSwitch":true,"scenarios":[]}}]}}}]}}`)}
	want := []string{"ChartTime scenarioSwitch", "ChartTime scenarios: []"}

	inline := Document{Kind: "SmallMultiple", Name: "sm", Raw: json.RawMessage(`{"kind":"SmallMultiple","metadata":{"name":"sm"},
		"spec":{"base":{"kind":"ChartTime","spec":{}},"panels":{"items":[{"label":"a"}]}}}`)}
	if got := SmallMultipleSpanFeatures([]Document{override, inline}, nil); !slices.Equal(got, want) {
		t.Errorf("inline base: features = %v, want %v", got, want)
	}
	byRef := Document{Kind: "SmallMultiple", Name: "sm", Raw: json.RawMessage(`{"kind":"SmallMultiple","metadata":{"name":"sm"},
		"spec":{"base":{"kind":"ChartTime","ref":"trend"},"panels":{"items":[{"label":"a"}]}}}`)}
	if got := SmallMultipleSpanFeatures([]Document{byRef, override}, nil); !slices.Equal(got, want) {
		t.Errorf("base by ref: features = %v, want %v", got, want)
	}
	table := Document{Kind: "SmallMultiple", Name: "sm", Raw: json.RawMessage(`{"kind":"SmallMultiple","metadata":{"name":"sm"},
		"spec":{"base":{"kind":"Table","ref":"t"},"panels":{"items":[{"label":"a"}]}}}`)}
	if got := SmallMultipleSpanFeatures([]Document{table, override}, nil); len(got) != 0 {
		t.Errorf("table base: features = %v", got)
	}
}

func TestSmallMultipleSpanFeatures_DatasetDefaults(t *testing.T) {
	defaults := map[string]map[string]map[string]any{
		"sales": {
			"any":       {"dataFormatDigitsDecimal": float64(0)},
			"charttime": {"scenarioSwitch": true, "axisLabelsStep": float64(2)},
		},
		"plain": {"any": {"dataFormatDigitsDecimal": float64(1)}},
	}
	doc := func(raw string) []Document { return []Document{{Kind: "x", Raw: json.RawMessage(raw)}} }
	tests := []struct {
		name string
		docs []Document
		want []string
	}{
		{"time chart", doc(`{"kind":"ChartTime","spec":{"dataset":"sales"}}`),
			[]string{"ChartTime axisLabelsStep", "ChartTime dataFormatDigitsDecimal", "ChartTime scenarioSwitch"}},
		{"structure chart gets the any default only", doc(`{"kind":"ChartStructure","spec":{"dataset":"sales"}}`),
			[]string{"ChartStructure dataFormatDigitsDecimal"}},
		{"the spec wins over the default", doc(`{"kind":"ChartTime","spec":{"dataset":"sales","scenarioSwitch":false,"axisLabelsStep":1,"dataFormatDigitsDecimal":1}}`), nil},
		{"only the primary dataset counts", doc(`{"kind":"ChartTime","spec":{"dataset":["plain","sales"]}}`), nil},
		{"a default at the engine default", doc(`{"kind":"ChartTime","spec":{"dataset":"plain"}}`), nil},
		{"a table had the digits before", doc(`{"kind":"Table","spec":{"dataset":"sales"}}`), nil},
		{"a datasource binding gets no defaults", doc(`{"kind":"ChartTime","spec":{"dataset":"$sales"}}`), nil},
		{"dataset name from a param", doc(`{"kind":"ChartTime","spec":{"dataset":"${SET}"}}`),
			[]string{"ChartTime axisLabelsStep", "ChartTime dataFormatDigitsDecimal", "ChartTime scenarioSwitch"}},
		{"ref child that sets the dataset", []Document{
			{Kind: "ChartTime", Name: "trend", Raw: json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"trend"},"spec":{"dataset":"plain","scenarioSwitch":false}}`)},
			{Kind: "LayoutPage", Name: "p", Raw: json.RawMessage(`{"kind":"LayoutPage","spec":{"children":[{"kind":"ChartTime","ref":"trend","spec":{"dataset":"sales"}}]}}`)},
		}, []string{"ChartTime axisLabelsStep", "ChartTime dataFormatDigitsDecimal"}},
		{"small multiple panels", doc(`{"kind":"SmallMultiple","spec":{"dataset":"sales","base":{"kind":"ChartTime","spec":{"axisLabelsStep":1}},"panels":{"by":"g"}}}`),
			[]string{"ChartTime dataFormatDigitsDecimal", "ChartTime scenarioSwitch"}},
		{"small multiple item dataset", doc(`{"kind":"SmallMultiple","spec":{"dataset":"plain","base":{"kind":"ChartStructure","spec":{}},"panels":{"items":[{"label":"a","dataset":"sales"}]}}}`),
			[]string{"ChartStructure dataFormatDigitsDecimal"}},
		{"small multiple with a table base", doc(`{"kind":"SmallMultiple","spec":{"dataset":"sales","base":{"kind":"Table","spec":{}},"panels":{"by":"g"}}}`), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SmallMultipleSpanFeatures(tt.docs, defaults); !slices.Equal(got, tt.want) {
				t.Fatalf("features = %v, want %v", got, tt.want)
			}
			if got := SmallMultipleSpanFeatures(tt.docs, nil); len(got) != 0 {
				t.Fatalf("without the defaults: features = %v", got)
			}
		})
	}
}
