package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	inheritedClosest = "inherited-closest"
	inheritedPage    = "inherited-page"
)

// inheritedProp pairs a spec prop that accepts an inheritance keyword with the
// LayoutCard / LayoutPage field its value is taken from.
type inheritedProp struct{ prop, field string }

var (
	inheritedRuleset = inheritedProp{"ruleset", "ruleset"}

	inheritedComponentProps = []inheritedProp{
		{"scenarios", "titleScenarios"},
		{"variances", "titleVariances"},
		{"order", "titleOrder"},
		{"orderDirection", "titleOrderDirection"},
		inheritedRuleset,
	}

	// A ChartTime is drawn in date order, so it has no order to inherit.
	inheritedChartTimeProps = []inheritedProp{
		{"scenarios", "titleScenarios"},
		{"variances", "titleVariances"},
		inheritedRuleset,
	}

	// A LayoutCard inherits its own title fields, under the same name.
	inheritedCardProps = []inheritedProp{
		{"titleScenarios", "titleScenarios"},
		{"titleVariances", "titleVariances"},
		{"titleOrder", "titleOrder"},
		{"titleOrderDirection", "titleOrderDirection"},
		inheritedRuleset,
	}
)

func inheritedPropsOf(kind string) []inheritedProp {
	switch kind {
	case "Table", "ChartStructure":
		return inheritedComponentProps
	case "ChartTime":
		return inheritedChartTimeProps
	case "ChartScatter", "ChartBubble", "ChartBullet":
		return []inheritedProp{inheritedRuleset}
	case "LayoutCard":
		return inheritedCardProps
	}
	return nil
}

