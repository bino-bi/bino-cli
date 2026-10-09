package lint

import (
	"context"
	"encoding/json"
	"fmt"
)

// chartTimeNoEffectFields are the ChartTime fields the template engine does not
// read. The schema still accepts them, so that existing reports keep validating.
var chartTimeNoEffectFields = []string{"level", "limit", "order", "orderDirection"}

// chartTimeFieldNoEffect warns when a ChartTime sets a field that does nothing.
//
// A time chart shows one value per period, in date order. The engine checked
// these four fields but never used them, and the renderer no longer writes them.
var chartTimeFieldNoEffect = Rule{
	ID:   "chart-time-field-no-effect",
	Name: "ChartTime Field Without Effect",
	Description: "'level', 'order', 'orderDirection' and 'limit' have no effect on a ChartTime: " +
		"a time chart always shows one value per period, in date order.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, doc := range docs {
			var root any
			if err := json.Unmarshal(doc.Raw, &root); err != nil {
				continue // Schema validation reports malformed documents.
			}
			walkNodes(root, "", func(node map[string]any, path string) {
				if kind, _ := node["kind"].(string); kind != "ChartTime" {
					return
				}
				componentSpec, _ := node["spec"].(map[string]any)
				for _, field := range chartTimeNoEffectFields {
					if componentSpec[field] == nil {
						continue
					}
					findings = append(findings, Finding{
						RuleID:  "chart-time-field-no-effect",
						Message: fmt.Sprintf("%s has no effect on a ChartTime and can be removed", field),
						File:    doc.File,
						DocIdx:  doc.Position,
						Path:    joinLintPath(path, "spec."+field),
					})
				}
			})
		}
		return findings
	},
}
