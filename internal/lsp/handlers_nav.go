package lsp

import (
	"context"
	"sort"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	reportspec "bino.bi/bino/internal/report/spec"
)

// Definition jumps from a reference (dataset/source/ref/page/...) to the manifest
// that declares it.
func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	pc, ok := s.resolve(params.TextDocument.URI, params.Position)
	if !ok || pc.Kind != reportspec.PosDatasetRef {
		return nil, nil
	}
	name := strings.TrimPrefix(pc.Prefix, "$")
	def, found := s.getNameIndex(ctx).Definition(pc.RefKind, name)
	if !found {
		return nil, nil
	}
	return protocol.LocationSlice{locationOf(def.File, def.NameRange)}, nil
}

// Implementation resolves one hop to the implementing manifest. bino references
// are largely 1:1, so it mirrors Definition (jump to the declaring manifest).
func (s *Server) Implementation(ctx context.Context, params *protocol.ImplementationParams) (protocol.DefinitionResult, error) {
	return s.Definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: params.TextDocumentPositionParams,
	})
}

// References returns every site that uses the symbol under the cursor (a
// metadata.name definition or a reference value).
func (s *Server) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	pc, ok := s.resolve(params.TextDocument.URI, params.Position)
	if !ok {
		return nil, nil
	}
	kind, name := s.symbolUnderCursor(pc)
	if name == "" {
		return nil, nil
	}
	ni := s.getNameIndex(ctx)
	var locs []protocol.Location
	if params.Context.IncludeDeclaration {
		if def, found := ni.Definition(kind, name); found {
			locs = append(locs, locationOf(def.File, def.NameRange))
		}
	}
	for _, r := range ni.References(kind, name) {
		locs = append(locs, locationOf(r.File, r.Range))
	}
	return locs, nil
}

// DocumentSymbol returns the manifests declared in a file as a navigable outline.
func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	doc, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	defs := s.getNameIndex(ctx).DefsInFile(doc.Path)
	sort.Slice(defs, func(i, j int) bool { return defs[i].DocIndex < defs[j].DocIndex })
	syms := make(protocol.DocumentSymbolSlice, 0, len(defs))
	for _, d := range defs {
		rng := RangeToProtocol(d.DocRange)
		detail := d.Kind
		syms = append(syms, protocol.DocumentSymbol{
			Name:           d.Name,
			Detail:         &detail,
			Kind:           symbolKind(d.Kind),
			Range:          rng,
			SelectionRange: RangeToProtocol(d.NameRange),
		})
	}
	return syms, nil
}

// Symbols answers workspace/symbol: a fuzzy match over every manifest name.
func (s *Server) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	q := strings.ToLower(params.Query)
	out := protocol.SymbolInformationSlice{}
	for _, d := range s.getNameIndex(ctx).Defs() {
		if q != "" && !strings.Contains(strings.ToLower(d.Name), q) {
			continue
		}
		out = append(out, protocol.SymbolInformation{
			BaseSymbolInformation: protocol.BaseSymbolInformation{
				Name: d.Name,
				Kind: symbolKind(d.Kind),
			},
			Location: locationOf(d.File, d.NameRange),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// symbolUnderCursor returns the (kind, name) of the symbol at a resolved position:
// a metadata.name definition or a reference value.
func (s *Server) symbolUnderCursor(pc reportspec.PositionContext) (kind, name string) {
	switch {
	case pc.Path == "metadata.name":
		return pc.EnclosingKind, pc.Prefix
	case pc.Kind == reportspec.PosDatasetRef:
		return pc.RefKind, strings.TrimPrefix(pc.Prefix, "$")
	default:
		return "", ""
	}
}

func locationOf(file string, r reportspec.Range) protocol.Location {
	return protocol.Location{URI: uri.File(file), Range: RangeToProtocol(r)}
}

// symbolKind maps a bino kind to an LSP SymbolKind for outline icons.
func symbolKind(kind string) protocol.SymbolKind {
	switch kind {
	case "DataSource", "ConnectionSecret":
		return protocol.SymbolKindStruct
	case "DataSet":
		return protocol.SymbolKindObject
	case "LayoutPage", "ReportArtefact", "DocumentArtefact":
		return protocol.SymbolKindModule
	default:
		return protocol.SymbolKindClass
	}
}