// inheritedPropUnresolved warns when a prop is set to inherited-closest or
// inherited-page and no enclosing LayoutCard or LayoutPage sets the field it
// would be taken from, so the component renders without the expected value.
var inheritedPropUnresolved = Rule{
	ID:   "inherited-prop-unresolved",
	Name: "Inherited Prop Unresolved",
	Description: "A prop set to 'inherited-closest' or 'inherited-page' needs an enclosing LayoutCard or " +
		"LayoutPage that sets the matching field (titleScenarios, titleVariances, titleOrder, " +
		"titleOrderDirection or ruleset).",
	Check: func(_ context.Context, docs []Document) []Finding {
		w := inheritWalker{standalone: make(map[string]specSite), seen: make(map[Finding]bool)}

		var pages []specSite
		var shared []string
		for _, doc := range docs {
			var payload struct {
				Spec map[string]any `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue // Schema validation reports malformed documents.
			}
			site := specSite{doc: doc, path: "spec", spec: payload.Spec}
			if doc.Kind == "LayoutPage" {
				pages = append(pages, site)
			} else if doc.Name != "" {
				site.ref = doc.Kind + ":" + doc.Name
				if _, ok := w.standalone[site.ref]; ok {
					shared = append(shared, site.ref)
				}
				w.standalone[site.ref] = site
			}
		}
		// Documents that share a name are variants, and constraints pick one per
		// artefact. Constraints are not evaluated here, so such a ref is not followed.
		for _, id := range shared {
			delete(w.standalone, id)
		}

		// A component only has ancestors where it is used, so the walk starts at
		// each page and follows refs. A document nothing references is never seen.
		for _, page := range pages {
			w.page = page.doc.Name
			scope := make(inheritScope)
			for _, p := range inheritedCardProps {
				value := page.spec[p.field]
				scope[p.field] = hasValue(value) && inheritKeyword(value) == ""
			}
			w.children(page, "children", page.spec["children"], []inheritScope{scope}, nil)
		}
		return w.findings
	},
}

// inheritScope holds, for one LayoutPage or LayoutCard, which inheritable
// fields it sets to a real value.
type inheritScope map[string]bool

// specSite is a spec object and the place it is written, which is where a
// finding about one of its fields points.
type specSite struct {
	doc  Document
	path string
	spec map[string]any
	ref  string // "Kind:name" when this is the spec of a referenced document
}

// specNode is the effective spec of a layout child: its inline spec merged over
// the spec of the document it references, if any (see ref.MergeSpec).
type specNode struct {
	inline specSite
	base   *specSite
}

// field returns the effective value of key and the site that wrote it.
func (n specNode) field(key string) (any, specSite) {
	if value, ok := n.inline.spec[key]; ok {
		return value, n.inline
	}
	if n.base != nil {
		return n.base.spec[key], *n.base
	}
	return nil, n.inline
}

type inheritWalker struct {
	standalone map[string]specSite // "Kind:name" of every document but pages
	page       string              // name of the page being walked
	findings   []Finding
	seen       map[Finding]bool
}

// children walks the 'children' or 'nodes' list written at owner. chain is the
// page followed by the enclosing cards from the outside in; refs are the
// referenced documents whose own list the walk is inside.
func (w *inheritWalker) children(owner specSite, key string, list any, chain []inheritScope, refs []string) {
	items, _ := list.([]any)
	for i, item := range items {
		child, _ := item.(map[string]any)
		kind := stringField(child, "kind")
		inline, _ := child["spec"].(map[string]any)
		node := specNode{inline: specSite{
			doc:  owner.doc,
			path: joinLintPath(owner.path, key+"."+strconv.Itoa(i)+".spec"),
			spec: inline,
		}}

		if ref := stringField(child, "ref"); ref != "" {
			id := kind + ":" + ref
			base, ok := w.standalone[id]
			// Not followed: a dangling ref (missing-required-reference reports it),
			// a shared name, and a document that references itself.
			if !ok || slices.Contains(refs, id) {
				continue
			}
			node.base = &base
		}
		w.node(kind, node, chain, refs)
	}
}

func (w *inheritWalker) node(kind string, n specNode, chain []inheritScope, refs []string) {
	scope := make(inheritScope)
	for _, p := range inheritedPropsOf(kind) {
		value, site := n.field(p.prop)
		keyword := inheritKeyword(value)
		// A keyword never adds a value to the scope: where it resolves, an
		// ancestor already sets the field.
		switch {
		case keyword == "":
			scope[p.field] = hasValue(value)
		case !inheritResolves(keyword, p.field, chain):
			w.report(site, p, keyword)
		}
	}

	// Grid and Tree hold children but are no inheritance source.
	listKey := "children"
	switch kind {
	case "LayoutCard":
		chain = append(slices.Clip(chain), scope)
	case "Grid":
	case "Tree":
		listKey = "nodes"
	default:
		return
	}
	list, site := n.field(listKey)
	if site.ref != "" {
		refs = append(slices.Clip(refs), site.ref)
	}
	w.children(site, listKey, list, chain, refs)
}

func (w *inheritWalker) report(site specSite, p inheritedProp, keyword string) {
	missing := fmt.Sprintf("LayoutPage %q does not set '%s'; set it there", w.page, p.field)
	if keyword == inheritedClosest {
		missing = fmt.Sprintf("neither an enclosing LayoutCard nor LayoutPage %q sets '%s'; set it on one of them",
			w.page, p.field)
	}
	finding := Finding{
		RuleID: "inherited-prop-unresolved",
		Message: fmt.Sprintf("'%s: %s' has nothing to inherit: %s or give '%s' an explicit value",
			p.prop, keyword, missing, p.prop),
		File:   site.doc.File,
		DocIdx: site.doc.Position,
		Path:   joinLintPath(site.path, p.prop),
	}
	// A document referenced several times on one page would repeat the finding.
	if !w.seen[finding] {
		w.seen[finding] = true
		w.findings = append(w.findings, finding)
	}
}

// inheritResolves reports whether a keyword finds a value. inherited-closest
// takes the first ancestor that sets the field, so it resolves when any does.
func inheritResolves(keyword, field string, chain []inheritScope) bool {
	if keyword == inheritedPage {
		return chain[0][field]
	}
	return slices.ContainsFunc(chain, func(scope inheritScope) bool { return scope[field] })
}

// inheritKeyword returns the inheritance keyword a value holds, or "". Only the
// plain string is a keyword; inside a list the same text is an ordinary entry.
func inheritKeyword(value any) string {
	s, _ := value.(string)
	if s = strings.TrimSpace(s); s == inheritedClosest || s == inheritedPage {
		return s
	}
	return ""
}

// hasValue reports whether a string-or-list field is set to something.
func hasValue(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	}
	return false
}
