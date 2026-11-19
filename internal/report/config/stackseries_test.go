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
}
