package lsp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"go.lsp.dev/protocol"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// These pure builders turn a resolved context + project data into completion
// items. They take explicit inputs (no Backend) so they are unit-testable; the
// handler wires live data in.

// completeKinds offers every manifest kind from the live merged schema, each
// documented with the kind's spec prose and required fields.
func completeKinds(m *schemaModel) []protocol.CompletionItem {
	kinds := m.kinds()
	sort.Strings(kinds)
	items := make([]protocol.CompletionItem, 0, len(kinds))
	for _, k := range kinds {
		item := protocol.CompletionItem{
			Label: k,
			Kind:  protocol.CompletionItemKindClass,
		}
		if doc := kindDocMarkdown(m, k); doc != "" {
			item.Documentation = protocol.String(doc)
		}
		items = append(items, item)
	}
	return items
}

// kindDocMarkdown renders a kind's description plus its required spec fields.
func kindDocMarkdown(m *schemaModel, kind string) string {
	desc, req := m.kindDoc(kind)
	if desc == "" && len(req) == 0 {
		return ""
	}
	out := desc
	if len(req) > 0 {
		if out != "" {
			out += "\n\n"
		}
		out += "Required: `" + strings.Join(req, "`, `") + "`"
	}
	return out
}

// documentScaffolds offers one full-manifest snippet per kind for a fresh
// document (empty buffer, or right after a `---`): apiVersion, kind,
// metadata.name and the kind's required spec fields as tabstops, defaults and
// enum choices pre-wired. Plain-text bodies are emitted for clients without
// snippet support.
func documentScaffolds(m *schemaModel, snippets bool) []protocol.CompletionItem {
	kinds := m.kinds()
	sort.Strings(kinds)
	apiVersion := scaffoldAPIVersion(m)
	items := make([]protocol.CompletionItem, 0, len(kinds))
	for _, k := range kinds {
		item := protocol.CompletionItem{
			Label:      k + " manifest",
			Kind:       protocol.CompletionItemKindSnippet,
			FilterText: protocol.NewOptional(k),
			SortText:   protocol.NewOptional("2_" + k), // after plain field keys
			InsertText: protocol.NewOptional(scaffoldBody(m, k, apiVersion, snippets)),
			Detail:     protocol.NewOptional("scaffold a full " + k + " document"),
		}
		if snippets {
			item.InsertTextFormat = protocol.InsertTextFormatSnippet
		} else {
			item.InsertTextFormat = protocol.InsertTextFormatPlainText
		}
		if doc := kindDocMarkdown(m, k); doc != "" {
			item.Documentation = protocol.String(doc)
		}
		items = append(items, item)
	}
	return items
}

// scaffoldAPIVersion reads the schema's apiVersion const/enum, with the
// current version as fallback.
func scaffoldAPIVersion(m *schemaModel) string {
	node := m.resolveAt([]string{"apiVersion"}, nil)
	if vals := node.enumValues(); len(vals) > 0 {
		return vals[0]
	}
	return "bino.bi/v1alpha1"
}

// scaffoldBody renders a kind's full-document template. Tabstops cover
// metadata.name and every required spec field; defaults are pre-filled and
// enums become snippet choices.
func scaffoldBody(m *schemaModel, kind, apiVersion string, snippets bool) string {
	var b strings.Builder
	b.WriteString("apiVersion: " + apiVersion + "\n")
	b.WriteString("kind: " + kind + "\n")
	b.WriteString("metadata:\n")
	if snippets {
		b.WriteString("  name: ${1:name}\n")
	} else {
		b.WriteString("  name: name\n")
	}
	b.WriteString("spec:")
	var required []propInfo
	for _, p := range m.resolveAt([]string{"spec"}, map[string]string{"": kind}).props() {
		if p.Required {
			required = append(required, p)
		}
	}
	sort.Slice(required, func(i, j int) bool { return required[i].Name < required[j].Name })
	if len(required) == 0 {
		if snippets {
			b.WriteString("\n  $2")
		} else {
			b.WriteString(" {}")
		}
		return b.String()
	}
	tab := 2
	for _, p := range required {
		b.WriteString("\n  " + p.Name + ": " + scaffoldValue(p, tab, snippets))
		tab++
	}
	return b.String()
}

