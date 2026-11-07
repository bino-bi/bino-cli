package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"bino.bi/bino/internal/report/ref"
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
		w.child(owner, key+"."+strconv.Itoa(i), item, chain, refs)
	}
}

// child walks one child written at owner under path.
func (w *inheritWalker) child(owner specSite, path string, item any, chain []inheritScope, refs []string) {
	child, _ := item.(map[string]any)
	kind := stringField(child, "kind")
	inline, _ := child["spec"].(map[string]any)
	node := specNode{inline: specSite{
		doc:  owner.doc,
		path: joinLintPath(owner.path, path+".spec"),
		spec: inline,
	}}

	if refName := stringField(child, "ref"); refName != "" {
		id := kind + ":" + refName
		base, ok := w.standalone[id]
		// Not followed: a dangling ref (missing-required-reference reports it),
		// a shared name, and a document that references itself.
		if !ok || slices.Contains(refs, id) {
			return
		}
		node.base = &base
	}
	w.node(kind, node, chain, refs)
}

// scope is what a node of kind passes down, and reports its keywords that
// resolve nowhere in chain.
func (w *inheritWalker) scope(kind string, n specNode, chain []inheritScope) inheritScope {
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
	return scope
}

func (w *inheritWalker) node(kind string, n specNode, chain []inheritScope, refs []string) {
	scope := w.scope(kind, n, chain)

	// Grid, Tree and the base of a SmallMultiple are no inheritance source.
	listKey := "children"
	switch kind {
	case "LayoutCard":
		chain = append(slices.Clip(chain), scope)
	case "Grid":
	case "Tree":
		listKey = "nodes"
	case "SmallMultiple":
		base, site := n.field("base")
		if merged, ok := mergedBase(n); ok {
			// a partial override of the base merges into the document's base, as in render
			base, site = merged, *n.base
		}
		if site.ref != "" {
			refs = append(slices.Clip(refs), site.ref)
		}
		// with a card, every panel sits in a LayoutCard made from it; the
		// base is checked once per panel card, and findings are deduplicated
		for _, cardChain := range w.panelCardChains(n, chain) {
			w.child(site, "base", base, cardChain, refs)
		}
		return
	default:
		return
	}
	list, site := n.field(listKey)
	if site.ref != "" {
		refs = append(slices.Clip(refs), site.ref)
	}
	w.children(site, listKey, list, chain, refs)
}

// panelCardChains are the chains the panels of a SmallMultiple see: chain
// alone without a card, else chain with the card of each kind of panel. The
// card is the ref site's card merged field by field over the document's, as
// in render, and an item card goes over that. Keywords in the cards are
// checked against chain.
func (w *inheritWalker) panelCardChains(n specNode, chain []inheritScope) [][]inheritScope {
	cardNode := specNode{}
	if card, ok := n.inline.spec["card"].(map[string]any); ok {
		cardNode.inline = specSite{doc: n.inline.doc, path: joinLintPath(n.inline.path, "card"), spec: card, ref: n.inline.ref}
	}
	if n.base != nil {
		if card, ok := n.base.spec["card"].(map[string]any); ok {
			docCard := specSite{doc: n.base.doc, path: joinLintPath(n.base.path, "card"), spec: card, ref: n.base.ref}
			if cardNode.inline.spec == nil {
				cardNode.inline = docCard
			} else {
				cardNode.base = &docCard
			}
		}
	}
	if cardNode.inline.spec == nil {
		return [][]inheritScope{chain}
	}
	cardScope := w.scope("LayoutCard", cardNode, chain)
	withScope := func(scope inheritScope) []inheritScope { return append(slices.Clip(chain), scope) }

	panelsValue, panelsSite := n.field("panels")
	panels, _ := panelsValue.(map[string]any)
	items, _ := panels["items"].([]any)
	var chains [][]inheritScope
	if by, _ := panels["by"].(string); by != "" || len(items) == 0 {
		chains = append(chains, withScope(cardScope))
	}
	for i, item := range items {
		entry, _ := item.(map[string]any)
		itemCard, ok := entry["card"].(map[string]any)
		if !ok {
			chains = append(chains, withScope(cardScope))
			continue
		}
		itemSite := specSite{
			doc:  panelsSite.doc,
			path: joinLintPath(panelsSite.path, "panels.items."+strconv.Itoa(i)+".card"),
			spec: itemCard,
			ref:  panelsSite.ref,
		}
		itemScope := w.scope("LayoutCard", specNode{inline: itemSite}, chain)
		scope := maps.Clone(cardScope)
		// only the fields the item writes replace those of the shared card
		for _, p := range inheritedPropsOf("LayoutCard") {
			if _, ok := itemCard[p.prop]; ok {
				scope[p.field] = itemScope[p.field]
			}
		}
		chains = append(chains, withScope(scope))
	}
	return chains
}

// mergedBase is the base of a referenced SmallMultiple with the base of the
// ref site merged over it (ref.MergeSpec). ok is false when either is missing.
func mergedBase(n specNode) (any, bool) {
	inline, ok := n.inline.spec["base"].(map[string]any)
	if !ok || n.base == nil {
		return nil, false
	}
	doc, ok := n.base.spec["base"].(map[string]any)
	if !ok {
		return nil, false
	}
	docJSON, err1 := json.Marshal(doc)
	inlineJSON, err2 := json.Marshal(inline)
	if err1 != nil || err2 != nil {
		return nil, false
	}
	merged, err := ref.MergeSpec(docJSON, inlineJSON)
	if err != nil {
		return nil, false
	}
	var out map[string]any
	if json.Unmarshal(merged, &out) != nil {
		return nil, false
	}
	return out, true
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
