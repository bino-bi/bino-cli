package lsp

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

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
	pc, rawValue, ok := resolveForCompletion(doc.Text, line, col)
	if !ok {
		return nil, nil
	}
	items, incomplete := s.assembleCompletion(ctx, doc, pc, rawValue)
	padAfterColon(doc, params.Position, items)
	if incomplete {
		return &protocol.CompletionList{IsIncomplete: true, Items: items}, nil
	}
	return protocol.CompletionItemSlice(items), nil
}

// padAfterColon prefixes the insert text with a space when the cursor sits
// immediately after a ':' — with the colon as a trigger character, accepting a
// value completion must produce `key: value`, not `key:value`. Items carrying
// an explicit TextEdit manage their own replacement range and are left alone.
func padAfterColon(doc *Document, pos protocol.Position, items []protocol.CompletionItem) {
	if pos.Character == 0 {
		return
	}
	text, ok := doc.lineText(int(pos.Line) + 1)
	if !ok {
		return
	}
	// Byte offset of the UTF-16 cursor within the line (the position is UTF-16
	// units; the buffer is UTF-8 bytes).
	byteOff, u16 := 0, 0
	for _, r := range text {
		if u16 >= int(pos.Character) {
			break
		}
		u16 += utf16Len(r)
		byteOff += utf8.RuneLen(r)
	}
	if byteOff == 0 || byteOff > len(text) || text[byteOff-1] != ':' {
		return
	}
	for i := range items {
		if items[i].TextEdit != nil {
			continue
		}
		text := items[i].Label
		if t, ok := items[i].InsertText.Get(); ok {
			text = t
		}
		items[i].InsertText = protocol.NewOptional(" " + text)
	}
}

// resolveForCompletion resolves the cursor, falling back to a quoted repair of
// an unquoted `@...` value on the cursor line (invalid YAML that breaks the
// whole-document parse) so completion keeps working while an author types a
// registry ref. rawValue reports that the buffer carries the token unquoted:
// the accepted completion must then replace the raw span with a quoted value.
func resolveForCompletion(text string, line, col int) (pc reportspec.PositionContext, rawValue bool, ok bool) {
	if pc, ok := reportspec.ResolvePositionPath(text, line, col); ok {
		return pc, false, true
	}
	repaired, _, raw, ok := reportspec.RepairUnquotedAt(text, line)
	if !ok {
		return reportspec.PositionContext{}, false, false
	}
	rcol := col
	if col > raw.StartCol {
		rcol = col + 1 // account for the inserted opening quote
	}
	pc, ok = reportspec.ResolvePositionPath(repaired, line, rcol)
	if !ok {
		return reportspec.PositionContext{}, false, false
	}
	pc.ReplaceRange = raw // replace the raw unquoted token; NewText adds quotes
	return pc, true, true
}

// CompletionResolve is a passthrough; items already carry their documentation.
func (s *Server) CompletionResolve(_ context.Context, item *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	return item, nil
}

func (s *Server) assembleCompletion(ctx context.Context, doc *Document, pc reportspec.PositionContext, rawValue bool) (items []protocol.CompletionItem, incomplete bool) {
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
		schema := s.getSchema(ctx)
		if pc.Path == "kind" {
			// The document root `kind:` — the schema's full kind enum, which
			// includes plugin kinds the nested layoutChild enum excludes.
			return completeKinds(schema), schema.empty()
		}
		vals := schema.resolveAt(pathSegments(pc.Path), pc.KindsByPath).enumValues()
		return completeEnum(vals), schema.empty()
	case reportspec.PosKey:
		schema := s.getSchema(ctx)
		node := schema.resolveAt(pathSegments(pc.Path), pc.KindsByPath)
		items := completeFields(node.props(), keySet(pc.PresentKeys))
		if pc.Path == "(root)" && len(pc.PresentKeys) == 0 {
			// A fresh document (empty buffer, or right after `---`): offer
			// full-manifest scaffolds alongside the bare root keys.
			items = append(items, documentScaffolds(schema, s.snippetSupport)...)
		}
		return items, schema.empty()
	case reportspec.PosScenarioItem:
		available := s.scenarioColumns(ctx, pc.BoundDatasets)
		return completeScenarios(available)
	case reportspec.PosVarianceItem:
		available := s.scenarioColumns(ctx, pc.BoundDatasets)
		return completeVariances(scenarioSlots(available), s.snippetSupport), available == nil
	case reportspec.PosDatasetRef:
		idx := s.getIndex(ctx)
		refs := completeRefs(idx, pc.RefKind, s.packageOrigins())
		if pc.FieldName == "ruleset" {
			refs = append(refs, rulesetKeywordItems()...)
		}
		if pc.FieldName == "ref" {
			applyRefTextEdits(refs, doc, pc, rawValue)
		}
		return refs, idx == nil
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
		schema := s.getSchema(ctx)
		node := schema.resolveAt(pathSegments(pc.Path), pc.KindsByPath)
		if enum := node.enumValues(); len(enum) > 0 {
			return completeEnum(enum), false
		}
		if node.isBool() {
			return completeEnum([]string{"true", "false"}), false
		}
		if node.isObject() {
			// An object-shaped position typed as a scalar so far — e.g. a bare
			// `- ` slot under `children:`, or the first key being typed under a
			// still-empty `spec:` — offer the object's keys, plus child
			// scaffolds when the slot is a layout child.
			items := completeFields(node.props(), nil)
			items = append(items, childScaffolds(node, s.snippetSupport)...)
			return items, schema.empty()
		}
		return nil, schema.empty()
	default:
		return nil, false
	}
}

