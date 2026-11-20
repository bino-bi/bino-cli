package config

import (
	"encoding/json"
	"regexp"
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
	for _, feature := range walkedFeatures(docs, defaults, stackSeriesPrefix) {
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

// StructureSumsFeatures returns what the documents use of the template engine
// release with sum rows in a structure chart, as names for a message, sorted.
// It is empty when every such field is left at its default. The features are:
// sums, scenarioLabelPosition top, referenceLine, italicCategories and the
// stack keys percentMark, labels value, thickness total and totalTitle of a
// ChartStructure, cardOverlayArea of a LayoutCard, groupSeparator of an
// Internationalization, and the marks pointer and bracket and a difference on
// a ChartStructure of an Annotation. The chart fields are found like those of
// StackSeriesFeatures.
func StructureSumsFeatures(docs []Document, defaults map[string]map[string]map[string]any) []string {
	found := map[string]bool{}
	for _, feature := range walkedFeatures(docs, defaults, structureSumsPrefix) {
		found[feature] = true
	}
	// a SmallMultiple with ChartStructure panels, by name: a document, or an inline child at any depth
	structurePanels := map[string]bool{}
	var collect func(v any, name string)
	collect = func(v any, name string) {
		switch node := v.(type) {
		case map[string]any:
			if metadata, ok := node["metadata"].(map[string]any); ok {
				if own, _ := metadata["name"].(string); own != "" {
					name = own
				}
			}
			spec, _ := node["spec"].(map[string]any)
			base, _ := spec["base"].(map[string]any)
			if node["kind"] == "SmallMultiple" && base["kind"] == "ChartStructure" && name != "" {
				structurePanels[name] = true
			}
			for _, child := range node {
				collect(child, "")
			}
		case []any:
			for _, child := range node {
				collect(child, "")
			}
		}
	}
	for _, doc := range docs {
		var v any
		if json.Unmarshal(doc.Raw, &v) == nil {
			collect(v, doc.Name)
		}
	}
	for _, doc := range docs {
		if doc.Kind != "Annotation" {
			continue
		}
		var node struct {
			Spec struct {
				Mark   string `json:"mark"`
				Target struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"target"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &node) != nil {
			continue
		}
		target := node.Spec.Target
		switch node.Spec.Mark {
		case "pointer", "bracket":
			found["Annotation mark: "+node.Spec.Mark] = true
		case "difference":
			if target.Kind == "ChartStructure" || (target.Kind == "SmallMultiple" && structurePanels[target.Name]) {
				found["Annotation mark: difference on a ChartStructure"] = true
			}
		}
	}
	features := make([]string, 0, len(found))
	for feature := range found {
		features = append(features, feature)
	}
	sort.Strings(features)
	return features
}

// structureOrderVariance matches an order of a ChartStructure by a variance.
var structureOrderVariance = regexp.MustCompile(`^dr?(ac|fc|pp|pl)[1-4]_(ac|fc|pp|pl)[1-4](_(pos|neg|neu|opinv|op))?$`)

// structureSumsChart notes the structure sums fields of a ChartStructure spec.
func structureSumsChart(spec map[string]any, found map[string]bool) {
	note := func(field string) { found[structureSumsPrefix+"ChartStructure "+field] = true }
	if sums, _ := spec["sums"].(string); sums != "" && sums != "none" {
		note("sums")
	}
	if spec["scenarioLabelPosition"] == "top" {
		note("scenarioLabelPosition")
	}
	if spec["referenceLine"] != nil {
		note("referenceLine")
	}
	if rows, _ := spec["italicCategories"].([]any); len(rows) > 0 {
		note("italicCategories")
	}
	if spec["varianceWaterfall"] == true {
		note("varianceWaterfall")
	}
	if spec["dataFormatPercentSign"] == false {
		note("dataFormatPercentSign")
	}
	if order, _ := spec["order"].(string); structureOrderVariance.MatchString(order) {
		note("order by a variance")
	}
	if list, _ := spec["sumScenarios"].([]any); len(list) > 0 {
		note("sumScenarios")
	}
	if lines, _ := spec["rowLines"].(float64); lines >= 1 {
		note("rowLines")
	}
	if list, _ := spec["rowLines"].([]any); len(list) > 0 {
		note("rowLines as a list")
	}
	if spec["orderMark"] == true {
		note("orderMark")
	}
	if name, _ := spec["sumDataset"].(string); name != "" {
		note("sumDataset")
	}
	stack, _ := spec["stack"].(map[string]any)
	if stack["percentMark"] == true {
		note("stack.percentMark")
	}
	if stack["labels"] == "value" {
		note("stack.labels")
	}
	if stack["thickness"] == "total" {
		note("stack.thickness")
	}
	if title, _ := stack["totalTitle"].(string); title != "" {
		note("stack.totalTitle")
	}
}

// TimeTotalsFeatures returns what the documents use of the template engine
// release with the total columns of a time chart, as names for a message,
// sorted. It is empty when every such field is left at its default. The
// features are totals (also without their own unitScaling), scalingIndicator,
// dataFormatPercentSign false, varianceWaterfall and referenceColumn of a
// ChartTime, and of its stacked area the stack by dimensions, two-row labels,
// periodLines and pointPosition end. They are found like those of StackSeriesFeatures.
func TimeTotalsFeatures(docs []Document, defaults map[string]map[string]map[string]any) []string {
	return walkedFeatures(docs, defaults, timeTotalsPrefix)
}

// timeTotalsChart notes the time totals fields of a ChartTime spec.
func timeTotalsChart(spec map[string]any, found map[string]bool) {
	if totals, ok := spec["totals"].(map[string]any); ok {
		found[timeTotalsPrefix+"ChartTime totals"] = true
		// only a spec that stands alone tells: an override of a referenced chart, of a
		// SmallMultiple base or of a dataset default has no dataset, and the unitScaling can come from below it
		scaling, set := totals["unitScaling"]
		if _, whole := spec["dataset"]; whole && (!set || scaling == nil || scaling == "") {
			found[timeTotalsPrefix+"ChartTime totals without unitScaling"] = true
		}
		if list, _ := totals["scenarios"].([]any); len(list) > 0 {
			found[timeTotalsPrefix+"ChartTime totals.scenarios"] = true
		}
	}
	if spec["varianceWaterfall"] == true {
		found[timeTotalsPrefix+"ChartTime varianceWaterfall"] = true
	}
	if spec["referenceColumn"] == true {
		found[timeTotalsPrefix+"ChartTime referenceColumn"] = true
	}
	if n, ok := spec["scalingIndicator"].(float64); ok && n > 0 {
		found[timeTotalsPrefix+"ChartTime scalingIndicator"] = true
	}
	if spec["dataFormatPercentSign"] == false {
		found[timeTotalsPrefix+"ChartTime dataFormatPercentSign"] = true
	}
	// only a spec that names both tells: chartMode in one layer (a ref override, a SmallMultiple base,
	// a dataset default) and the stack in another is not seen, and chartMode auto depends on the data
	if spec["chartMode"] == "line" {
		if stack, _ := spec["stack"].(map[string]any); stack["by"] == "dimensions" {
			found[timeTotalsPrefix+"ChartTime stacked area by series"] = true
		}
		if spec["axisLabelsMode"] == "two-row" {
			found[timeTotalsPrefix+"ChartTime two-row labels in line mode"] = true
		}
	}
	if n, ok := spec["periodLines"].(float64); ok && n >= 1 {
		found[timeTotalsPrefix+"ChartTime periodLines"] = true
	}
	if spec["pointPosition"] == "end" {
		found[timeTotalsPrefix+"ChartTime pointPosition"] = true
	}
}
