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
// an Annotation. The chart fields are found where SmallMultipleSpanFeatures
// finds its own: at any depth, in a panel item, and in the defaults of the
// primary dataset, which may be nil.
func StackSeriesFeatures(docs []Document, defaults map[string]map[string]map[string]any) []string {
	found := map[string]bool{}
	for _, feature := range walkedFeatures(docs, defaults, true) {
		found[feature] = true
	}
	for _, doc := range docs {
		if doc.Kind != "Annotation" {
			continue
		}
		var node struct {
			Spec struct {
				Mark     string `json:"mark"`
				MarkText string `json:"markText"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &node) != nil {
			continue
		}
		if node.Spec.Mark == "difference" {
			found["Annotation mark: difference"] = true
		}
		if node.Spec.MarkText != "" {
			found["Annotation markText"] = true
		}
	}
	features := make([]string, 0, len(found))
	for feature := range found {
		features = append(features, feature)
	}
	sort.Strings(features)
	return features
}

// stackSeriesChart notes the stack series fields of a ChartTime spec.
func stackSeriesChart(spec map[string]any, found map[string]bool) {
	if spec["axisLabelsMode"] == "two-row" {
		found[stackSeriesPrefix+"ChartTime axisLabelsMode: two-row"] = true
	}
	stack, _ := spec["stack"].(map[string]any)
	if stack["notation"] == "scenario" {
		found[stackSeriesPrefix+"ChartTime stack.notation"] = true
	}
	if legend, _ := stack["legend"].(string); legend != "" && legend != "left" {
		found[stackSeriesPrefix+"ChartTime stack.legend"] = true
	}
	if stack["percentMark"] == true {
		found[stackSeriesPrefix+"ChartTime stack.percentMark"] = true
	}
}
