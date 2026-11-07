package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/ref"
	reportspec "bino.bi/bino/internal/report/spec"
)

// smallMultipleSpec is the spec of a SmallMultiple: one base component drawn
// once per panel, on one dataset and one scale, as a baseline-aligned grid.
type smallMultipleSpec struct {
	ChartTitle        string                 `json:"chartTitle"`
	SelectedStyle     string                 `json:"selectedStyle"`
	I18nNamespace     string                 `json:"i18nNamespace"`
	Dataset           json.RawMessage        `json:"dataset"`
	Filter            string                 `json:"filter"`
	UnitScaling       json.RawMessage        `json:"unitScaling"`
	PercentageScaling json.RawMessage        `json:"percentageScaling"`
	Scale             json.RawMessage        `json:"scale"`
	Base              smallMultipleBase      `json:"base"`
	Caption           *string                `json:"caption"`
	Card              json.RawMessage        `json:"card"`
	Panels            smallMultiplePanelSpec `json:"panels"`
	Columns           int                    `json:"columns"`
	CellHeight        string                 `json:"cellHeight"`
	CellWidth         string                 `json:"cellWidth"`
	CellGap           string                 `json:"cellGap"`
	ShowBorders       *bool                  `json:"showBorders"`
}

type smallMultipleBase struct {
	Kind   string            `json:"kind"`
	Ref    string            `json:"ref"`
	Params map[string]string `json:"params"`
	Spec   json.RawMessage   `json:"spec"`
}

type smallMultiplePanelSpec struct {
	By    string              `json:"by"`
	Order string              `json:"order"`
	Limit int                 `json:"limit"`
	Param string              `json:"param"`
	Items []smallMultipleItem `json:"items"`
}

type smallMultipleItem struct {
	Label   string          `json:"label"`
	Filter  string          `json:"filter"`
	Dataset string          `json:"dataset"`
	Spec    json.RawMessage `json:"spec"`
	Card    json.RawMessage `json:"card"`
}

// panel is one cell of a SmallMultiple.
type panel struct {
	label string
	// value is the by value as a filter literal; empty for an item.
	value string
	// num is the by value when it is a number, so numbers sort as numbers.
	num   float64
	isNum bool
	item  *smallMultipleItem
	// sort keys of a by panel
	index float64
	sum   float64
}

const defaultPanelCaption = "${panel.label}"

// unitScaledKinds are the base kinds with unitScaling and percentageScaling.
var unitScaledKinds = map[string]bool{"ChartStructure": true, "ChartTime": true, "Table": true}

// SmallMultipleScaleProblems lists why the scale fields of a SmallMultiple do
// not give every panel the same scale. Lint and render share it.
func SmallMultipleScaleProblems(baseKind string, unitScaling, percentageScaling, scale json.RawMessage) []string {
	var out []string
	if s := scalarText(scale); s == "" || s == "auto" {
		out = append(out, "scale must be a fixed factor or \"none\", not "+describeScalar(s))
	}
	if unitScaledKinds[baseKind] {
		if s := scalarText(unitScaling); s == "" || s == "auto" {
			out = append(out, "unitScaling must be a number or a ScalingGroup name, not "+describeScalar(s))
		}
	}
	if len(percentageScaling) > 0 && scalarText(percentageScaling) == "auto" {
		out = append(out, "percentageScaling must not be \"auto\"")
	}
	return out
}

func describeScalar(s string) string {
	if s == "" {
		return "unset"
	}
	return strconv.Quote(s)
}

// scalarText is a JSON string or number as text, "" when unset or null.
func scalarText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return string(raw)
}

