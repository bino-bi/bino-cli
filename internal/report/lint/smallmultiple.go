package lint

import (
	"context"
	"encoding/json"
	"fmt"
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
	Panels struct {
		By string `json:"by"`
	} `json:"panels"`
}

// smallMultipleSites finds the SmallMultiple specs: inline children at any
// depth and standalone documents. With merged, a ref child with spec
// overrides counts too, merged over its document and reported at the child,
// and a document is left out when every ref to it overrides it. Without, each
// field is checked where it is written.
func smallMultipleSites(docs []Document, merged bool) []smallMultipleSite {
	byName := map[string]Document{}
	for _, doc := range docs {
		if doc.Kind == "SmallMultiple" {
			byName[doc.Name] = doc
		}
	}
	var sites []smallMultipleSite
	overridden, plain := map[string]bool{}, map[string]bool{}
	add := func(doc Document, path string, raw json.RawMessage) {
		var s smallMultipleLintSpec
		if json.Unmarshal(raw, &s) == nil {
			sites = append(sites, smallMultipleSite{doc: doc, path: path, spec: s})
		}
	}
	for _, doc := range docs {
		var root map[string]json.RawMessage
		if doc.Kind == "SmallMultiple" || json.Unmarshal(doc.Raw, &root) != nil {
			continue
		}
		var walk func(raw json.RawMessage, path string)
		walk = func(raw json.RawMessage, path string) {
			var obj map[string]json.RawMessage
			if json.Unmarshal(raw, &obj) == nil {
				var kind, ref string
				_ = json.Unmarshal(obj["kind"], &kind)
				_ = json.Unmarshal(obj["ref"], &ref)
				if kind == "SmallMultiple" && ref != "" && obj["spec"] == nil {
					plain[ref] = true
				}
				if kind == "SmallMultiple" && obj["spec"] != nil {
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
		if doc.Kind == "SmallMultiple" && (!merged || !overridden[doc.Name] || plain[doc.Name]) {
			add(doc, "spec", specOf(doc))
		}
	}
	return sites
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
