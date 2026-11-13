package config

import (
	"bytes"
	"encoding/json"
	"maps"
	"sort"
	"strings"

	reportspec "bino.bi/bino/internal/report/spec"
)

// SmallMultipleSpanFeatures returns what the documents use of the template
// engine release with cell spans in bn-grid, as names for a message, sorted.
// It is empty when every such field is left at its default, which is the
// output of every engine. The features are: rowSpan, columnSpan and background
// of a Grid child and of a SmallMultiple panel, axisTitles of a SmallMultiple,
// titleId of a LayoutPage, an empty scenarios list, the digits, axisLabelsStep,
// scenarioSwitch and axisTitlePosition of a chart, and the style keys
// bn-grid.cellBackground and bn-layout-card.titleFontSize. Documents are
// searched at any depth.
//
// A chart also gets fields from the defaults of its primary dataset, which no
// manifest shows. defaults holds them by dataset name and kind token
// ("charttime", "chartstructure", "any"); it may be nil when the data is not
// known.
func SmallMultipleSpanFeatures(docs []Document, defaults map[string]map[string]map[string]any) []string {
	w := spanWalk{found: map[string]bool{}, bases: map[string]map[string]any{}, defaults: defaults}
	roots := make([]any, 0, len(docs))
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		roots = append(roots, v)
		if node, ok := v.(map[string]any); ok && doc.Name != "" {
			w.bases[doc.Kind+":"+doc.Name], _ = node["spec"].(map[string]any)
		}
	}
	for _, root := range roots {
		w.walk(root)
	}
	features := make([]string, 0, len(w.found))
	for feature := range w.found {
		features = append(features, feature)
	}
	sort.Strings(features)
	return features
}

// spanWalk collects the features of SmallMultipleSpanFeatures.
type spanWalk struct {
	found map[string]bool
	// bases maps kind:name to the spec of a document, for a 'ref'.
	bases    map[string]map[string]any
	defaults map[string]map[string]map[string]any
}

func (w spanWalk) walk(v any) {
	switch node := v.(type) {
	case map[string]any:
		kind, _ := node["kind"].(string)
		if spec, ok := node["spec"].(map[string]any); ok {
			ref, _ := node["ref"].(string)
			switch kind {
			case "Grid":
				children, _ := spec["children"].([]any)
				cellFeatures("Grid", children, w.found)
			case "SmallMultiple":
				w.smallMultiple(spec, w.bases[kind+":"+ref])
			case "LayoutPage":
				if id, _ := spec["titleId"].(string); id != "" {
					w.found["LayoutPage titleId"] = true
				}
			case "ChartTime", "ChartStructure":
				chartFeatures(kind, spec, w.found)
				merged := overlay(w.bases[kind+":"+ref], spec)
				w.chartDefaults(kind, merged, merged["dataset"])
			case "ComponentStyle":
				styleFeatures(spec["content"], w.found)
			}
		}
		for _, child := range node {
			w.walk(child)
		}
	case []any:
		for _, child := range node {
			w.walk(child)
		}
	}
}

// overlay is base with the fields of over on top, one level deep.
func overlay(base, over map[string]any) map[string]any {
	merged := maps.Clone(base)
	if merged == nil {
		merged = map[string]any{}
	}
	maps.Copy(merged, over)
	return merged
}

// chartDefaults notes the features that the defaults of the primary dataset
// give a chart for the fields its spec leaves unset. A dataset name with a
// param placeholder is only known at render time: then every dataset counts.
func (w spanWalk) chartDefaults(kind string, spec map[string]any, datasetField any) {
	if len(w.defaults) == 0 {
		return
	}
	var datasets reportspec.DatasetList
	raw, err := json.Marshal(datasetField)
	if err != nil || json.Unmarshal(raw, &datasets) != nil {
		return
	}
	entries := datasets.Entries()
	if len(entries) == 0 || !entries[0].IsRef() {
		return // an inline DataSet gets no defaults
	}
	var sets []map[string]map[string]any
	switch name := entries[0].Ref; {
	case strings.Contains(name, "${"):
		for _, set := range w.defaults {
			sets = append(sets, set)
		}
	case !strings.HasPrefix(name, "$"): // a `$` DataSource binding gets no defaults
		sets = append(sets, w.defaults[name])
	}
	for _, set := range sets {
		unset := map[string]any{}
		for _, token := range []string{"any", strings.ToLower(kind)} {
			for field, value := range set[token] {
				if spec[field] == nil {
					unset[field] = value
				}
			}
		}
		chartFeatures(kind, unset, w.found)
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

// smallMultiple notes the features of a SmallMultiple spec. referenced is the
// spec of the document a child refs, nil for a document or an inline child: a
// spec that only overrides a part has no base of its own.
func (w spanWalk) smallMultiple(spec, referenced map[string]any) {
	if spec["axisTitles"] == "rowEnd" {
		w.found["SmallMultiple axisTitles"] = true
	}
	panels, _ := spec["panels"].(map[string]any)
	items, _ := panels["items"].([]any)
	overrides, _ := panels["overrides"].([]any)
	cellFeatures("SmallMultiple", items, w.found)
	cellFeatures("SmallMultiple", overrides, w.found)

	merged := overlay(referenced, spec)
	base, _ := merged["base"].(map[string]any)
	if own, ok := spec["base"].(map[string]any); ok {
		refBase, _ := referenced["base"].(map[string]any)
		base = overlay(refBase, own)
	}
	kind, _ := base["kind"].(string)
	if kind != "ChartTime" && kind != "ChartStructure" {
		return
	}
	// The base with a spec is a node with kind and spec, so the walk finds
	// its own fields. An item spec has no kind of its own.
	baseRef, _ := base["ref"].(string)
	baseSpec, _ := base["spec"].(map[string]any)
	baseSpec = overlay(w.bases[kind+":"+baseRef], baseSpec)
	// The panels get the dataset of the SmallMultiple, or of their item.
	w.chartDefaults(kind, baseSpec, merged["dataset"])
	for _, item := range items {
		fields, _ := item.(map[string]any)
		if itemSpec, ok := fields["spec"].(map[string]any); ok {
			chartFeatures(kind, itemSpec, w.found)
		}
		if name, _ := fields["dataset"].(string); name != "" {
			w.chartDefaults(kind, baseSpec, name)
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
	grid, _ := sections["bn-grid"].(map[string]any)
	// transparent is the default, and the look of the engines without the key
	if background, _ := grid["cellBackground"].(string); strings.TrimSpace(background) != "" && !strings.EqualFold(strings.TrimSpace(background), "transparent") {
		found["ComponentStyle bn-grid.cellBackground"] = true
	}
	if card, _ := sections["bn-layout-card"].(map[string]any); card["titleFontSize"] != nil {
		found["ComponentStyle bn-layout-card.titleFontSize"] = true
	}
}
