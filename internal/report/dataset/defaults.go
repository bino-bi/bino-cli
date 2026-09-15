package dataset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Dataset defaults
//
// A component's primary dataset may fill the spec fields its author left
// unset through columns named `_spec_<token>_<field>` (one kind) or
// `_spec_any_<field>` (every kind that has the field). The executor folds
// them once per dataset into a Defaults map; the renderer merges that map
// under the author's spec.
//
// An object-valued field (Table thereof, partof, columnthereof; chart stack;
// scatter and bubble axis objects) takes one of three forms per dataset:
//   - a JSON cell in `_spec_<token>_<field>`;
//   - index columns `_spec_<token>_<field>_<i>_<key>` (a numeric segment is a
//     zero-based list index, any other a key), reassembled into the value;
//   - a row-keyed marker: for a list of objects keyed by the row dimensions
//     (thereof, partof) the cell holds a level such as `category` on the rows
//     to expand, and the entry is built from that row's own dimension values;
//     for columnthereof the cell holds the scenario on the rows whose
//     columnGroup should be spread.

// DefaultsPrefix starts every dataset-default column. It is matched
// case-sensitively.
const DefaultsPrefix = "_spec_"

// Defaults is the dataset-default map of one dataset: kind token (or "any")
// -> spec field -> JSON value in the shape the field's schema expects.
type Defaults map[string]map[string]json.RawMessage

// rowLevels and columnLevels are the dimension hierarchies a marker cell
// refers to, outermost first (see standardColumns in schema.go).
var (
	rowLevels    = []string{"rowGroup", "category", "subCategory"}
	columnLevels = []string{"columnGroup", "columnSubGroup"}
)

// columnthereofKeys is the item shape of the column-keyed marker form.
var columnthereofKeys = []string{"name", "scenario", "subGroups"}

// specColumn is one parsed `_spec_` column.
type specColumn struct {
	name  string
	token string
	field string
	rest  []string // path segments after the field (index form)
}

// fieldGroup is every column of one (token, field) pair.
type fieldGroup struct {
	token, field string
	plain        *specColumn  // `_spec_<token>_<field>`
	index        []specColumn // `_spec_<token>_<field>_...`
}

// fold carries the state of one FoldDefaults call.
type fold struct {
	name     string
	rows     []map[string]json.RawMessage
	warnings []Warning
}

func (f *fold) warn(format string, args ...any) {
	f.warnings = append(f.warnings, Warning{DataSet: f.name, Message: fmt.Sprintf(format, args...)})
}

// FoldDefaults reads the `_spec_` columns of the serialized rows. Across rows,
// NULL and empty cells are ignored; a list field merges the distinct values
// in row order; any other field must hold one value, otherwise it is dropped
// with a warning. Unknown tokens, unknown fields, unparseable cells and mixed
// forms of one object field are warnings too; never an error.
func FoldDefaults(name string, data json.RawMessage) (Defaults, []Warning) {
	f := &fold{name: name}
	if err := json.Unmarshal(data, &f.rows); err != nil || len(f.rows) == 0 {
		return nil, nil
	}

	groups := f.groupColumns()
	var out Defaults
	for _, g := range groups {
		value, ok := f.foldGroup(g)
		if !ok {
			continue
		}
		if out == nil {
			out = Defaults{}
		}
		if out[g.token] == nil {
			out[g.token] = map[string]json.RawMessage{}
		}
		out[g.token][g.field] = value
	}
	return out, f.warnings
}

// groupColumns parses every `_spec_` column of the rows and groups them by
// (token, field), in a deterministic order.
func (f *fold) groupColumns() []*fieldGroup {
	seen := map[string]bool{}
	var columns []string
	for _, row := range f.rows {
		for col := range row {
			if strings.HasPrefix(col, DefaultsPrefix) && !seen[col] {
				seen[col] = true
				columns = append(columns, col)
			}
		}
	}
	sort.Strings(columns)

	byKey := map[string]*fieldGroup{}
	var groups []*fieldGroup
	for _, col := range columns {
		segments := strings.Split(strings.TrimPrefix(col, DefaultsPrefix), "_")
		if len(segments) < 2 || segments[1] == "" {
			f.warn("%s: expected _spec_<kind>_<field>", col)
			continue
		}
		sc := specColumn{name: col, token: segments[0], field: segments[1], rest: segments[2:]}
		key := sc.token + "\x00" + sc.field
		g := byKey[key]
		if g == nil {
			g = &fieldGroup{token: sc.token, field: sc.field}
			byKey[key] = g
			groups = append(groups, g)
		}
		if len(sc.rest) == 0 {
			c := sc
			g.plain = &c
		} else {
			g.index = append(g.index, sc)
		}
	}
	return groups
}