// scaffoldValue renders one required field's placeholder.
func scaffoldValue(p propInfo, tab int, snippets bool) string {
	if !snippets {
		switch {
		case p.Default != nil:
			return renderDefault(p.Default)
		case len(p.Enum) > 0:
			return p.Enum[0]
		default:
			return ""
		}
	}
	n := strconv.Itoa(tab)
	switch {
	case p.Default != nil:
		return "${" + n + ":" + renderDefault(p.Default) + "}"
	case len(p.Enum) > 0:
		return "${" + n + "|" + strings.Join(p.Enum, ",") + "|}"
	default:
		return "${" + n + "}"
	}
}

// childScaffolds offers layout-child templates for a bare `- ` slot: a
// referenced component (kind + ref) and an inline one (kind + spec). The kind
// enum comes from the slot's own schema, so plugin exclusions hold.
func childScaffolds(node schemaNode, snippets bool) []protocol.CompletionItem {
	kp, ok := node.prop("kind")
	if !ok || len(kp.Enum) == 0 {
		return nil
	}
	refBody := "kind: " + kp.Enum[0] + "\n  ref: component_name"
	inlineBody := "kind: " + kp.Enum[0] + "\n  spec:\n    "
	format := protocol.InsertTextFormatPlainText
	if snippets {
		choices := "${1|" + strings.Join(kp.Enum, ",") + "|}"
		refBody = "kind: " + choices + "\n  ref: ${2:component_name}"
		inlineBody = "kind: " + choices + "\n  spec:\n    $2"
		format = protocol.InsertTextFormatSnippet
	}
	return []protocol.CompletionItem{
		{
			Label:            "kind + ref (referenced component)",
			Kind:             protocol.CompletionItemKindSnippet,
			FilterText:       protocol.NewOptional("kind ref"),
			SortText:         protocol.NewOptional("2_ref"),
			InsertText:       protocol.NewOptional(refBody),
			InsertTextFormat: format,
			Documentation:    protocol.String("Reference a named component defined elsewhere in the bundle (or installed from the registry)."),
		},
		{
			Label:            "kind + spec (inline component)",
			Kind:             protocol.CompletionItemKindSnippet,
			FilterText:       protocol.NewOptional("kind spec"),
			SortText:         protocol.NewOptional("2_spec"),
			InsertText:       protocol.NewOptional(inlineBody),
			InsertTextFormat: format,
			Documentation:    protocol.String("Define the component inline, right inside the layout."),
		},
	}
}

// completeFields offers an object position's properties not already present,
// required fields first, annotated with type/required/default and the schema
// description.
func completeFields(props []propInfo, present map[string]bool) []protocol.CompletionItem {
	sort.Slice(props, func(i, j int) bool { return props[i].Name < props[j].Name })
	items := make([]protocol.CompletionItem, 0, len(props))
	for _, p := range props {
		if present[p.Name] {
			continue
		}
		item := protocol.CompletionItem{
			Label:            p.Name,
			Kind:             protocol.CompletionItemKindField,
			InsertText:       protocol.NewOptional(p.Name + ": "),
			InsertTextFormat: protocol.InsertTextFormatPlainText,
			SortText:         protocol.NewOptional(fieldSortKey(p)),
		}
		if d := propDetail(p); d != "" {
			item.Detail = protocol.NewOptional(d)
		}
		if p.Description != "" {
			item.Documentation = protocol.String(p.Description)
		}
		items = append(items, item)
	}
	return items
}

// fieldSortKey ranks required fields before optional ones; names break ties.
func fieldSortKey(p propInfo) string {
	if p.Required {
		return "0_" + p.Name
	}
	return "1_" + p.Name
}

// propDetail renders the type/required/default summary shown beside a field.
func propDetail(p propInfo) string {
	var parts []string
	if p.Type != "" {
		parts = append(parts, p.Type)
	}
	if p.Required {
		parts = append(parts, "required")
	}
	if p.Default != nil {
		if r := renderDefault(p.Default); r != "" {
			parts = append(parts, "default: "+r)
		}
	}
	return strings.Join(parts, " · ")
}

