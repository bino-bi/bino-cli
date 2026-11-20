package lint

import (
	"context"
	"encoding/json"
	"fmt"
)

// chartScatterIsoAxisBelowZero warns when a ChartScatter has iso-lines and an
// axis whose typed min is below 0.
//
// The engine draws the hyperbolic lines in the first quadrant only. On an axis
// below 0 it leaves all of them out and says so only in its layout state, so
// the build is green and the chart has no lines.
var chartScatterIsoAxisBelowZero = Rule{
	ID:   "chart-scatter-iso-axis-below-zero",
	Name: "ChartScatter Iso-Lines On An Axis Below 0",
	Description: "Iso-lines of a ChartScatter are not drawn when 'x.min' or 'y.min' is below 0: " +
		"the lines exist only where both values are positive.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, doc := range docs {
			var root any
			if err := json.Unmarshal(doc.Raw, &root); err != nil {
				continue // Schema validation reports malformed documents.
			}
			walkNodes(root, "", func(node map[string]any, path string) {
				if kind, _ := node["kind"].(string); kind != "ChartScatter" {
					return
				}
				componentSpec, _ := node["spec"].(map[string]any)
				if componentSpec["iso"] == nil {
					return
				}
				for _, axis := range []string{"x", "y"} {
					mapping, _ := componentSpec[axis].(map[string]any)
					lower, ok := mapping["min"].(float64)
					if !ok || lower >= 0 {
						continue
					}
					findings = append(findings, Finding{
						RuleID:  "chart-scatter-iso-axis-below-zero",
						Message: fmt.Sprintf("the iso-lines are not drawn: %s.min is below 0", axis),
						File:    doc.File,
						DocIdx:  doc.Position,
						Path:    joinLintPath(path, "spec."+axis+".min"),
					})
				}
			})
		}
		return findings
	},
}