// foldGroup turns one field's columns into the field's JSON value.
func (f *fold) foldGroup(g *fieldGroup) (json.RawMessage, bool) {
	first := g.plain
	if first == nil {
		first = &g.index[0]
	}

	var kind string
	switch {
	case g.token == AnyToken:
		k, ok := AnyKindWithField(g.field)
		if !ok {
			f.warn("%s: no kind has a spec field %q", first.name, g.field)
			return nil, false
		}
		kind = k
	case KindTokens[g.token] != "":
		kind = KindTokens[g.token]
		if _, ok := SpecField(kind, g.field); !ok {
			f.warn("%s: %s has no spec field %q", first.name, kind, g.field)
			return nil, false
		}
	default:
		f.warn("%s: unknown kind token %q; expected one of %s, %s", first.name, g.token, strings.Join(SortedKindTokens(), ", "), AnyToken)
		return nil, false
	}
	if g.field == "dataset" {
		f.warn("%s: the dataset binding cannot be set from data", first.name)
		return nil, false
	}

	ftype, _ := SpecField(kind, g.field)
	if ftype != FieldObject {
		if len(g.index) > 0 {
			f.warn("%s: %s is not an object field", g.index[0].name, g.field)
			return nil, false
		}
		return f.foldScalarOrList(g.plain, ftype)
	}
	return f.foldObject(g, kind)
}

// foldScalarOrList handles the plain column of a scalar or list field.
func (f *fold) foldScalarOrList(col *specColumn, ftype FieldType) (json.RawMessage, bool) {
	cells := cellValues(f.rows, col.name)
	if len(cells) == 0 {
		return nil, false
	}
	if ftype == FieldList {
		return mustJSON(distinct(splitList(cells))), true
	}
	// Compare parsed values, so "1" and true, or 100 and "100", agree.
	parsed, bad, values := parseCells(cells, ftype)
	if bad != "" {
		f.warn("%s: cannot parse %q as %s", col.name, bad, ftype)
		return nil, false
	}
	if len(parsed) > 1 {
		f.warn("%s: %d distinct values (%s) in dataset %s", col.field, len(parsed), strings.Join(values, ", "), f.name)
		return nil, false
	}
	return parsed[0], true
}

// foldObject handles an object-valued field in one of its three forms.
func (f *fold) foldObject(g *fieldGroup, kind string) (json.RawMessage, bool) {
	if g.plain == nil {
		return f.foldIndexForm(g, kind)
	}

	// Split the plain column's cells into JSON values and marker cells.
	var jsonCells, markerCells []string
	for _, row := range f.rows {
		cell, ok := objectCell(row[g.plain.name])
		if !ok {
			continue
		}
		if strings.HasPrefix(cell, "[") || strings.HasPrefix(cell, "{") {
			jsonCells = append(jsonCells, cell)
		} else {
			markerCells = append(markerCells, cell)
		}
	}
	if len(jsonCells) == 0 && len(markerCells) == 0 {
		return f.foldIndexForm(g, kind)
	}

	forms := []string{}
	if len(markerCells) > 0 {
		forms = append(forms, "marker")
	}
	if len(jsonCells) > 0 {
		forms = append(forms, "json")
	}
	if len(g.index) > 0 {
		forms = append(forms, "index")
	}
	if len(forms) > 1 {
		f.warn("%s: two forms (%s) in dataset %s", g.field, strings.Join(forms, ", "), f.name)
		return nil, false
	}

	if len(jsonCells) > 0 {
		return f.foldJSONForm(g, kind, jsonCells)
	}
	return f.foldMarkerForm(g, kind)
}

// objectCell returns the text of an object field's cell: a JSON array or
// object as its compact JSON, a string trimmed. NULL and "" are not values.
func objectCell(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	if raw[0] == '[' || raw[0] == '{' {
		s, err := canonicalJSON(raw)
		if err != nil {
			return "", false
		}
		return s, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return strings.TrimSpace(string(raw)), true
	}
	s = strings.TrimSpace(s)
	return s, s != ""
}