// pathSegments converts a resolver dotted path to schema-walk segments; the
// "(root)" sentinel and the empty path resolve to the document root.
func pathSegments(dotted string) []string {
	if dotted == "" || dotted == "(root)" {
		return nil
	}
	return strings.Split(dotted, ".")
}

// applyRefTextEdits pins each ref candidate to the resolved value range so
// clients replace the exact scalar, quoting names YAML reserves (`@scope/...`)
// when the buffer carries no quotes yet — a plain scalar cannot start with `@`,
// so a prefix already starting with `@` implies existing quotes, UNLESS the
// resolve went through the unquoted-`@` repair (rawValue).
func applyRefTextEdits(items []protocol.CompletionItem, doc *Document, pc reportspec.PositionContext, rawValue bool) {
	rng := doc.RangeToProtocol(pc.ReplaceRange)
	for i := range items {
		text := items[i].Label
		if strings.HasPrefix(text, "@") && (rawValue || !strings.HasPrefix(pc.Prefix, "@")) {
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
			// Debug, not Warn: this fires per keystroke under the 100ms
			// budget and would spam the editor log when the backend is slow.
			s.log.Debugf("column completion: columns for %q unavailable: %v", ds, err)
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
	case reportspec.PosKindValue:
		schema := s.getSchema(ctx)
		// A hovered kind token gets the kind's own prose; fall back to the
		// position's schema description (e.g. the layoutChild kind slot doc).
		if md := kindHover(schema, pc.Prefix); md != "" {
			return md
		}
		return schema.resolveAt(pathSegments(pc.Path), pc.KindsByPath).doc()
	case reportspec.PosKey:
		// pc.Path is the enclosing MAPPING's path; the hovered key token is
		// pc.Prefix (empty on a blank line — nothing to document).
		node := s.getSchema(ctx).resolveAt(pathSegments(pc.Path), pc.KindsByPath)
		if p, ok := node.prop(pc.Prefix); ok {
			return propHover(p)
		}
		return ""
	case reportspec.PosFreeValue:
		return valueHover(s.getSchema(ctx).resolveAt(pathSegments(pc.Path), pc.KindsByPath))
	default:
		return ""
	}
}

// kindHover renders a known kind's title, spec prose, and required fields.
func kindHover(m *schemaModel, kind string) string {
	if kind == "" {
		return ""
	}
	doc := kindDocMarkdown(m, kind)
	if doc == "" {
		return ""
	}
	return "**" + kind + "**\n\n" + doc
}

// propHover renders a field's schema metadata for a hovered key token.
func propHover(p propInfo) string {
	var parts []string
	if d := propDetail(p); d != "" {
		parts = append(parts, "`"+p.Name+"` — "+d)
	}
	if p.Description != "" {
		parts = append(parts, p.Description)
	}
	if len(p.Enum) > 0 {
		parts = append(parts, "One of: `"+strings.Join(p.Enum, "`, `")+"`")
	}
	return strings.Join(parts, "\n\n")
}

// valueHover renders a value position's schema doc: description, default, and
// admissible values.
func valueHover(n schemaNode) string {
	var parts []string
	if d := n.doc(); d != "" {
		parts = append(parts, d)
	}
	if def := n.defaultValue(); def != nil {
		if r := renderDefault(def); r != "" {
			parts = append(parts, "Default: `"+r+"`")
		}
	}
	if enum := n.enumValues(); len(enum) > 0 {
		parts = append(parts, "One of: `"+strings.Join(enum, "`, `")+"`")
	}
	return strings.Join(parts, "\n\n")
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
