package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"bino.bi/bino/internal/report/annotation"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/schema"
)

var annotationTargetMissing = Rule{
	ID:          "annotation-target-missing",
	Name:        "Annotation Target Missing",
	Description: "The target or to of an Annotation must be a component with a slot on a rendered page. A child of a Grid or a Tree cannot be a target.",
	Check: func(_ context.Context, docs []Document) []Finding {
		views := annotationViews(docs)
		var findings []Finding
		forEachAnnotation(docs, func(doc Document, a schema.AnnotationSpec) {
			for _, end := range annotationEnds(a) {
				key := annotation.Key(end.target.Kind, end.target.Name)
				included, onSlot, nested := false, false, false
				for _, view := range views {
					if _, ok := view.annotations[doc.Name]; !ok {
						continue
					}
					included = true
					for _, page := range view.pages {
						onSlot = onSlot || page.slots[key]
						nested = nested || page.nested[key]
					}
				}
				if onSlot || !included {
					continue
				}
				message := fmt.Sprintf("%s %q is not on any rendered page", end.target.Kind, end.target.Name)
				if nested {
					message = fmt.Sprintf("%s %q sits inside a Grid or a Tree: grid and tree children are not supported yet", end.target.Kind, end.target.Name)
				}
				findings = append(findings, Finding{
					RuleID:   "annotation-target-missing",
					Message:  message,
					File:     doc.File,
					DocIdx:   doc.Position,
					Path:     "spec." + end.field + ".name",
					Severity: "error",
				})
			}
		})
		return findings
	},
}

var annotationArrowPages = Rule{
	ID:          "annotation-arrow-pages",
	Name:        "Annotation Arrow Pages",
	Description: "Both ends of an arrow must be on the same page.",
	Check: func(_ context.Context, docs []Document) []Finding {
		views := annotationViews(docs)
		var findings []Finding
		forEachAnnotation(docs, func(doc Document, a schema.AnnotationSpec) {
			if a.To == nil {
				return
			}
			from := annotation.Key(a.Target.Kind, a.Target.Name)
			to := annotation.Key(a.To.Kind, a.To.Name)
			fromFound, toFound, together := false, false, false
			for _, view := range views {
				if _, ok := view.annotations[doc.Name]; !ok {
					continue
				}
				for _, page := range view.pages {
					fromFound = fromFound || page.slots[from]
					toFound = toFound || page.slots[to]
					together = together || (page.slots[from] && page.slots[to])
				}
			}
			// One end on no page at all is annotation-target-missing.
			if fromFound && toFound && !together {
				findings = append(findings, Finding{
					RuleID:   "annotation-arrow-pages",
					Message:  fmt.Sprintf("the arrow goes from %s %q to %s %q, which are on different pages", a.Target.Kind, a.Target.Name, a.To.Kind, a.To.Name),
					File:     doc.File,
					DocIdx:   doc.Position,
					Path:     "spec.to",
					Severity: "error",
				})
			}
		})
		return findings
	},
}

var annotationListUnknown = Rule{
	ID:          "annotation-list-unknown",
	Name:        "Annotation List Unknown",
	Description: "An AnnotationList must only name annotations that exist after constraint filtering.",
	Check: func(_ context.Context, docs []Document) []Finding {
		return forEachListedName(docs, func(view annotationView, _ *annotationPageView, _ annotationListUse, name string) (string, string) {
			if _, ok := view.annotations[name]; ok {
				return "", ""
			}
			return "annotation-list-unknown", fmt.Sprintf("annotation %q does not exist for artefact %s", name, view.artefact)
		})
	},
}