// foldJSONForm handles form 1: every JSON cell must be the same value.
func (f *fold) foldJSONForm(g *fieldGroup, kind string, cells []string) (json.RawMessage, bool) {
	var values []string
	seen := map[string]bool{}
	for _, cell := range cells {
		canon, err := canonicalJSON([]byte(cell))
		if err != nil {
			f.warn("%s: invalid JSON", g.plain.name)
			return nil, false
		}
		if !seen[canon] {
			seen[canon] = true
			values = append(values, canon)
		}
	}
	if len(values) > 1 {
		f.warn("%s: %d distinct values (%s) in dataset %s", g.field, len(values), strings.Join(values, ", "), f.name)
		return nil, false
	}
	return shapeObjectValue(kind, g.field, json.RawMessage(values[0])), true
}

// canonicalJSON re-encodes JSON text compactly with sorted object keys, so
// equal values compare equal whatever their source spelled.
func canonicalJSON(text []byte) (string, error) {
	var v any
	if err := json.Unmarshal(text, &v); err != nil {
		return "", err
	}
	return string(mustJSON(v)), nil
}

// shapeObjectValue stores a JSON value in the shape the field's schema
// expects: a list of objects takes an array (an object with the item's keys
// is wrapped into one), a plain object takes an object; anything else is
// stored as a JSON string so the target type decides (the attributes object
// form, whose keys are labels).
func shapeObjectValue(kind, field string, value json.RawMessage) json.RawMessage {
	isList := IsListOfObjects(kind, field)
	switch {
	case isList && value[0] == '[':
		return value
	case isList && value[0] == '{':
		var obj map[string]json.RawMessage
		if json.Unmarshal(value, &obj) == nil {
			keys, _ := ObjectKeys(kind, []string{field, "0"})
			if keysSubset(obj, keys) {
				return json.RawMessage("[" + string(value) + "]")
			}
		}
	case !isList && value[0] == '{':
		return value
	}
	return mustJSON(string(value))
}

func keysSubset(obj map[string]json.RawMessage, keys []string) bool {
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range obj {
		if !allowed[k] {
			return false
		}
	}
	return true
}

// foldMarkerForm handles form 3: the plain column holds a level (row-keyed)
// or a scenario (column-keyed), or a bare string for fields that accept one.
func (f *fold) foldMarkerForm(g *fieldGroup, kind string) (json.RawMessage, bool) {
	col := g.plain.name
	if IsListOfObjects(kind, g.field) {
		keys, _ := ObjectKeys(kind, []string{g.field, "0"})
		switch {
		case isSubset(keys, rowLevels):
			return f.foldRowMarker(g, keys)
		case strings.Join(keys, ",") == strings.Join(columnthereofKeys, ","):
			return f.foldColumnMarker(g)
		}
	} else if ObjectAcceptsString(kind, g.field) {
		cells := cellValues(f.rows, col)
		values := distinct(cells)
		if len(values) > 1 {
			f.warn("%s: %d distinct values (%s) in dataset %s", g.field, len(values), strings.Join(values, ", "), f.name)
			return nil, false
		}
		return mustJSON(values[0]), true
	}
	cells := cellValues(f.rows, col)
	f.warn("%s: %q is not JSON and %s has no marker form", col, cells[0], g.field)
	return nil, false
}

func isSubset(keys, of []string) bool {
	allowed := map[string]bool{}
	for _, k := range of {
		allowed[k] = true
	}
	for _, k := range keys {
		if !allowed[k] {
			return false
		}
	}
	return true
}

// foldRowMarker builds one entry per marked row from the row's own dimension
// values, down to the level the cell names, deduplicated in row order.
func (f *fold) foldRowMarker(g *fieldGroup, keys []string) (json.RawMessage, bool) {
	col := g.plain.name
	has := map[string]bool{}
	for _, k := range keys {
		has[k] = true
	}
	var entries []json.RawMessage
	seen := map[string]bool{}
	for _, row := range f.rows {
		level, ok := objectCell(row[col])
		if !ok {
			continue
		}
		depth := -1
		for i, l := range rowLevels {
			if l == level {
				depth = i
			}
		}
		if depth < 0 {
			f.warn("%s: %q is neither a level (%s) nor JSON", col, level, strings.Join(rowLevels, ", "))
			return nil, false
		}
		entry := map[string]string{}
		for _, dim := range rowLevels[:depth+1] {
			if !has[dim] {
				f.warn("%s: level %q is not supported (keys: %s)", col, level, strings.Join(keys, ", "))
				return nil, false
			}
			v, ok := canonicalCell(row[dim])
			if !ok {
				f.warn("%s: row marked %q has no %s value", col, level, dim)
				return nil, false
			}
			entry[dim] = v
		}
		b := mustJSON(entry)
		if !seen[string(b)] {
			seen[string(b)] = true
			entries = append(entries, b)
		}
	}
	return mustJSON(entries), true
}

