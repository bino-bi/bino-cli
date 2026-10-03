package graph

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	reportspec "bino.bi/bino/internal/report/spec"
)

// ColumnRef is one dataset column a component reads.
type ColumnRef struct {
	// Dataset is the binding as written, "$name" for a DataSource.
	Dataset string `json:"dataset"`
	// Column is the column name, ColumnAuto, or an unresolved token such as
	// "${PARAM}" or "inherited-page".
	Column string `json:"column"`
	// Role is one of the Role* constants.
	Role string `json:"role"`
	// Field is the top-level spec field; empty for implicit reads.
	Field string `json:"field,omitempty"`
}

// ColumnAuto marks a field the engine resolves from the rows at render time.
const ColumnAuto = "auto"

// Column roles. columnRefs returns refs grouped in this order.
const (
	RoleScenario  = "scenario"
	RoleVariance  = "variance"
	RoleMeasure   = "measure"
	RoleGroup     = "group"
	RoleOrder     = "order"
	RoleFilter    = "filter"
	RoleAttribute = "attribute"
	RoleTemplate  = "template"
	RoleImplicit  = "implicit"
)

var roleOrder = map[string]int{
	RoleScenario: 0, RoleVariance: 1, RoleMeasure: 2, RoleGroup: 3, RoleOrder: 4,
	RoleFilter: 5, RoleAttribute: 6, RoleTemplate: 7, RoleImplicit: 8,
}

// The rules below mirror bn-template-engine v1.0.0-next.27.

const (
	inheritedClosest = "inherited-closest"
	inheritedPage    = "inherited-page"
)

// titles are the LayoutPage / LayoutCard fields that Table, ChartStructure
// and ChartTime inherit through inherited-closest and inherited-page.
type titles struct {
	scenarios, variances, order string
}

// ancestors are the nearest LayoutCard or LayoutPage and the LayoutPage
// around a component; nil when there is none.
type ancestors struct {
	closest, page *titles
}

// inherit returns the ancestor value for an inherited-* keyword. A keyword
// that does not resolve stays as written, as in the engine.
func (a ancestors) inherit(v string, pick func(*titles) string) string {
	var t *titles
	switch strings.TrimSpace(v) {
	case inheritedClosest:
		t = a.closest
	case inheritedPage:
		t = a.page
	default:
		return v
	}
	if t == nil || pick(t) == "" {
		return v
	}
	return pick(t)
}

// resolve resolves a LayoutCard's own inherited title fields.
func (a ancestors) resolve(t titles) titles {
	return titles{
		scenarios: a.inherit(t.scenarios, func(t *titles) string { return t.scenarios }),
		variances: a.inherit(t.variances, func(t *titles) string { return t.variances }),
		order:     a.inherit(t.order, func(t *titles) string { return t.order }),
	}
}

// parseTitles reads the inheritable title fields of a page or card spec.
func parseTitles(raw json.RawMessage) titles {
	f := specFields(raw)
	return titles{
		scenarios: f.joined("titleScenarios"),
		variances: f.joined("titleVariances"),
		order:     f.str("titleOrder"),
	}
}

// fields is a component spec decoded field by field, so one malformed field
// never hides the others.
type fields map[string]json.RawMessage

// specFields decodes a bare spec or a full manifest with a spec wrapper.
func specFields(raw json.RawMessage) fields {
	var f fields
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	if inner, ok := f["spec"]; ok {
		if _, isDoc := f["metadata"]; isDoc {
			return specFields(inner)
		}
	}
	return f
}

