package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/report/spec"
)

// smallMultipleSite is one SmallMultiple spec: a standalone document or an
// inline layout child.
type smallMultipleSite struct {
	doc  Document
	path string // JSON path of the spec in the document
	spec smallMultipleLintSpec
}

type smallMultipleLintSpec struct {
	Dataset           spec.DatasetList `json:"dataset"`
	UnitScaling       json.RawMessage  `json:"unitScaling"`
	PercentageScaling json.RawMessage  `json:"percentageScaling"`
	Scale             json.RawMessage  `json:"scale"`
	Base              struct {
		Kind string          `json:"kind"`
		Spec json.RawMessage `json:"spec"`
	} `json:"base"`
	Caption *string         `json:"caption"`
	Card    json.RawMessage `json:"card"`
	Panels  struct {
		By    string `json:"by"`
		Order string `json:"order"`
		Limit int    `json:"limit"`
		Items []struct {
			Label      string          `json:"label"`
			Dataset    string          `json:"dataset"`
			Card       json.RawMessage `json:"card"`
			ColumnSpan int             `json:"columnSpan"`
		} `json:"items"`
		Overrides []struct {
			Value      json.RawMessage `json:"value"`
			ColumnSpan int             `json:"columnSpan"`
		} `json:"overrides"`
	} `json:"panels"`
	Columns    int    `json:"columns"`
	AxisTitles string `json:"axisTitles"`
}

// kindSite is one spec of a kind: a standalone document or an inline layout
// child.
type kindSite struct {
	doc  Document
	path string // JSON path of the spec in the document
	raw  json.RawMessage
}

// smallMultipleSites finds the SmallMultiple specs: inline children at any
// depth and standalone documents. With merged, a ref child with spec
// overrides counts too, merged over its document and reported at the child,
// and a document is left out when every ref to it overrides it. Without, each
// field is checked where it is written.
func smallMultipleSites(docs []Document, merged bool) []smallMultipleSite {
	var sites []smallMultipleSite
	for _, site := range kindSites(docs, "SmallMultiple", merged) {
		var s smallMultipleLintSpec
		if json.Unmarshal(site.raw, &s) == nil {
			sites = append(sites, smallMultipleSite{doc: site.doc, path: site.path, spec: s})
		}
	}
	return sites
}

// kindSites finds the specs of one kind, as smallMultipleSites describes.
func kindSites(docs []Document, wanted string, merged bool) []kindSite {
	byName := map[string]Document{}
	for _, doc := range docs {
		if doc.Kind == wanted {
			byName[doc.Name] = doc
		}
	}
	var sites []kindSite
	overridden, plain := map[string]bool{}, map[string]bool{}
	add := func(doc Document, path string, raw json.RawMessage) {
		sites = append(sites, kindSite{doc: doc, path: path, raw: raw})
	}
	for _, doc := range docs {
		var root map[string]json.RawMessage
		if doc.Kind == wanted || json.Unmarshal(doc.Raw, &root) != nil {
			continue
		}
		var walk func(raw json.RawMessage, path string)
		walk = func(raw json.RawMessage, path string) {
			var obj map[string]json.RawMessage
			if json.Unmarshal(raw, &obj) == nil {
				kind, ref := rawString(obj["kind"]), rawString(obj["ref"])
				if kind == wanted && ref != "" && obj["spec"] == nil {
					plain[ref] = true
				}
				if kind == wanted && obj["spec"] != nil {
					if ref == "" {
						add(doc, joinLintPath(path, "spec"), obj["spec"])
					} else if target, ok := byName[ref]; ok && merged {
						overridden[ref] = true
						add(doc, joinLintPath(path, "spec"), mergedSpec(target, obj["spec"]))
					}
					return
				}
				for k, v := range obj {
					walk(v, joinLintPath(path, k))
				}
				return
			}
			var list []json.RawMessage
			if json.Unmarshal(raw, &list) == nil {
				for i, v := range list {
					walk(v, fmt.Sprintf("%s[%d]", path, i))
				}
			}
		}
		walk(root["spec"], "spec")
	}
	for _, doc := range docs {
		if doc.Kind == wanted && (!merged || !overridden[doc.Name] || plain[doc.Name]) {
			add(doc, "spec", specOf(doc))
		}
	}
	return sites
}

