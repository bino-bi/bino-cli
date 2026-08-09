// Package httpserver provides the HTTP server shared by the preview dev
// server, the production serve command, and the ephemeral servers that back
// Chrome during PDF rendering and screenshot capture.
//
// # Context and Cancellation
//
// The server respects context cancellation at the following points:
//   - Server.Start() blocks until the context is canceled, then initiates shutdown
//   - Graceful shutdown waits up to 5 seconds for in-flight requests
//   - ContentFunc receives request context for per-request cancellation
//   - SSE event handlers respect their request context for cleanup
//   - CDN proxy requests use the request context for upstream fetches
//
// When the parent context is canceled:
//   - httpServer.Shutdown() is called with a 5-second timeout
//   - New requests are rejected
//   - In-flight requests are allowed to complete within the timeout
//   - SSE connections are closed when their request context is canceled
package httpserver

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/layoutstate"
	"bino.bi/bino/internal/runtimecfg"
	"bino.bi/bino/internal/web"
)

// requestInfoKey is the context key for storing request info.
type requestInfoKey struct{}

// RequestInfo holds request information accessible from ContentFunc via context.
type RequestInfo struct {
	Path     string
	RawQuery string
	Query    url.Values
}

// WithRequestInfo returns a new context with the request info attached.
func WithRequestInfo(ctx context.Context, info RequestInfo) context.Context {
	return context.WithValue(ctx, requestInfoKey{}, info)
}

// GetRequestInfo extracts request info from context, returning zero value if not present.
func GetRequestInfo(ctx context.Context) RequestInfo {
	if info, ok := ctx.Value(requestInfoKey{}).(RequestInfo); ok {
		return info
	}
	return RequestInfo{}
}

// HTTPError is an error that carries an HTTP status code.
// ContentFunc implementations can return this to signal a specific HTTP response code.
type HTTPError struct {
	Code    int
	Message string
}

func (e *HTTPError) Error() string {
	return e.Message
}

// NewHTTPError creates an HTTPError with the given status code and message.
func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{Code: code, Message: message}
}

// ContentFunc returns dynamic content bytes and its MIME type per request.
// The context parameter carries the request context, which is canceled when:
//   - The client disconnects
//   - The request times out
//   - The server is shutting down
type ContentFunc func(context.Context) ([]byte, string, error)

// EmbeddingFunc renders a named document as a standalone HTML document with
// no preview chrome (toolbar, modals, preview stylesheet, preview JS bundle).
// The optional kind disambiguates names that collide across kinds (names are
// unique per kind, not globally); an empty kind resolves by a fixed priority.
// The optional language overrides the artefact language for this render; an
// empty language means "use whatever the manifest says". This package leaves
// the set of valid languages to the implementation.
// The output is build-equivalent so it can be safely embedded in an iframe.
// Implementations should return *HTTPError to signal 404 (unknown name) or
// 503 (preview still booting); other errors are reported as 500.
type EmbeddingFunc func(ctx context.Context, name, kind, language string) ([]byte, error)

// EmbeddingOverrideFunc records (or, when remove is true, drops) unsaved
// editor-buffer content for an absolute file path so the embedding renderer
// reflects the buffer instead of disk. Implementations validate that file is
// within the project root and should return *HTTPError to signal a rejected
// path (400/403); other errors are reported as 500.
type EmbeddingOverrideFunc func(file, content string, remove bool) error

// StaticContent returns a ContentFunc that always responds with identical bytes.
func StaticContent(body []byte, contentType string) ContentFunc {
	clone := append([]byte(nil), body...)
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	return func(context.Context) ([]byte, string, error) {
		return clone, contentType, nil
	}
}

// LocalAsset describes a file that must be served via the preview HTTP server.
type LocalAsset struct {
	URLPath   string
	FilePath  string
	MediaType string
}

// Config controls Server construction.
type Config struct {
	ListenAddr      string
	CacheDir        string
	CDNBaseURL      string
	Logger          logx.Logger
	HTTPClient      *http.Client
	ExplorerHandler http.Handler
}

// maxContextCacheEntries limits the number of cached context entries to prevent
// unbounded memory growth during long-running preview sessions.
const maxContextCacheEntries = 100

// contextCacheEntry holds cached HTML content for LRU eviction.
// The entry stores its own path so the eviction loop can delete the map key.
type contextCacheEntry struct {
	path string
	html []byte
}

