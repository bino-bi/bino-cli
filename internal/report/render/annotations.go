package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/report/annotation"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/schema"
)

// annotationDoc is one Annotation document.
type annotationDoc struct {
	name string
	spec schema.AnnotationSpec
}

// annotationPage collects what a page's marks need while its children render.
// Numbers depend on every list and target of the page, so they are known only
// after the last child: a list writes a placeholder until then.
type annotationPage struct {
	docs []annotationDoc
	// wanted holds the components an annotation points at.
	wanted map[string]bool
	// targets maps a component to the ids of its slots, one per place it sits.
	targets map[string][]string
	// places maps a component to what it draws, one per place it sits.
	places map[string][]annotationPlace
	// panels maps a SmallMultiple to the panel labels annotations point at.
	panels map[string][]string
	lists  []schema.AnnotationListSpec
	nextID int
}

// annotationPlace is one place of a target: its effective spec and the kind
// that draws it. For a panel of a SmallMultiple that is the base kind, and a
// by panel only has the rows with its value in the by column.
type annotationPlace struct {
	kind  string
	spec  json.RawMessage
	by    string
	label string
}

// annotationData looks up and decodes the rows of a binding once per render.
type annotationData struct {
	raw     map[string]json.RawMessage
	decoded map[string]annotation.Rows
}

// annotationLayerItem is one entry of the items attribute of bn-annotation-layer.
type annotationLayerItem struct {
	Label   string                  `json:"label,omitempty"`
	Mark    string                  `json:"mark"`
	Color   string                  `json:"color,omitempty"`
	Target  string                  `json:"target"`
	Point   *schema.AnnotationPoint `json:"point,omitempty"`
	To      string                  `json:"to,omitempty"`
	ToPoint *schema.AnnotationPoint `json:"toPoint,omitempty"`
}

// annotationListItem is one entry of the items attribute of bn-annotation-list.
type annotationListItem struct {
	Label string `json:"label,omitempty"`
	Title string `json:"title,omitempty"`
	HTML  string `json:"html,omitempty"`
}

// withAnnotations prepares the annotations of this render. Serve mode shows
// none, whatever their constraints say.
func (rc *renderCtx) withAnnotations(docs []config.Document, sets []dataset.Result, sources []datasource.Result, mode Mode, engineVersion string) error {
	if mode == ModeServe {
		return nil
	}
	annotations, err := collectAnnotations(docs)
	if err != nil {
		return err
	}
	if len(annotations) == 0 {
		return nil
	}
	if err := engine.CheckAnnotationSupport(engineVersion); err != nil {
		return fmt.Errorf("render: annotation-engine-version: %w", err)
	}
	rc.annotations = annotations
	rc.annotationData = newAnnotationData(sets, sources)
	return nil
}

// collectAnnotations reads the Annotation documents in manifest order.
func collectAnnotations(docs []config.Document) ([]annotationDoc, error) {
	var out []annotationDoc
	for _, doc := range docs {
		if doc.Kind != schema.KindAnnotation {
			continue
		}
		s, err := annotation.Parse(doc.Raw)
		if err != nil {
			return nil, fmt.Errorf("render: parse annotation %s: %w", doc.Name, err)
		}
		out = append(out, annotationDoc{name: doc.Name, spec: s})
	}
	return out, nil
}

func newAnnotationData(sets []dataset.Result, sources []datasource.Result) *annotationData {
	d := &annotationData{raw: map[string]json.RawMessage{}, decoded: map[string]annotation.Rows{}}
	for _, r := range sets {
		if len(r.Data) > 0 {
			d.raw[r.Name] = r.Data
		}
	}
	for _, r := range sources {
		if len(r.Data) > 0 {
			d.raw["$"+r.Name] = r.Data
		}
	}
	return d
}

// rows returns the rows of a binding, or nil when they are not known.
func (d *annotationData) rows(binding string) annotation.Rows {
	if rows, ok := d.decoded[binding]; ok {
		return rows
	}
	raw, ok := d.raw[binding]
	if !ok {
		return nil
	}
	rows, err := annotation.DecodeRows(raw)
	if err != nil {
		rows = nil
	}
	d.decoded[binding] = rows
	return rows
}

