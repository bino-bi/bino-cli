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
}

// panel is one cell of a SmallMultiple.
type panel struct {
	label string
	// value is the by value as a filter literal; empty for an item.
	value string
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

	panels, err := smallMultiplePanels(s, name, primary, rc)
	if err != nil {
		return "", err
	}
	if len(panels) == 0 {
		rc.warn(fmt.Sprintf("SmallMultiple %q: no panels", name))
		return "", nil
	}
	if defaults := rc.datasetDefaults[primary]; hasDefaultFilter(defaults, s.Base.Kind) {
		rc.warn(fmt.Sprintf("SmallMultiple %q: dataset %q sets a default filter; the panel filters replace it", name, primary))
	}

	columns := s.Columns
	if columns <= 0 {
		columns = int(math.Ceil(math.Sqrt(float64(len(panels)))))
	}
	rows := (len(panels) + columns - 1) / columns

	caption := defaultPanelCaption
	if s.Caption != nil {
		caption = *s.Caption
	}

	var shared json.RawMessage // the resolved base when it is the same for every panel
	children := make([]gridChild, 0, len(panels))
	for i, p := range panels {
		baseSpec := shared
		if baseSpec == nil {
			resolved, skipped, err := resolveSmallMultipleBase(s, p, rc)
			if err != nil {
				return "", fmt.Errorf("SmallMultiple %q: base: %w", name, err)
			}
			if skipped {
				return "", nil
			}
			baseSpec = resolved
			if s.Panels.Param == "" {
				shared = resolved
			}
		}
		cellSpec, err := smallMultipleCellSpec(s, p, i, baseSpec)
		if err != nil {
			return "", fmt.Errorf("SmallMultiple %q: panel %q: %w", name, p.label, err)
		}
		children = append(children, gridChild{
			Row:      stringOrInt(strconv.Itoa(i / columns)),
			Column:   stringOrInt(strconv.Itoa(i % columns)),
			Kind:     s.Base.Kind,
			Metadata: layoutChildMeta{Name: fmt.Sprintf("%s-%d", name, i+1)},
			Spec:     cellSpec,
			Caption:  replacePanelPlaceholders(caption, p, i, func(v string) string { return v }),
		})
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
	data, _ := json.Marshal(labels)
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
func smallMultiplePanels(s smallMultipleSpec, name, primary string, rc *renderCtx) ([]panel, error) {
	var panels []panel
	if by := s.Panels.By; by != "" {
		if primary == "" || strings.HasPrefix(primary, "$") {
			return nil, fmt.Errorf("SmallMultiple %q: panels.by needs a DataSet as the first dataset, not %q", name, primary)
		}
		byPanels, err := panelsFromRows(rc.datasetRows[primary], by, s.Panels.Order)
		if err != nil {
			return nil, fmt.Errorf("SmallMultiple %q: dataset %q: %w", name, primary, err)
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
	less := func(a, b *panel) bool { return a.label < b.label }
	switch {
	case key == "dataset" && hasIndex:
		less = func(a, b *panel) bool {
			if a.index != b.index {
				return a.index < b.index
			}
			return a.label < b.label
		}
	case key != "dataset" && key != "label":
		less = func(a, b *panel) bool {
			if a.sum != b.sum {
				return a.sum < b.sum
			}
			return a.label < b.label
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

// resolveSmallMultipleBase is the base spec of a panel: the referenced
// document with the base params (plus the panel value as panels.param) and
// the base.spec overrides, or the inline base.spec.
func resolveSmallMultipleBase(s smallMultipleSpec, p panel, rc *renderCtx) (json.RawMessage, bool, error) {
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
		params[s.Panels.Param] = p.label
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
// first: the base, the shared fields of the SmallMultiple, the item spec, the
// generated filter.
func smallMultipleCellSpec(s smallMultipleSpec, p panel, i int, base json.RawMessage) (json.RawMessage, error) {
	jsonEscape := func(v string) string {
		data, _ := json.Marshal(v)
		return string(data[1 : len(data)-1])
	}
	spec := json.RawMessage(replacePanelPlaceholders(string(base), p, i, jsonEscape))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(spec, &fields); err != nil {
		return nil, fmt.Errorf("base spec is not an object: %w", err)
	}
	baseFilter := ""
	if raw, ok := fields["filter"]; ok {
		_ = json.Unmarshal(raw, &baseFilter)
	}

	shared := map[string]json.RawMessage{"scale": s.Scale}
	if len(s.Dataset) > 0 {
		shared["dataset"] = s.Dataset
	}
	if p.item != nil && p.item.Dataset != "" {
		data, _ := json.Marshal(p.item.Dataset)
		shared["dataset"] = data
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
	if p.item != nil && len(p.item.Spec) > 0 {
		itemSpec := replacePanelPlaceholders(string(p.item.Spec), p, i, jsonEscape)
		if merged, err = ref.MergeSpec(merged, json.RawMessage(itemSpec)); err != nil {
			return nil, fmt.Errorf("item spec: %w", err)
		}
	}

	panelPart := ""
	if p.item != nil {
		panelPart = p.item.Filter
	} else {
		panelPart = s.Panels.By + " = " + p.value
	}
	filter := joinFilters(s.Filter, baseFilter, panelPart)
	if filter == "" {
		return merged, nil
	}
	filterJSON, _ := json.Marshal(filter)
	return ref.MergeSpec(merged, json.RawMessage(`{"filter":`+string(filterJSON)+`}`))
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
