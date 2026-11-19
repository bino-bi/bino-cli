package config

import (
	"encoding/json"
	"sort"
)

// StackSeriesFeatures returns what the documents use of the template engine
// release with stacks by series, as names for a message, sorted. It is empty
// when every such field is left at its default. The features are: notation,
// a legend other than left and percentMark of a ChartTime stack,
// axisLabelsMode two-row of a ChartTime, and mark difference and markText of
// an Annotation. Documents are searched at any depth, so an inline child and
// the base of a SmallMultiple count too.
func StackSeriesFeatures(docs []Document) []string {
	found := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch node := v.(type) {
		case []any:
			for _, e := range node {
				walk(e)
			}
		case map[string]any:
			if spec, ok := node["spec"].(map[string]any); ok {
				switch node["kind"] {
				case "ChartTime":
					stackSeriesChart(spec, found)
				case "Annotation":
					if spec["mark"] == "difference" {
						found["Annotation mark: difference"] = true
					}
					if text, _ := spec["markText"].(string); text != "" {
						found["Annotation markText"] = true
					}
				}
			}
			for _, e := range node {
				walk(e)
			}
		}
	}
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		// the kind of a document can be known from its header only
		if node, ok := v.(map[string]any); ok && node["kind"] == nil && doc.Kind != "" {
			node["kind"] = doc.Kind
		}
		walk(v)
	}
	features := make([]string, 0, len(found))
	for feature := range found {
		features = append(features, feature)
	}
	sort.Strings(features)
	return features
}

func stackSeriesChart(spec map[string]any, found map[string]bool) {
	if spec["axisLabelsMode"] == "two-row" {
		found["ChartTime axisLabelsMode: two-row"] = true
	}
	stack, _ := spec["stack"].(map[string]any)
	if stack["notation"] == "scenario" {
		found["ChartTime stack.notation"] = true
	}
	if legend, _ := stack["legend"].(string); legend != "" && legend != "left" {
		found["ChartTime stack.legend"] = true
	}
	if stack["percentMark"] == true {
		found["ChartTime stack.percentMark"] = true
	}
}