// Server hosts the preview HTTP experience with CDN proxying support.
type Server struct {
	cfg         Config
	listener    net.Listener
	httpServer  *http.Server
	httpClient  *http.Client
	maxCDNBytes int64
	sse         *SSEHub

	bootMu     sync.RWMutex
	bootStatus []byte // latest boot-status JSON payload for late subscribers

	contentMu sync.RWMutex
	contentFn ContentFunc
	routes    map[string]ContentFunc

	embeddingMu       sync.RWMutex
	embeddingFn       EmbeddingFunc
	embeddingOverride EmbeddingOverrideFunc

	// layoutMu guards the fingerprint of the last layout-state findings logged
	// to the terminal. The inspector re-posts a snapshot after every hot
	// reload, so without it an unchanged report would log on every keystroke.
	layoutMu          sync.Mutex
	layoutFindingsKey string

	// contextCache stores the latest context HTML per path for initial client fetch.
	// This enables two-phase rendering where clients request context after SSE connects.
	// Uses LRU eviction when maxContextCacheEntries is exceeded.
	// The map provides O(1) lookup; the list maintains access order (front=oldest).
	contextCache map[string]*list.Element
	contextLRU   *list.List

	assetMu     sync.RWMutex
	localAssets map[string]LocalAsset

	data *dataStore
}

// New constructs a Server ready to start accepting requests.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if cfg.Logger == nil {
		cfg.Logger = logx.Nop()
	}
	if cfg.CDNBaseURL == "" {
		cfg.CDNBaseURL = "https://pub-5000c2eb6ba64ece971b69ce37fed581.r2.dev/"
	}
	if !strings.HasSuffix(cfg.CDNBaseURL, "/") {
		cfg.CDNBaseURL += "/"
	}

	listener, err := net.Listen("tcp", cfg.ListenAddr) //nolint:noctx // server startup, no context needed for listen
	if err != nil {
		return nil, fmt.Errorf("preview: listen on %s: %w", cfg.ListenAddr, err)
	}

	runtimeCfg := runtimecfg.Current()
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: runtimeCfg.CDNTimeout}
	}

	srv := &Server{
		cfg:         cfg,
		listener:    listener,
		httpClient:  client,
		maxCDNBytes: runtimeCfg.MaxCDNBytes,
		sse:         NewSSEHub(),
		data:        newDataStore(defaultDataKeep),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", compressionHandlerFunc(cfg.Logger, srv.handleRoot))
	mux.Handle("/assets/", compressionHandlerFunc(cfg.Logger, srv.handleAsset))
	mux.Handle("/cdn/", compressionHandlerFunc(cfg.Logger, srv.handleCDN))
	mux.HandleFunc("/__preview/events", srv.handleEvents) // SSE uses its own compression
	mux.HandleFunc("/__preview/context", compressionHandlerFunc(cfg.Logger, srv.handleContext))
	mux.HandleFunc("/__preview/boot-status", compressionHandlerFunc(cfg.Logger, srv.handleBootStatus))
	mux.HandleFunc("GET /__bino/data/datasource/{name}", compressionHandlerFunc(cfg.Logger, srv.handleData(DataKindDatasource)))
	mux.HandleFunc("GET /__bino/data/dataset/{name}", compressionHandlerFunc(cfg.Logger, srv.handleData(DataKindDataset)))
	mux.HandleFunc("GET /__embedding/{name}", compressionHandlerFunc(cfg.Logger, srv.handleEmbedding))
	mux.HandleFunc("POST /__bino/embedding/override", srv.handleEmbeddingOverride)
	mux.HandleFunc("POST /__bino/layout-state", compressionHandlerFunc(cfg.Logger, srv.handleLayoutState))
	mux.HandleFunc("GET /healthz", srv.handleHealthz)
	mux.Handle("/__bino/", web.Handler("/__bino/"))
	if cfg.ExplorerHandler != nil {
		mux.Handle("/__explorer/", cfg.ExplorerHandler)
	}

	srv.httpServer = &http.Server{
		Handler: mux,
		// Slowloris guard: bino serve advertises production use and may bind
		// non-localhost addresses, so bound the time a client may take to
		// send its request headers.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No ReadTimeout/WriteTimeout: SSE responses (/__preview/events) stay
		// open indefinitely and CDN-proxied downloads can be slow; both would
		// be killed by connection-wide deadlines.
	}
	srv.contentFn = StaticContent([]byte("Hello world"), "text/plain; charset=utf-8")
	return srv, nil
}

// URL returns the HTTP base address for the server.
func (s *Server) URL() string {
	if s.listener == nil {
		return ""
	}
	return fmt.Sprintf("http://%s", s.listener.Addr().String())
}

// SetContentFunc installs the function used to render root responses.
func (s *Server) SetContentFunc(fn ContentFunc) {
	if fn == nil {
		return
	}
	s.contentMu.Lock()
	defer s.contentMu.Unlock()
	s.contentFn = fn
}

// SetEmbeddingFunc installs the function used to render /__embedding/{name}
// responses. Passing nil clears the installed function, causing subsequent
// requests to receive a 503 response.
func (s *Server) SetEmbeddingFunc(fn EmbeddingFunc) {
	s.embeddingMu.Lock()
	defer s.embeddingMu.Unlock()
	s.embeddingFn = fn
}

