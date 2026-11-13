package config

import (
	"bytes"
	"encoding/json"
	"sort"
)

// SmallMultipleSpanFeatures returns what the documents use of the template
// engine release with cell spans in bn-grid, as names for a message, sorted.
// It is empty when every such field is left at its default, which is the
// output of every engine. The features are: rowSpan, columnSpan and background
// of a Grid child and of a SmallMultiple panel, axisTitles of a SmallMultiple,
// titleId of a LayoutPage, an empty scenarios list, the digits, axisLabelsStep,
// scenarioSwitch and axisTitlePosition of a chart, and the style keys
// bn-grid.cellBackground and bn-layout-card.titleFontSize. Documents are
// searched at any depth. Dataset defaults are not looked at.
func SmallMultipleSpanFeatures(docs []Document) []string {
	found := map[string]bool{}
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		spanFeatures(v, found)
	}
	features := make([]string, 0, len(found))
	for feature := range found {
		features = append(features, feature)
	}
	sort.Strings(features)
	return features
}

func spanFeatures(v any, found map[string]bool) {
	switch node := v.(type) {
	case map[string]any:
		kind, _ := node["kind"].(string)
		if spec, ok := node["spec"].(map[string]any); ok {
			switch kind {
			case "Grid":
				children, _ := spec["children"].([]any)
				cellFeatures("Grid", children, found)
			case "SmallMultiple":
				smallMultipleFeatures(spec, found)
			case "LayoutPage":
				if id, _ := spec["titleId"].(string); id != "" {
					found["LayoutPage titleId"] = true
				}
			case "ChartTime", "ChartStructure":
				chartFeatures(kind, spec, found)
			case "ComponentStyle":
				styleFeatures(spec["content"], found)
			}
		}
		for _, child := range node {
			spanFeatures(child, found)
		}
	case []any:
		for _, child := range node {
			spanFeatures(child, found)
		}
	}
}

// cellFeatures notes the span and background fields of grid children or
// SmallMultiple panels.
func cellFeatures(kind string, cells []any, found map[string]bool) {
	for _, cell := range cells {
		fields, _ := cell.(map[string]any)
		for _, span := range []string{"rowSpan", "columnSpan"} {
			if n, ok := fields[span].(float64); ok && n > 1 {
				found[kind+" "+span] = true
			}
		}
		if background, _ := fields["background"].(string); background != "" {
			found[kind+" background"] = true
		}
	}
}

func smallMultipleFeatures(spec map[string]any, found map[string]bool) {
	if spec["axisTitles"] == "rowEnd" {
		found["SmallMultiple axisTitles"] = true
	}
	panels, _ := spec["panels"].(map[string]any)
	items, _ := panels["items"].([]any)
	overrides, _ := panels["overrides"].([]any)
	cellFeatures("SmallMultiple", items, found)
	cellFeatures("SmallMultiple", overrides, found)
	// The base is a node with kind and spec, so the walk finds it. An item
	// spec has no kind of its own.
	base, _ := spec["base"].(map[string]any)
	if kind, _ := base["kind"].(string); kind == "ChartTime" || kind == "ChartStructure" {
		for _, item := range items {
			fields, _ := item.(map[string]any)
			if itemSpec, ok := fields["spec"].(map[string]any); ok {
				chartFeatures(kind, itemSpec, found)
			}
		}
	}
}

func chartFeatures(kind string, spec map[string]any, found map[string]bool) {
	if scenarios, ok := spec["scenarios"].([]any); ok && len(scenarios) == 0 {
		found[kind+" scenarios: []"] = true
	}
	// The defaults of the engine, which older engines use for every chart.
	if n, ok := spec["dataFormatDigitsDecimal"].(float64); ok && n != 1 {
		found[kind+" dataFormatDigitsDecimal"] = true
	}
	if n, ok := spec["dataFormatDigitsPercent"].(float64); ok && n != 2 {
		found[kind+" dataFormatDigitsPercent"] = true
	}
	if kind != "ChartTime" {
		return
	}
	if n, ok := spec["axisLabelsStep"].(float64); ok && n > 1 {
		found["ChartTime axisLabelsStep"] = true
	}
	if spec["scenarioSwitch"] == true {
		found["ChartTime scenarioSwitch"] = true
	}
	if position, _ := spec["axisTitlePosition"].(string); position != "" && position != "left" {
		found["ChartTime axisTitlePosition"] = true
	}
}

// styleFeatures notes the new keys of a ComponentStyle content, which is an
// object or a JSON string.
func styleFeatures(content any, found map[string]bool) {
	if text, ok := content.(string); ok {
		if json.Unmarshal(bytes.TrimSpace([]byte(text)), &content) != nil {
			return
		}
	}
	sections, _ := content.(map[string]any)
	if grid, _ := sections["bn-grid"].(map[string]any); grid["cellBackground"] != nil {
		found["ComponentStyle bn-grid.cellBackground"] = true
	}
	if card, _ := sections["bn-layout-card"].(map[string]any); card["titleFontSize"] != nil {
		found["ComponentStyle bn-layout-card.titleFontSize"] = true
	}
}
