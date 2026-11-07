package config

import "encoding/json"

// gridCellLayoutDefaults are the values of the bn-grid cell layout fields that
// produce the grid of engines without them.
var gridCellLayoutDefaults = map[string]string{
	"cellHeight":          "auto",
	"cellWidth":           "auto",
	"cellVerticalAlign":   "top",
	"cellHorizontalAlign": "left",
}

// UsesGridCellLayout reports whether any document asks bn-grid for cell size,
// alignment or captions: a Grid, standalone or nested inline at any depth, with
// a non-default cell layout field or a child caption, or any SmallMultiple,
// which always renders as such a grid.
func UsesGridCellLayout(docs []Document) bool {
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		if usesGridCellLayout(v) {
			return true
		}
	}
	return false
}

// UsesSmallMultipleCard reports whether any SmallMultiple, standalone or
// nested inline at any depth, wraps its panels in cards.
func UsesSmallMultipleCard(docs []Document) bool {
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		if usesSmallMultipleCard(v) {
			return true
		}
	}
	return false
}

func usesSmallMultipleCard(v any) bool {
	switch node := v.(type) {
	case map[string]any:
		if node["kind"] == "SmallMultiple" {
			if spec, ok := node["spec"].(map[string]any); ok && spec["card"] != nil {
				return true
			}
		}
		for _, child := range node {
			if usesSmallMultipleCard(child) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if usesSmallMultipleCard(child) {
				return true
			}
		}
	}
	return false
}

func usesGridCellLayout(v any) bool {
	switch node := v.(type) {
	case map[string]any:
		if node["kind"] == "SmallMultiple" {
			return true
		}
		if node["kind"] == "Grid" {
			if spec, ok := node["spec"].(map[string]any); ok && gridSpecUsesCellLayout(spec) {
				return true
			}
		}
		for _, child := range node {
			if usesGridCellLayout(child) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if usesGridCellLayout(child) {
				return true
			}
		}
	}
	return false
}

func gridSpecUsesCellLayout(spec map[string]any) bool {
	for field, def := range gridCellLayoutDefaults {
		if value, ok := spec[field].(string); ok && value != "" && value != def {
			return true
		}
	}
	children, _ := spec["children"].([]any)
	for _, child := range children {
		if c, ok := child.(map[string]any); ok {
			if caption, ok := c["caption"].(string); ok && caption != "" {
				return true
			}
		}
	}
	return false
}