// withAnnotationPage starts the mark collection for a page.
func (rc *renderCtx) withAnnotationPage() *renderCtx {
	if len(rc.annotations) == 0 {
		return rc
	}
	c := *rc
	page := &annotationPage{
		docs:    rc.annotations,
		wanted:  map[string]bool{},
		targets: map[string][]string{},
		places:  map[string][]annotationPlace{},
		panels:  map[string][]string{},
	}
	want := func(end schema.AnnotationTarget) {
		key := annotation.TargetKey(end)
		if end.Panel != "" && !page.wanted[key] {
			component := annotation.Key(end.Kind, end.Name)
			page.panels[component] = append(page.panels[component], end.Panel)
		}
		page.wanted[key] = true
	}
	for _, a := range rc.annotations {
		want(a.spec.Target)
		if a.spec.To != nil {
			want(*a.spec.To)
		}
	}
	c.annotationPage = page
	return &c
}

// withoutAnnotationPage ends the mark collection below a Grid or a Tree. Their
// children cannot be targets, and a card inside one must not make its own an
// exception: lint reports them all alike.
func (rc *renderCtx) withoutAnnotationPage() *renderCtx {
	if rc.annotationPage == nil {
		return rc
	}
	c := *rc
	c.annotationPage = nil
	return &c
}

// forSmallMultiple ends the mark collection like withoutAnnotationPage, but
// keeps the page for the panels: a panel can be a target.
func (rc *renderCtx) forSmallMultiple() *renderCtx {
	if rc.annotationPage == nil {
		return rc
	}
	c := *rc
	c.annotationPage = nil
	c.panelPage = rc.annotationPage
	return &c
}

// panelAnnotationTarget gives a panel that an annotation points at a target id
// that is unique on the page, and keeps what the point check needs. It is ""
// for a panel without annotation.
func (rc *renderCtx) panelAnnotationTarget(name, baseKind, by string, p panel, cellSpec json.RawMessage) string {
	page := rc.panelPage
	if page == nil {
		return ""
	}
	key := annotation.TargetKey(schema.AnnotationTarget{Kind: schema.KindSmallMultiple, Name: name, Panel: p.label})
	if !page.wanted[key] {
		return ""
	}
	page.nextID++
	id := fmt.Sprintf("t%d", page.nextID)
	page.targets[key] = append(page.targets[key], id)
	place := annotationPlace{kind: baseKind, spec: cellSpec}
	if p.item == nil {
		place.by, place.label = by, p.label
	}
	page.places[key] = append(page.places[key], place)
	return id
}

// warnPanelTargets reports the panels annotations name that the SmallMultiple
// does not have, or has more than once. The values of panels.by are only known
// with the data, so lint cannot always tell.
func (rc *renderCtx) warnPanelTargets(name string, panels []panel) {
	page := rc.panelPage
	if page == nil {
		return
	}
	for _, label := range page.panels[annotation.Key(schema.KindSmallMultiple, name)] {
		count := 0
		for _, p := range panels {
			if p.label == label {
				count++
			}
		}
		switch {
		case count == 0:
			rc.warn(fmt.Sprintf("annotation-target-missing: SmallMultiple %q has no panel %q", name, label))
		case count > 1:
			rc.warn(fmt.Sprintf("annotation-panel-ambiguous: SmallMultiple %q has %d panels with the label %q; the mark goes on each of them", name, count, label))
		}
	}
}

// childKey is the component a layout child renders: the document it refs, or
// the name of an inline child.
func childKey(child layoutChild) string {
	name := child.Ref
	if name == "" {
		name = child.Metadata.Name
	}
	return annotation.Key(child.Kind, name)
}

