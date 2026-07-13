package lsp

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"go.lsp.dev/protocol"

	"bino.bi/bino/internal/report/config"
	reportspec "bino.bi/bino/internal/report/spec"
)

// Completion resolves the cursor to a PositionContext and assembles the matching
// candidate set. It serves from cached schema/index and bounds live column
// introspection so the popup is never blocked.
func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (protocol.CompletionResult, error) {
	doc, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	line, col := doc.PositionToLineCol(params.Position)
	pc, ok := reportspec.ResolvePositionPath(doc.Text, line, col)
	if !ok {
		return nil, nil
	}
	items, incomplete := s.assembleCompletion(ctx, pc)
	if incomplete {
		return &protocol.CompletionList{IsIncomplete: true, Items: items}, nil
	}
	return protocol.CompletionItemSlice(items), nil
}

// CompletionResolve is a passthrough; items already carry their documentation.
func (s *Server) CompletionResolve(_ context.Context, item *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	return item, nil
}

func (s *Server) assembleCompletion(ctx context.Context, pc reportspec.PositionContext) (items []protocol.CompletionItem, incomplete bool) {
	// A completion panic must never crash the server or break the editor session;
	// log it and return no suggestions.
	defer func() {
		if r := recover(); r != nil {
			s.log.Errorf("assembleCompletion panic (kind=%d field=%q): %v\n%s", pc.Kind, pc.FieldName, r, debug.Stack())
			items, incomplete = nil, false
		}
	}()
	switch pc.Kind {
	case reportspec.PosKindValue:
		return completeKinds(s.getSchema(ctx)), false
	case reportspec.PosKey:
		return completeFields(s.getSchema(ctx), pc.EnclosingKind, nil), false
	case reportspec.PosScenarioItem:
		available := s.scenarioColumns(ctx, pc.BoundDatasets)
		return completeScenarios(available)
	case reportspec.PosVarianceItem:
		available := s.scenarioColumns(ctx, pc.BoundDatasets)
		return completeVariances(scenarioSlots(available)), available == nil
	case reportspec.PosDatasetRef:
		refs := completeRefs(s.getIndex(ctx), pc.RefKind, s.packageOrigins())
		if pc.FieldName == "ruleset" {
			refs = append(refs, rulesetKeywordItems()...)
		}
		if pc.FieldName == "ref" {
			applyRefTextEdits(refs, pc)
		}
		return refs, false
	case reportspec.PosParamKey:
		decls, ok := s.paramsForTarget(ctx, pc.RefKind, pc.RefName)
		if !ok {
			return nil, false
		}
		return completeParamKeys(decls, keySet(pc.PresentKeys)), false
	case reportspec.PosParamValue:
		decls, ok := s.paramsForTarget(ctx, pc.RefKind, pc.RefName)
		if !ok {
			return nil, false
		}
		return completeParamValues(findParam(decls, pc.FieldName)), false
	case reportspec.PosQueryScalar:
		cols := s.unionColumns(ctx, pc.BoundDatasets)
		if cols == nil {
			return nil, true // not warm yet — re-query once introspection lands
		}
		return completeColumns(cols), false
	case reportspec.PosFreeValue:
		if enum := completeEnum(s.getSchema(ctx), pc.EnclosingKind, pc.FieldName); len(enum) > 0 {
			return enum, false
		}
		return nil, false
	default:
		return nil, false
	}
}

// applyRefTextEdits pins each ref candidate to the resolved value range so
// clients replace the exact scalar, quoting names YAML reserves (`@scope/...`)
// when the buffer carries no quotes yet — a plain scalar cannot start with `@`,
// so a prefix already starting with `@` implies existing quotes.
func applyRefTextEdits(items []protocol.CompletionItem, pc reportspec.PositionContext) {
	rng := RangeToProtocol(pc.ReplaceRange)
	for i := range items {
		text := items[i].Label
		if strings.HasPrefix(text, "@") && !strings.HasPrefix(pc.Prefix, "@") {
			text = "\"" + text + "\""
		}
		items[i].TextEdit = &protocol.TextEdit{Range: rng, NewText: text}
	}
}

// keySet converts a key list to a membership set (nil for empty).
func keySet(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return set
}

