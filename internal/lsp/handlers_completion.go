package lsp

import (
	"context"
	"runtime/debug"
	"strings"
	"time"

	"go.lsp.dev/protocol"

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
		return completeRefs(s.getIndex(ctx), pc.RefKind), false
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
		cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		cols, err := s.backend.Columns(cctx, strings.TrimPrefix(pc.Prefix, "$"))
		if err != nil || len(cols) == 0 {
			return ""
		}
		return "**" + pc.Prefix + "** columns:\n\n" + "`" + strings.Join(cols, "`, `") + "`"
	case reportspec.PosKey, reportspec.PosFreeValue:
		return s.getSchema(ctx).fieldDoc(pc.EnclosingKind, pc.FieldName)
	default:
		return ""
	}
}