// noteAnnotationSpec keeps the effective spec of a component an annotation
// points at. Two places of one component can bind different datasets.
func (rc *renderCtx) noteAnnotationSpec(child layoutChild, effectiveSpec json.RawMessage) {
	if rc.annotationPage == nil {
		return
	}
	if key := childKey(child); rc.annotationPage.wanted[key] {
		rc.annotationPage.places[key] = append(rc.annotationPage.places[key], annotationPlace{kind: child.Kind, spec: effectiveSpec})
	}
}

// writeAnnotationTarget marks the slot of a component an annotation points at,
// with an id that is unique on the page: one document can sit there twice.
func (rc *renderCtx) writeAnnotationTarget(b *strings.Builder, child layoutChild) {
	page := rc.annotationPage
	if page == nil {
		return
	}
	key := childKey(child)
	if !page.wanted[key] {
		return
	}
	page.nextID++
	id := fmt.Sprintf("t%d", page.nextID)
	page.targets[key] = append(page.targets[key], id)
	writeAttr(b, "data-bino-annotation-target", id)
}

// annotationListPlaceholder notes a list and stands in for it until the numbers
// are known. Text attributes are escaped, so a comment cannot appear by accident.
func (rc *renderCtx) annotationListPlaceholder(s schema.AnnotationListSpec) string {
	page := rc.annotationPage
	if page == nil {
		return ""
	}
	page.lists = append(page.lists, s)
	return annotationListMarker(len(page.lists) - 1)
}

func annotationListMarker(i int) string {
	return fmt.Sprintf("<!--bino-annotation-list-%d-->", i)
}

// finishAnnotationPage appends the layer and replaces the list placeholders in
// the page rendered so far, and returns the result.
func (rc *renderCtx) finishAnnotationPage(b *strings.Builder, selectedStyle string) (string, error) {
	page := rc.annotationPage
	if page == nil {
		return b.String(), nil
	}
	labels, placed := page.number(rc)
	items := page.layerItems(labels, placed)
	if len(items) > 0 {
		value, err := json.Marshal(items)
		if err != nil {
			return "", fmt.Errorf("render: annotation layer: %w", err)
		}
		b.WriteString("  <bn-annotation-layer slot='annotations'")
		writeAttr(b, "items", string(value))
		writeAttr(b, "selected-style", selectedStyle)
		b.WriteString("></bn-annotation-layer>\n")
	}
	out := b.String()
	for i, list := range page.lists {
		listHTML, err := page.renderList(rc, list, labels)
		if err != nil {
			return "", err
		}
		out = strings.Replace(out, annotationListMarker(i), listHTML, 1)
	}
	return out, nil
}

// onPage reports whether the target of an annotation sits on this page.
func (p *annotationPage) onPage(a annotationDoc) bool {
	return len(p.targets[annotation.TargetKey(a.spec.Target)]) > 0
}

// number gives the annotations of this page their numbers: first in the order
// of the lists, then in manifest order. Only an annotation with a text and with
// a mark gets one, so numbers have no gaps.
func (p *annotationPage) number(rc *renderCtx) (labels map[string]string, placed map[string]bool) {
	labels = map[string]string{}
	placed = map[string]bool{}
	for _, a := range p.docs {
		if p.onPage(a) {
			placed[a.name] = p.canPlace(rc, a)
		}
	}

	next := 1
	assign := func(a annotationDoc) {
		if _, done := labels[a.name]; done || !p.onPage(a) || !placed[a.name] || !annotation.HasContent(a.spec) {
			return
		}
		labels[a.name] = fmt.Sprint(next)
		next++
	}
	for _, list := range p.lists {
		for _, a := range p.listed(list) {
			assign(a)
		}
	}
	for _, a := range p.docs {
		assign(a)
	}
	return labels, placed
}

// listed returns the annotations a list names that exist, or all of them.
func (p *annotationPage) listed(list schema.AnnotationListSpec) []annotationDoc {
	if len(list.Annotations) == 0 {
		return p.docs
	}
	byName := make(map[string]annotationDoc, len(p.docs))
	for _, a := range p.docs {
		byName[a.name] = a
	}
	var out []annotationDoc
	for _, name := range list.Annotations {
		if a, ok := byName[name]; ok {
			out = append(out, a)
		}
	}
	return out
}

