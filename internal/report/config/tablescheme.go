package config

import (
	"encoding/json"
	"strings"

	"bino.bi/bino/internal/report/spec"
)

// TableCalculationSchemeProps returns the calculation scheme fields that any
// Table, at any depth, turns on: runningSum, thereofPosition above and
// varianceWaterfall. The defaults are the output of every engine. Each field
// comes from the first place that sets it, as at render time: the own spec,
// the referenced Table of a child with a 'ref', then the defaults of the
// primary dataset. defaults returns the Table spec defaults that a dataset
// carries; it may be nil when the data is not known.
func TableCalculationSchemeProps(docs []Document, defaults func(binding string) map[string]any) []string {
	bases := map[string]map[string]any{}
	roots := make([]any, 0, len(docs))
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		roots = append(roots, v)
		if node, ok := v.(map[string]any); ok && doc.Kind == "Table" && doc.Name != "" {
			bases[doc.Name], _ = node["spec"].(map[string]any)
		}
	}
	var props []string
	for _, root := range roots {
		props = tableCalculationSchemeProps(root, bases, defaults, props)
	}
	return props
}

func tableCalculationSchemeProps(v any, bases map[string]map[string]any, defaults func(string) map[string]any, props []string) []string {
	switch node := v.(type) {
	case map[string]any:
		if kind, _ := node["kind"].(string); kind == "Table" {
			props = spec.MergeTableCalculationSchemeProps(props, spec.TableCalculationSchemeProps(effectiveTableSpec(node, bases, defaults)))
		}
		for _, child := range node {
			props = tableCalculationSchemeProps(child, bases, defaults, props)
		}
	case []any:
		for _, child := range node {
			props = tableCalculationSchemeProps(child, bases, defaults, props)
		}
	}
	return props
}

// effectiveTableSpec returns the spec of a Table node as the render resolves
// it: the dataset defaults under the referenced Table under the own spec. A
// null counts as not set.
func effectiveTableSpec(table map[string]any, bases map[string]map[string]any, defaults func(string) map[string]any) map[string]any {
	merged := map[string]any{}
	ref, _ := table["ref"].(string)
	own, _ := table["spec"].(map[string]any)
	for _, layer := range []map[string]any{bases[ref], own} {
		for k, v := range layer {
			merged[k] = v
		}
	}
	var fallback map[string]any
	if defaults != nil {
		var datasets spec.DatasetList
		if raw, err := json.Marshal(merged["dataset"]); err == nil && json.Unmarshal(raw, &datasets) == nil {
			// A `$` DataSource binding gets no defaults.
			if entries := datasets.Entries(); len(entries) > 0 && entries[0].IsRef() && !strings.HasPrefix(entries[0].Ref, "$") {
				fallback = defaults(entries[0].Ref)
			}
		}
	}
	for k, v := range fallback {
		if merged[k] == nil {
			merged[k] = v
		}
	}
	return merged
}
