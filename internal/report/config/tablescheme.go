package config

import (
	"encoding/json"

	"bino.bi/bino/internal/report/spec"
)

// TableCalculationSchemeProps returns the calculation scheme fields that any
// Table, at any depth, turns on: runningSum, thereofPosition above and
// varianceWaterfall. The defaults are the output of every engine.
func TableCalculationSchemeProps(docs []Document) []string {
	var props []string
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		props = tableCalculationSchemeProps(v, props)
	}
	return props
}

func tableCalculationSchemeProps(v any, props []string) []string {
	switch node := v.(type) {
	case map[string]any:
		if kind, _ := node["kind"].(string); kind == "Table" {
			if tableSpec, ok := node["spec"].(map[string]any); ok {
				props = spec.MergeTableCalculationSchemeProps(props, spec.TableCalculationSchemeProps(tableSpec))
			}
		}
		for _, child := range node {
			props = tableCalculationSchemeProps(child, props)
		}
	case []any:
		for _, child := range node {
			props = tableCalculationSchemeProps(child, props)
		}
	}
	return props
}
