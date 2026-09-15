package lint

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/spec"
)

// constantsKindTokens and constantsAnyToken are the kind tokens a
// constants.spec key may use; dataset.SpecField knows each kind's fields.
var constantsKindTokens = dataset.KindTokens

const constantsAnyToken = dataset.AnyToken

// datasetConstantsSpec validates the YAML form of dataset defaults:
// constants.spec.<kind>.<field> must name a known kind token and a field of
// that kind's spec.
var datasetConstantsSpec = Rule{
	ID:          "dataset-constants-spec",
	Name:        "DataSet Constants Spec",
	Description: "constants.spec.<kind>.<field> must name a known kind token (text, table, chartstructure, charttime, chartscatter, chartbubble, chartbullet, any) and a field of that kind's spec.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding

		tokens := make([]string, 0, len(constantsKindTokens)+1)
		for t := range constantsKindTokens {
			tokens = append(tokens, t)
		}
		sort.Strings(tokens)
		tokens = append(tokens, constantsAnyToken)
		tokenList := strings.Join(tokens, ", ")

		for _, doc := range docs {
			if doc.Kind != "DataSet" {
				continue
			}
			specMap, err := spec.ToMap(doc.Raw)
			if err != nil {
				continue
			}
			constants, _ := specMap["constants"].(map[string]any)
			byKind, _ := constants["spec"].(map[string]any)
			if len(byKind) == 0 {
				continue
			}

			for _, token := range sortedKeys(byKind) {
				kind, known := constantsKindTokens[token]
				if !known && token != constantsAnyToken {
					findings = append(findings, Finding{
						RuleID:  "dataset-constants-spec",
						Message: fmt.Sprintf("constants.spec.%s: unknown kind token; expected one of %s", token, tokenList),
						File:    doc.File,
						DocIdx:  doc.Position,
						Path:    "spec.constants.spec." + token,
					})
					continue
				}
				fields, _ := byKind[token].(map[string]any)
				for _, field := range sortedKeys(fields) {
					if hasSpecField(kind, field) {
						continue
					}
					msg := fmt.Sprintf("constants.spec.%s.%s: %s has no spec field %q", token, field, kind, field)
					if token == constantsAnyToken {
						msg = fmt.Sprintf("constants.spec.any.%s: no kind has a spec field %q", field, field)
					}
					findings = append(findings, Finding{
						RuleID:  "dataset-constants-spec",
						Message: msg,
						File:    doc.File,
						DocIdx:  doc.Position,
						Path:    "spec.constants.spec." + token + "." + field,
					})
				}
			}
		}

		return findings
	},
}

// hasSpecField reports whether kind's spec declares field; an empty kind
// (the any token) matches when any targetable kind declares it.
func hasSpecField(kind, field string) bool {
	if kind != "" {
		_, ok := dataset.SpecField(kind, field)
		return ok
	}
	_, ok := dataset.AnyKindHasField(field)
	return ok
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
