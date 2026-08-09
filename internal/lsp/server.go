package lsp

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/registry"
	reportspec "bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/version"
)

// Server is bino's Language Server. It implements protocol.Server, owns the
// per-session document/position state, and delegates heavy project data to a
// Backend (in-process or daemon-proxy).
type Server struct {
	protocol.UnimplementedServer

	backend  Backend
	docs     *DocumentStore
	analyzer *Analyzer
	log      logx.Logger
	phase2   bool
	root     string // project root (for .env-targeting code actions)

	// snippetSupport records the client's completionItem.snippetSupport
	// capability (written once during Initialize, before any completion).
	// Snippet-format items (scaffolds, the variance builder) are gated on it.
	snippetSupport bool

	ctx    context.Context
	cancel context.CancelFunc
	client protocol.Client

	mu      sync.RWMutex
	schema  *schemaModel
	index   []IndexDoc
	nameIdx *reportspec.NameIndex
	lock    *registry.Lockfile

	// draftDiags and projectDiags are merged per-file before publishing, since
	// PublishDiagnostics fully replaces a document's diagnostic set per call:
	// draftDiags comes from the per-keystroke Analyzer (ValidateDraft, open
	// buffers only); projectDiags comes from refreshProjectDiagnostics
	// (ValidateProject, whole project on disk — lint/env-var/engine-compat
	// findings that ValidateDraft never sees).
	draftDiags   map[string][]protocol.Diagnostic
	projectDiags map[string][]protocol.Diagnostic
}

// NewServer constructs a Server over the given backend. phase2 advertises the
// navigation/refactor capabilities; root is the project root used by code actions.
func NewServer(backend Backend, log logx.Logger, phase2 bool, root string) *Server {
	return &Server{
		backend: backend,
		docs:    NewDocumentStore(),
		log:     log,
		phase2:  phase2,
		root:    root,
	}
}

// Serve wires the server onto a jsonrpc2 stream and blocks until the connection
// closes. protocol.NewServer starts the read loop internally.
func (s *Server) Serve(ctx context.Context, stream jsonrpc2.Stream) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Wire read-loop-visible state BEFORE protocol.NewServer spawns the read
	// goroutine. A client request (e.g. didOpen → s.analyzer.Schedule) can
	// otherwise arrive before these fields are assigned — a data race, and a nil
	// dereference if the analyzer is not set yet. These writes happen-before the
	// spawned goroutine, so handlers observe them without locking.
	s.ctx = ctx
	s.cancel = cancel
	s.analyzer = NewAnalyzer(ctx, s.backend, s.docs, s.publishDraft, s.log, 0)

	_, conn, client := protocol.NewServer(ctx, s, stream)
	// client is produced by NewServer after the read loop has started, so the
	// store must be synchronized with publishDiagnostics' load.
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	s.backend.OnProjectChange(s.onProjectChange)

	<-conn.Done()
	s.analyzer.Shutdown()
	return conn.Err()
}

// StdioStream frames LSP messages over stdin/stdout.
func StdioStream() jsonrpc2.Stream {
	return jsonrpc2.NewStream(stdio{})
}

// SocketStream serves LSP over a TCP connection (debugging).
func SocketStream(ctx context.Context, port int) (jsonrpc2.Stream, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	return jsonrpc2.NewStream(conn), nil
}

// stdio is an io.ReadWriteCloser bridging os.Stdin/os.Stdout. Closing it is a
// no-op so the read loop owns shutdown, not a stream close.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return nil }

// publishDiagnostics pushes diagnostics for a document version to the client.
func (s *Server) publishDiagnostics(u uri.URI, ver int32, diags []protocol.Diagnostic) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()
	if client == nil {
		return
	}
	_ = client.PublishDiagnostics(s.ctx, &protocol.PublishDiagnosticsParams{ //nolint:errcheck // diagnostics push; the editor may have disconnected
		URI:         u,
		Version:     protocol.NewOptional(ver),
		Diagnostics: diags,
	})
}

// publishDraft is the Analyzer's publish callback: it records the draft
// (ValidateDraft) diagnostics for path, then publishes them merged with any
// cached project-wide diagnostics for the same file.
func (s *Server) publishDraft(u uri.URI, ver int32, diags []protocol.Diagnostic) {
	path := u.FsPath()
	s.mu.Lock()
	if s.draftDiags == nil {
		s.draftDiags = make(map[string][]protocol.Diagnostic)
	}
	s.draftDiags[path] = diags
	s.mu.Unlock()
	s.publishFile(path, ver)
}