// canPlace reports whether the ends of an annotation are on this page and can
// exist in the data. Why not goes to the build log.
func (p *annotationPage) canPlace(rc *renderCtx, a annotationDoc) bool {
	if a.spec.To != nil && len(p.targets[annotation.TargetKey(*a.spec.To)]) == 0 {
		rc.warn(fmt.Sprintf("annotation-arrow-pages: annotation %s: %s is not on the page of its target", a.name, describeTarget(*a.spec.To)))
		return false
	}
	ok := p.checkEnd(rc, a.name, a.spec.Target)
	if a.spec.To != nil {
		ok = p.checkEnd(rc, a.name, *a.spec.To) && ok
	}
	return ok
}

// checkEnd is fine when the point can exist in one place of the component: the
// engine has the last word for the others.
func (p *annotationPage) checkEnd(rc *renderCtx, name string, end schema.AnnotationTarget) bool {
	reason := ""
	for _, place := range p.places[annotation.TargetKey(end)] {
		var rows annotation.Rows
		if rc.annotationData != nil {
			rows = rc.annotationData.rows(annotation.Binding(place.spec))
		}
		if place.by != "" {
			rows = annotation.PanelRows(rows, place.by, place.label)
		}
		if reason = annotation.CheckPoint(place.kind, end.Point, rows); reason == "" {
			return true
		}
	}
	rc.warn(fmt.Sprintf("annotation-point-missing: annotation %s on %s: %s", name, describeTarget(end), reason))
	return false
}

// describeTarget names an end of an annotation in a message.
func describeTarget(end schema.AnnotationTarget) string {
	if end.Panel != "" {
		return fmt.Sprintf("%s %s panel %q", end.Kind, end.Name, end.Panel)
	}
	return end.Kind + " " + end.Name
}

// layerItems builds the marks. A component that sits on the page twice gets its
// mark twice; an arrow pairs the places of its two ends in order.
func (p *annotationPage) layerItems(labels map[string]string, placed map[string]bool) []annotationLayerItem {
	var items []annotationLayerItem
	for _, a := range p.docs {
		if !placed[a.name] {
			continue
		}
		ids := p.targets[annotation.TargetKey(a.spec.Target)]
		var toIDs []string
		if a.spec.To != nil {
			toIDs = p.targets[annotation.TargetKey(*a.spec.To)]
		}
		count := max(len(ids), len(toIDs))
		for i := range count {
			item := annotationLayerItem{
				Label:  labels[a.name],
				Mark:   a.spec.Mark,
				Color:  a.spec.Color,
				Target: ids[min(i, len(ids)-1)],
				Point:  a.spec.Target.Point,
			}
			if item.Mark == "" {
				item.Mark = "marker"
			}
			if a.spec.To != nil {
				item.To = toIDs[min(i, len(toIDs)-1)]
				item.ToPoint = a.spec.To.Point
			}
			items = append(items, item)
		}
	}
	return items
}

// renderList renders one AnnotationList. A name that is not on this page is
// left out: lint reports it.
func (p *annotationPage) renderList(rc *renderCtx, list schema.AnnotationListSpec, labels map[string]string) (string, error) {
	var items []annotationListItem
	for _, a := range p.listed(list) {
		if !p.onPage(a) || !annotation.HasContent(a.spec) {
			continue
		}
		items = append(items, annotationListItem{
			Label: labels[a.name],
			Title: a.spec.Title,
			HTML:  renderInlineMarkdown(a.spec.Text, rc.assetURLs),
		})
	}
	if len(items) == 0 {
		return "", nil
	}
	value, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("render: annotation list: %w", err)
	}
	var b strings.Builder
	b.WriteString("<bn-annotation-list")
	writeAttr(&b, "items", string(value))
	writeAttr(&b, "selected-style", list.SelectedStyle)
	b.WriteString("></bn-annotation-list>")
	return b.String(), nil
}
