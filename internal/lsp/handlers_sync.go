package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

// DidOpen stores the opened buffer and schedules analysis.
func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	td := params.TextDocument
	doc := s.docs.Set(td.URI, td.Text, td.Version)
	s.analyze(ctx, doc)
	return nil
}

// DidChange applies a Full-sync update (the whole document text) and reschedules
// analysis. Incremental changes are not negotiated, so a single whole-document
// change is expected.
func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	text, ok := wholeText(params.ContentChanges)
	if !ok {
		return nil
	}
	doc := s.docs.Set(params.TextDocument.URI, text, params.TextDocument.Version)
	s.analyze(ctx, doc)
	return nil
}

// analyze refreshes the name index for an edited buffer and schedules its
// validation. bino.toml is checked inline: it is not a manifest, and the
// draft validator would publish an empty result over its findings.
func (s *Server) analyze(ctx context.Context, doc *Document) {
	if isBinoTOML(doc.Path) {
		s.publishDraft(doc.URI, doc.Version, tomlDiagnostics(doc))
		return
	}
	s.updateNav(ctx, doc.Path)
	s.analyzer.Schedule(doc.URI)
}

// DidSave lets the project-change path (watcher / SSE) drive re-validation; the
// buffer already matches disk and was validated on the last didChange, so we do
// not re-run buffer validation here (avoids double-analyze). It only schedules
// the opt-in DuckDB check of the file's query blocks.
func (s *Server) DidSave(_ context.Context, params *protocol.DidSaveTextDocumentParams) error {
	s.scheduleQueryCheck(params.TextDocument.URI)
	return nil
}

// DidClose drops the buffer, cancels its analysis, and clears its draft
// diagnostics so only on-disk (project) findings remain visible for the file.
func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.analyzer.Cancel(params.TextDocument.URI)
	s.docs.Remove(params.TextDocument.URI)
	s.updateNav(ctx, params.TextDocument.URI.FsPath())
	s.clearDraft(params.TextDocument.URI)
	return nil
}

// wholeText extracts the full document text from a Full-sync content-change list.
func wholeText(changes []protocol.TextDocumentContentChangeEvent) (string, bool) {
	if len(changes) == 0 {
		return "", false
	}
	switch c := changes[len(changes)-1].(type) {
	case *protocol.TextDocumentContentChangeWholeDocument:
		return c.Text, true
	case *protocol.TextDocumentContentChangePartial:
		return c.Text, true
	default:
		return "", false
	}
}