// rawString is a JSON string value, "" when it is none.
func rawString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// smallMultipleScale requires one shared scale for all panels.
var smallMultipleScale = Rule{
	ID:          "small-multiple-scale",
	Name:        "Small Multiple Scale",
	Description: "A SmallMultiple needs a fixed scale and, for ChartStructure, ChartTime and Table, a fixed unitScaling, so all panels share one scale.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, site := range smallMultipleSites(docs, true) {
			s := site.spec
			problems := render.SmallMultipleScaleProblems(s.Base.Kind, s.UnitScaling, s.PercentageScaling, s.Scale)
			if len(problems) == 0 {
				continue
			}
			findings = append(findings, Finding{
				RuleID:   "small-multiple-scale",
				Message:  "all panels of a SmallMultiple must share one scale: " + strings.Join(problems, "; "),
				File:     site.doc.File,
				DocIdx:   site.doc.Position,
				Path:     site.path,
				Severity: "error",
			})
		}
		return findings
	},
}

// smallMultipleBaseDataset warns about a base dataset that is never used.
var smallMultipleBaseDataset = Rule{
	ID:          "small-multiple-base-dataset",
	Name:        "Small Multiple Base Dataset",
	Description: "The dataset of a SmallMultiple replaces base.spec.dataset in every panel.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, site := range smallMultipleSites(docs, false) {
			var base struct {
				Dataset json.RawMessage `json:"dataset"`
			}
			if json.Unmarshal(site.spec.Base.Spec, &base) != nil || len(base.Dataset) == 0 {
				continue
			}
			findings = append(findings, Finding{
				RuleID:  "small-multiple-base-dataset",
				Message: "base.spec.dataset is replaced by the dataset of the SmallMultiple; remove it",
				File:    site.doc.File,
				DocIdx:  site.doc.Position,
				Path:    joinLintPath(site.path, "base.spec.dataset"),
			})
		}
		return findings
	},
}

// smallMultipleByDatasource rejects panels.by on a $datasource: its rows are
// not available when the panels are built.
var smallMultipleByDatasource = Rule{
	ID:          "small-multiple-by-datasource",
	Name:        "Small Multiple By Datasource",
	Description: "panels.by needs a DataSet as the first dataset of a SmallMultiple, not a $datasource.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, site := range smallMultipleSites(docs, false) {
			entries := site.spec.Dataset.Entries()
			if site.spec.Panels.By == "" || len(entries) == 0 || !strings.HasPrefix(entries[0].Ref, "$") {
				continue
			}
			findings = append(findings, Finding{
				RuleID:   "small-multiple-by-datasource",
				Message:  fmt.Sprintf("panels.by needs a DataSet as the first dataset, not %q; wrap it in a DataSet or list the panels in panels.items", entries[0].Ref),
				File:     site.doc.File,
				DocIdx:   site.doc.Position,
				Path:     joinLintPath(site.path, "dataset"),
				Severity: "error",
			})
		}
		return findings
	},
}

// smallMultipleCard checks the card of a SmallMultiple: an item card needs the
// card of the SmallMultiple, and a caption is drawn above the card title.
var smallMultipleCard = Rule{
	ID:          "small-multiple-card",
	Name:        "Small Multiple Card",
	Description: "panels.items[].card needs card on the SmallMultiple, and a caption next to a card names each panel twice.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, site := range smallMultipleSites(docs, true) {
			s := site.spec
			hasCard := len(s.Card) > 0 && string(s.Card) != "null"
			if hasCard && s.Caption != nil && *s.Caption != "" {
				findings = append(findings, Finding{
					RuleID:  "small-multiple-card",
					Message: "caption is drawn above the card title of every panel; remove caption, the card title names the panel",
					File:    site.doc.File,
					DocIdx:  site.doc.Position,
					Path:    joinLintPath(site.path, "caption"),
				})
			}
			if hasCard {
				continue
			}
			for i, item := range s.Panels.Items {
				if len(item.Card) == 0 || string(item.Card) == "null" {
					continue
				}
				findings = append(findings, Finding{
					RuleID:   "small-multiple-card",
					Message:  "panels.items[].card only changes the card of the SmallMultiple; set card on the SmallMultiple",
					File:     site.doc.File,
					DocIdx:   site.doc.Position,
					Path:     joinLintPath(site.path, fmt.Sprintf("panels.items[%d].card", i)),
					Severity: "error",
				})
			}
		}
		return findings
	},
}

