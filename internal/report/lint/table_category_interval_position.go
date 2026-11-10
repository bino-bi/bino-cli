package lint

import (
	"context"
	"encoding/json"
	"strings"

	"bino.bi/bino/internal/report/spec"
)

// tableCategoryIntervalPositionUnused warns when spec.categoryIntervalPosition
// is set on a Table that has no interval groups to put left of the category
// column, so the value is ignored at render time.
var tableCategoryIntervalPositionUnused = Rule{
	ID:   "table-category-interval-position-unused",
	Name: "Table Category Interval Position Unused",
	Description: "Table 'spec.categoryIntervalPosition' moves the category column between the interval groups, " +
		"which exist only with 'interval' set and without 'columnthereof'; other tables ignore the value.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		forEachCategoryIntervalPosition(docs, nil, func(doc Document, t positionedTable) {
			if spec.TableIntervalActive(t.interval, t.columnthereof) {
				return
			}
			// Only the data knows a dataset default, so the message leaves it open.
			reason := "the Table sets no 'interval', so the category column stays first unless a dataset default sets one"
			fix := "set 'interval'"
			if t.columnthereof {
				reason = "'columnthereof' turns the interval off, so the category column stays first"
				fix = "remove 'columnthereof'"
			}
			findings = append(findings, Finding{
				RuleID: "table-category-interval-position-unused",
				Message: "'spec.categoryIntervalPosition' is set but " + reason +
					"; " + fix + " or remove 'categoryIntervalPosition'",
				File:   doc.File,
				DocIdx: doc.Position,
				Path:   t.path,
			})
		})
		return findings
	},
}

// CategoryIntervalPositionFindings checks the categoryIntervalPosition of the
// Tables with interval setname against the data. rows returns the rows of a
// dataset binding, nil when they are not known. defaults returns the Table
// spec defaults that a dataset carries. Date intervals are left to the engine:
// only it knows their buckets.
func CategoryIntervalPositionFindings(docs []Document, rows func(binding string) json.RawMessage, defaults func(binding string) map[string]any) []Finding {
	var findings []Finding
	forEachCategoryIntervalPosition(docs, defaults, func(doc Document, t positionedTable) {
		if t.columnthereof || !strings.EqualFold(strings.TrimSpace(t.interval), spec.TableIntervalSetname) {
			return
		}
		names := t.datasets.Strings()
		if len(names) == 0 || len(names) != len(t.datasets.Entries()) {
			return
		}
		data := make([]json.RawMessage, 0, len(names))
		for _, name := range names {
			r := rows(name)
			if len(r) == 0 {
				return
			}
			data = append(data, r)
		}
		groups, ok := spec.SetnameGroupCount(data...)
		if !ok || groups == 0 || t.position <= groups {
			return
		}
		findings = append(findings, Finding{
			RuleID:   "table-category-interval-position-too-large",
			Message:  spec.CategoryIntervalPositionTooLarge(t.position, groups, names),
			File:     doc.File,
			DocIdx:   doc.Position,
			Path:     t.path,
			Severity: "warning",
		})
	})
	return findings
}

// positionedTable is a Table with a categoryIntervalPosition above 0, and the
// fields that decide what the value does.
type positionedTable struct {
	path          string // path of spec.categoryIntervalPosition, or of the Table that inherits it
	position      int
	interval      string
	columnthereof bool
	datasets      spec.DatasetList
}

// forEachCategoryIntervalPosition visits every Table, at any depth, with a
// categoryIntervalPosition above 0. Each field comes from the first place
// that sets it, as at render time: the own spec, the referenced Table of a
// child with a 'ref', then the defaults of the primary dataset. defaults may
// be nil when the data is not known.
func forEachCategoryIntervalPosition(docs []Document, defaults func(binding string) map[string]any, visit func(Document, positionedTable)) {
	bases := tableSpecsByName(docs)
	for _, doc := range docs {
		var root any
		if err := json.Unmarshal(doc.Raw, &root); err != nil {
			continue // Schema validation reports malformed documents.
		}
		walkTables(root, "", func(table map[string]any, path string) {
			own, _ := table["spec"].(map[string]any)
			base := bases[stringField(table, "ref")]
			var fallback map[string]any
			field := func(key string) any {
				if v, ok := own[key]; ok {
					return v
				}
				if v, ok := base[key]; ok {
					return v
				}
				return fallback[key]
			}
			var t positionedTable
			decodeField(field("dataset"), &t.datasets)
			if entries := t.datasets.Entries(); defaults != nil && len(entries) > 0 && entries[0].IsRef() {
				fallback = defaults(strings.TrimSpace(entries[0].Ref))
			}
			position, _ := field("categoryIntervalPosition").(float64)
			if position <= 0 {
				return
			}
			t.position = int(position)
			t.path = path
			if _, ok := own["categoryIntervalPosition"]; ok {
				t.path = joinLintPath(path, "spec.categoryIntervalPosition")
			}
			if interval, ok := field("interval").(string); ok {
				t.interval = interval
			}
			var columnthereof spec.ColumnthereofList
			if decodeField(field("columnthereof"), &columnthereof) {
				t.columnthereof = len(columnthereof) > 0
			}
			visit(doc, t)
		})
	}
}

// decodeField decodes one decoded JSON value into its typed form.
func decodeField(value, into any) bool {
	if value == nil {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, into) == nil
}
