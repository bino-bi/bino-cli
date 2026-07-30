package lsp

import (
	"encoding/json"
	"strings"
)

// schemaModel is a lazily-parsed view of the merged JSON schema, cached on the
// server and rebuilt on project change. It exposes what completion and hover
// need: the kind list and a path-walking resolver over the schema's
// $ref/allOf/oneOf/if-then composition.
type schemaModel struct {
	doc map[string]any
}

func parseSchema(merged json.RawMessage) *schemaModel {
	var doc map[string]any
	if json.Unmarshal(merged, &doc) != nil {
		return &schemaModel{}
	}
	return &schemaModel{doc: doc}
}

// empty reports that no schema is loaded (cold start or backend failure).
// Completion built from an empty model must be marked incomplete so the client
// re-queries instead of caching the empty list for the whole typing session.
func (m *schemaModel) empty() bool { return m.doc == nil }

// kinds returns every manifest kind known to the schema, preferring the explicit
// properties.kind.enum and falling back to the per-kind allOf consts.
func (m *schemaModel) kinds() []string {
	if m.doc == nil {
		return nil
	}
	if props, ok := m.doc["properties"].(map[string]any); ok {
		if kind, ok := props["kind"].(map[string]any); ok {
			if enum, ok := kind["enum"].([]any); ok {
				return toStrings(enum)
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, block := range m.allOf() {
		if c := kindConst(block); c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// propInfo is the completion-relevant metadata of one schema property.
type propInfo struct {
	Name        string
	Description string
	Enum        []string
	Type        string
	Default     any
	Required    bool
}

// schemaNode is the resolver's verdict for a path: the set of raw schema
// objects that could describe the position. Multiple variants arise from
// oneOf/anyOf and from allOf members, which are kept flat — helpers union
// across them, which is exactly the candidate semantics completion wants.
type schemaNode struct {
	m        *schemaModel
	variants []map[string]any
}

// maxSchemaDepth caps normalize recursion as a backstop; the visited-$ref set
// is the real cycle guard (layoutChild → layoutCardSpec → children is cyclic).
const maxSchemaDepth = 32

// resolveAt walks the schema along a document path (mapping keys and sequence
// indices), discriminating kind-conditional branches with the document's
// kinds-by-path (see reportspec.PositionContext.KindsByPath; "" = root).
func (m *schemaModel) resolveAt(path []string, kinds map[string]string) schemaNode {
	if m.doc == nil {
		return schemaNode{m: m}
	}
	cur := m.normalize(m.doc, kinds[""], 0, map[string]bool{})
	dotted := ""
	for _, seg := range path {
		if dotted == "" {
			dotted = seg
		} else {
			dotted += "." + seg
		}
		var next []map[string]any
		for _, v := range cur {
			next = append(next, m.step(v, seg)...)
		}
		var expanded []map[string]any
		for _, v := range next {
			expanded = append(expanded, m.normalize(v, kinds[dotted], 0, map[string]bool{})...)
		}
		cur = expanded
	}
	return schemaNode{m: m, variants: cur}
}

// normalize expands one raw schema node into its concrete variants: $refs are
// followed, allOf members flattened in, kind-discriminated conditionals
// resolved against the document's kind at this level, other conditionals
// unioned (then + else), and oneOf/anyOf branches all kept.
func (m *schemaModel) normalize(node any, kind string, depth int, seen map[string]bool) []map[string]any {
	obj, ok := node.(map[string]any)
	if !ok || depth > maxSchemaDepth {
		return nil
	}
	if ref, ok := obj["$ref"].(string); ok {
		if seen[ref] {
			return nil
		}
		seen[ref] = true
		out := m.normalize(m.resolveRef(ref), kind, depth+1, seen)
		delete(seen, ref) // guard chains, not siblings: diamonds stay resolvable
		return out
	}
	variants := []map[string]any{obj}
	expand := func(sub any) {
		variants = append(variants, m.normalize(sub, kind, depth+1, seen)...)
	}
	if allOf, ok := obj["allOf"].([]any); ok {
		for _, member := range allOf {
			mo, ok := member.(map[string]any)
			if !ok {
				continue
			}
			if _, isCond := mo["if"]; isCond {
				m.expandConditional(mo, kind, expand)
				continue
			}
			expand(mo)
		}
	}
	// An object-level conditional (if/then/else beside other keys) — the shape
	// layoutChild uses to switch a child's spec on `ref` presence.
	if _, isCond := obj["if"]; isCond {
		m.expandConditional(obj, kind, expand)
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if branches, ok := obj[key].([]any); ok {
			for _, b := range branches {
				expand(b)
			}
		}
	}
	return variants
}

// expandConditional applies one if/then/else block: a kind-discriminated
// condition contributes `then` only when the document kind matches (nothing
// when the kind is unknown — offering every kind's fields would be noise); any
// other condition cannot be decided statically, so both branches are unioned.
func (m *schemaModel) expandConditional(obj map[string]any, kind string, expand func(any)) {
	if kc := kindConst(obj); kc != "" {
		if kc == kind {
			expand(obj["then"])
		}
		return
	}
	expand(obj["then"])
	expand(obj["else"])
}

// step descends one path segment: a numeric segment enters `items`, a named
// one enters `properties` (falling back to `additionalProperties` for
// map-shaped nodes like i18n content).
func (m *schemaModel) step(obj map[string]any, seg string) []map[string]any {
	if isIndexSegment(seg) {
		if items, ok := obj["items"].(map[string]any); ok {
			return []map[string]any{items}
		}
		return nil
	}
	if props, ok := obj["properties"].(map[string]any); ok {
		if sub, ok := props[seg].(map[string]any); ok {
			return []map[string]any{sub}
		}
	}
	if ap, ok := obj["additionalProperties"].(map[string]any); ok {
		return []map[string]any{ap}
	}
	return nil
}

func isIndexSegment(seg string) bool {
	if seg == "" {
		return false
	}
	for i := 0; i < len(seg); i++ {
		if seg[i] < '0' || seg[i] > '9' {
			return false
		}
	}
	return true
}

// props returns the union of the variants' properties. `required` entries may
// name properties defined in a sibling variant (tableSpec declares required
// while tableSpecBase declares the properties), so requiredness is collected
// across all variants first.
func (n schemaNode) props() []propInfo {
	required := map[string]bool{}
	for _, v := range n.variants {
		if req, ok := v["required"].([]any); ok {
			for _, r := range toStrings(req) {
				required[r] = true
			}
		}
	}
	index := map[string]int{}
	var out []propInfo
	for _, v := range n.variants {
		props, ok := v["properties"].(map[string]any)
		if !ok {
			continue
		}
		for name, raw := range props {
			if i, ok := index[name]; ok {
				n.m.enrichProp(&out[i], raw)
				continue
			}
			p := propInfo{Name: name, Required: required[name]}
			n.m.enrichProp(&p, raw)
			index[name] = len(out)
			out = append(out, p)
		}
	}
	return out
}

// enrichProp fills a property's completion metadata from its (possibly
// $ref/oneOf-composed) schema, unioning enums and keeping the first
// description/type/default found.
func (m *schemaModel) enrichProp(p *propInfo, raw any) {
	for _, v := range m.normalize(raw, "", 0, map[string]bool{}) {
		if p.Description == "" {
			p.Description, _ = v["description"].(string)
		}
		if p.Type == "" {
			p.Type, _ = v["type"].(string)
		}
		if p.Default == nil {
			p.Default = v["default"]
		}
		p.Enum = appendUniqueStrings(p.Enum, enumOf(v))
	}
}

// enumValues returns the union of the variants' enum/const values, including
// the item enums of array-shaped variants (a oneOf of `enum` and `array of
// enum` completes the same tokens either way).
func (n schemaNode) enumValues() []string {
	var out []string
	for _, v := range n.variants {
		out = appendUniqueStrings(out, enumOf(v))
		if items, ok := v["items"].(map[string]any); ok {
			for _, iv := range n.m.normalize(items, "", 0, map[string]bool{}) {
				out = appendUniqueStrings(out, enumOf(iv))
			}
		}
	}
	return out
}

// defaultValue returns the first declared default across the variants.
func (n schemaNode) defaultValue() any {
	for _, v := range n.variants {
		if d, ok := v["default"]; ok && d != nil {
			return d
		}
	}
	return nil
}

// doc returns the first non-empty description across the variants.
func (n schemaNode) doc() string {
	for _, v := range n.variants {
		if d, _ := v["description"].(string); d != "" {
			return d
		}
	}
	return ""
}

// prop returns the named property's metadata at this node, if declared.
func (n schemaNode) prop(name string) (propInfo, bool) {
	for _, p := range n.props() {
		if p.Name == name {
			return p, true
		}
	}
	return propInfo{}, false
}

// isObject reports whether any variant is object-shaped (a position where key
// completion applies, e.g. a bare `- ` under `children:`).
func (n schemaNode) isObject() bool {
	for _, v := range n.variants {
		if t, _ := v["type"].(string); t == "object" {
			return true
		}
		if _, ok := v["properties"].(map[string]any); ok {
			return true
		}
	}
	return false
}

// isBool reports whether any variant is boolean-typed.
func (n schemaNode) isBool() bool {
	for _, v := range n.variants {
		if t, _ := v["type"].(string); t == "boolean" {
			return true
		}
	}
	return false
}

// enumOf reads a node's own enum (or const, a one-value enum) as strings.
func enumOf(v map[string]any) []string {
	if enum, ok := v["enum"].([]any); ok {
		return toStrings(enum)
	}
	if c, ok := v["const"].(string); ok {
		return []string{c}
	}
	return nil
}

func appendUniqueStrings(dst []string, add []string) []string {
	for _, s := range add {
		found := false
		for _, d := range dst {
			if d == s {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, s)
		}
	}
	return dst
}

// resolveRef follows a local JSON pointer ("#/$defs/foo").
func (m *schemaModel) resolveRef(ref string) map[string]any {
	if m.doc == nil || !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var cur any = m.doc
	for p := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
		p = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = cm[p]
	}
	out, _ := cur.(map[string]any)
	return out
}

func (m *schemaModel) allOf() []map[string]any {
	if m.doc == nil {
		return nil
	}
	raw, ok := m.doc["allOf"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, b := range raw {
		if block, ok := b.(map[string]any); ok {
			out = append(out, block)
		}
	}
	return out
}

// kindConst reads if.properties.kind.const from a conditional block.
func kindConst(block map[string]any) string {
	ifClause, ok := block["if"].(map[string]any)
	if !ok {
		return ""
	}
	props, ok := ifClause["properties"].(map[string]any)
	if !ok {
		return ""
	}
	kind, ok := props["kind"].(map[string]any)
	if !ok {
		return ""
	}
	c, _ := kind["const"].(string)
	return c
}

func toStrings(raw []any) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