// renderSmallMultipleComponent expands a SmallMultiple into a grid with one
// cell per panel and renders it like a Grid.
func renderSmallMultipleComponent(s smallMultipleSpec, name string, rc *renderCtx) (string, error) {
	if problems := SmallMultipleScaleProblems(s.Base.Kind, s.UnitScaling, s.PercentageScaling, s.Scale); len(problems) > 0 {
		return "", fmt.Errorf("SmallMultiple %q: all panels must share one scale: %s", name, strings.Join(problems, "; "))
	}
	if s.Panels.Param != "" && s.Base.Ref == "" {
		return "", fmt.Errorf("SmallMultiple %q: panels.param needs base.ref", name)
	}
	var datasets reportspec.DatasetList
	if len(s.Dataset) > 0 {
		if err := json.Unmarshal(s.Dataset, &datasets); err != nil {
			return "", fmt.Errorf("SmallMultiple %q: dataset: %w", name, err)
		}
	}
	primary := ""
	if entries := datasets.Entries(); len(entries) > 0 {
		primary = entries[0].Ref
	}

	baseSpec, skipped, err := resolveSmallMultipleBase(s, rc)
	if err != nil {
		return "", fmt.Errorf("SmallMultiple %q: base: %w", name, err)
	}
	if skipped {
		return "", nil
	}
	panels, err := smallMultiplePanels(s, name, primary, specHasFilter(baseSpec), rc)
	if err != nil {
		return "", err
	}
	if len(panels) == 0 {
		rc.warn(fmt.Sprintf("SmallMultiple %q: no panels", name))
		return "", nil
	}
	warned := map[string]bool{}
	for _, p := range panels {
		ds := primary
		if p.item != nil && p.item.Dataset != "" {
			ds = p.item.Dataset
		}
		if !warned[ds] && hasDefaultFilter(rc.datasetDefaults[ds], s.Base.Kind) {
			warned[ds] = true
			rc.warn(fmt.Sprintf("SmallMultiple %q: dataset %q sets a default filter; the panel filters replace it", name, ds))
		}
	}

	columns := s.Columns
	if columns <= 0 {
		columns = int(math.Ceil(math.Sqrt(float64(len(panels)))))
	}
	rows := (len(panels) + columns - 1) / columns

	hasCard := isSet(s.Card)
	caption := defaultPanelCaption
	if hasCard {
		caption = "" // the card title names the panel
	}
	if s.Caption != nil {
		caption = *s.Caption
	}

	children := make([]gridChild, 0, len(panels))
	for i, p := range panels {
		cellSpec, err := smallMultipleCellSpec(s, p, i, baseSpec)
		if err != nil {
			return "", fmt.Errorf("SmallMultiple %q: panel %q: %w", name, p.label, err)
		}
		child := gridChild{
			Row:      stringOrInt(strconv.Itoa(i / columns)),
			Column:   stringOrInt(strconv.Itoa(i % columns)),
			Kind:     s.Base.Kind,
			Metadata: layoutChildMeta{Name: fmt.Sprintf("%s-%d", name, i+1)},
			Spec:     cellSpec,
			Caption:  replacePanelPlaceholders(caption, p, i, func(v string) string { return v }),
		}
		if p.item != nil && isSet(p.item.Card) && !hasCard {
			return "", fmt.Errorf("SmallMultiple %q: panel %q: panels.items[].card needs card", name, p.label)
		}
		if hasCard {
			cardSpec, err := smallMultipleCardSpec(s, p, i, child)
			if err != nil {
				return "", fmt.Errorf("SmallMultiple %q: panel %q: card: %w", name, p.label, err)
			}
			child.Kind = "LayoutCard"
			child.Spec = cardSpec
		}
		children = append(children, child)
	}

	showBorders := false
	if s.ShowBorders != nil {
		showBorders = *s.ShowBorders
	}
	hidden := false
	grid := gridSpec{
		ChartTitle:          s.ChartTitle,
		RowHeaders:          headerIDs("r", rows),
		ColumnHeaders:       headerIDs("c", columns),
		ShowRowHeaders:      &hidden,
		ShowColumnHeaders:   &hidden,
		ShowBorders:         &showBorders,
		CellGap:             s.CellGap,
		CellHeight:          orDefault(s.CellHeight, "equal"),
		CellWidth:           orDefault(s.CellWidth, "equal"),
		CellVerticalAlign:   "baseline",
		CellHorizontalAlign: "baseline",
		I18nNamespace:       s.I18nNamespace,
		SelectedStyle:       s.SelectedStyle,
		Children:            children,
	}
	return renderGridComponent(grid, rc)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// headerIDs are hidden header labels; bn-grid numbers them 0, 1, ... as slot ids.
func headerIDs(prefix string, n int) json.RawMessage {
	labels := make([]string, n)
	for i := range labels {
		labels[i] = prefix + strconv.Itoa(i)
	}
	data, err := json.Marshal(labels)
	if err != nil {
		return nil
	}
	return data
}

func hasDefaultFilter(defaults dataset.Defaults, kind string) bool {
	if _, ok := defaults[dataset.AnyToken]["filter"]; ok {
		return true
	}
	_, ok := defaults[kindTokenOf(kind)]["filter"]
	return ok
}

// smallMultiplePanels lists the panels: the by values in order, then the items.
func smallMultiplePanels(s smallMultipleSpec, name, primary string, baseFilter bool, rc *renderCtx) ([]panel, error) {
	var panels []panel
	if by := s.Panels.By; by != "" {
		if primary == "" || strings.HasPrefix(primary, "$") {
			return nil, fmt.Errorf("SmallMultiple %q: panels.by needs a DataSet as the first dataset, not %q", name, primary)
		}
		byPanels, err := panelsFromRows(rc.datasetRows[primary], by, s.Panels.Order)
		if err != nil {
			return nil, fmt.Errorf("SmallMultiple %q: dataset %q: %w", name, primary, err)
		}
		if s.Filter != "" || baseFilter {
			rc.warn(fmt.Sprintf("SmallMultiple %q: the panels, their order and limit come from all rows of dataset %q; filters only apply inside each panel. Filter the DataSet to leave panels out", name, primary))
		}
		if byPanels == nil {
			rc.warn(fmt.Sprintf("SmallMultiple %q: column %q is not in the rows of dataset %q (column names are case-sensitive)", name, by, primary))
		}
		if limit := s.Panels.Limit; limit > 0 && len(byPanels) > limit {
			rc.warn(fmt.Sprintf("SmallMultiple %q: limit %d dropped %d panel(s)", name, limit, len(byPanels)-limit))
			byPanels = byPanels[:limit]
		}
		panels = append(panels, byPanels...)
	}
	for i := range s.Panels.Items {
		item := &s.Panels.Items[i]
		if item.Dataset != "" {
			if rows, ok := rc.datasetRows[item.Dataset]; ok && isEmptyRows(rows) {
				rc.warn(fmt.Sprintf("SmallMultiple %q: panel %q: dataset %q has no rows", name, item.Label, item.Dataset))
			}
		}
		panels = append(panels, panel{label: item.Label, item: item})
	}
	return panels, nil
}

// specHasFilter reports whether a resolved spec sets a filter.
func specHasFilter(spec json.RawMessage) bool {
	var v struct {
		Filter string `json:"filter"`
	}
	return json.Unmarshal(spec, &v) == nil && strings.TrimSpace(v.Filter) != ""
}

func isEmptyRows(rows json.RawMessage) bool {
	var list []json.RawMessage
	return json.Unmarshal(rows, &list) == nil && len(list) == 0
}

// panelsFromRows returns one panel per distinct value of column by, in order.
// It is nil when no row has the column.
func panelsFromRows(rows json.RawMessage, by, order string) ([]panel, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(rows))
	dec.UseNumber()
	var list []map[string]any
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	key, desc := parsePanelOrder(order)
	indexColumn := by + "Index"
	byValue := map[string]*panel{}
	var panels []*panel
	hasIndex := false
	found := false
	for _, row := range list {
		v, ok := row[by]
		if !ok {
			continue
		}
		found = true
		label, literal, ok := panelValue(v)
		if !ok {
			continue // a null value has no panel
		}
		p := byValue[label]
		if p == nil {
			p = &panel{label: label, value: literal, index: math.Inf(1)}
			p.num, p.isNum = toFloat(v)
			_, isNumber := v.(json.Number)
			p.isNum = p.isNum && isNumber
			byValue[label] = p
			panels = append(panels, p)
		}
		if n, ok := toFloat(row[indexColumn]); ok {
			hasIndex = true
			p.index = math.Min(p.index, n)
		}
		if key != "dataset" && key != "label" {
			if n, ok := toFloat(row[key]); ok {
				p.sum += n
			}
		}
	}
	if !found {
		return nil, nil
	}
	byLabel := func(a, b *panel) bool {
		if a.isNum && b.isNum && a.num != b.num {
			return a.num < b.num
		}
		return a.label < b.label
	}
	less := byLabel
	switch {
	case key == "dataset" && hasIndex:
		less = func(a, b *panel) bool {
			if a.index != b.index {
				return a.index < b.index
			}
			return byLabel(a, b)
		}
	case key != "dataset" && key != "label":
		less = func(a, b *panel) bool {
			if a.sum != b.sum {
				return a.sum < b.sum
			}
			return byLabel(a, b)
		}
	}
	sort.SliceStable(panels, func(i, j int) bool {
		if desc {
			return less(panels[j], panels[i])
		}
		return less(panels[i], panels[j])
	})
	out := make([]panel, len(panels))
	for i, p := range panels {
		out[i] = *p
	}
	return out, nil
}

