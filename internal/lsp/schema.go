package lsp

import "encoding/json"

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
	spec := m.specSchema(kind)
	if spec == nil {
		return nil
	}
	props, ok := spec["properties"].(map[string]any)
	if !ok {
		return nil
	}
	out := make([]propInfo, 0, len(props))
	for name, raw := range props {
		p := propInfo{Name: name}
		if obj, ok := raw.(map[string]any); ok {
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
	spec := m.specSchema(kind)
	if spec == nil {
		return ""
	}
	props, ok := spec["properties"].(map[string]any)
	if !ok {
		return ""
	}
	obj, ok := props[field].(map[string]any)
	if !ok {
		return ""
	}
	d, _ := obj["description"].(string)
	return d
}

// fieldEnum returns the enum values declared for spec.<field> of a kind, if any.
func (m *schemaModel) fieldEnum(kind, field string) []string {
	spec := m.specSchema(kind)
	if spec == nil {
		return nil
	}
	props, ok := spec["properties"].(map[string]any)
	if !ok {
		return nil
	}
	obj, ok := props[field].(map[string]any)
	if !ok {
		return nil
	}
	if enum, ok := obj["enum"].([]any); ok {
		return toStrings(enum)
	}
	return nil
}

// specSchema extracts then.properties.spec for a kind from the merged schema's
// allOf blocks (mirrors mcp.extractSpecSchema, scoped to what completion needs).
func (m *schemaModel) specSchema(kind string) map[string]any {
	for _, block := range m.allOf() {
		if kindConst(block) != kind {
			continue
		}
		then, ok := block["then"].(map[string]any)
		if !ok {
			return nil
		}
		props, ok := then["properties"].(map[string]any)
		if !ok {
			return nil
		}
		spec, _ := props["spec"].(map[string]any)
		return spec
	}
	return nil
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
