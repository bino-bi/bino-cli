package lsp

import (
	"context"
	"net"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// noopClient satisfies protocol.Client for the transport test. PublishDiagnostics
// MUST return nil: the same jsonrpc2 rule that motivates the fix applies to the
// client end too — a non-nil error from a notification handler tears down the
// connection (the server pushes diagnostics on didOpen).
type noopClient struct{ protocol.UnimplementedClient }

func (noopClient) PublishDiagnostics(context.Context, *protocol.PublishDiagnosticsParams) error {
	return nil
}

// TestServer_LifecycleNotificationsReturnNil guards the contract directly: these
// handlers must return nil, never the embedded UnimplementedServer's
// errNotImplemented (which jsonrpc2 treats as a fatal connection error).
func TestServer_LifecycleNotificationsReturnNil(t *testing.T) {
	s := newTestServer()
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"SetTrace", func() error { return s.SetTrace(ctx, &protocol.SetTraceParams{}) }},
		{"DidChangeConfiguration", func() error {
			return s.DidChangeConfiguration(ctx, &protocol.DidChangeConfigurationParams{})
		}},
		{"DidChangeWatchedFiles", func() error {
			return s.DidChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{})
		}},
		{"DidChangeWorkspaceFolders", func() error {
			return s.DidChangeWorkspaceFolders(ctx, &protocol.DidChangeWorkspaceFoldersParams{})
		}},
		{"WorkDoneProgressCancel", func() error {
			return s.WorkDoneProgressCancel(ctx, &protocol.WorkDoneProgressCancelParams{})
		}},
		{"Progress", func() error { return s.Progress(ctx, &protocol.ProgressParams{}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Errorf("%s must be a no-op (return nil), got %v", tc.name, err)
			}
		})
	}
}

// TestServer_SetTraceDoesNotKillConnection drives the real jsonrpc2 transport end
// to end. vscode-languageclient sends $/setTrace on every editor configuration
// change; before the lifecycle no-op handlers existed it fell through to
// UnimplementedServer, whose errNotImplemented made jsonrpc2 fail the connection
// — silencing every later request. This is the editor-only failure that no
// direct-handler test (which never sends notifications over the wire) caught.
func TestServer_SetTraceDoesNotKillConnection(t *testing.T) {
	ctx := t.Context()

	srv := newTestServer()
	serverEnd, clientEnd := net.Pipe()
	go func() { _ = srv.Serve(ctx, jsonrpc2.NewStream(serverEnd)) }()
	_, _, remote := protocol.NewClient(ctx, noopClient{}, jsonrpc2.NewStream(clientEnd))

	u := uri.File("/proj/report.yaml")
	completion := func() error {
		cctx, c := context.WithTimeout(ctx, 3*time.Second)
		defer c()
		// Cursor on `sales` in the `dataset:` reference (line 4, col 12).
		_, err := remote.Completion(cctx, completionParams(u, 4, 12))
		return err
	}

	if _, err := remote.Initialize(ctx, &protocol.InitializeParams{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := remote.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("initialized: %v", err)
	}
	if err := remote.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: u, LanguageID: "yaml", Version: 1, Text: tableDoc},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	// Sanity: completion answers before the notification.
	if err := completion(); err != nil {
		t.Fatalf("completion before $/setTrace failed: %v", err)
	}

	// The notification vscode-languageclient sends on every config change.
	if err := remote.SetTrace(ctx, &protocol.SetTraceParams{Value: protocol.TraceValueOff}); err != nil {
		t.Fatalf("setTrace notify: %v", err)
	}

	// Regression: the connection must still answer. Pre-fix this hung until the
	// per-call deadline and returned an error.
	if err := completion(); err != nil {
		t.Fatalf("completion after $/setTrace failed — the notification killed the connection: %v", err)
	}
}