var annotationListOffPage = Rule{
	ID:          "annotation-list-off-page",
	Name:        "Annotation List Off Page",
	Description: "An AnnotationList must only name annotations whose target is on its page.",
	Check: func(_ context.Context, docs []Document) []Finding {
		return forEachListedName(docs, func(view annotationView, page *annotationPageView, _ annotationListUse, name string) (string, string) {
			doc, ok := view.annotations[name]
			if !ok {
				return "", ""
			}
			a, err := annotation.Parse(doc.Raw)
			if err != nil || page.slots[annotation.Key(a.Target.Kind, a.Target.Name)] {
				return "", ""
			}
			return "annotation-list-off-page", fmt.Sprintf("the target of annotation %q is not on page %q", name, page.doc.Name)
		})
	},
}

var annotationUnlisted = Rule{
	ID:          "annotation-unlisted",
	Name:        "Annotation Unlisted",
	Description: "An annotation with a title or a text should be shown by an AnnotationList on its page.",
	Check: func(_ context.Context, docs []Document) []Finding {
		views := annotationViews(docs)
		seen := map[string]bool{}
		var findings []Finding
		for _, view := range views {
			for _, page := range view.pages {
				forEachAnnotation(docs, func(doc Document, a schema.AnnotationSpec) {
					if _, ok := view.annotations[doc.Name]; !ok || !annotation.HasContent(a) || !page.slots[annotation.Key(a.Target.Kind, a.Target.Name)] {
						return
					}
					if page.lists.show(doc.Name) || seen[doc.Name+"\x00"+page.doc.Name] {
						return
					}
					seen[doc.Name+"\x00"+page.doc.Name] = true
					findings = append(findings, Finding{
						RuleID:   "annotation-unlisted",
						Message:  fmt.Sprintf("page %q has no AnnotationList that shows this text", page.doc.Name),
						File:     doc.File,
						DocIdx:   doc.Position,
						Path:     "spec",
						Severity: "info",
					})
				})
			}
		}
		return findings
	},
}

var annotationListInLive = Rule{
	ID:          "annotation-list-in-live",
	Name:        "Annotation List In Live",
	Description: "A page with an AnnotationList that a LiveReportArtefact serves has an empty slot there: live artefacts show no annotations.",
	Check: func(_ context.Context, docs []Document) []Finding {
		livePages := pagesServedLive(docs)
		var findings []Finding
		for _, page := range walkAnnotationPages(docs, nil, nil) {
			if len(page.lists) == 0 {
				continue
			}
			for _, live := range livePages {
				if !live.pages(page.doc.Name) {
					continue
				}
				findings = append(findings, Finding{
					RuleID:   "annotation-list-in-live",
					Message:  fmt.Sprintf("LiveReportArtefact %q serves this page, its AnnotationList slot stays empty there", live.name),
					File:     page.lists[0].file,
					DocIdx:   page.lists[0].docIdx,
					Path:     page.lists[0].path,
					Severity: "warning",
				})
				break
			}
		}
		return findings
	},
}

// AnnotationPointFindings checks the points of the annotations against the data.
// rows returns the rows of a binding ("$name" for a DataSource), nil when they
// are not known.
func AnnotationPointFindings(docs []Document, rows func(binding string) annotation.Rows) []Finding {
	views := annotationViews(docs)
	var findings []Finding
	forEachAnnotation(docs, func(doc Document, a schema.AnnotationSpec) {
		for _, end := range annotationEnds(a) {
			key := annotation.Key(end.target.Kind, end.target.Name)
			binding, found := "", false
			for _, view := range views {
				if _, ok := view.annotations[doc.Name]; !ok {
					continue
				}
				for _, page := range view.pages {
					if page.slots[key] && !found {
						binding, found = page.bindings[key], true
					}
				}
			}
			if !found {
				continue // annotation-target-missing says so
			}
			var data annotation.Rows
			if binding != "" {
				data = rows(binding)
			}
			if reason := annotation.CheckPoint(end.target.Kind, end.target.Point, data); reason != "" {
				findings = append(findings, Finding{
					RuleID:   "annotation-point-missing",
					Message:  fmt.Sprintf("%s %q: %s", end.target.Kind, end.target.Name, reason),
					File:     doc.File,
					DocIdx:   doc.Position,
					Path:     "spec." + end.field + ".point",
					Severity: "warning",
				})
			}
		}
	})
	return findings
}