// smallMultipleSpan checks the spans of the panels against the grid.
var smallMultipleSpan = Rule{
	ID:          "small-multiple-span",
	Name:        "Small Multiple Span",
	Description: "A columnSpan of a SmallMultiple panel must not be larger than columns.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, site := range smallMultipleSites(docs, true) {
			columns := site.spec.Columns
			if columns <= 0 {
				continue // the default is never narrower than the widest panel
			}
			report := func(path string, span int) {
				if span <= columns {
					return
				}
				findings = append(findings, Finding{
					RuleID:   "small-multiple-span",
					Message:  fmt.Sprintf("columnSpan %d is larger than columns (%d)", span, columns),
					File:     site.doc.File,
					DocIdx:   site.doc.Position,
					Path:     joinLintPath(site.path, path),
					Severity: "error",
				})
			}
			for i, item := range site.spec.Panels.Items {
				report(fmt.Sprintf("panels.items[%d].columnSpan", i), item.ColumnSpan)
			}
			for i, override := range site.spec.Panels.Overrides {
				report(fmt.Sprintf("panels.overrides[%d].columnSpan", i), override.ColumnSpan)
			}
		}
		return findings
	},
}

// smallMultipleAxisTitles rejects axisTitles rowEnd on a base without
// axisTitlePosition.
var smallMultipleAxisTitles = Rule{
	ID:          "small-multiple-axis-titles",
	Name:        "Small Multiple Axis Titles",
	Description: "axisTitles: rowEnd needs a ChartTime base.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, site := range smallMultipleSites(docs, true) {
			if site.spec.AxisTitles != "rowEnd" || site.spec.Base.Kind == "ChartTime" || site.spec.Base.Kind == "" {
				continue
			}
			findings = append(findings, Finding{
				RuleID:   "small-multiple-axis-titles",
				Message:  fmt.Sprintf("axisTitles: rowEnd needs a ChartTime base, not %s: only a ChartTime can move its axis titles", site.spec.Base.Kind),
				File:     site.doc.File,
				DocIdx:   site.doc.Position,
				Path:     joinLintPath(site.path, "axisTitles"),
				Severity: "error",
			})
		}
		return findings
	},
}

// gridCellSpan checks the spans of the children of a Grid.
var gridCellSpan = Rule{
	ID:          "grid-cell-span",
	Name:        "Grid Cell Span",
	Description: "A rowSpan or columnSpan of a Grid child must end inside the grid, and no child may sit in a position that a span covers.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, site := range kindSites(docs, "Grid", true) {
			for _, problem := range render.GridSpanProblems(site.raw) {
				path := fmt.Sprintf("children[%d]", problem.Child)
				if problem.Field != "" {
					path += "." + problem.Field
				}
				findings = append(findings, Finding{
					RuleID:   "grid-cell-span",
					Message:  problem.Message,
					File:     site.doc.File,
					DocIdx:   site.doc.Position,
					Path:     joinLintPath(site.path, path),
					Severity: "error",
				})
			}
		}
		return findings
	},
}

// SmallMultipleOverrideFindings reports the panels.overrides entries whose
// value no panel has. The panels come from the data: rows returns the rows of
// a DataSet, nil when they are not known.
func SmallMultipleOverrideFindings(docs []Document, rows func(binding string) json.RawMessage) []Finding {
	var findings []Finding
	for _, site := range smallMultipleSites(docs, true) {
		panels := site.spec.Panels
		entries := site.spec.Dataset.Entries()
		if panels.By == "" || len(panels.Overrides) == 0 || len(entries) == 0 || !entries[0].IsRef() {
			continue
		}
		labels, known := render.SmallMultiplePanelLabels(rows(entries[0].Ref), panels.By, panels.Order, panels.Limit)
		if !known {
			continue
		}
		for i, override := range panels.Overrides {
			value := render.PanelValueText(override.Value)
			if slices.Contains(labels, value) {
				continue
			}
			findings = append(findings, Finding{
				RuleID:   "small-multiple-override-unmatched",
				Message:  fmt.Sprintf("no panel has the value %q: the override does nothing", value),
				File:     site.doc.File,
				DocIdx:   site.doc.Position,
				Path:     joinLintPath(site.path, fmt.Sprintf("panels.overrides[%d].value", i)),
				Severity: "warning",
			})
		}
	}
	return findings
}
