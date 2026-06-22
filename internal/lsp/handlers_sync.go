package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

// DidOpen stores the opened buffer and schedules analysis.
func (s *Server) DidOpen(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
	td := params.TextDocument
	s.docs.Set(td.URI, td.Text, td.Version)
	s.invalidateNav()
	s.analyzer.Schedule(td.URI)
	return nil
}

// DidChange applies a Full-sync update (the whole document text) and reschedules
// analysis. Incremental changes are not negotiated, so a single whole-document
// change is expected.
func (s *Server) DidChange(_ context.Context, params *protocol.DidChangeTextDocumentParams) error {
	text, ok := wholeText(params.ContentChanges)
	if !ok {
		return nil
	}
	s.docs.Set(params.TextDocument.URI, text, params.TextDocument.Version)
	s.invalidateNav()
	s.analyzer.Schedule(params.TextDocument.URI)
	return nil
}

// DidSave lets the project-change path (watcher / SSE) drive re-validation; the
// buffer already matches disk and was validated on the last didChange, so we do
// not re-run buffer validation here (avoids double-analyze).
func (s *Server) DidSave(_ context.Context, _ *protocol.DidSaveTextDocumentParams) error {
	return nil
}

// DidClose drops the buffer and cancels its analysis.
func (s *Server) DidClose(_ context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.analyzer.Cancel(params.TextDocument.URI)
	s.docs.Remove(params.TextDocument.URI)
	s.invalidateNav()
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