// foldColumnMarker builds one columnthereof entry per (scenario, columnGroup)
// of the marked rows, with the distinct columnSubGroup values as subGroups.
func (f *fold) foldColumnMarker(g *fieldGroup) (json.RawMessage, bool) {
	col := g.plain.name
	type entry struct {
		scenario, name string
		subGroups      []string
		seen           map[string]bool
	}
	var entries []*entry
	byKey := map[string]*entry{}
	for _, row := range f.rows {
		scenario, ok := objectCell(row[col])
		if !ok {
			continue
		}
		group, ok := canonicalCell(row[columnLevels[0]])
		if !ok {
			f.warn("%s: row marked %q has no %s value", col, scenario, columnLevels[0])
			return nil, false
		}
		key := scenario + "\x00" + group
		e := byKey[key]
		if e == nil {
			e = &entry{scenario: scenario, name: group, seen: map[string]bool{}}
			byKey[key] = e
			entries = append(entries, e)
		}
		if sub, ok := canonicalCell(row[columnLevels[1]]); ok && !e.seen[sub] {
			e.seen[sub] = true
			e.subGroups = append(e.subGroups, sub)
		}
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		item := map[string]any{"scenario": e.scenario, "name": e.name}
		if len(e.subGroups) > 0 {
			item["subGroups"] = e.subGroups
		}
		out = append(out, item)
	}
	return mustJSON(out), true
}

// foldIndexForm handles form 2: index columns reassembled into the value.
func (f *fold) foldIndexForm(g *fieldGroup, kind string) (json.RawMessage, bool) {
	if len(g.index) == 0 {
		return nil, false
	}
	root := &jsonNode{}
	for _, col := range g.index {
		path := append([]string{g.field}, col.rest...)
		if isIndex(col.rest[len(col.rest)-1]) {
			f.warn("%s: expected a key after index %s", col.name, col.rest[len(col.rest)-1])
			return nil, false
		}
		ftype, ok := FieldTypeAt(kind, path)
		if !ok {
			f.warn("%s: %s item has no key %q", col.name, g.field, col.rest[len(col.rest)-1])
			return nil, false
		}
		var leaf json.RawMessage
		switch ftype {
		case FieldObject:
			f.warn("%s: nested objects need their own index columns", col.name)
			return nil, false
		case FieldList:
			cells := cellValues(f.rows, col.name)
			if len(cells) == 0 {
				continue
			}
			leaf = mustJSON(distinct(splitList(cells)))
		default:
			c := col
			v, ok := f.foldScalarOrList(&c, ftype)
			if !ok {
				if len(cellValues(f.rows, col.name)) == 0 {
					continue
				}
				return nil, false
			}
			leaf = v
		}
		root.set(col.rest, leaf)
	}
	value, err := root.build(kind, []string{g.field})
	if err != nil {
		f.warn("%s: %s in dataset %s", g.field, err, f.name)
		return nil, false
	}
	if value == nil {
		return nil, false
	}
	return value, true
}

func isIndex(seg string) bool {
	_, err := strconv.Atoi(seg)
	return err == nil && seg != ""
}

// jsonNode is a partially assembled JSON value: either a leaf, an object
// (keys) or a list (indices).
type jsonNode struct {
	leaf  json.RawMessage
	keys  map[string]*jsonNode
	items map[int]*jsonNode
}

func (n *jsonNode) set(path []string, leaf json.RawMessage) {
	if len(path) == 0 {
		n.leaf = leaf
		return
	}
	seg := path[0]
	if i, err := strconv.Atoi(seg); err == nil {
		if n.items == nil {
			n.items = map[int]*jsonNode{}
		}
		if n.items[i] == nil {
			n.items[i] = &jsonNode{}
		}
		n.items[i].set(path[1:], leaf)
		return
	}
	if n.keys == nil {
		n.keys = map[string]*jsonNode{}
	}
	if n.keys[seg] == nil {
		n.keys[seg] = &jsonNode{}
	}
	n.keys[seg].set(path[1:], leaf)
}