func (f fields) str(name string) string {
	var s string
	if json.Unmarshal(f[name], &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func (f fields) boolean(name string) bool {
	var v bool
	return json.Unmarshal(f[name], &v) == nil && v
}

// joined returns a string-or-list field as the comma string the engine sees.
func (f fields) joined(name string) string {
	var s reportspec.StringOrSlice
	if json.Unmarshal(f[name], &s) != nil {
		return ""
	}
	return strings.TrimSpace(s.String())
}

// decode unmarshals one field into v and reports success.
func (f fields) decode(name string, v any) bool {
	raw, ok := f[name]
	return ok && json.Unmarshal(raw, v) == nil
}

// split splits a comma list the way the engine does.
func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isAuto(s string) bool {
	return s == "" || strings.EqualFold(s, ColumnAuto)
}

// use is one column read before it is attributed to datasets.
type use struct{ role, field, column string }

type uses []use

func (u *uses) add(role, field string, cols ...string) {
	for _, c := range cols {
		if c != "" {
			*u = append(*u, use{role, field, c})
		}
	}
}

var (
	slotToken     = regexp.MustCompile(`^(?:ac|pp|fc|pl)[1-4]$`)
	varianceToken = regexp.MustCompile(`^(?:dr|d)((?:ac|pp|fc|pl)\d)_((?:ac|pp|fc|pl)\d)_(?:pos|neg|neu)$`)
	measureToken  = regexp.MustCompile(`^(?:dr|d)((?:ac|pp|fc|pl)[1-4])_((?:ac|pp|fc|pl)[1-4])(?:_(?:pos|neg|neu))?$`)
)

// addTokens adds a scenario or variance list. A variance reads both slots;
// any other token is a column as written (the engine pastes it into SQL).
func (u *uses) addTokens(role, field, list string) {
	for _, tok := range split(list) {
		if m := varianceToken.FindStringSubmatch(tok); m != nil {
			u.add(RoleVariance, field, m[1], m[2])
			continue
		}
		u.add(role, field, tok)
	}
}

// addScenarios adds the scenarios and variances of Table, ChartStructure and
// ChartTime after inheritance. Blank or auto scenarios are auto-detected;
// blank variances mean none.
func (u *uses) addScenarios(f fields, anc ancestors) {
	scenarios := anc.inherit(f.joined("scenarios"), func(t *titles) string { return t.scenarios })
	if isAuto(scenarios) {
		u.add(RoleScenario, "scenarios", ColumnAuto)
	} else {
		u.addTokens(RoleScenario, "scenarios", scenarios)
	}
	u.addTokens(RoleVariance, "variances", anc.inherit(f.joined("variances"), func(t *titles) string { return t.variances }))
}

// addOrder adds an order field; blankIsAuto is false for the Table, whose
// blank order is its implicit category.
func (u *uses) addOrder(order string, blankIsAuto bool) {
	switch {
	case order == "" && !blankIsAuto:
	case isAuto(order):
		u.add(RoleOrder, "order", ColumnAuto)
	default:
		for _, tok := range split(order) {
			u.add(RoleOrder, "order", dimColumn(tok))
		}
	}
}

// dimColumn maps a lowercase level or order value to its column name.
func dimColumn(v string) string {
	switch strings.ToLower(v) {
	case "rowgroup":
		return "rowGroup"
	case "rowgroupindex":
		return "rowGroupIndex"
	case "category":
		return "category"
	case "categoryindex":
		return "categoryIndex"
	case "subcategory":
		return "subCategory"
	case "subcategoryindex":
		return "subCategoryIndex"
	}
	return v
}

// measure returns the token of an XY or bullet mapping: a bare string or
// an object with a measure key.
func measure(f fields, name string) string {
	if s := f.str(name); s != "" {
		return s
	}
	var obj struct {
		Measure string `json:"measure"`
	}
	if f.decode(name, &obj) {
		return strings.TrimSpace(obj.Measure)
	}
	return ""
}

func (u *uses) addMeasure(field, tok string) {
	if m := measureToken.FindStringSubmatch(tok); m != nil {
		u.add(RoleVariance, field, m[1], m[2])
		return
	}
	u.add(RoleMeasure, field, tok)
}

var attributeExpr = regexp.MustCompile(`^\s*(?:set|first|last|min|max|avg|sum)\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)\s*$`)

// attributeExpressions returns the Table attribute expressions in written
// order. The JSON-object string form is read token by token because a Go
// map would lose the order.
func attributeExpressions(f fields) []string {
	var list reportspec.AttributesList
	if !f.decode("attributes", &list) {
		return nil
	}
	var out []string
	for _, item := range list.Items {
		out = append(out, item.Expression)
	}
	if list.Raw == "" {
		return out
	}
	dec := json.NewDecoder(strings.NewReader(list.Raw))
	if _, err := dec.Token(); err != nil {
		return out
	}
	for dec.More() {
		var expr string
		if _, err := dec.Token(); err != nil {
			return out
		}
		if err := dec.Decode(&expr); err != nil {
			return out
		}
		out = append(out, expr)
	}
	return out
}

func tableUses(f fields, anc ancestors) uses {
	var u uses
	u.addScenarios(f, anc)
	u.addOrder(anc.inherit(f.str("order"), func(t *titles) string { return t.order }), false)
	if f.boolean("grouped") {
		u.add(RoleGroup, "grouped", "rowGroup", "rowGroupIndex")
	}
	var thereof reportspec.ThereofList
	if f.decode("thereof", &thereof) && len(thereof) > 0 {
		u.add(RoleGroup, "thereof", "rowGroup", "category", "subCategory")
	}
	var partof reportspec.PartofList
	if typ := strings.ToLower(f.str("type")); typ != "" && typ != "list" && f.decode("partof", &partof) && len(partof) > 0 {
		u.add(RoleGroup, "partof", "rowGroup", "category")
	}
	var columnthereof reportspec.ColumnthereofList
	if f.decode("columnthereof", &columnthereof) && len(columnthereof) > 0 {
		for _, item := range columnthereof {
			u.add(RoleScenario, "columnthereof", strings.TrimSpace(item.Scenario))
		}
		u.add(RoleGroup, "columnthereof", "columnGroup", "columnSubGroup", "rowGroup", "subCategory")
	} else {
		switch interval := strings.ToLower(f.str("interval")); interval {
		case "", "none":
		case "setname":
			u.add(RoleGroup, "interval", "setname")
		default:
			u.add(RoleGroup, "interval", "date")
		}
	}
	for _, expr := range attributeExpressions(f) {
		if m := attributeExpr.FindStringSubmatch(expr); m != nil {
			u.add(RoleAttribute, "attributes", m[1])
		}
	}
	u.add(RoleFilter, "filter", filterColumns(f.str("filter"))...)
	u.add(RoleImplicit, "", "category", "categoryIndex", "operation")
	return u
}

func chartStructureUses(f fields, anc ancestors) uses {
	var u uses
	u.addScenarios(f, anc)
	level := strings.ToLower(f.str("level"))
	if isAuto(level) {
		u.add(RoleGroup, "level", ColumnAuto)
	} else {
		u.add(RoleGroup, "level", dimColumn(level))
	}
	u.addOrder(anc.inherit(f.str("order"), func(t *titles) string { return t.order }), true)
	var stack struct {
		By string `json:"by"`
	}
	if f.decode("stack", &stack) && strings.EqualFold(stack.By, "dimensions") {
		switch level {
		case "", ColumnAuto:
			u.add(RoleGroup, "stack", ColumnAuto)
		case "rowgroup", "rowgroupindex":
			u.add(RoleGroup, "stack", "category")
		case "category", "categoryindex":
			u.add(RoleGroup, "stack", "subCategory")
		}
	}
	u.add(RoleFilter, "filter", filterColumns(f.str("filter"))...)
	u.add(RoleImplicit, "", "operation")
	return u
}

func chartTimeUses(f fields, anc ancestors) uses {
	var u uses
	u.addScenarios(f, anc)
	u.add(RoleFilter, "filter", filterColumns(f.str("filter"))...)
	u.add(RoleImplicit, "", "date", "operation")
	return u
}

// xyUses covers ChartScatter and ChartBubble. Each level column also needs
// its index twin: the engine drops a dimension without a numeric index.
func xyUses(f fields, bubble bool) uses {
	var u uses
	axes := []string{"x", "y"}
	if bubble {
		axes = append(axes, "size", "share")
	}
	for _, axis := range axes {
		if tok := measure(f, axis); tok != "" {
			u.addMeasure(axis, tok)
		}
	}
	switch family := strings.ToLower(f.str("compareWith")); family {
	case "ac", "pp", "fc", "pl":
		if !bubble {
			break
		}
		// The comparison reads the same slot number of the other family
		// for x, y and size; share has no comparison.
		for _, axis := range axes[:3] {
			if tok := measure(f, axis); slotToken.MatchString(tok) {
				u.add(RoleMeasure, "compareWith", family+tok[2:])
			}
		}
	}
	addLevel := func(field, col string) {
		u.add(RoleGroup, field, col)
		if col != ColumnAuto {
			u.add(RoleImplicit, "", col+"Index")
		}
	}
	point := ColumnAuto
	switch strings.ToLower(f.str("level")) {
	case "category":
		point = "category"
	case "subcategory":
		point = "subCategory"
	}
	addLevel("level", point)
	// A blank or auto series level is the parent of the point level.
	series := map[string]string{"category": "rowGroup", "subCategory": "category", ColumnAuto: ColumnAuto}[point]
	switch strings.ToLower(f.str("seriesLevel")) {
	case "none":
		series = ""
	case "rowgroup":
		series = "rowGroup"
	case "category":
		series = "category"
	}
	if series != "" {
		addLevel("seriesLevel", series)
	}
	var facet *struct {
		Level string `json:"level"`
	}
	if f.decode("facet", &facet) && facet != nil {
		if strings.EqualFold(facet.Level, "rowgroup") {
			addLevel("facet", "rowGroup")
		} else {
			addLevel("facet", "category")
		}
	}
	u.add(RoleFilter, "filter", filterColumns(f.str("filter"))...)
	return u
}

func chartBulletUses(f fields) uses {
	var u uses
	for _, field := range []string{"actual", "target"} {
		if tok := measure(f, field); isAuto(tok) {
			u.add(RoleMeasure, field, ColumnAuto)
		} else {
			u.add(RoleMeasure, field, tok)
		}
	}
	if level := f.str("level"); isAuto(level) {
		u.add(RoleGroup, "level", ColumnAuto)
	} else {
		u.add(RoleGroup, "level", dimColumn(level))
	}
	u.addOrder(f.str("order"), true)
	u.add(RoleFilter, "filter", filterColumns(f.str("filter"))...)
	u.add(RoleImplicit, "", "operation")
	return u
}

// columnRefs returns the dataset columns a component reads. datasets are
// the component's bindings as written. It never fails: a field that does
// not decode gives no refs.
func columnRefs(kind string, raw json.RawMessage, datasets []string, anc ancestors) []ColumnRef {
	f := specFields(raw)
	if f == nil {
		return nil
	}
	var refs []ColumnRef
	switch kind {
	case "Text", "Label":
		for _, m := range templateColumns(f.str("value")) {
			refs = append(refs, ColumnRef{Dataset: m[0], Column: m[1], Role: RoleTemplate, Field: "value"})
		}
	case "Tree":
		refs = treeRefs(f, anc)
	default:
		var u uses
		switch kind {
		case "Table":
			u = tableUses(f, anc)
		case "ChartStructure":
			u = chartStructureUses(f, anc)
		case "ChartTime":
			u = chartTimeUses(f, anc)
		case "ChartScatter":
			u = xyUses(f, false)
		case "ChartBubble":
			u = xyUses(f, true)
		case "ChartBullet":
			u = chartBulletUses(f)
		}
		for _, ds := range datasets {
			for _, x := range u {
				refs = append(refs, ColumnRef{Dataset: ds, Column: x.column, Role: x.role, Field: x.field})
			}
		}
	}
	return sortRefs(refs)
}

// treeRefs collects the refs of inline tree nodes. Each node uses its own
// datasets; nodes with a ref have no dataset edge in the graph and are
// skipped.
func treeRefs(f fields, anc ancestors) []ColumnRef {
	var nodes []treeNodeSpec
	if !f.decode("nodes", &nodes) {
		return nil
	}
	var refs []ColumnRef
	for _, node := range nodes {
		if node.Ref != "" || len(node.Spec) == 0 {
			continue
		}
		var p struct {
			Dataset reportspec.DatasetList `json:"dataset"`
		}
		var datasets []string
		if json.Unmarshal(node.Spec, &p) == nil {
			datasets = p.Dataset.Strings()
		}
		for _, r := range columnRefs(node.Kind, node.Spec, datasets, anc) {
			if r.Field != "" {
				r.Field = "nodes[" + node.ID + "]." + r.Field
			}
			refs = append(refs, r)
		}
	}
	return refs
}

// sortRefs drops duplicates and groups refs by dataset (first-seen order)
// and role, keeping rule order inside a group.
func sortRefs(refs []ColumnRef) []ColumnRef {
	seen := make(map[ColumnRef]bool, len(refs))
	dsOrder := map[string]int{}
	out := refs[:0]
	for _, r := range refs {
		if seen[r] {
			continue
		}
		seen[r] = true
		if _, ok := dsOrder[r.Dataset]; !ok {
			dsOrder[r.Dataset] = len(dsOrder)
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if dsOrder[out[i].Dataset] != dsOrder[out[j].Dataset] {
			return dsOrder[out[i].Dataset] < dsOrder[out[j].Dataset]
		}
		return roleOrder[out[i].Role] < roleOrder[out[j].Role]
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// filterToken lexes an AlaSQL WHERE fragment: strings, ${PARAM}, comments,
// quoted identifiers, words (group 3 prefix, 4 name, 5 call) and numbers.
var filterToken = regexp.MustCompile(`'(?:\\.|''|[^'\\])*'|"(?:\\.|""|[^"\\])*"|\$\{[^}]*\}|--[^\n]*|\[([^\]']*)\]|` +
	"`([^`']*)`" + `|([@:$]?)([A-Za-z_][A-Za-z0-9_]*)(\s*\()?|\d+(?:\.\d*)?(?:[eE][+-]?\d+)?`)

var filterKeywords = map[string]bool{
	"AND": true, "OR": true, "NOT": true, "IN": true, "IS": true, "NULL": true, "LIKE": true, "ILIKE": true,
	"BETWEEN": true, "TRUE": true, "FALSE": true, "CASE": true, "WHEN": true, "THEN": true, "ELSE": true,
	"END": true, "EXISTS": true, "ANY": true, "ALL": true, "SOME": true, "ESCAPE": true, "REGEXP": true,
	"GLOB": true, "AS": true,
}

// filterColumns returns the column names in a component filter. Names keep
// their case: AlaSQL columns are case sensitive.
func filterColumns(filter string) []string {
	var out []string
	for _, m := range filterToken.FindAllStringSubmatch(filter, -1) {
		switch {
		case m[1] != "":
			out = append(out, m[1])
		case m[2] != "":
			out = append(out, m[2])
		case m[4] != "" && m[3] == "" && m[5] == "" && !filterKeywords[strings.ToUpper(m[4])]:
			out = append(out, m[4])
		}
	}
	return out
}

// templateRef matches data.<ds>[i].<col> and data['<ds>'][i].<col> in a
// Text template; the row index is required, so data.ds.length is skipped.
var templateRef = regexp.MustCompile(`(?:^|[^\w$])data(?:\.([A-Za-z_$][\w$]*)|\[\s*['"]([^'"]+)['"]\s*\])\s*\[[^\]]*\]\s*\??\.([A-Za-z_$][\w$]*)`)

// templateColumns returns the [dataset, column] pairs a template reads.
func templateColumns(value string) [][2]string {
	matches := templateRef.FindAllStringSubmatch(value, -1)
	out := make([][2]string, 0, len(matches))
	for _, m := range matches {
		ds := m[1]
		if ds == "" {
			ds = m[2]
		}
		out = append(out, [2]string{ds, m[3]})
	}
	return out
}
