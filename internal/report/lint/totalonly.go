package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// totalOnlyScenarios reports a scenario that a chart shall draw at its total
// only, but that is also in the scenarios of the chart. The chart draws it in
// its periods or rows then, and ignores the entry.
var totalOnlyScenarios = Rule{
	ID:          "total-only-scenario",
	Name:        "Total Only Scenario",
	Description: "A scenario of totals.scenarios (ChartTime) or sumScenarios (ChartStructure) must not be in scenarios.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, doc := range docs {
			var root any
			if json.Unmarshal(doc.Raw, &root) != nil {
				continue
			}
			walkTotalOnly(root, "", func(path, field, scenario string) {
				findings = append(findings, Finding{
					RuleID:  "total-only-scenario",
					Message: fmt.Sprintf("%s lists %q, which is also in scenarios; a scenario of the chart is not drawn at the total only, remove it from one of the two", field, scenario),
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    path,
				})
			})
		}
		return findings
	},
}

// walkTotalOnly calls report for every scenario of totals.scenarios of a
// ChartTime and of sumScenarios of a ChartStructure that is also in the
// scenarios of that chart, at any depth of a document.
func walkTotalOnly(node any, path string, report func(path, field, scenario string)) {
	switch value := node.(type) {
	case map[string]any:
		if spec, ok := value["spec"].(map[string]any); ok {
			specPath := joinLintPath(path, "spec")
			drawn := map[string]bool{}
			if list, ok := spec["scenarios"].([]any); ok {
				for _, entry := range list {
					if name, ok := entry.(string); ok {
						drawn[name] = true
					}
				}
			}
			check := func(list any, field string) {
				entries, _ := list.([]any)
				for index, entry := range entries {
					if name, ok := entry.(string); ok && drawn[name] {
						report(joinLintPath(specPath, field)+"["+strconv.Itoa(index)+"]", field, name)
					}
				}
			}
			switch value["kind"] {
			case "ChartTime":
				if totals, ok := spec["totals"].(map[string]any); ok {
					check(totals["scenarios"], "totals.scenarios")
				}
			case "ChartStructure":
				check(spec["sumScenarios"], "sumScenarios")
			}
		}
		for key, child := range value {
			walkTotalOnly(child, joinLintPath(path, key), report)
		}
	case []any:
		for index, child := range value {
			walkTotalOnly(child, path+"["+strconv.Itoa(index)+"]", report)
		}
	}
}