// build serializes the node; lists must be contiguous from 0 and objects
// must carry their required keys. path locates the node in kind's spec.
func (n *jsonNode) build(kind string, path []string) (json.RawMessage, error) {
	switch {
	case n.leaf != nil:
		return n.leaf, nil
	case n.items != nil:
		indices := make([]int, 0, len(n.items))
		for i := range n.items {
			indices = append(indices, i)
		}
		sort.Ints(indices)
		parts := make([]string, 0, len(indices))
		for want, i := range indices {
			if i != want {
				return nil, fmt.Errorf("index columns must be contiguous from 0 (missing %d)", want)
			}
			item, err := n.items[i].build(kind, append(append([]string{}, path...), strconv.Itoa(i)))
			if err != nil {
				return nil, err
			}
			if item != nil {
				parts = append(parts, string(item))
			}
		}
		return json.RawMessage("[" + strings.Join(parts, ",") + "]"), nil
	case n.keys != nil:
		obj := map[string]json.RawMessage{}
		for k, child := range n.keys {
			v, err := child.build(kind, append(append([]string{}, path...), k))
			if err != nil {
				return nil, err
			}
			if v != nil {
				obj[k] = v
			}
		}
		if len(obj) == 0 {
			return nil, nil
		}
		_, required := ObjectKeys(kind, path)
		for _, k := range required {
			if _, ok := obj[k]; !ok {
				return nil, fmt.Errorf("missing required key %q", k)
			}
		}
		return mustJSON(obj), nil
	}
	return nil, nil
}

// cellValues returns the canonical string form of every non-empty cell of a
// column, in row order.
func cellValues(rows []map[string]json.RawMessage, col string) []string {
	var cells []string
	for _, row := range rows {
		raw, ok := row[col]
		if !ok {
			continue
		}
		s, ok := canonicalCell(raw)
		if ok {
			cells = append(cells, s)
		}
	}
	return cells
}

// canonicalCell turns a JSON cell into its string form: numbers without a
// trailing ".0", booleans as true/false, strings trimmed, a JSON array as its
// elements joined with a comma. NULL and empty strings are not values.
func canonicalCell(raw json.RawMessage) (string, bool) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	switch val := v.(type) {
	case nil:
		return "", false
	case string:
		s := strings.TrimSpace(val)
		return s, s != ""
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(val), true
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := canonicalCell(mustJSON(item)); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ","), len(parts) > 0
	default:
		return strings.TrimSpace(string(raw)), true
	}
}

// splitList expands comma lists and JSON array strings into their items.
func splitList(cells []string) []string {
	var items []string
	for _, cell := range cells {
		if strings.HasPrefix(cell, "[") {
			var arr []string
			if err := json.Unmarshal([]byte(cell), &arr); err == nil {
				items = append(items, arr...)
				continue
			}
		}
		for _, part := range strings.Split(cell, ",") {
			items = append(items, strings.TrimSpace(part))
		}
	}
	return items
}

func distinct(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// parseCells parses every cell and returns the distinct parsed values in row
// order with the cell text of each; bad is the first cell that does not parse.
func parseCells(cells []string, ftype FieldType) (parsed []json.RawMessage, bad string, values []string) {
	seen := map[string]bool{}
	for _, cell := range cells {
		p, err := parseCell(cell, ftype)
		if err != nil {
			return nil, cell, nil
		}
		if seen[string(p)] {
			continue
		}
		seen[string(p)] = true
		parsed = append(parsed, p)
		values = append(values, cell)
	}
	return parsed, "", values
}

// parseCell converts one canonical cell into the JSON value a scalar field
// expects.
func parseCell(cell string, ftype FieldType) (json.RawMessage, error) {
	switch ftype {
	case FieldNumber:
		f, err := strconv.ParseFloat(cell, 64)
		if err != nil {
			return nil, err
		}
		return mustJSON(f), nil
	case FieldBool:
		switch strings.ToLower(cell) {
		case "true", "1":
			return mustJSON(true), nil
		case "false", "0":
			return mustJSON(false), nil
		}
		return nil, fmt.Errorf("not a boolean")
	default:
		return mustJSON(cell), nil
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v) //nolint:errcheck // strings, numbers, bools, maps and slices of them always marshal
	return b
}