// unionColumns returns the deduped, order-preserving union of the bound
// datasets' columns, bounded by a 100ms deadline. It returns nil when no columns
// are available in time so the caller can mark the list incomplete.
func (s *Server) unionColumns(ctx context.Context, datasets []string) []string {
	if len(datasets) == 0 {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	seen := make(map[string]bool)
	var out []string
	got := false
	for _, ds := range datasets {
		cols, err := s.backend.Columns(cctx, strings.TrimPrefix(ds, "$"))
		if err != nil {
			continue
		}
		got = true
		for _, c := range cols {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	if !got {
		return nil
	}
	return out
}

// scenarioColumns returns the bound datasets' columns as a membership set (for
// intersecting the canonical scenario slots), or nil when unavailable in time.
func (s *Server) scenarioColumns(ctx context.Context, datasets []string) map[string]bool {
	cols := s.unionColumns(ctx, datasets)
	if cols == nil {
		return nil
	}
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		set[c] = true
	}
	return set
}

// Hover explains the token under the cursor: scenario/variance meaning, a
// referenced dataset's columns, or a field's schema description.
func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	doc, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	line, col := doc.PositionToLineCol(params.Position)
	pc, ok := reportspec.ResolvePositionPath(doc.Text, line, col)
	if !ok {
		return nil, nil
	}
	md := s.hoverText(ctx, pc)
	if md == "" {
		return nil, nil
	}
	return &protocol.Hover{Contents: &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: md}}, nil
}

func (s *Server) hoverText(ctx context.Context, pc reportspec.PositionContext) string {
	switch pc.Kind {
	case reportspec.PosScenarioItem:
		return scenarioMeaning(pc.Prefix)
	case reportspec.PosVarianceItem:
		return varianceMeaning(pc.Prefix)
	case reportspec.PosDatasetRef:
		if pc.Prefix == "" {
			return ""
		}
		if pc.FieldName == "ref" || pc.FieldName == "page" {
			return s.refTargetHover(ctx, pc)
		}
		cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		cols, err := s.backend.Columns(cctx, strings.TrimPrefix(pc.Prefix, "$"))
		if err != nil || len(cols) == 0 {
			return ""
		}
		return "**" + pc.Prefix + "** columns:\n\n" + "`" + strings.Join(cols, "`, `") + "`"
	case reportspec.PosParamKey, reportspec.PosParamValue:
		decls, ok := s.paramsForTarget(ctx, pc.RefKind, pc.RefName)
		if !ok {
			return ""
		}
		name := pc.FieldName
		if pc.Kind == reportspec.PosParamKey {
			name = pc.Prefix
		}
		return paramDeclMarkdown(findParam(decls, name))
	case reportspec.PosKey, reportspec.PosFreeValue:
		return s.getSchema(ctx).fieldDoc(pc.EnclosingKind, pc.FieldName)
	default:
		return ""
	}
}

// refTargetHover summarizes a component/page reference target: its kind,
// registry origin (from bino.lock), and declared params.
func (s *Server) refTargetHover(ctx context.Context, pc reportspec.PositionContext) string {
	name := pc.Prefix
	def, ok := s.getNameIndex(ctx).Definition(pc.RefKind, name)
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — %s", name, def.Kind)
	if o, ok := s.packageOrigins()[normPath(def.File)]; ok {
		fmt.Fprintf(&b, " · registry %s", o.Version)
		if o.Tag != "" {
			fmt.Fprintf(&b, " (tag: %s)", o.Tag)
		}
	}
	if decls, ok := s.paramsForTarget(ctx, def.Kind, name); ok && len(decls) > 0 {
		b.WriteString("\n\n| Param | Type | Default | Description |\n|---|---|---|---|\n")
		for _, p := range decls {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", p.Name, paramDetail(p), paramDefaultCell(p), p.Description)
		}
	}
	return b.String()
}

// paramDefaultCell renders a declaration's default for the hover table.
func paramDefaultCell(p config.LayoutPageParamSpec) string {
	if p.Default == nil {
		return "—"
	}
	return "`" + *p.Default + "`"
}

// paramDeclMarkdown renders one param declaration for hover.
func paramDeclMarkdown(p *config.LayoutPageParamSpec) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — %s", p.Name, paramDetail(*p))
	if p.Description != "" {
		fmt.Fprintf(&b, "\n\n%s", p.Description)
	}
	if p.Options != nil {
		if len(p.Options.Items) > 0 {
			b.WriteString("\n\nOptions:")
			for _, o := range p.Options.Items {
				fmt.Fprintf(&b, "\n- `%s`", o.Value)
				if o.Label != "" {
					fmt.Fprintf(&b, " — %s", o.Label)
				}
			}
		}
		if p.Options.Min != nil {
			fmt.Fprintf(&b, "\n\nMin: %v", *p.Options.Min)
		}
		if p.Options.Max != nil {
			fmt.Fprintf(&b, "\n\nMax: %v", *p.Options.Max)
		}
	}
	return b.String()
}
