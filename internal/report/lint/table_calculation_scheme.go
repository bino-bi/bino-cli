package lint

import (
	"context"
	"encoding/json"

	"bino.bi/bino/internal/report/spec"
)

// tableThereofPositionUnused warns when a Table asks for its thereof rows
// above the parent but lists no thereof rows.
var tableThereofPositionUnused = Rule{
	ID:   "table-thereof-position-unused",
	Name: "Table Thereof Position Unused",
	Description: "Table 'spec.thereofPosition: above' prints the thereof rows before their parent, " +
		"so it does nothing on a table without a 'thereof' list.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		forEachCalculationSchemeTable(docs, "thereofPosition", func(doc Document, path string, field func(string) any) {
			var thereof spec.ThereofList
			if decodeField(field("thereof"), &thereof) && len(thereof) > 0 {
				return
			}
			findings = append(findings, Finding{
				RuleID: "table-thereof-position-unused",
				// Only the data knows a dataset default, so the message leaves it open.
				Message: "'spec.thereofPosition' is 'above' but the Table sets no 'thereof' list, so no row moves " +
					"unless a dataset default sets one; add 'thereof' or remove 'thereofPosition'",
				File:   doc.File,
				DocIdx: doc.Position,
				Path:   path,
			})
		})
		return findings
	},
}

// tableVarianceWaterfallUnused warns when a Table asks for a variance
// waterfall but draws no absolute variance column as bars.
var tableVarianceWaterfallUnused = Rule{
	ID:   "table-variance-waterfall-unused",
	Name: "Table Variance Waterfall Unused",
	Description: "Table 'spec.varianceWaterfall' draws the absolute variance columns of 'barColumns' as a waterfall, " +
		"so it does nothing when 'barColumns' lists no absolute variance.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		forEachCalculationSchemeTable(docs, "varianceWaterfall", func(doc Document, path string, field func(string) any) {
			var barColumns spec.StringOrSlice
			if decodeField(field("barColumns"), &barColumns) && spec.HasAbsoluteVarianceColumn(barColumns) {
				return
			}
			findings = append(findings, Finding{
				RuleID: "table-variance-waterfall-unused",
				Message: "'spec.varianceWaterfall' is set but 'barColumns' lists no absolute variance column, so no waterfall is drawn " +
					"unless a dataset default sets one; add a column such as 'dac1_pp1_pos' to 'barColumns' or remove 'varianceWaterfall'",
				File:   doc.File,
				DocIdx: doc.Position,
				Path:   path,
			})
		})
		return findings
	},
}

// forEachCalculationSchemeTable visits every Table, at any depth, that turns
// on the given calculation scheme field. field reads a spec field from the
// first place that sets it, as at render time: the own spec, then the
// referenced Table of a child with a 'ref'. path points at the field, or at
// the Table that inherits it.
func forEachCalculationSchemeTable(docs []Document, prop string, visit func(doc Document, path string, field func(string) any)) {
	bases := tableSpecsByName(docs)
	for _, doc := range docs {
		var root any
		if err := json.Unmarshal(doc.Raw, &root); err != nil {
			continue // Schema validation reports malformed documents.
		}
		walkTables(root, "", func(table map[string]any, path string) {
			own, _ := table["spec"].(map[string]any)
			base := bases[stringField(table, "ref")]
			field := func(key string) any {
				if v, ok := own[key]; ok {
					return v
				}
				return base[key]
			}
			if len(spec.TableCalculationSchemeProps(map[string]any{prop: field(prop)})) == 0 {
				return
			}
			if _, ok := own[prop]; ok {
				path = joinLintPath(path, "spec."+prop)
			}
			visit(doc, path, field)
		})
	}
}