// SetEmbeddingOverrideFunc installs the function used to handle
// POST /__bino/embedding/override. Passing nil clears it, causing subsequent
// requests to receive a 503 response.
func (s *Server) SetEmbeddingOverrideFunc(fn EmbeddingOverrideFunc) {
	s.embeddingMu.Lock()
	defer s.embeddingMu.Unlock()
	s.embeddingOverride = fn
}

// SetContentRoutes replaces the map of path-specific content functions served by the root handler.
// Paths should start with a leading slash. Passing nil clears existing routes.
func (s *Server) SetContentRoutes(routes map[string]ContentFunc) {
	var normalized map[string]ContentFunc
	if len(routes) > 0 {
		normalized = make(map[string]ContentFunc, len(routes))
		for p, fn := range routes {
			if fn == nil {
				continue
			}
			if p == "" {
				continue
			}
			if !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
			normalized[p] = fn
		}
	}
	s.contentMu.Lock()
	s.routes = normalized
	s.contentMu.Unlock()
}

// UpdateContentRoutes merges the supplied entries into the existing route map
// without dropping unspecified routes. Used by selective preview refreshes
// where only a subset of artefacts has been re-rendered. Empty paths and nil
// functions are skipped; paths are normalised to begin with a leading slash.
func (s *Server) UpdateContentRoutes(partial map[string]ContentFunc) {
	if len(partial) == 0 {
		return
	}
	s.contentMu.Lock()
	defer s.contentMu.Unlock()
	if s.routes == nil {
		s.routes = make(map[string]ContentFunc, len(partial))
	}
	for p, fn := range partial {
		if fn == nil || p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		s.routes[p] = fn
	}
}

// PutDatasource registers a JSON body for a bn-datasource component under
// (name, hash). The renderer emits a body URL of the form
// /__bino/data/datasource/<name>?hash=<hash> that resolves to this body.
func (s *Server) PutDatasource(name, hash string, body []byte) {
	if s == nil || s.data == nil {
		return
	}
	s.data.Put(DataKindDatasource, name, hash, body)
}

// PutDataset registers a JSON body for a bn-dataset component under
// (name, hash). The renderer emits a body URL of the form
// /__bino/data/dataset/<name>?hash=<hash> that resolves to this body.
func (s *Server) PutDataset(name, hash string, body []byte) {
	if s == nil || s.data == nil {
		return
	}
	s.data.Put(DataKindDataset, name, hash, body)
}

