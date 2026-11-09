package config

import (
	"encoding/json"
	"regexp"
	"strings"
)

// orderVarianceToken is a variance order key, with or without the sentiment.
var orderVarianceToken = regexp.MustCompile(`^(?:dr|d)(?:ac|pp|fc|pl)[1-4]_(?:ac|pp|fc|pl)[1-4](?:_(?:pos|neg|neu|opinv|op))?$`)

// UsesTableOrderMeasure reports whether any document sorts a Table by a
// variance or by an @<setname> interval block: in the order of a Table at any
// depth, or in a titleOrder that a Table can inherit.
func UsesTableOrderMeasure(docs []Document) bool {
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		if usesTableOrderMeasure(v) {
			return true
		}
	}
	return false
}

func usesTableOrderMeasure(v any) bool {
	switch node := v.(type) {
	case map[string]any:
		if s, ok := node["titleOrder"].(string); ok && IsMeasureOrder(s) {
			return true
		}
		if kind, _ := node["kind"].(string); kind == "Table" {
			if spec, ok := node["spec"].(map[string]any); ok {
				if s, ok := spec["order"].(string); ok && IsMeasureOrder(s) {
					return true
				}
			}
		}
		for _, child := range node {
			if usesTableOrderMeasure(child) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if usesTableOrderMeasure(child) {
				return true
			}
		}
	}
	return false
}

// IsMeasureOrder reports whether an order list has a variance or an '@' key.
func IsMeasureOrder(order string) bool {
	for _, item := range strings.Split(order, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if strings.Contains(item, "@") || orderVarianceToken.MatchString(item) {
			return true
		}
	}
	return false
}