func parsePanelOrder(order string) (key string, desc bool) {
	fields := strings.Fields(order)
	if len(fields) == 0 {
		return "dataset", false
	}
	return fields[0], len(fields) > 1 && fields[1] == "desc"
}

// panelValue is the label of a by value and its AlaSQL literal: a string
// quoted with ' doubled, a number or boolean as is.
func panelValue(v any) (label, literal string, ok bool) {
	switch x := v.(type) {
	case string:
		return x, "'" + strings.ReplaceAll(x, "'", "''") + "'", true
	case json.Number:
		return x.String(), x.String(), true
	case bool:
		return strconv.FormatBool(x), strconv.FormatBool(x), true
	}
	return "", "", false
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

// resolveSmallMultipleBase is the base spec of every panel: the referenced
// document with the base params (panels.param gets the ${panel.label}
// placeholder) and the base.spec overrides, or the inline base.spec.
func resolveSmallMultipleBase(s smallMultipleSpec, rc *renderCtx) (spec json.RawMessage, skipped bool, err error) {
	if s.Base.Ref == "" {
		if len(s.Base.Spec) == 0 {
			return json.RawMessage("{}"), false, nil
		}
		return s.Base.Spec, false, nil
	}
	params := map[string]string{}
	for k, v := range s.Base.Params {
		params[k] = v
	}
	if s.Panels.Param != "" {
		// Replaced per panel like the other placeholders: quoted as SQL in the
		// filter and as JSON elsewhere.
		params[s.Panels.Param] = "${panel.label}"
	}
	res, err := ref.Resolve(ref.Ref{Kind: s.Base.Kind, Name: s.Base.Ref, Spec: s.Base.Spec, Params: params}, rc.refOptions())
	if err != nil {
		return nil, false, err
	}
	if res.Skipped {
		return nil, true, nil
	}
	return res.Spec, false, nil
}

// smallMultipleCellSpec builds the spec of one panel. Precedence, lowest
// first: the base, the item spec, the shared fields of the SmallMultiple, the
// generated filter. The shared fields come after the item, so no panel leaves
// the shared scale.
func smallMultipleCellSpec(s smallMultipleSpec, p panel, i int, base json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, fmt.Errorf("base spec is not an object: %w", err)
	}
	// The base filter is SQL: a label in it is quoted as an SQL string.
	baseFilter := ""
	if raw, ok := fields["filter"]; ok {
		if err := json.Unmarshal(raw, &baseFilter); err != nil {
			return nil, fmt.Errorf("base filter: %w", err)
		}
		baseFilter = replacePanelPlaceholders(baseFilter, p, i, sqlEscape)
		delete(fields, "filter")
	}
	baseJSON, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	baseJSON = []byte(replacePanelPlaceholders(string(baseJSON), p, i, jsonEscape))
	if p.item != nil && len(p.item.Spec) > 0 {
		itemSpec := replacePanelPlaceholders(string(p.item.Spec), p, i, jsonEscape)
		if baseJSON, err = ref.MergeSpec(baseJSON, json.RawMessage(itemSpec)); err != nil {
			return nil, fmt.Errorf("item spec: %w", err)
		}
	}
	fields = map[string]json.RawMessage{}
	if err := json.Unmarshal(baseJSON, &fields); err != nil {
		return nil, fmt.Errorf("base spec: %w", err)
	}
	delete(fields, "filter") // the generated filter is the whole filter

	shared := map[string]json.RawMessage{"scale": s.Scale}
	if len(s.Dataset) > 0 {
		shared["dataset"] = s.Dataset
	}
	if p.item != nil && p.item.Dataset != "" {
		shared["dataset"] = jsonString(p.item.Dataset)
	}
	if unitScaledKinds[s.Base.Kind] {
		shared["unitScaling"] = s.UnitScaling
		if len(s.PercentageScaling) > 0 {
			shared["percentageScaling"] = s.PercentageScaling
		}
	}
	for k, v := range shared {
		fields[k] = v
	}
	merged, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}

	panelPart := ""
	if p.item != nil {
		panelPart = p.item.Filter
	} else {
		panelPart = "[" + s.Panels.By + "] = " + p.value
	}
	// Always set, also when empty: a default filter of the dataset must not
	// narrow one panel and not the others.
	filterJSON := jsonString(joinFilters(s.Filter, baseFilter, panelPart))
	return ref.MergeSpec(merged, json.RawMessage(`{"filter":`+string(filterJSON)+`}`))
}