// publishFile publishes the merge of cached draft + project diagnostics for a
// file. PublishDiagnostics fully replaces a document's diagnostic set per
// call, so callers must always go through this rather than publishing either
// source directly. While a draft entry exists for the file, project
// diagnostics of the classes ValidateDraft reproduces are dropped — an
// open-and-saved invalid file must not show every schema error twice.
func (s *Server) publishFile(path string, ver int32) {
	s.mu.RLock()
	draft, hasDraft := s.draftDiags[path]
	project := s.projectDiags[path]
	merged := make([]protocol.Diagnostic, 0, len(draft)+len(project))
	merged = append(merged, draft...)
	for _, d := range project {
		if hasDraft && draftCoveredCode(d) {
			continue
		}
		merged = append(merged, d)
	}
	s.mu.RUnlock()
	s.publishDiagnostics(uri.File(path), ver, merged)
}

// draftCoveredCode reports whether a project diagnostic belongs to a class the
// per-keystroke ValidateDraft also produces for open buffers.
func draftCoveredCode(d protocol.Diagnostic) bool {
	code, ok := d.Code.(protocol.String)
	if !ok {
		return false
	}
	switch string(code) {
	case "schema-validation", "validation-error", "yaml-syntax":
		return true
	}
	return false
}

// clearDraft drops the cached draft diagnostics for a closed buffer and
// republishes so only on-disk project findings remain visible for the file.
func (s *Server) clearDraft(u uri.URI) {
	path := u.FsPath()
	s.mu.Lock()
	_, had := s.draftDiags[path]
	delete(s.draftDiags, path)
	s.mu.Unlock()
	if had {
		s.publishFile(path, 0)
	}
}

// refreshProjectDiagnostics re-validates the whole project on disk (schema,
// lint rules, missing ${VAR}, engine-compat) and publishes the result for
// every affected file — not just currently open buffers — since these
// findings are invisible to the per-keystroke, draft-only Analyzer. Intended
// to run off the hot path (see onProjectChange).
func (s *Server) refreshProjectDiagnostics() {
	_, diags, err := s.backend.ValidateProject(s.ctx, false)
	if err != nil {
		// Keep the previously published project diagnostics: disk state has not
		// changed, so they are still accurate. (The draft path clears on error
		// instead, because its buffer text has moved on.)
		s.log.Warnf("validate-project failed: %v", err)
		return
	}
	byFile := make(map[string][]Diag)
	for _, d := range diags {
		byFile[d.File] = append(byFile[d.File], d)
	}
	published := make(map[string][]protocol.Diagnostic, len(byFile))
	for file, fileDiags := range byFile {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		published[file] = backfillDiagnostics(&Document{Path: file, Text: string(content)}, fileDiags)
	}

	s.mu.Lock()
	stale := s.projectDiags
	s.projectDiags = published
	s.mu.Unlock()

	// Republish every file that has fresh project diagnostics plus every file
	// that lost its last one (so its diagnostics get cleared, not left stale).
	touched := make(map[string]struct{}, len(published)+len(stale))
	for f := range published {
		touched[f] = struct{}{}
	}
	for f := range stale {
		touched[f] = struct{}{}
	}
	for f := range touched {
		s.publishFile(f, 0)
	}
}

// onProjectChange invalidates caches and re-analyzes open buffers when the
// project changed on disk (watcher / SSE).
func (s *Server) onProjectChange() {
	s.mu.Lock()
	s.schema = nil
	s.index = nil
	s.nameIdx = nil
	s.lock = nil
	s.mu.Unlock()
	go s.refreshProjectDiagnostics()
	if s.analyzer == nil {
		return
	}
	for _, d := range s.docs.All() {
		s.analyzer.Schedule(d.URI)
	}
}

// invalidateNav drops the cached name index so the next navigation request
// rebuilds it over the current buffers (called on buffer edits).
func (s *Server) invalidateNav() {
	s.mu.Lock()
	s.nameIdx = nil
	s.mu.Unlock()
}

// resolve maps a document position to a PositionContext over the live buffer.
func (s *Server) resolve(u uri.URI, pos protocol.Position) (reportspec.PositionContext, bool) {
	doc, ok := s.docs.Get(u)
	if !ok {
		return reportspec.PositionContext{}, false
	}
	line, col := doc.PositionToLineCol(pos)
	return reportspec.ResolvePositionPath(doc.Text, line, col)
}

