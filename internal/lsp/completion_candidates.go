package lsp

import (
	"sort"

	"go.lsp.dev/protocol"

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

// completeRefs offers manifest names valid at a reference position. An empty
// refKind (e.g. a layout child `ref:` whose target kind is contextual) offers
// every document.
func completeRefs(index []IndexDoc, refKind string) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(index))
	for _, d := range index {
		if refKind != "" && d.Kind != refKind {
			continue
		}
		items = append(items, protocol.CompletionItem{
			Label:  d.Name,
			Kind:   protocol.CompletionItemKindReference,
			Detail: protocol.NewOptional(d.Kind),
		})
	}
	return items
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
