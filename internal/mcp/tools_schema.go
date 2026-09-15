package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/schema/walk"
)

// outlineMaxDepth caps the outline's recursion below spec, counted in walker
// segments (an array's items count as one): attributes[].label is three deep
// and emitted, edges[].style.color is four deep and is not.
const outlineMaxDepth = 3

type outlineKindInput struct {
	Kind string `json:"kind" jsonschema:"the manifest kind to outline, e.g. Table or LayoutPage"`
}

// outlineField is one spec field of a kind, addressed by a dotted path where
// [] marks array items (attributes[].label).
type outlineField struct {
	Path        string   `json:"path"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
}

type outlineKindOutput struct {
	Kind        string         `json:"kind"`
	Found       bool           `json:"found"`
	Description string         `json:"description,omitempty"`
	Required    []string       `json:"required"`
	Fields      []outlineField `json:"fields"`
	// ChildKinds lists the kinds a layout kind's children slot admits; the
	// slot's own keys appear in Fields but its per-kind specs are not expanded.
	ChildKinds []string `json:"childKinds,omitempty"`
}

type scaffoldKindInput struct {
	Kind string `json:"kind" jsonschema:"the manifest kind to scaffold, e.g. Table or DataSet"`
}

type scaffoldKindOutput struct {
	Kind        string   `json:"kind"`
	Found       bool     `json:"found"`
	Description string   `json:"description,omitempty"`
	Required    []string `json:"required"`
	YAML        string   `json:"yaml,omitempty"`
}

func (h *handlers) registerSchemaTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "outline_kind",
		Description: "Compact outline of a manifest kind's spec: one entry per field with its dotted path ([] marks array items, three levels deep), type, required flag, enum, default and full description. Layout kinds report the allowed child kinds as childKinds instead of expanding the children slot. Start here before writing a manifest; use describe_kind only for a shape the outline leaves ambiguous. Note: required on a nested field is true when any schema alternative requires it.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in outlineKindInput) (*mcpsdk.CallToolResult, outlineKindOutput, error) {
		m, err := h.schemaModel(ctx)
		if err != nil {
			return nil, outlineKindOutput{}, err
		}
		return nil, outlineKind(m, in.Kind), nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "scaffold_kind",
		Description: "Starter YAML document for a manifest kind: apiVersion, kind, metadata.name and every required spec field, pre-filled with its default or first enum value. Fill in the placeholders, then validate_draft.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in scaffoldKindInput) (*mcpsdk.CallToolResult, scaffoldKindOutput, error) {
		m, err := h.schemaModel(ctx)
		if err != nil {
			return nil, scaffoldKindOutput{}, err
		}
		return nil, scaffoldKind(m, in.Kind), nil
	})
}

// schemaModel parses the merged schema (built-in + plugin kinds). The
// aggregator embeds every plugin spec as a kind-discriminated allOf block and
// extends the kind enum, which is exactly the shape the walker resolves, so one
// model serves built-in and plugin kinds alike. Local $refs inside a plugin
// spec resolve against the merged root $defs, as they do for validation.
func (h *handlers) schemaModel(ctx context.Context) (*walk.Model, error) {
	agg, err := h.aggregator(ctx)
	if err != nil {
		return nil, err
	}
	m := walk.Parse(agg.MergedSchema())
	if m.Empty() {
		return nil, fmt.Errorf("merged schema failed to parse")
	}
	return m, nil
}

// scaffoldKind renders the plain-text document scaffold the LSP offers for a
// fresh buffer, with the kind's prose and required fields alongside.
func scaffoldKind(m *walk.Model, kind string) scaffoldKindOutput {
	out := scaffoldKindOutput{Kind: kind, Required: []string{}}
	if !slices.Contains(m.Kinds(), kind) {
		return out
	}
	out.Found = true
	out.Description, out.Required = m.KindDoc(kind)
	if out.Required == nil {
		out.Required = []string{}
	}
	out.YAML = walk.ScaffoldBody(m, kind, walk.ScaffoldAPIVersion(m), false)
	return out
}

// outlineKind projects a kind's spec into a flat, path-sorted field list.
func outlineKind(m *walk.Model, kind string) outlineKindOutput {
	out := outlineKindOutput{Kind: kind, Required: []string{}, Fields: []outlineField{}}
	if !slices.Contains(m.Kinds(), kind) {
		return out
	}
	out.Found = true
	out.Description, out.Required = m.KindDoc(kind)
	if out.Required == nil {
		out.Required = []string{}
	}
	o := &outliner{m: m, kinds: map[string]string{"": kind}}
	o.walk(nil, "")
	slices.SortFunc(o.fields, func(a, b outlineField) int { return strings.Compare(a.Path, b.Path) })
	out.Fields = append(out.Fields, o.fields...)
	out.ChildKinds = o.childKinds
	return out
}

// outliner accumulates the outline of one kind while walking its spec. The
// kinds map carries only the root kind: nested kind-discriminated conditionals
// (a layout child's per-kind spec) therefore contribute nothing, which is what
// keeps children[].spec a bare object instead of every child kind's fields.
type outliner struct {
	m          *walk.Model
	kinds      map[string]string
	fields     []outlineField
	childKinds []string
}

// walk emits the properties of the node at spec + segs (prefix is the
// rendered path of that node) and descends into object properties and array
// items until outlineMaxDepth. A children slot — array items that carry a kind
// enum and a spec — is emitted one level deep and recorded in childKinds.
func (o *outliner) walk(segs []string, prefix string) {
	props := o.m.ResolveAt(specPath(segs), o.kinds).Props()
	sortProps(props)
	depth := len(segs) + 1
	for _, p := range props {
		path := prefix + p.Name
		o.fields = append(o.fields, toField(p, path))
		if depth+1 <= outlineMaxDepth {
			obj := o.m.ResolveAt(specPath(segs, p.Name), o.kinds)
			if obj.IsObject() && !obj.IsMap() {
				o.walk(append(slices.Clone(segs), p.Name), path+".")
				continue
			}
		}
		if depth+2 > outlineMaxDepth {
			continue
		}
		items := o.m.ResolveAt(specPath(segs, p.Name, "0"), o.kinds)
		if !items.IsObject() || items.IsMap() {
			continue
		}
		kindProp, hasKind := items.Prop("kind")
		_, hasSpec := items.Prop("spec")
		if hasKind && len(kindProp.Enum) > 0 && hasSpec {
			o.childKinds = appendUnique(o.childKinds, kindProp.Enum)
			slot := items.Props()
			sortProps(slot)
			for _, q := range slot {
				o.fields = append(o.fields, toField(q, path+"[]."+q.Name))
			}
			continue
		}
		o.walk(append(slices.Clone(segs), p.Name, "0"), path+"[].")
	}
}

// specPath builds a fresh walker path spec + segs + extra.
func specPath(segs []string, extra ...string) []string {
	out := make([]string, 0, 1+len(segs)+len(extra))
	out = append(out, "spec")
	out = append(out, segs...)
	return append(out, extra...)
}

// sortProps orders properties by name; Props() iterates a map.
func sortProps(props []walk.PropInfo) {
	slices.SortFunc(props, func(a, b walk.PropInfo) int { return strings.Compare(a.Name, b.Name) })
}

func toField(p walk.PropInfo, path string) outlineField {
	return outlineField{
		Path:        path,
		Type:        strings.Join(p.Types, "|"),
		Required:    p.Required,
		Enum:        p.Enum,
		Default:     p.Default,
		Description: p.Description,
	}
}

func appendUnique(dst, add []string) []string {
	for _, s := range add {
		if !slices.Contains(dst, s) {
			dst = append(dst, s)
		}
	}
	return dst
}