type annotationEnd struct {
	field  string
	target schema.AnnotationTarget
}

func annotationEnds(a schema.AnnotationSpec) []annotationEnd {
	ends := []annotationEnd{{"target", a.Target}}
	if a.To != nil {
		ends = append(ends, annotationEnd{"to", *a.To})
	}
	return ends
}

func forEachAnnotation(docs []Document, fn func(Document, schema.AnnotationSpec)) {
	for _, doc := range docs {
		if doc.Kind != schema.KindAnnotation {
			continue
		}
		if a, err := annotation.Parse(doc.Raw); err == nil {
			fn(doc, a)
		}
	}
}

// forEachListedName runs check over every name in every list and reports each
// list and name once, however many artefacts show the page.
func forEachListedName(docs []Document, check func(annotationView, *annotationPageView, annotationListUse, string) (ruleID, message string)) []Finding {
	seen := map[string]bool{}
	var findings []Finding
	for _, view := range annotationViews(docs) {
		for _, page := range view.pages {
			for _, list := range page.lists {
				for i, name := range list.spec.Annotations {
					ruleID, message := check(view, page, list, name)
					key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", list.file, list.docIdx, list.path, name)
					if ruleID == "" || seen[key] {
						continue
					}
					seen[key] = true
					findings = append(findings, Finding{
						RuleID:   ruleID,
						Message:  message,
						File:     list.file,
						DocIdx:   list.docIdx,
						Path:     fmt.Sprintf("%s.annotations[%d]", list.path, i),
						Severity: "error",
					})
				}
			}
		}
	}
	return findings
}

// annotationView is what one ReportArtefact renders.
type annotationView struct {
	artefact    string
	annotations map[string]Document
	pages       []*annotationPageView
}

// annotationPageView is what one page holds.
type annotationPageView struct {
	doc Document
	// slots holds the components with a slot, the only possible targets.
	slots map[string]bool
	// nested holds the children of a Grid or a Tree.
	nested map[string]bool
	// bindings maps a component to the first dataset it binds.
	bindings map[string]string
	lists    annotationListUses
}

type annotationListUse struct {
	file   string
	docIdx int
	path   string
	spec   schema.AnnotationListSpec
}

type annotationListUses []annotationListUse

// show reports whether a list on the page shows the annotation.
func (l annotationListUses) show(name string) bool {
	for _, list := range l {
		if len(list.spec.Annotations) == 0 {
			return true
		}
		for _, listed := range list.spec.Annotations {
			if listed == name {
				return true
			}
		}
	}
	return false
}