// renderDefault renders a schema default for display (strings unquoted).
func renderDefault(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// completeEnum offers a value position's admissible tokens.
func completeEnum(vals []string) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(vals))
	for _, v := range vals {
		items = append(items, protocol.CompletionItem{
			Label: v,
			Kind:  protocol.CompletionItemKindEnumMember,
		})
	}
	return items
}

// completeScenarios offers the canonical IBCS scenario slots, filtered to the
// columns the bound dataset emits when available is non-nil. When available is
// nil (columns not yet known), it returns the full set and reports incomplete so
// the editor re-queries once the cache warms.
func completeScenarios(available map[string]bool) (items []protocol.CompletionItem, incomplete bool) {
	for _, c := range dataset.StandardColumns() {
		if c.Group != "Measures" {
			continue
		}
		if available != nil && !available[c.Name] {
			continue
		}
		item := protocol.CompletionItem{
			Label: c.Name,
			Kind:  protocol.CompletionItemKindValue,
		}
		if doc := scenarioMeaning(c.Name); doc != "" {
			item.Documentation = protocol.String(doc)
		}
		items = append(items, item)
	}
	items = append(items, protocol.CompletionItem{
		Label:         "auto",
		Kind:          protocol.CompletionItemKindKeyword,
		Documentation: protocol.String("Let bino infer the scenario from the column."),
	})
	return items, available == nil
}

// completeVariances offers a guided builder for the variance grammar
// d{B}_{A}_{sentiment} / dr{B}_{A}_{sentiment} plus concrete combinations of
// the bound scenarios. The builder is a snippet, so it only appears for
// snippet-capable clients.
func completeVariances(scenarios []string, snippets bool) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	if snippets {
		items = append(items, protocol.CompletionItem{
			Label:            "d…/dr…_…_… (variance builder)",
			Kind:             protocol.CompletionItemKindSnippet,
			InsertText:       protocol.NewOptional("${1|d,dr|}${2:ac1}_${3:pp1}_${4|pos,neg,neu|}"),
			InsertTextFormat: protocol.InsertTextFormatSnippet,
			Documentation:    protocol.String("Variance of {3} vs {2} with a sentiment suffix; 'd' is absolute, 'dr' is relative (%)."),
		})
	}
	// Concrete pos-sentiment variances for each ordered pair, absolute and
	// relative interleaved so both prefixes survive the cap.
	const maxItems = 48
	for i := 0; i < len(scenarios) && len(items)+2 <= maxItems; i++ {
		for j := 0; j < len(scenarios) && len(items)+2 <= maxItems; j++ {
			if i == j {
				continue
			}
			for _, prefix := range []string{"d", "dr"} {
				token := prefix + scenarios[i] + "_" + scenarios[j] + "_pos"
				items = append(items, protocol.CompletionItem{
					Label:         token,
					Kind:          protocol.CompletionItemKindValue,
					Documentation: protocol.String(varianceMeaning(token)),
				})
			}
		}
	}
	return items
}

// completeColumns offers raw column names — used inside a DataSet query/prql
// block scalar to complete the upstream source's columns.
func completeColumns(cols []string) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(cols))
	for _, c := range cols {
		items = append(items, protocol.CompletionItem{
			Label: c,
			Kind:  protocol.CompletionItemKindField,
		})
	}
	return items
}

// completeRefs offers manifest names valid at a reference position. An empty
// refKind (e.g. a layout child `ref:` with no sibling kind yet) offers every
// document. origins annotates names installed from the package registry.
func completeRefs(index []IndexDoc, refKind string, origins map[string]pkgOrigin) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(index))
	for _, d := range index {
		if refKind != "" && d.Kind != refKind {
			continue
		}
		item := protocol.CompletionItem{
			Label:      d.Name,
			Kind:       protocol.CompletionItemKindReference,
			Detail:     protocol.NewOptional(d.Kind),
			FilterText: protocol.NewOptional(d.Name),
		}
		if o, ok := origins[normPath(d.File)]; ok {
			item.Detail = protocol.NewOptional(d.Kind + " · " + o.Name + "@" + o.Version + " (registry)")
			if o.Tag != "" {
				item.Documentation = protocol.String("Follows tag '" + o.Tag + "'.")
			}
		}
		items = append(items, item)
	}
	return items
}

