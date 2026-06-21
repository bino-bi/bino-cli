package lsp

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const dataDoc = `kind: DataSet
metadata:
  name: sales
spec:
  source: sales_src
`

const reportDoc = `kind: Table
metadata:
  name: rev_table
spec:
  dataset: sales
`

// navServer opens a data manifest and a report that references it.
func navServer(t *testing.T) (*Server, uri.URI, uri.URI) {
	t.Helper()
	s := newTestServer()
	dataURI := uri.File("/proj/data.yaml")
	reportURI := uri.File("/proj/report.yaml")
	s.docs.Set(dataURI, dataDoc, 1)
	s.docs.Set(reportURI, reportDoc, 1)
	return s, dataURI, reportURI
}

func TestDefinition(t *testing.T) {
	s, dataURI, reportURI := navServer(t)
	// Cursor on `sales` in report.yaml's `dataset:` (line 5, col ~12).
	res, _ := s.Definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: reportURI},
			Position:     protocol.Position{Line: 4, Character: 12},
		},
	})
	locs, ok := res.(protocol.LocationSlice)
	if !ok || len(locs) != 1 {
		t.Fatalf("expected one definition location, got %T %v", res, res)
	}
	if locs[0].URI != dataURI {
		t.Errorf("definition URI = %s, want %s", locs[0].URI, dataURI)
	}
	if locs[0].Range.Start.Line != 2 { // metadata.name is on line 3 (0-based 2)
		t.Errorf("definition line = %d, want 2", locs[0].Range.Start.Line)
	}
}

func TestReferences(t *testing.T) {
	s, dataURI, _ := navServer(t)
	// Cursor on the `sales` definition (data.yaml metadata.name).
	res, _ := s.References(context.Background(), &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: dataURI},
			Position:     protocol.Position{Line: 2, Character: 8},
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if len(res) != 2 { // the definition + the one dataset reference
		t.Fatalf("references = %d, want 2 (%v)", len(res), res)
	}
}

func TestRename(t *testing.T) {
	s, dataURI, reportURI := navServer(t)
	edit, _ := s.Rename(context.Background(), &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: dataURI},
			Position:     protocol.Position{Line: 2, Character: 8},
		},
		NewName: "revenue",
	})
	if edit == nil {
		t.Fatal("expected a workspace edit")
	}
	if len(edit.Changes[dataURI]) != 1 {
		t.Errorf("data.yaml edits = %d, want 1 (the definition)", len(edit.Changes[dataURI]))
	}
	if len(edit.Changes[reportURI]) != 1 {
		t.Errorf("report.yaml edits = %d, want 1 (the reference)", len(edit.Changes[reportURI]))
	}
	if got := edit.Changes[reportURI][0].NewText; got != "revenue" {
		t.Errorf("reference rewrite = %q, want revenue", got)
	}
}

func TestDocumentSymbol(t *testing.T) {
	s, dataURI, _ := navServer(t)
	res, _ := s.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: dataURI},
	})
	syms, ok := res.(protocol.DocumentSymbolSlice)
	if !ok || len(syms) != 1 {
		t.Fatalf("expected 1 document symbol, got %T %v", res, res)
	}
	if syms[0].Name != "sales" {
		t.Errorf("symbol name = %q, want sales", syms[0].Name)
	}
}

func TestWorkspaceSymbol(t *testing.T) {
	s, _, _ := navServer(t)
	res, _ := s.Symbols(context.Background(), &protocol.WorkspaceSymbolParams{Query: "sal"})
	syms, ok := res.(protocol.SymbolInformationSlice)
	if !ok || len(syms) == 0 {
		t.Fatalf("expected workspace symbols for 'sal', got %T %v", res, res)
	}
	found := false
	for _, sym := range syms {
		if sym.Name == "sales" {
			found = true
		}
	}
	if !found {
		t.Errorf("workspace symbol 'sales' not found in %v", syms)
	}
}
