package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// tableTypesWithTotalRow lists the Table spec.type values that render a
// grand-total row at the bottom of the table. Only for those does spec.sumTitle
// — the label of that row — appear anywhere in the output.
var tableTypesWithTotalRow = map[string]bool{
	"sum": true,
	"opt": true,
}

// tableSumTitleUnused warns when spec.sumTitle is set on a Table whose type
// renders no grand-total row, so the label is silently dropped at render time.
var tableSumTitleUnused = Rule{
	ID:   "table-sum-title-unused",
	Name: "Table Sum Title Unused",
	Description: "Table 'spec.sumTitle' labels the grand-total row, which is only rendered for " +
		"'type: sum' and 'type: opt'; other table types ignore the value.",
	Check: func(_ context.Context, docs []Document) []Finding {
		bases := tableSpecsByName(docs)

		var findings []Finding
		for _, doc := range docs {
			var root any
			if err := json.Unmarshal(doc.Raw, &root); err != nil {
				continue // Schema validation reports malformed documents.
			}

			walkTables(root, "", func(table map[string]any, path string) {
				spec, _ := table["spec"].(map[string]any)
				if strings.TrimSpace(stringField(spec, "sumTitle")) == "" {
					return
				}

				tableType := effectiveTableType(table, spec, bases)
				if tableTypesWithTotalRow[tableType] {
					return
				}

				findings = append(findings, Finding{
					RuleID:  "table-sum-title-unused",
					Message: sumTitleUnusedMessage(tableType),
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    joinLintPath(path, "spec.sumTitle"),
				})
			})
		}
		return findings
	},
}

// sumTitleUnusedMessage explains which table type swallowed the label.
func sumTitleUnusedMessage(tableType string) string {
	if tableType == "" {
		tableType = "list"
	}
	return fmt.Sprintf(
		"'spec.sumTitle' is set but 'type: %s' renders no grand-total row, so the label is never shown; "+
			"use 'type: sum' or 'type: opt', or remove 'sumTitle'",
		tableType,
	)
}

// effectiveTableType resolves the table type that will actually be rendered.
// A child that carries a 'ref' inherits the referenced Table's spec, with its
// own inline spec merged on top (see render.resolveChildSpec), so an override
// that sets only sumTitle still renders the referenced document's type.
func effectiveTableType(table, spec map[string]any, bases map[string]map[string]any) string {
	if t := stringField(spec, "type"); t != "" {
		return t
	}
	if ref := stringField(table, "ref"); ref != "" {
		return stringField(bases[ref], "type")
	}
	return ""
}

// tableSpecsByName indexes the spec of every standalone Table document, so that
// children referencing one by name can resolve its type.
func tableSpecsByName(docs []Document) map[string]map[string]any {
	bases := make(map[string]map[string]any)
	for _, doc := range docs {
		if doc.Kind != "Table" || doc.Name == "" {
			continue
		}
		var payload struct {
			Spec map[string]any `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			continue
		}
		bases[doc.Name] = payload.Spec
	}
	return bases
}

// walkTables visits every Table in a document: the root of a Table manifest plus
// every Table nested in a LayoutPage/LayoutCard/Grid 'children' list or a Tree
// 'nodes' list, at any depth. Inline children are not materialized as separate
// documents (only inline DataSets and DataSources are), so the rule has to
// descend into them itself. See walkNodes for the path convention.
func walkTables(node any, path string, visit func(table map[string]any, path string)) {
	walkNodes(node, path, func(obj map[string]any, objPath string) {
		if kind, _ := obj["kind"].(string); kind == "Table" {
			visit(obj, objPath)
		}
	})
}

// stringField reads a string field from a decoded JSON object, tolerating a nil
// map and a non-string value.
func stringField(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	s, _ := obj[key].(string)
	return s
}

func joinLintPath(base, segment string) string {
	if base == "" {
		return segment
	}
	return base + "." + segment
}