// handleData returns an http.HandlerFunc that serves registered JSON payloads
// for the given kind ("datasource" or "dataset"). The "name" path segment and
// "hash" query parameter together identify the payload; if either is missing
// or the lookup fails, the handler responds with 404 and a small JSON error
// body.
func (s *Server) handleData(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		hash := r.URL.Query().Get("hash")
		if name == "" || hash == "" {
			writeDataNotFound(w, "missing name or hash")
			return
		}
		body, ok := s.data.Get(kind, name, hash)
		if !ok {
			writeDataNotFound(w, fmt.Sprintf("no %s %q at hash %q", kind, name, hash))
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// The URL changes whenever the content changes, so the body at a given
		// URL is immutable. Encourage caches to retain it indefinitely.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// handleEmbedding serves a single named artefact as standalone HTML for use
// in iframes. The response carries no preview chrome; the body is whatever
// the installed EmbeddingFunc returns.
func (s *Server) handleEmbedding(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing artefact name", http.StatusNotFound)
		return
	}

	s.embeddingMu.RLock()
	fn := s.embeddingFn
	s.embeddingMu.RUnlock()

	if fn == nil {
		http.Error(w, "preview still booting", http.StatusServiceUnavailable)
		return
	}

	body, err := fn(r.Context(), name, r.URL.Query().Get("kind"), r.URL.Query().Get("language"))
	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			http.Error(w, httpErr.Message, httpErr.Code)
			if httpErr.Code >= 500 {
				s.cfg.Logger.Errorf("embedding render failed for %q: %v", name, err)
			}
			return
		}
		http.Error(w, "failed to render embedding", http.StatusInternalServerError)
		s.cfg.Logger.Errorf("embedding render failed for %q: %v", name, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_, _ = w.Write(body)
}

// handleEmbeddingOverride records or clears unsaved editor-buffer content for a
// file so subsequent /__embedding renders reflect the buffer instead of disk.
// Body JSON: {"file":"<abs path>","content":"<text>"} sets the override;
// {"file":"<abs path>","clear":true} clears it. The VS Code extension posts
// this from Node (not the browser), so no CORS header is emitted.
func (s *Server) handleEmbeddingOverride(w http.ResponseWriter, r *http.Request) {
	s.embeddingMu.RLock()
	fn := s.embeddingOverride
	s.embeddingMu.RUnlock()
	if fn == nil {
		http.Error(w, "preview still booting", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
		Clear   bool   `json:"clear"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOverrideBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.File == "" {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}

	if err := fn(req.File, req.Content, req.Clear); err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			http.Error(w, httpErr.Message, httpErr.Code)
			if httpErr.Code >= 500 {
				s.cfg.Logger.Errorf("embedding override failed for %q: %v", req.File, err)
			}
			return
		}
		http.Error(w, "failed to apply override", http.StatusInternalServerError)
		s.cfg.Logger.Errorf("embedding override failed for %q: %v", req.File, err)
		return
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusNoContent)
}

// maxOverrideBytes bounds the size of an embedding-override request body. A
// single manifest buffer is small; 10 MiB is a generous ceiling that guards
// against a runaway client.
const maxOverrideBytes = 10 << 20

// maxLayoutStateBytes bounds a layout-state capture. The inspector posts a
// summary snapshot — tens of components at a few hundred bytes each — so this
// leaves room for a very large report without accepting a full-detail dump.
const maxLayoutStateBytes = 8 << 20

// handleLayoutState derives render-time findings from a layout-state capture
// posted by the preview inspector.
//
// The analysis lives here rather than in the browser so the inspector, the
// build warnings and the MCP tooling all report the same thing, and so it can
// be unit-tested. It is pure computation: no filesystem, no SQL, no state
// beyond the log-dedup fingerprint.
func (s *Server) handleLayoutState(w http.ResponseWriter, r *http.Request) {
	var snap layoutstate.Snapshot
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLayoutStateBytes)).Decode(&snap); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !layoutstate.SupportedVersion(snap.State.Version) {
		http.Error(w, "unsupported layout-state version", http.StatusUnprocessableEntity)
		return
	}

	findings := layoutstate.Analyze(snap)
	s.logLayoutFindings(findings)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if err := json.NewEncoder(w).Encode(struct {
		Findings []layoutstate.Finding `json:"findings"`
	}{Findings: findings}); err != nil {
		s.cfg.Logger.Debugf("layout-state response write failed: %v", err)
	}
}

// logLayoutFindings reports the findings to the terminal once per distinct
// result, so a `bino preview` user sees them without opening the inspector.
func (s *Server) logLayoutFindings(findings []layoutstate.Finding) {
	if s.cfg.Logger == nil {
		return
	}

	var key strings.Builder
	for _, f := range findings {
		key.WriteString(f.Rule)
		key.WriteByte('\x00')
		key.WriteString(f.ComponentID)
		key.WriteByte('\n')
	}

	s.layoutMu.Lock()
	changed := key.String() != s.layoutFindingsKey
	s.layoutFindingsKey = key.String()
	s.layoutMu.Unlock()

	if !changed || len(findings) == 0 {
		return
	}
	s.cfg.Logger.Warnf("layout inspector found %d render issue(s):", len(findings))
	for _, f := range findings {
		s.cfg.Logger.Warnf("  %s", f)
	}
}

// handleHealthz reports liveness for production deployments of `bino serve`
// (load balancers, container orchestrators, uptime probes).
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeDataNotFound(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	// JSON-marshal the message to safely escape any quote/backslash/control
	// characters that could come from the request URL.
	body, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: message})
	if err != nil {
		body = []byte(`{"error":"unknown"}`)
	}
	_, _ = w.Write(body)
}

// SetLocalAssets updates the set of files that should be served under the /assets/ prefix.
func (s *Server) SetLocalAssets(assets []LocalAsset) {
	table := make(map[string]LocalAsset, len(assets))
	for _, asset := range assets {
		if asset.URLPath == "" || asset.FilePath == "" {
			continue
		}
		table[asset.URLPath] = asset
	}
	s.assetMu.Lock()
	s.localAssets = table
	s.assetMu.Unlock()
}

// BroadcastContent caches the latest HTML for a route and notifies connected
// SSE clients that the content has changed. The notification carries only the
// path, not the HTML body — clients fetch fresh content via
// /__preview/context?path=<path> on demand. This keeps SSE messages small
// (one per path, dozens of bytes) so a refresh that touches many routes
// cannot exhaust the per-client SSE channel buffer and drop the trailing
// refresh-done event.
func (s *Server) BroadcastContent(reqPath string, html []byte) {
	if s == nil || len(html) == 0 {
		return
	}
	if reqPath == "" {
		reqPath = "/"
	}
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}

	// Store in context cache so /__preview/context can serve it.
	s.contentMu.Lock()
	if s.contextCache == nil {
		s.contextCache = make(map[string]*list.Element)
		s.contextLRU = list.New()
	}
	if elem, ok := s.contextCache[reqPath]; ok {
		e, _ := elem.Value.(*contextCacheEntry)
		e.html = append([]byte(nil), html...)
		s.contextLRU.MoveToBack(elem)
	} else {
		entry := &contextCacheEntry{path: reqPath, html: append([]byte(nil), html...)}
		s.contextCache[reqPath] = s.contextLRU.PushBack(entry)
	}
	s.evictOldestCacheEntries()
	s.contentMu.Unlock()

	if s.sse == nil {
		return
	}
	data, err := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: reqPath})
	if err != nil {
		s.cfg.Logger.Warnf("preview: marshal sse payload: %v", err)
		return
	}
	s.sse.Broadcast(FormatSSE("path-changed", data))
}

// BroadcastBootStatus stores the latest cold-start status payload (JSON) and
// pushes it to connected SSE clients as a "boot-status" event. Late-connecting
// clients can fetch the cached snapshot via /__preview/boot-status so they
// don't sit on a stale loading screen after boot finishes.
func (s *Server) BroadcastBootStatus(payload []byte) {
	if s == nil {
		return
	}
	if len(payload) > 0 {
		s.bootMu.Lock()
		s.bootStatus = append(s.bootStatus[:0], payload...)
		s.bootMu.Unlock()
	}
	if s.sse == nil {
		return
	}
	s.sse.Broadcast(FormatSSE("boot-status", payload))
}

// handleBootStatus serves the latest boot status JSON. Returns 204 when no
// status has been recorded yet (server is still constructing the reporter).
func (s *Server) handleBootStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.bootMu.RLock()
	payload := append([]byte(nil), s.bootStatus...)
	s.bootMu.RUnlock()
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if len(payload) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(payload)
}

// BroadcastRefreshing notifies connected SSE clients that a content refresh has started.
func (s *Server) BroadcastRefreshing(reason string) {
	if s == nil || s.sse == nil {
		return
	}
	payload, _ := json.Marshal(struct { //nolint:errcheck // a struct of strings cannot fail to marshal
		Reason string `json:"reason"`
	}{Reason: reason})
	s.sse.Broadcast(FormatSSE("refreshing", payload))
}

// BroadcastRefreshDone notifies connected SSE clients that the content
// refresh has completed. The paths slice lists every route that received a
// fresh content event during this cycle so each client can detect when its
// own view was not part of the refresh (typically because that artefact's
// render failed or the route does not exist).
func (s *Server) BroadcastRefreshDone(paths []string) {
	if s == nil || s.sse == nil {
		return
	}
	if paths == nil {
		paths = []string{}
	}
	payload, err := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		payload = []byte(`{"paths":[]}`)
	}
	s.sse.Broadcast(FormatSSE("refresh-done", payload))
}

// BroadcastRefreshError notifies connected SSE clients that the content
// refresh has failed. Pass an empty path to broadcast a global error that
// every client should surface; pass a route path (e.g. "/myArt") to scope
// the error to clients viewing that path. Without this signal a failed
// refresh is invisible in the browser: BroadcastRefreshDone still fires,
// and the DOM is left in its previous state because no `content` event
// arrives.
func (s *Server) BroadcastRefreshError(routePath, message string) {
	if s == nil || s.sse == nil {
		return
	}
	payload, err := json.Marshal(struct {
		Path    string `json:"path,omitempty"`
		Message string `json:"message"`
	}{Path: routePath, Message: message})
	if err != nil {
		payload = []byte(`{"message":"unknown"}`)
	}
	s.sse.Broadcast(FormatSSE("refresh-error", payload))
}

// evictOldestCacheEntries removes the oldest cache entries to stay within maxContextCacheEntries.
// Entries are removed from the front of the LRU list (oldest first) in O(1) per entry.
// Must be called with contentMu held.
func (s *Server) evictOldestCacheEntries() {
	// Evict ~10% when over limit to avoid frequent eviction
	targetSize := maxContextCacheEntries - maxContextCacheEntries/10
	if targetSize < 1 {
		targetSize = 1
	}
	for s.contextLRU.Len() > targetSize {
		oldest := s.contextLRU.Front()
		entry, _ := oldest.Value.(*contextCacheEntry)
		delete(s.contextCache, entry.path)
		s.contextLRU.Remove(oldest)
	}
}

// Start begins serving requests until the context is done or an error occurs.
// When the context is canceled:
//  1. The server stops accepting new connections
//  2. A graceful shutdown is initiated with a 5-second timeout
//  3. In-flight requests are allowed to complete within the timeout
//  4. If requests don't complete within 5 seconds, they are forcibly terminated
//
// Returns nil on graceful shutdown, or an error if shutdown fails.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.Serve(s.listener)
	}()

	select {
	case <-ctx.Done():
		// Close all SSE connections first to allow graceful shutdown.
		// Without this, long-lived SSE connections would block shutdown.
		if s.sse != nil {
			s.sse.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("preview: shutdown failed: %w", err)
		}
		return nil
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("preview: server error: %w", err)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	fn, ok := s.lookupContentFunc(r.URL.Path)
	if !ok || fn == nil {
		http.NotFound(w, r)
		return
	}

	// Inject request info into context for ContentFunc to access query params
	reqInfo := RequestInfo{
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
		Query:    r.URL.Query(),
	}
	ctx := WithRequestInfo(r.Context(), reqInfo)

	body, contentType, err := fn(ctx)
	if err != nil {
		// Check if it's an HTTPError with a specific status code
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			http.Error(w, httpErr.Message, httpErr.Code)
			if httpErr.Code >= 500 {
				s.cfg.Logger.Errorf("content function failed: %v", err)
			}
			return
		}
		http.Error(w, "failed to render content", http.StatusInternalServerError)
		s.cfg.Logger.Errorf("content function failed: %v", err)
		return
	}
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body)
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.lookupLocalAsset(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.serveLocalAsset(w, r, asset); err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		s.cfg.Logger.Warnf("asset proxy failed: %v", err)
	}
}

func (s *Server) handleCDN(w http.ResponseWriter, r *http.Request) {
	if err := s.serveCDNProxy(w, r); err != nil {
		s.cfg.Logger.Warnf("cdn proxy failed: %v", err)
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.sse == nil {
		http.Error(w, "preview events unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set SSE headers before creating the compressed writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// The embedded preview's EventSource runs inside the VS Code webview (origin
	// vscode-webview://), so this localhost SSE stream is cross-origin. Without
	// this header the browser blocks the connection and the preview never
	// receives refresh events (the canvas stops auto-reloading). No credentials
	// are used, so "*" is safe for this local-only preview server.
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a compressed response writer for SSE
	compType := selectCompression(r.Header.Get("Accept-Encoding"))
	writer := w
	var cleanup func() error

	if compType != compressionNone {
		cw := newSSECompressedWriter(w, compType)
		writer = cw
		cleanup = cw.Close
		defer func() {
			if cleanup != nil {
				_ = cleanup() //nolint:errcheck // finalizing the SSE compressor; the stream is ending anyway
			}
		}()
	}

	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}

	clientCh := s.sse.Subscribe()
	defer s.sse.Unsubscribe(clientCh)

	if _, err := writer.Write(FormatSSE("ready", []byte(`{}`))); err != nil {
		return
	}
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-clientCh:
			if !ok {
				return
			}
			if _, err := writer.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := writer.Write(keepAliveFrame); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleContext serves the cached context HTML for initial client fetch.
// Clients call this on SSE "ready" to get the latest context without waiting for broadcast.
func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the path from query param, defaulting to current page path
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = "/"
	}
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}

	s.contentMu.Lock()
	elem, ok := s.contextCache[reqPath]
	var html []byte
	if ok {
		entry, _ := elem.Value.(*contextCacheEntry)
		html = entry.html
		s.contextLRU.MoveToBack(elem) // Mark as recently used
	}
	s.contentMu.Unlock()

	if !ok || len(html) == 0 {
		http.Error(w, "context not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_, _ = w.Write(html)
}

func (s *Server) lookupContentFunc(reqPath string) (ContentFunc, bool) {
	s.contentMu.RLock()
	defer s.contentMu.RUnlock()
	if reqPath == "" {
		reqPath = "/"
	}
	if fn, ok := s.routes[reqPath]; ok {
		return fn, true
	}
	if len(s.routes) > 0 && reqPath != "/" {
		return nil, false
	}
	if s.contentFn == nil {
		return nil, false
	}
	if len(s.routes) == 0 {
		return s.contentFn, true
	}
	return s.contentFn, reqPath == "/"
}

func (s *Server) lookupLocalAsset(reqPath string) (LocalAsset, bool) {
	s.assetMu.RLock()
	defer s.assetMu.RUnlock()
	if s.localAssets == nil {
		return LocalAsset{}, false
	}
	asset, ok := s.localAssets[reqPath]
	return asset, ok
}

func (s *Server) serveLocalAsset(w http.ResponseWriter, r *http.Request, asset LocalAsset) error {
	file, err := os.Open(asset.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return fmt.Errorf("preview: asset missing %s", asset.FilePath)
		}
		return fmt.Errorf("preview: open asset %s: %w", asset.FilePath, err)
	}
	defer file.Close() //nolint:errcheck // read-only handle
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("preview: stat asset %s: %w", asset.FilePath, err)
	}
	if asset.MediaType != "" {
		w.Header().Set("Content-Type", asset.MediaType)
	}
	http.ServeContent(w, r, filepath.Base(asset.FilePath), info.ModTime(), file)
	return nil
}

func (s *Server) serveCDNProxy(w http.ResponseWriter, r *http.Request) error {
	relPath, err := sanitizeCDNPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid cdn path", http.StatusBadRequest)
		return err
	}

	// bn-template-engine is served from local cache only (no remote CDN proxy)
	if strings.HasPrefix(relPath, "bn-template-engine/") {
		return s.serveLocalEngineFile(w, r, relPath)
	}

	disableCache := cacheBypassed(r)
	localPath := ""
	if s.cfg.CacheDir != "" {
		localPath = filepath.Join(s.cfg.CacheDir, filepath.FromSlash(relPath))
	}

	// For other CDN files, use cache-first strategy
	if localPath != "" && !disableCache {
		_, statErr := os.Stat(localPath) //nolint:gosec // G304: localPath is built from config CacheDir + sanitized CDN path
		if statErr == nil {
			http.ServeFile(w, r, localPath) //nolint:gosec // G703: localPath built from config CacheDir + sanitized relPath (sanitizeCDNPath rejects ..)
			return nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("preview: cache lookup failed: %w", statErr)
		}
		// statErr is ErrNotExist, continue to fetch from CDN
	}

	// Attempt to fetch from remote CDN
	body, headers, statusCode, fetchErr := s.fetchFromCDN(r.Context(), relPath)

	// If fetch failed, report error
	if fetchErr != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return fetchErr
	}

	copyHeaders(w.Header(), headers, "Content-Type", "Content-Length")
	// Disable caching for preview/development to ensure fresh content
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(statusCode)
	if _, err := w.Write(body); err != nil { //nolint:gosec // G705: proxying trusted CDN content, not user-supplied data
		return fmt.Errorf("preview: write response body: %w", err)
	}

	if statusCode == http.StatusOK && localPath != "" && !disableCache {
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil { //nolint:gosec // G703: localPath is built from config CacheDir + known CDN paths
			return fmt.Errorf("preview: ensure cache dir: %w", err)
		}
		if err := os.WriteFile(localPath, body, 0o644); err != nil { //nolint:gosec // G306: cache files need standard read perms
			return fmt.Errorf("preview: write cache file: %w", err)
		}
	}

	return nil
}

// serveLocalEngineFile serves template engine files from local cache or CDN.
// The relPath is expected to be like "bn-template-engine/v1.2.3/bn-template-engine.esm.js".
//
// For SNAPSHOT versions:
//   - The main ESM file (bn-template-engine.esm.js) is always fetched from the remote CDN
//   - Other bundle artifacts are served from local cache if available, otherwise fetched from CDN
//
// For regular versions:
//   - All files are served from local cache only (no CDN fallback)
func (s *Server) serveLocalEngineFile(w http.ResponseWriter, r *http.Request, relPath string) error {
	// Parse version from path: bn-template-engine/{version}/...
	parts := strings.SplitN(relPath, "/", 3)
	if len(parts) < 2 {
		http.Error(w, "invalid template engine path", http.StatusBadRequest)
		return fmt.Errorf("preview: invalid template engine path: %s", relPath)
	}
	version := parts[1]
	isSnapshot := version == "SNAPSHOT"
	isMainESM := len(parts) >= 3 && parts[2] == "bn-template-engine.esm.js"

	// For SNAPSHOT's main ESM file, always fetch from remote CDN (don't cache)
	if isSnapshot && isMainESM {
		return s.proxyFromCDN(w, r, relPath, false) // don't cache the main ESM
	}

	// For all other files, try local cache first
	if s.cfg.CacheDir != "" {
		localPath := filepath.Join(s.cfg.CacheDir, filepath.FromSlash(relPath))
		if _, err := os.Stat(localPath); err == nil { //nolint:gosec // G304: localPath is built from config CacheDir + sanitized engine path
			http.ServeFile(w, r, localPath) //nolint:gosec // G703: localPath built from config CacheDir + sanitized relPath (sanitizeCDNPath rejects ..)
			return nil
		}
	}

	// For SNAPSHOT, fetch other files from CDN and cache them
	if isSnapshot {
		return s.proxyFromCDN(w, r, relPath, true) // cache other bundle artifacts
	}

	// For regular versions, require local cache (no CDN fallback)
	http.Error(w, "template engine not found - run 'bino setup --template-engine' to install", http.StatusNotFound)
	return fmt.Errorf("preview: template engine file not found: %s", relPath)
}

// proxyFromCDN fetches a file from the remote CDN and optionally caches it locally.
func (s *Server) proxyFromCDN(w http.ResponseWriter, r *http.Request, relPath string, cacheLocally bool) error {
	body, headers, statusCode, err := s.fetchFromCDN(r.Context(), relPath)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return err
	}

	copyHeaders(w.Header(), headers, "Content-Type", "Content-Length")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(statusCode)
	if _, err := w.Write(body); err != nil { //nolint:gosec // G705: proxying trusted CDN content, not user-supplied data
		return fmt.Errorf("preview: write response body: %w", err)
	}

	// Cache the file locally if requested
	if cacheLocally && statusCode == http.StatusOK && s.cfg.CacheDir != "" {
		localPath := filepath.Join(s.cfg.CacheDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil { //nolint:gosec // G703: localPath is built from config CacheDir + known CDN paths
			return fmt.Errorf("preview: ensure cache dir: %w", err)
		}
		if err := os.WriteFile(localPath, body, 0o644); err != nil { //nolint:gosec // G306: cache files need standard read perms
			return fmt.Errorf("preview: write cache file: %w", err)
		}
	}

	return nil
}

// fetchFromCDN attempts to fetch a file from the remote CDN.
// Returns the body, headers, status code, and any error.
func (s *Server) fetchFromCDN(ctx context.Context, relPath string) (body []byte, headers http.Header, status int, err error) {
	remoteURL := s.cfg.CDNBaseURL + relPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, http.NoBody) //nolint:gosec // G704: CDNBaseURL is a trusted server-configured value
	if err != nil {
		return nil, nil, 0, fmt.Errorf("preview: build upstream request: %w", err)
	}

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req) //nolint:gosec // G704: CDNBaseURL is a trusted server-configured value
	if err != nil {
		return nil, nil, 0, fmt.Errorf("preview: upstream request: %w", err)
	}
	defer resp.Body.Close()

	if limit := s.maxCDNBytes; limit > 0 && resp.ContentLength > limit {
		return nil, nil, 0, fmt.Errorf("preview: upstream body exceeded %d bytes", limit)
	}

	body, err = readUpstreamBody(resp.Body, s.maxCDNBytes)
	if err != nil {
		return nil, nil, 0, err
	}

	return body, resp.Header, resp.StatusCode, nil
}

var errBodyTooLarge = errors.New("cdn body exceeded limit")

func readUpstreamBody(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	buf := &bytes.Buffer{}
	lr := &io.LimitedReader{R: r, N: limit + 1}
	if _, err := buf.ReadFrom(lr); err != nil {
		return nil, fmt.Errorf("preview: read upstream body: %w", err)
	}
	if lr.N == 0 {
		return nil, fmt.Errorf("preview: %w (%d bytes)", errBodyTooLarge, limit)
	}
	return buf.Bytes(), nil
}

func sanitizeCDNPath(raw string) (string, error) {
	trimmed := strings.TrimPrefix(raw, "/cdn/")
	trimmed = strings.TrimLeft(trimmed, "/")
	if trimmed == "" {
		return "", errors.New("empty path")
	}

	// Check for path traversal BEFORE cleaning
	// This catches both encoded (%2e%2e) and plain (..) attempts
	if strings.Contains(trimmed, "..") {
		return "", errors.New("path traversal detected")
	}

	cleaned := path.Clean("/" + trimmed)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." {
		return "", errors.New("invalid path")
	}

	// Double-check after cleaning in case of edge cases
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/..") {
		return "", errors.New("path traversal detected")
	}
	return cleaned, nil
}

func cacheBypassed(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("cache") == "0" || q.Get("chache") == "0"
}

func copyHeaders(dst, src http.Header, keys ...string) {
	for _, key := range keys {
		if values, ok := src[key]; ok {
			dst[key] = append([]string(nil), values...)
		}
	}
}

var keepAliveFrame = []byte(": keep-alive\n\n")

// SSEHub manages Server-Sent Event client connections and broadcasts.
type SSEHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

// NewSSEHub creates a new SSE hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[chan []byte]struct{})}
}

// Subscribe registers a new SSE client and returns its channel.
//
// The buffer is sized to absorb a full refresh cycle (refreshing +
// path-changed × N + refresh-done) without falling back to the non-blocking
// drop path in Broadcast. With path-changed events being just a path
// fragment, even reports with hundreds of artefacts comfortably fit; the
// previous 4-slot buffer used to drop refresh-done when many large
// content events were broadcast in succession.
func (h *SSEHub) Subscribe() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes an SSE client.
func (h *SSEHub) Unsubscribe(ch chan []byte) {
	if ch == nil {
		return
	}
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Broadcast sends a message to all connected SSE clients.
func (h *SSEHub) Broadcast(msg []byte) {
	if len(msg) == 0 {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Close disconnects all SSE clients by closing their channels.
// This causes handleEvents to return, allowing graceful HTTP shutdown.
func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}

// ClientCount returns the number of currently connected SSE clients.
func (h *SSEHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// FormatSSE encodes an event name and data payload as an SSE frame.
func FormatSSE(event string, data []byte) []byte {
	var buf bytes.Buffer
	if event != "" {
		buf.WriteString("event: ")
		buf.WriteString(event)
		buf.WriteByte('\n')
	}
	if len(data) == 0 {
		buf.WriteString("data:\n\n")
		return buf.Bytes()
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		buf.WriteString("data: ")
		buf.Write(line)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