// completeParamKeys offers the target document's declared params not already
// present in the `params:` mapping.
func completeParamKeys(declared []config.LayoutPageParamSpec, present map[string]bool) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(declared))
	for _, p := range declared {
		if present[p.Name] {
			continue
		}
		item := protocol.CompletionItem{
			Label:            p.Name,
			Kind:             protocol.CompletionItemKindField,
			Detail:           protocol.NewOptional(paramDetail(p)),
			InsertText:       protocol.NewOptional(p.Name + ": "),
			InsertTextFormat: protocol.InsertTextFormatPlainText,
		}
		if doc := paramDoc(p); doc != "" {
			item.Documentation = protocol.String(doc)
		}
		items = append(items, item)
	}
	return items
}

// completeParamValues offers the value candidates a declared param admits.
func completeParamValues(decl *config.LayoutPageParamSpec) []protocol.CompletionItem {
	if decl == nil {
		return nil
	}
	switch decl.Type {
	case "select":
		if decl.Options == nil {
			return nil
		}
		items := make([]protocol.CompletionItem, 0, len(decl.Options.Items))
		for _, o := range decl.Options.Items {
			item := protocol.CompletionItem{Label: o.Value, Kind: protocol.CompletionItemKindEnumMember}
			if o.Label != "" {
				item.Documentation = protocol.String(o.Label)
			}
			if decl.Default != nil && o.Value == *decl.Default {
				item.Detail = protocol.NewOptional("default")
			}
			items = append(items, item)
		}
		return items
	case "boolean":
		return []protocol.CompletionItem{
			{Label: "true", Kind: protocol.CompletionItemKindValue},
			{Label: "false", Kind: protocol.CompletionItemKindValue},
		}
	default:
		if decl.Default != nil && *decl.Default != "" {
			return []protocol.CompletionItem{{
				Label:  *decl.Default,
				Kind:   protocol.CompletionItemKindValue,
				Detail: protocol.NewOptional("default"),
			}}
		}
		return nil
	}
}

// paramDetail renders a declaration's type plus its required/default marker.
func paramDetail(p config.LayoutPageParamSpec) string {
	t := p.Type
	if t == "" {
		t = "string"
	}
	switch {
	case p.Default != nil:
		return t + " · default: " + *p.Default
	case p.Required:
		return t + " · required"
	}
	return t
}

// paramDoc renders a declaration's description plus select options.
func paramDoc(p config.LayoutPageParamSpec) string {
	doc := p.Description
	if p.Type == "select" && p.Options != nil && len(p.Options.Items) > 0 {
		values := make([]string, 0, len(p.Options.Items))
		for _, it := range p.Options.Items {
			values = append(values, it.Value)
		}
		if doc != "" {
			doc += "\n\n"
		}
		doc += "Options: " + strings.Join(values, ", ")
	}
	return doc
}

// findParam returns the declaration named name, or nil.
func findParam(declared []config.LayoutPageParamSpec, name string) *config.LayoutPageParamSpec {
	for i := range declared {
		if declared[i].Name == name {
			return &declared[i]
		}
	}
	return nil
}

// rulesetKeywordItems offers the engine's ruleset inheritance keywords alongside
// named RuleSet references.
func rulesetKeywordItems() []protocol.CompletionItem {
	return []protocol.CompletionItem{
		{
			Label:  "inherited-closest",
			Kind:   protocol.CompletionItemKindKeyword,
			Detail: protocol.NewOptional("inherit from the nearest layout card or page"),
		},
		{
			Label:  "inherited-page",
			Kind:   protocol.CompletionItemKindKeyword,
			Detail: protocol.NewOptional("inherit from the surrounding layout page"),
		},
	}
}

// scenarioSlots returns the scenario slot names (ac1..pl4) present in a column
// set, preserving canonical order — the input to the variance builder.
func scenarioSlots(available map[string]bool) []string {
	var out []string
	for _, c := range dataset.StandardColumns() {
		if c.Group != "Measures" {
			continue
		}
		if available == nil || available[c.Name] {
			out = append(out, c.Name)
		}
	}
	return out
}
