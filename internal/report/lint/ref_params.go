package lint

import (
	"context"
	"encoding/json"

	"bino.bi/bino/internal/report/config"
)

// refParams validates parameter declarations on param-capable documents and
// parameter values passed to refs (layout children, grid children, tree nodes).
var refParams = Rule{
	ID:          "ref-params",
	Name:        "Ref Params",
	Description: "Validates metadata.params declarations and parameter values passed to referenced documents.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		// Definition-side: validate metadata.params on param-capable documents
		// and index their declarations by kind:name for the ref-site checks.
		declared := make(map[string][]config.LayoutPageParamSpec)
		for _, doc := range docs {
			if _, ok := config.ParamCapableKinds[doc.Kind]; !ok {
				continue
			}
			warnings, err := config.ValidateLayoutPageParams(config.Document{
				Kind:   doc.Kind,
				Name:   doc.Name,
				Params: doc.Params,
			})
			for _, w := range warnings {
				findings = append(findings, Finding{
					RuleID:  "ref-params",
					Message: w,
					File:    doc.File,
					DocIdx:  doc.Position,
				})
			}
			if err != nil {
				findings = append(findings, Finding{
					RuleID:  "ref-params",
					Message: err.Error(),
					File:    doc.File,
					DocIdx:  doc.Position,
				})
			}
			declared[doc.Kind+":"+doc.Name] = doc.Params
		}

		// Ref-site: walk container documents for refs and validate the params
		// passed against the target's declarations. Missing targets are skipped
		// silently (the render/graph layers report those).
		for _, doc := range docs {
			switch doc.Kind {
			case "LayoutPage", "LayoutCard", "Grid", "Tree":
			default:
				continue
			}
			var node any
			if err := json.Unmarshal(doc.Raw, &node); err != nil {
				continue
			}
			walkRefSites(node, func(kind, ref string, params map[string]string) {
				target, ok := declared[kind+":"+ref]
				if !ok {
					return
				}
				warnings, err := config.ValidateRefParams(kind, ref, params, target)
				for _, w := range warnings {
					findings = append(findings, Finding{
						RuleID:  "ref-params",
						Message: w,
						File:    doc.File,
						DocIdx:  doc.Position,
					})
				}
				if err != nil {
					findings = append(findings, Finding{
						RuleID:  "ref-params",
						Message: err.Error(),
						File:    doc.File,
						DocIdx:  doc.Position,
					})
				}
			})
		}

		return findings
	},
}

// walkRefSites recursively visits every object carrying both a string "kind"
// and a string "ref" (layout children, grid children, tree nodes), passing
// along any "params" values (absent params visit with an empty map so
// required-param checks still apply).
func walkRefSites(node any, visit func(kind, ref string, params map[string]string)) {
	switch v := node.(type) {
	case map[string]any:
		kind, _ := v["kind"].(string)
		ref, _ := v["ref"].(string)
		if kind != "" && ref != "" {
			params := make(map[string]string)
			if rawParams, ok := v["params"].(map[string]any); ok {
				for name, value := range rawParams {
					if s, ok := value.(string); ok {
						params[name] = s
					}
				}
			}
			visit(kind, ref, params)
		}
		for _, child := range v {
			walkRefSites(child, visit)
		}
	case []any:
		for _, item := range v {
			walkRefSites(item, visit)
		}
	}
}