// getNameIndex returns the cached name→location index, building it from the
// on-disk manifests overlaid with open buffers (so it reflects unsaved edits).
func (s *Server) getNameIndex(ctx context.Context) *reportspec.NameIndex {
	s.mu.RLock()
	ni := s.nameIdx
	s.mu.RUnlock()
	if ni != nil {
		return ni
	}
	files := make(map[string]string)
	for _, d := range s.getIndex(ctx) {
		if _, seen := files[d.File]; seen {
			continue
		}
		if b, err := os.ReadFile(d.File); err == nil {
			files[d.File] = string(b)
		}
	}
	for _, doc := range s.docs.All() {
		if doc.Path != "" {
			files[doc.Path] = doc.Text
		}
	}
	ni, _ = reportspec.BuildNameIndex(files) //nolint:errcheck // BuildNameIndex never returns a non-nil error
	s.mu.Lock()
	s.nameIdx = ni
	s.mu.Unlock()
	return ni
}

// backendFetchBudget bounds cold-start schema/index fetches: a slow or wedged
// backend must degrade to an incomplete (re-queried) completion list instead of
// blocking the popup until the client gives up.
const backendFetchBudget = 500 * time.Millisecond

// getSchema returns the cached merged-schema model, loading it on first use.
func (s *Server) getSchema(ctx context.Context) *schemaModel {
	s.mu.RLock()
	m := s.schema
	s.mu.RUnlock()
	if m != nil {
		return m
	}
	fctx, cancel := context.WithTimeout(ctx, backendFetchBudget)
	defer cancel()
	raw, err := s.backend.MergedSchema(fctx)
	if err != nil {
		// Warn, not Debug: this is the mechanism behind "no suggestions", and
		// an editor-spawned server never runs with --verbose.
		s.log.Warnf("merged schema unavailable, serving without schema: %v", err)
		return &schemaModel{}
	}
	m = parseSchema(raw)
	s.mu.Lock()
	s.schema = m
	s.mu.Unlock()
	return m
}

// getIndex returns the cached project index, loading it on first use.
func (s *Server) getIndex(ctx context.Context) []IndexDoc {
	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()
	if idx != nil {
		return idx
	}
	fctx, cancel := context.WithTimeout(ctx, backendFetchBudget)
	defer cancel()
	idx, err := s.backend.Index(fctx)
	if err != nil {
		s.log.Warnf("index unavailable, serving without project index: %v", err)
		return nil
	}
	s.mu.Lock()
	s.index = idx
	s.mu.Unlock()
	return idx
}

// --- lifecycle ---

func (s *Server) Initialize(_ context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	// Single-root scope: the backend already knows the project root from the CLI
	// (--work-dir), so the client-supplied root is not needed in v1.
	if params != nil {
		s.snippetSupport = clientSnippetSupport(params.Capabilities)
	}
	resolve := true
	caps := protocol.ServerCapabilities{
		TextDocumentSync:   protocol.TextDocumentSyncKindFull,
		HoverProvider:      protocol.Boolean(true),
		CompletionProvider: &protocol.CompletionOptions{TriggerCharacters: []string{":", "$", "@"}, ResolveProvider: &resolve},
	}
	if s.phase2 {
		caps.DefinitionProvider = protocol.Boolean(true)
		caps.ReferencesProvider = protocol.Boolean(true)
		caps.ImplementationProvider = protocol.Boolean(true)
		caps.DocumentSymbolProvider = protocol.Boolean(true)
		caps.WorkspaceSymbolProvider = protocol.Boolean(true)
		caps.RenameProvider = &protocol.RenameOptions{PrepareProvider: boolPtr(true)}
		caps.CodeActionProvider = &protocol.CodeActionOptions{CodeActionKinds: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}}
	}
	return &protocol.InitializeResult{
		Capabilities: caps,
		ServerInfo:   protocol.ServerInfo{Name: "bino-lsp", Version: protocol.NewOptional(version.Version)},
	}, nil
}

func (s *Server) Initialized(_ context.Context, _ *protocol.InitializedParams) error {
	// Surface project-wide findings (lint, missing ${VAR}, engine compat) at
	// startup; previously they appeared only after the first on-disk change.
	// s.client can lag this call by a moment (Serve wires it after the read loop
	// starts), but ValidateProject takes long enough in practice and every
	// DidOpen re-merges projectDiags per file, so this self-heals without extra
	// synchronization.
	go s.refreshProjectDiagnostics()
	return nil
}

// clientSnippetSupport reads completionItem.snippetSupport from the client's
// declared capabilities.
func clientSnippetSupport(caps protocol.ClientCapabilities) bool {
	td := caps.TextDocument
	if td == nil || td.Completion == nil || td.Completion.CompletionItem == nil {
		return false
	}
	sup := td.Completion.CompletionItem.SnippetSupport
	return sup != nil && *sup
}

func (s *Server) Shutdown(_ context.Context) error {
	if s.analyzer != nil {
		s.analyzer.Shutdown()
	}
	return nil
}

// Exit terminates the server: it cancels the connection context so Serve's
// <-conn.Done() returns even if the client does not close the stream.
func (s *Server) Exit(_ context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }
