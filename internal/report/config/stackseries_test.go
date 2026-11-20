package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStackSeriesFeatures(t *testing.T) {
	doc := func(raw string) Document {
		var head struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal([]byte(raw), &head)
		return Document{Kind: head.Kind, Raw: []byte(raw)}
	}

	plain := []Document{
		doc(`{"kind":"ChartTime","spec":{"stack":{"by":"dimensions","legend":"left"},"axisLabelsMode":"smart"}}`),
		doc(`{"kind":"ChartStructure","spec":{"stack":{"by":"dimensions","notation":"scenario"}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"arrow"}}`),
	}
	if got := StackSeriesFeatures(plain, nil); len(got) != 0 {
		t.Errorf("defaults: got %v, want none", got)
	}

	used := []Document{
		doc(`{"kind":"Grid","spec":{"children":[{"kind":"ChartTime","ref":"c","spec":{"axisLabelsMode":"two-row"}}]}}`),
		doc(`{"kind":"ChartTime","spec":{"stack":{"by":"dimensions","notation":"scenario","legend":"both","percentMark":true}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"difference"}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"frame","markText":"61%"}}`),
	}
	want := []string{
		"Annotation mark: difference",
		"Annotation markText",
		"ChartTime axisLabelsMode: two-row",
		"ChartTime stack.legend",
		"ChartTime stack.notation",
		"ChartTime stack.percentMark",
	}
	if got := StackSeriesFeatures(used, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	item := []Document{
		doc(`{"kind":"SmallMultiple","spec":{"base":{"kind":"ChartTime","spec":{"dataset":"d"}},"panels":{"items":[{"label":"a","spec":{"stack":{"by":"dimensions","notation":"scenario"}}}]}}}`),
	}
	if got := StackSeriesFeatures(item, nil); !reflect.DeepEqual(got, []string{"ChartTime stack.notation"}) {
		t.Errorf("panel item: got %v", got)
	}
	if got := SmallMultipleSpanFeatures(used, nil); len(got) != 0 {
		t.Errorf("the span gate must not report stack series fields, got %v", got)
	}

	byDefault := []Document{doc(`{"kind":"ChartTime","spec":{"dataset":"sales"}}`)}
	defaults := map[string]map[string]map[string]any{"sales": {"charttime": {"axisLabelsMode": "two-row"}}}
	if got := StackSeriesFeatures(byDefault, defaults); !reflect.DeepEqual(got, []string{"ChartTime axisLabelsMode: two-row"}) {
		t.Errorf("dataset default: got %v", got)
	}

	partial := []Document{doc(`{"kind":"ChartTime","spec":{"dataset":"sales","stack":{"by":"dimensions","legend":"left"}}}`)}
	stackDefault := map[string]map[string]map[string]any{"sales": {"charttime": {"stack": map[string]any{"notation": "scenario", "legend": "both"}}}}
	if got := StackSeriesFeatures(partial, stackDefault); !reflect.DeepEqual(got, []string{"ChartTime stack.notation"}) {
		t.Errorf("stack keys of a dataset default: got %v", got)
	}
}

func TestStructureSumsFeatures(t *testing.T) {
	doc := func(raw string) Document {
		var head struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal([]byte(raw), &head)
		return Document{Kind: head.Kind, Raw: []byte(raw)}
	}

	plain := []Document{
		doc(`{"kind":"ChartStructure","spec":{"sums":"none","scenarioLabelPosition":"bottom","italicCategories":[],"stack":{"by":"scenarios","labels":"share","thickness":"equal"}}}`),
		doc(`{"kind":"ChartTime","spec":{"stack":{"by":"dimensions","percentMark":true}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"difference","target":{"kind":"ChartTime","name":"c"}}}`),
		doc(`{"kind":"LayoutCard","spec":{"cardLayout":"full"}}`),
		doc(`{"kind":"Internationalization","spec":{"code":"en","content":{}}}`),
	}
	if got := StructureSumsFeatures(plain, nil); len(got) != 0 {
		t.Errorf("defaults: got %v, want none", got)
	}

	waterfall := []Document{doc(`{"kind":"ChartStructure","spec":{"varianceWaterfall":true,"dataFormatPercentSign":false,"order":"dac1_pp1_pos"}}`)}
	wantWaterfall := []string{"ChartStructure dataFormatPercentSign", "ChartStructure order by a variance", "ChartStructure varianceWaterfall"}
	if got := StructureSumsFeatures(waterfall, nil); !reflect.DeepEqual(got, wantWaterfall) {
		t.Errorf("waterfall: got %v, want %v", got, wantWaterfall)
	}
	byScenario := []Document{doc(`{"kind":"ChartStructure","spec":{"varianceWaterfall":false,"dataFormatPercentSign":true,"order":"ac1"}}`)}
	if got := StructureSumsFeatures(byScenario, nil); len(got) != 0 {
		t.Errorf("order by a scenario: got %v, want none", got)
	}

	used := []Document{
		doc(`{"kind":"Grid","spec":{"children":[{"kind":"ChartStructure","ref":"c","spec":{"sums":"total","scenarioLabelPosition":"top"}}]}}`),
		doc(`{"kind":"ChartStructure","spec":{"referenceLine":{"value":50},"italicCategories":["Rest"],"stack":{"by":"scenarios","mode":"relative","percentMark":true,"labels":"value","thickness":"total","totalTitle":"All"}}}`),
		doc(`{"kind":"LayoutCard","spec":{"cardOverlayArea":"3 / 3 / 5 / 4"}}`),
		doc(`{"kind":"Internationalization","spec":{"code":"en","groupSeparator":"thin-space","content":{}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"pointer","target":{"kind":"ChartTime","name":"c"}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"bracket","target":{"kind":"ChartStructure","name":"c"}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"difference","target":{"kind":"ChartStructure","name":"c"}}}`),
	}
	want := []string{
		"Annotation mark: bracket",
		"Annotation mark: difference on a ChartStructure",
		"Annotation mark: pointer",
		"ChartStructure italicCategories",
		"ChartStructure referenceLine",
		"ChartStructure scenarioLabelPosition",
		"ChartStructure stack.labels",
		"ChartStructure stack.percentMark",
		"ChartStructure stack.thickness",
		"ChartStructure stack.totalTitle",
		"ChartStructure sums",
		"Internationalization groupSeparator",
		"LayoutCard cardOverlayArea",
	}
	if got := StructureSumsFeatures(used, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// each gate reports only its own fields
	if got := SmallMultipleSpanFeatures(used, nil); len(got) != 0 {
		t.Errorf("the span gate must not report structure sums fields, got %v", got)
	}
	if got := StackSeriesFeatures(used, nil); !reflect.DeepEqual(got, []string{"Annotation mark: difference"}) {
		t.Errorf("the stack series gate: got %v", got)
	}

	panel := []Document{
		{Kind: "SmallMultiple", Name: "by_region", Raw: []byte(`{"kind":"SmallMultiple","spec":{"base":{"kind":"ChartStructure"}}}`)},
		doc(`{"kind":"Annotation","spec":{"mark":"difference","target":{"kind":"SmallMultiple","name":"by_region","panel":"North"}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"difference","target":{"kind":"SmallMultiple","name":"other"}}}`),
	}
	if got := StructureSumsFeatures(panel, nil); !reflect.DeepEqual(got, []string{"Annotation mark: difference on a ChartStructure"}) {
		t.Errorf("panel of a SmallMultiple: got %v", got)
	}

	inline := []Document{
		doc(`{"kind":"LayoutPage","metadata":{"name":"page"},"spec":{"children":[{"kind":"SmallMultiple","metadata":{"name":"by_region"},"spec":{"base":{"kind":"ChartStructure"}}}]}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"difference","target":{"kind":"SmallMultiple","name":"by_region","panel":"North"}}}`),
	}
	if got := StructureSumsFeatures(inline, nil); !reflect.DeepEqual(got, []string{"Annotation mark: difference on a ChartStructure"}) {
		t.Errorf("inline SmallMultiple: got %v", got)
	}

	byDefault := []Document{doc(`{"kind":"ChartStructure","spec":{"dataset":"sales","stack":{"by":"scenarios"}}}`)}
	defaults := map[string]map[string]map[string]any{"sales": {"chartstructure": {"sums": "total", "stack": map[string]any{"thickness": "total"}}}}
	wantDefault := []string{"ChartStructure stack.thickness", "ChartStructure sums"}
	if got := StructureSumsFeatures(byDefault, defaults); !reflect.DeepEqual(got, wantDefault) {
		t.Errorf("dataset default: got %v, want %v", got, wantDefault)
	}
}

func TestTimeTotalsFeatures(t *testing.T) {
	doc := func(spec string) []Document {
		return []Document{{Kind: "ChartTime", Name: "c", Raw: json.RawMessage(`{"kind":"ChartTime","metadata":{"name":"c"},"spec":` + spec + `}`)}}
	}
	if got := TimeTotalsFeatures(doc(`{"dataset":"d","dataFormatPercentSign":true,"scalingIndicator":0}`), nil); len(got) != 0 {
		t.Fatalf("defaults are no feature, got %v", got)
	}
	used := doc(`{"dataset":"d","totals":{"unitScaling":280},"scalingIndicator":100,"dataFormatPercentSign":false}`)
	want := []string{"ChartTime dataFormatPercentSign", "ChartTime scalingIndicator", "ChartTime totals"}
	if got := TimeTotalsFeatures(used, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	waterfall := doc(`{"dataset":"d","totals":{"by":"all"},"varianceWaterfall":true,"referenceColumn":true}`)
	wantWaterfall := []string{"ChartTime referenceColumn", "ChartTime totals", "ChartTime totals without unitScaling", "ChartTime varianceWaterfall"}
	if got := TimeTotalsFeatures(waterfall, nil); !reflect.DeepEqual(got, wantWaterfall) {
		t.Fatalf("got %v, want %v", got, wantWaterfall)
	}
	if got := TimeTotalsFeatures(doc(`{"dataset":"d","varianceWaterfall":false,"referenceColumn":false}`), nil); len(got) != 0 {
		t.Fatalf("false is no feature, got %v", got)
	}
	// the other gates do not report them
	if got := StackSeriesFeatures(used, nil); len(got) != 0 {
		t.Fatalf("stack series gate got %v", got)
	}
	// a structure chart has no such fields
	structure := []Document{{Kind: "ChartStructure", Raw: json.RawMessage(`{"kind":"ChartStructure","spec":{"dataset":"d","totals":{"unitScaling":1}}}`)}}
	if got := TimeTotalsFeatures(structure, nil); len(got) != 0 {
		t.Fatalf("structure chart got %v", got)
	}
}
