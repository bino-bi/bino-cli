package config

import (
	"encoding/json"
	"regexp"
	"strings"
)

// operationSentimentToken is a variance token with the _op or _opinv suffix.
var operationSentimentToken = regexp.MustCompile(`^(?:dr|d)(?:ac|pp|fc|pl)[1-4]_(?:ac|pp|fc|pl)[1-4]_(?:opinv|op)$`)

// UsesOperationSentiment reports whether any document has a variance token
// with the _op or _opinv suffix: a string value, a list entry, or an item of a
// comma-separated or JSON list string, at any depth.
func UsesOperationSentiment(docs []Document) bool {
	for _, doc := range docs {
		var v any
		if err := json.Unmarshal(doc.Raw, &v); err != nil {
			continue
		}
		if usesOperationSentiment(v) {
			return true
		}
	}
	return false
}

func usesOperationSentiment(v any) bool {
	switch node := v.(type) {
	case string:
		for _, item := range strings.Split(node, ",") {
			if operationSentimentToken.MatchString(strings.Trim(item, " \t\n[]\"'")) {
				return true
			}
		}
	case map[string]any:
		for _, child := range node {
			if usesOperationSentiment(child) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if usesOperationSentiment(child) {
				return true
			}
		}
	}
	return false
}
