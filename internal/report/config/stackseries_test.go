package config

import (
	"reflect"
	"testing"
)

func TestStackSeriesFeatures(t *testing.T) {
	doc := func(raw string) Document { return Document{Raw: []byte(raw)} }

	plain := []Document{
		doc(`{"kind":"ChartTime","spec":{"stack":{"by":"dimensions","legend":"left"},"axisLabelsMode":"smart"}}`),
		doc(`{"kind":"ChartStructure","spec":{"stack":{"by":"dimensions","notation":"scenario"}}}`),
		doc(`{"kind":"Annotation","spec":{"mark":"arrow"}}`),
	}
	if got := StackSeriesFeatures(plain); len(got) != 0 {
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
	if got := StackSeriesFeatures(used); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
