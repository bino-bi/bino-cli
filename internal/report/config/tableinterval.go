package config

import "encoding/json"

// UsesCategoryIntervalPosition reports whether any Table, at any depth, sets
// categoryIntervalPosition above 0. The value 0 is the layout of every engine.
func UsesCategoryIntervalPosition(docs []Document) bool {
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		if usesCategoryIntervalPosition(v) {
			return true
		}
	}
	return false
}

func usesCategoryIntervalPosition(v any) bool {
	switch node := v.(type) {
	case map[string]any:
		if kind, _ := node["kind"].(string); kind == "Table" {
			if spec, ok := node["spec"].(map[string]any); ok {
				if n, ok := spec["categoryIntervalPosition"].(float64); ok && n > 0 {
					return true
				}
			}
		}
		for _, child := range node {
			if usesCategoryIntervalPosition(child) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if usesCategoryIntervalPosition(child) {
				return true
			}
		}
	}
	return false
}
