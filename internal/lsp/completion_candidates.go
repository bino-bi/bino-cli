package lsp

import (
	"sort"
	"strings"

	"go.lsp.dev/protocol"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// These pure builders turn a resolved context + project data into completion
// items. They take explicit inputs (no Backend) so they are unit-testable; the
// handler wires live data in.

// completeKinds offers every manifest kind from the live merged schema.
func completeKinds(m *schemaModel) []protocol.CompletionItem {
	kinds := m.kinds()
	sort.Strings(kinds)
	items := make([]protocol.CompletionItem, 0, len(kinds))
	for _, k := range kinds {
		items = append(items, protocol.CompletionItem{
			Label: k,
			Kind:  protocol.CompletionItemKindClass,
		})
	}
	return items
}

// completeFields offers the spec fields of a kind not already present.
func completeFields(m *schemaModel, kind string, present map[string]bool) []protocol.CompletionItem {
	props := m.specProps(kind)
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
		}
		if p.Description != "" {
			item.Documentation = protocol.String(p.Description)
		}
		items = append(items, item)
	}
	return items
}

// completeEnum offers the enum values of spec.<field> for a kind.
func completeEnum(m *schemaModel, kind, field string) []protocol.CompletionItem {
	vals := m.fieldEnum(kind, field)
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
// d{B}_{A}_{sentiment} plus concrete combinations of the bound scenarios.
func completeVariances(scenarios []string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{
		{
			Label:            "d…_…_… (variance builder)",
			Kind:             protocol.CompletionItemKindSnippet,
			InsertText:       protocol.NewOptional("d${1:ac1}_${2:pp1}_${3|pos,neg,neu|}"),
			InsertTextFormat: protocol.InsertTextFormatSnippet,
			Documentation:    protocol.String("Absolute variance of {2} vs {1} with a sentiment suffix. Prefix with 'dr' for a relative (%) variance."),
		},
	}
	// Concrete pos-sentiment variances for each ordered pair, capped for sanity.
	const maxItems = 24
	for i := 0; i < len(scenarios) && len(items) < maxItems; i++ {
		for j := 0; j < len(scenarios) && len(items) < maxItems; j++ {
			if i == j {
				continue
			}
			token := "d" + scenarios[i] + "_" + scenarios[j] + "_pos"
			items = append(items, protocol.CompletionItem{
				Label:         token,
				Kind:          protocol.CompletionItemKindValue,
				Documentation: protocol.String(varianceMeaning(token)),
			})
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