// annotationViews builds one view per ReportArtefact, after constraint and page
// filtering, or one view over everything when the project has none.
func annotationViews(docs []Document) []annotationView {
	var views []annotationView
	for _, doc := range docs {
		if doc.Kind != "ReportArtefact" {
			continue
		}
		ctx := artefactConstraintContext(doc)
		if ctx == nil {
			continue
		}
		var payload struct {
			Spec struct {
				LayoutPages config.LayoutPagesOrRefs `json:"layoutPages"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &payload) != nil {
			continue
		}
		format := getArtefactFormat(doc.Raw)
		selected := func(page Document) bool {
			return pageMatchesArtefact(page, doc, format) && pageSelected(payload.Spec.LayoutPages, page.Name)
		}
		views = append(views, annotationViewFor(doc.Name, docs, ctx, selected))
	}
	if len(views) == 0 {
		views = append(views, annotationViewFor("", docs, nil, nil))
	}
	return views
}

func artefactConstraintContext(doc Document) *spec.ConstraintContext {
	specMap, err := spec.ToMap(doc.Raw)
	if err != nil {
		return nil
	}
	return &spec.ConstraintContext{Labels: doc.Labels, Spec: specMap, Mode: spec.ModeBuild, ArtefactKind: "report"}
}

func annotationViewFor(artefact string, docs []Document, ctx *spec.ConstraintContext, selected func(Document) bool) annotationView {
	view := annotationView{artefact: artefact, annotations: map[string]Document{}}
	for _, doc := range docs {
		if doc.Kind == schema.KindAnnotation && docIncluded(doc, ctx) {
			view.annotations[doc.Name] = doc
		}
	}
	view.pages = walkAnnotationPages(docs, ctx, selected)
	return view
}

func docIncluded(doc Document, ctx *spec.ConstraintContext) bool {
	if ctx == nil || len(doc.Constraints) == 0 {
		return true
	}
	match, err := spec.EvaluateParsedConstraintsWithContext(doc.Constraints, ctx, doc.Kind, doc.Name)
	return err == nil && match
}

// walkAnnotationPages collects what each rendered page holds. A nil ctx keeps
// everything.
func walkAnnotationPages(docs []Document, ctx *spec.ConstraintContext, selected func(Document) bool) []*annotationPageView {
	refs := map[string]Document{}
	for _, doc := range docs {
		if docIncluded(doc, ctx) {
			refs[annotation.Key(doc.Kind, doc.Name)] = doc
		}
	}
	var pages []*annotationPageView
	for _, doc := range docs {
		if doc.Kind != "LayoutPage" || !docIncluded(doc, ctx) || (selected != nil && !selected(doc)) {
			continue
		}
		var payload struct {
			Spec struct {
				Children []layoutChildSpec `json:"children"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &payload) != nil {
			continue
		}
		page := &annotationPageView{doc: doc, slots: map[string]bool{}, nested: map[string]bool{}, bindings: map[string]string{}}
		page.walk(payload.Spec.Children, doc, "spec.children", ctx, refs, map[string]bool{})
		pages = append(pages, page)
	}
	return pages
}

func (p *annotationPageView) walk(children []layoutChildSpec, owner Document, basePath string, ctx *spec.ConstraintContext, refs map[string]Document, cards map[string]bool) {
	for i, child := range children {
		if !childIncluded(child, ctx) {
			continue
		}
		childPath := fmt.Sprintf("%s[%d]", basePath, i)
		name := child.Ref
		if name == "" {
			name = child.Metadata.Name
		}
		key := annotation.Key(child.Kind, name)
		referenced, isRef := refs[key]
		if child.Ref != "" && !isRef {
			continue // missing or filtered: the child does not render
		}

		switch child.Kind {
		case schema.KindAnnotationList:
			p.addList(child, owner, childPath, referenced, isRef)
			continue
		case schema.KindLayoutCard:
			p.walkCard(child, owner, childPath, referenced, isRef, ctx, refs, cards)
			continue
		}
		if name != "" {
			p.slots[key] = true
			if _, known := p.bindings[key]; !known {
				binding := annotation.FirstDataset(child.Spec)
				if binding == "" && isRef {
					binding = annotation.FirstDataset(specOf(referenced))
				}
				p.bindings[key] = binding
			}
		}
		if child.Kind == schema.KindGrid || child.Kind == schema.KindTree {
			p.addNested(child.Spec)
			if isRef {
				p.addNested(specOf(referenced))
			}
		}
	}
}

func childIncluded(child layoutChildSpec, ctx *spec.ConstraintContext) bool {
	if ctx == nil || len(child.Metadata.Constraints) == 0 {
		return true
	}
	constraints, err := spec.ParseMixedConstraints(child.Metadata.Constraints)
	if err != nil {
		return false
	}
	match, err := spec.EvaluateParsedConstraintsWithContext(constraints, ctx, child.Kind, child.Metadata.Name)
	return err == nil && match
}

func (p *annotationPageView) addList(child layoutChildSpec, owner Document, childPath string, referenced Document, isRef bool) {
	use := annotationListUse{file: owner.File, docIdx: owner.Position, path: childPath + ".spec"}
	raw := child.Spec
	if child.Ref != "" && isRef {
		use = annotationListUse{file: referenced.File, docIdx: referenced.Position, path: "spec"}
		raw = specOf(referenced)
	}
	if len(raw) > 0 && json.Unmarshal(raw, &use.spec) != nil {
		use.spec = schema.AnnotationListSpec{}
	}
	p.lists = append(p.lists, use)
}

func (p *annotationPageView) walkCard(child layoutChildSpec, owner Document, childPath string, referenced Document, isRef bool, ctx *spec.ConstraintContext, refs map[string]Document, cards map[string]bool) {
	raw := child.Spec
	basePath := childPath + ".spec.children"
	nextOwner := owner
	if child.Ref != "" && isRef {
		if cards[child.Ref] {
			return
		}
		cards[child.Ref] = true
		defer delete(cards, child.Ref)
		raw = specOf(referenced)
		basePath = "spec.children"
		nextOwner = referenced
	}
	var card struct {
		Children []layoutChildSpec `json:"children"`
	}
	if json.Unmarshal(raw, &card) == nil {
		p.walk(card.Children, nextOwner, basePath, ctx, refs, cards)
	}
}

// addNested records every component named below a Grid or a Tree.
func (p *annotationPageView) addNested(raw json.RawMessage) {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return
	}
	var visit func(any)
	visit = func(node any) {
		switch t := node.(type) {
		case []any:
			for _, e := range t {
				visit(e)
			}
		case map[string]any:
			if kind, ok := t["kind"].(string); ok {
				if ref, _ := t["ref"].(string); ref != "" {
					p.nested[annotation.Key(kind, ref)] = true
				} else if meta, ok := t["metadata"].(map[string]any); ok {
					if name, _ := meta["name"].(string); name != "" {
						p.nested[annotation.Key(kind, name)] = true
					}
				}
			}
			for _, e := range t {
				visit(e)
			}
		}
	}
	visit(v)
}

