package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

// Client-sent notifications bino does not act on. They MUST return nil rather
// than fall through to protocol.UnimplementedServer (which returns a non-nil
// errNotImplemented): go.lsp.dev/jsonrpc2 treats a non-nil error from a
// notification handler as a fatal connection error (dispatch.go calls c.fail),
// which silently tears down the session. vscode-languageclient sends $/setTrace
// on every editor configuration change, so an unhandled notification kills
// completion/definition/hover/diagnostics the moment any setting changes.

// SetTrace acknowledges a trace-level change. bino logs to stderr regardless, so
// there is nothing to toggle; the handler exists purely to keep the connection
// alive.
func (s *Server) SetTrace(_ context.Context, _ *protocol.SetTraceParams) error { return nil }

// DidChangeConfiguration acknowledges a client configuration push. bino reads no
// client settings (the project root comes from --work-dir), so this is a no-op.
func (s *Server) DidChangeConfiguration(_ context.Context, _ *protocol.DidChangeConfigurationParams) error {
	return nil
}

// DidChangeWatchedFiles is a no-op: the backend already watches the project on
// disk itself (in-process watcher / daemon SSE), and bino registers no client
// file watchers.
func (s *Server) DidChangeWatchedFiles(_ context.Context, _ *protocol.DidChangeWatchedFilesParams) error {
	return nil
}

// DidChangeWorkspaceFolders is a no-op: bino is single-root (scoped by
// --work-dir) and does not track client workspace folders.
func (s *Server) DidChangeWorkspaceFolders(_ context.Context, _ *protocol.DidChangeWorkspaceFoldersParams) error {
	return nil
}

// WorkDoneProgressCancel is a no-op: bino initiates no work-done progress, so
// there is nothing to cancel.
func (s *Server) WorkDoneProgressCancel(_ context.Context, _ *protocol.WorkDoneProgressCancelParams) error {
	return nil
}

// Progress is a no-op: bino neither reports nor consumes progress.
func (s *Server) Progress(_ context.Context, _ *protocol.ProgressParams) error { return nil }