// cardLayoutFields are LayoutCard fields the SmallMultiple sets itself: a
// panel card holds exactly one component.
var cardLayoutFields = []string{"children", "cardLayout", "cardCustomTemplate", "cardGridGap"}

// smallMultipleCardSpec wraps the component of a panel in a LayoutCard: the
// card of the SmallMultiple, then the card of the item. The card has no
// border and names the panel in its title unless the card says otherwise.
func smallMultipleCardSpec(s smallMultipleSpec, p panel, i int, component gridChild) (json.RawMessage, error) {
	card := json.RawMessage(replacePanelPlaceholders(string(s.Card), p, i, jsonEscape))
	if p.item != nil && isSet(p.item.Card) {
		var err error
		itemCard := replacePanelPlaceholders(string(p.item.Card), p, i, jsonEscape)
		if card, err = ref.MergeSpec(card, json.RawMessage(itemCard)); err != nil {
			return nil, fmt.Errorf("item card: %w", err)
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(card, &fields); err != nil {
		return nil, fmt.Errorf("not an object: %w", err)
	}
	for _, f := range cardLayoutFields {
		if _, ok := fields[f]; ok {
			return nil, fmt.Errorf("%s is set by the SmallMultiple", f)
		}
	}
	if _, ok := fields["titleBusinessUnit"]; !ok {
		// the field is Markdown; a label is plain text, as in a caption
		fields["titleBusinessUnit"] = jsonString(markdownEscape(p.label))
	}
	if _, ok := fields["cardShowBorder"]; !ok {
		fields["cardShowBorder"] = json.RawMessage("false")
	}
	if _, ok := fields["cardFitToContent"]; !ok && s.CellHeight == "auto" {
		// a card fills its cell, and an auto row takes its height from the content
		fields["cardFitToContent"] = json.RawMessage("true")
	}
	children, err := json.Marshal([]map[string]any{{
		"kind":     component.Kind,
		"metadata": map[string]string{"name": component.Metadata.Name},
		"spec":     component.Spec,
	}})
	if err != nil {
		return nil, err
	}
	fields["children"] = children
	return json.Marshal(fields)
}

// isSet reports whether a raw JSON field is present and not null.
func isSet(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && string(raw) != "null"
}

// markdownEscape makes v literal text in inline Markdown: every ASCII
// punctuation character gets a backslash.
func markdownEscape(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r < 128 && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// jsonString is v as a JSON string.
func jsonString(v string) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil { // a string always marshals
		return json.RawMessage(`""`)
	}
	return data
}

// jsonEscape makes v safe inside a JSON string.
func jsonEscape(v string) string {
	data := jsonString(v)
	return string(data[1 : len(data)-1])
}

// sqlEscape makes v safe inside an SQL string literal.
func sqlEscape(v string) string {
	return strings.ReplaceAll(v, "'", "''")
}

// joinFilters ANDs the non-empty parts, each in parentheses when there are several.
func joinFilters(parts ...string) string {
	var kept []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	if len(kept) < 2 {
		return strings.Join(kept, "")
	}
	return "(" + strings.Join(kept, ") AND (") + ")"
}

// replacePanelPlaceholders replaces ${panel.label}, ${panel.value} and
// ${panel.index} (1-based); escape makes a value safe for the target text.
func replacePanelPlaceholders(text string, p panel, i int, escape func(string) string) string {
	if !strings.Contains(text, "${panel.") {
		return text
	}
	return strings.NewReplacer(
		"${panel.label}", escape(p.label),
		"${panel.value}", escape(p.label),
		"${panel.index}", strconv.Itoa(i+1),
	).Replace(text)
}
