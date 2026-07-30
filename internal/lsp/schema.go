package lsp

import (
	"encoding/json"
	"maps"
	"strings"
)

// schemaModel is a lazily-parsed view of the merged JSON schema, cached on the
// server and rebuilt on project change. It exposes only what completion needs:
// the kind list and per-kind spec field metadata.
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

// propInfo is the completion-relevant metadata of one spec field.
type propInfo struct {
	Name        string
	Description string
	Enum        []string
}

// specProps returns the spec.properties of a given kind, or nil.
func (m *schemaModel) specProps(kind string) []propInfo {
	props := m.specProperties(kind)
	out := make([]propInfo, 0, len(props))
	for name, raw := range props {
		p := propInfo{Name: name}
		if obj := m.deref(raw); obj != nil {
			p.Description, _ = obj["description"].(string)
			if enum, ok := obj["enum"].([]any); ok {
				p.Enum = toStrings(enum)
			}
		}
		out = append(out, p)
	}
	return out
}

// fieldDoc returns the description of spec.<field> for a kind, if any.
func (m *schemaModel) fieldDoc(kind, field string) string {
	obj := m.deref(m.specProperties(kind)[field])
	if obj == nil {
		return ""
	}
	d, _ := obj["description"].(string)
	return d
}

// fieldEnum returns the enum values declared for spec.<field> of a kind, if any.
func (m *schemaModel) fieldEnum(kind, field string) []string {
	obj := m.deref(m.specProperties(kind)[field])
	if obj == nil {
		return nil
	}
	if enum, ok := obj["enum"].([]any); ok {
		return toStrings(enum)
	}
	return nil
}

// specProperties returns the merged spec property map for a kind, following the
// $ref + allOf composition the schema uses (e.g. spec -> $defs/tableSpec ->
// allOf[base $ref, {properties}]).
func (m *schemaModel) specProperties(kind string) map[string]any {
	for _, block := range m.allOf() {
		if kindConst(block) != kind {
			continue
		}
		then, _ := block["then"].(map[string]any)
		props, _ := then["properties"].(map[string]any)
		return m.mergedProperties(props["spec"], 0)
	}
	return nil
}

// mergedProperties resolves $ref and merges allOf to produce the effective
// `properties` map of a schema node. Earlier entries win on key conflicts.
func (m *schemaModel) mergedProperties(node any, depth int) map[string]any {
	obj, ok := node.(map[string]any)
	if !ok || depth > 20 {
		return nil
	}
	if ref, ok := obj["$ref"].(string); ok {
		return m.mergedProperties(m.resolveRef(ref), depth+1)
	}
	out := map[string]any{}
	if props, ok := obj["properties"].(map[string]any); ok {
		maps.Copy(out, props)
	}
	if allOf, ok := obj["allOf"].([]any); ok {
		for _, sub := range allOf {
			for k, v := range m.mergedProperties(sub, depth+1) {
				if _, exists := out[k]; !exists {
					out[k] = v
				}
			}
		}
	}
	return out
}

// deref follows a single $ref on a field schema so its enum/description are
// readable; non-ref nodes are returned as-is.
func (m *schemaModel) deref(node any) map[string]any {
	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	if ref, ok := obj["$ref"].(string); ok {
		if r := m.resolveRef(ref); r != nil {
			return r
		}
	}
	return obj
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

// kindConst reads if.properties.kind.const from an allOf block.
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
