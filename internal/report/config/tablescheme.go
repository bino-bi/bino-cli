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
// primary dataset. defaults holds the Table spec defaults by dataset name; it
// may be nil when the data is not known.
func TableCalculationSchemeProps(docs []Document, defaults map[string]map[string]any) []string {
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

func tableCalculationSchemeProps(v any, bases, defaults map[string]map[string]any, props []string) []string {
	switch node := v.(type) {
	case map[string]any:
		switch kind, _ := node["kind"].(string); kind {
		case "Table":
			props = spec.MergeTableCalculationSchemeProps(props, tableProps(node, bases, defaults, false))
		case "SmallMultiple":
			// The panels get their dataset and the item spec at render time.
			multiple, _ := node["spec"].(map[string]any)
			if base, _ := multiple["base"].(map[string]any); base["kind"] == "Table" {
				props = spec.MergeTableCalculationSchemeProps(props, tableProps(base, bases, defaults, true))
				panels, _ := multiple["panels"].(map[string]any)
				items, _ := panels["items"].([]any)
				for _, item := range items {
					fields, _ := item.(map[string]any)
					itemSpec, _ := fields["spec"].(map[string]any)
					props = spec.MergeTableCalculationSchemeProps(props, spec.TableCalculationSchemeProps(itemSpec))
				}
			}
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

// tableProps returns the calculation scheme fields that a Table node turns on,
// resolved as the render does: the own spec over the referenced Table over the
// defaults of the primary dataset. A null counts as not set. With anyDataset,
// or with a param placeholder in the dataset name, the primary dataset is only
// known at render time: then the defaults of every dataset count.
func tableProps(table map[string]any, bases, defaults map[string]map[string]any, anyDataset bool) []string {
	merged := map[string]any{}
	ref, _ := table["ref"].(string)
	own, _ := table["spec"].(map[string]any)
	for _, layer := range []map[string]any{bases[ref], own} {
		for k, v := range layer {
			merged[k] = v
		}
	}
	var fallbacks []map[string]any
	var datasets spec.DatasetList
	if raw, err := json.Marshal(merged["dataset"]); err == nil && json.Unmarshal(raw, &datasets) == nil {
		if entries := datasets.Entries(); len(entries) > 0 && entries[0].IsRef() {
			switch name := entries[0].Ref; {
			case strings.Contains(name, "${"):
				anyDataset = true
			case !strings.HasPrefix(name, "$"): // a `$` DataSource binding gets no defaults
				fallbacks = append(fallbacks, defaults[name])
			}
		}
	}
	if anyDataset {
		for _, fallback := range defaults {
			fallbacks = append(fallbacks, fallback)
		}
	}
	props := spec.TableCalculationSchemeProps(merged)
	for _, fallback := range fallbacks {
		unset := map[string]any{}
		for k, v := range fallback {
			if merged[k] == nil {
				unset[k] = v
			}
		}
		props = spec.MergeTableCalculationSchemeProps(props, spec.TableCalculationSchemeProps(unset))
	}
	return props
}