func specOf(doc Document) json.RawMessage {
	var payload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if json.Unmarshal(doc.Raw, &payload) != nil {
		return nil
	}
	return payload.Spec
}

// pageSelected applies the layoutPages of an artefact or a live route. No
// entry selects every page, as in the pipeline.
func pageSelected(refs config.LayoutPagesOrRefs, name string) bool {
	if len(refs) == 0 {
		return true
	}
	for _, ref := range refs {
		if ok, err := path.Match(ref.Page, name); ref.Page == name || (err == nil && ok) {
			return true
		}
	}
	return false
}

// liveArtefactPages is a LiveReportArtefact and the pages its routes serve.
type liveArtefactPages struct {
	name  string
	pages func(page string) bool
}

func pagesServedLive(docs []Document) []liveArtefactPages {
	reports := map[string]config.LayoutPagesOrRefs{}
	for _, doc := range docs {
		if doc.Kind != "ReportArtefact" {
			continue
		}
		var payload struct {
			Spec struct {
				LayoutPages config.LayoutPagesOrRefs `json:"layoutPages"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &payload) == nil {
			reports[doc.Name] = payload.Spec.LayoutPages
		}
	}
	var out []liveArtefactPages
	for _, doc := range docs {
		if doc.Kind != "LiveReportArtefact" {
			continue
		}
		var payload struct {
			Spec config.LiveReportArtefactSpec `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &payload) != nil {
			continue
		}
		routes := payload.Spec.Routes
		out = append(out, liveArtefactPages{name: doc.Name, pages: func(page string) bool {
			for _, route := range routes {
				if route.Artifact != "" {
					if refs, ok := reports[route.Artifact]; ok && pageSelected(refs, page) {
						return true
					}
					continue
				}
				if len(route.LayoutPages) > 0 && pageSelected(route.LayoutPages, page) {
					return true
				}
			}
			return false
		}})
	}
	return out
}
