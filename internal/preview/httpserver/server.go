// Package httpserver provides a preview HTTP server for report development.
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
	"context"
	"encoding/base64"
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
	"bino.bi/bino/internal/runtimecfg"
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
	ListenAddr string
	CacheDir   string
	CDNBaseURL string
	Logger     logx.Logger
	HTTPClient *http.Client
}

// Server hosts the preview HTTP experience with CDN proxying support.
type Server struct {
	cfg         Config
	listener    net.Listener
	httpServer  *http.Server
	httpClient  *http.Client
	maxCDNBytes int64
	sse         *sseHub

	contentMu sync.RWMutex
	contentFn ContentFunc
	routes    map[string]ContentFunc

	// contextCache stores the latest context HTML per path for initial client fetch.
	// This enables two-phase rendering where clients request context after SSE connects.
	contextCache map[string][]byte

	assetMu     sync.RWMutex
	localAssets map[string]LocalAsset
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

	listener, err := net.Listen("tcp", cfg.ListenAddr)
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
		sse:         newSSEHub(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", compressionHandlerFunc(srv.handleRoot))
	mux.Handle("/assets/", compressionHandlerFunc(srv.handleAsset))
	mux.Handle("/cdn/", compressionHandlerFunc(srv.handleCDN))
	mux.HandleFunc("/__preview/events", srv.handleEvents) // SSE uses its own compression
	mux.HandleFunc("/__preview/context", compressionHandlerFunc(srv.handleContext))

	srv.httpServer = &http.Server{Handler: mux}
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

// BroadcastContent pushes the latest HTML for a route to connected SSE clients.
// It also caches the content so clients can fetch it via /__preview/context on initial connect.
func (s *Server) BroadcastContent(path string, html []byte) {
	if s == nil || len(html) == 0 {
		return
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Store in context cache for initial client fetch
	s.contentMu.Lock()
	if s.contextCache == nil {
		s.contextCache = make(map[string][]byte)
	}
	s.contextCache[path] = append([]byte(nil), html...)
	s.contentMu.Unlock()

	// Broadcast to connected SSE clients
	if s.sse == nil {
		return
	}
	payload := sseContentPayload{
		Path:       path,
		HTMLBase64: base64.StdEncoding.EncodeToString(html),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		s.cfg.Logger.Warnf("preview: marshal sse payload: %v", err)
		return
	}
	s.sse.Broadcast(formatSSE("content", data))
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

	// Create a compressed response writer for SSE
	compType := selectCompression(r.Header.Get("Accept-Encoding"))
	var writer http.ResponseWriter = w
	var cleanup func() error

	if compType != compressionNone {
		cw := newSSECompressedWriter(w, compType)
		writer = cw
		cleanup = cw.Close
		defer func() {
			if cleanup != nil {
				_ = cleanup()
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

	if _, err := writer.Write(formatSSE("ready", []byte(`{}`))); err != nil {
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
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	s.contentMu.RLock()
	html, ok := s.contextCache[path]
	s.contentMu.RUnlock()

	if !ok || len(html) == 0 {
		http.Error(w, "context not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_, _ = w.Write(html)
}

func (s *Server) lookupContentFunc(path string) (ContentFunc, bool) {
	s.contentMu.RLock()
	defer s.contentMu.RUnlock()
	if path == "" {
		path = "/"
	}
	if fn, ok := s.routes[path]; ok {
		return fn, true
	}
	if len(s.routes) > 0 && path != "/" {
		return nil, false
	}
	if s.contentFn == nil {
		return nil, false
	}
	if len(s.routes) == 0 {
		return s.contentFn, true
	}
	return s.contentFn, path == "/"
}

func (s *Server) lookupLocalAsset(path string) (LocalAsset, bool) {
	s.assetMu.RLock()
	defer s.assetMu.RUnlock()
	if s.localAssets == nil {
		return LocalAsset{}, false
	}
	asset, ok := s.localAssets[path]
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
	defer file.Close()
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

	disableCache := cacheBypassed(r)
	localPath := ""
	if s.cfg.CacheDir != "" {
		localPath = filepath.Join(s.cfg.CacheDir, filepath.FromSlash(relPath))
	}

	// Determine fetch strategy based on file type:
	// - bn-template-engine.esm.js: remote-first with cache fallback (always get latest)
	// - all other files: cache-first (performance optimization)
	remoteFirst := isRemoteFirstFile(relPath)

	// For cache-first files, try serving from cache
	if !remoteFirst && localPath != "" && !disableCache {
		_, statErr := os.Stat(localPath)
		if statErr == nil {
			http.ServeFile(w, r, localPath)
			return nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("preview: cache lookup failed: %w", statErr)
		}
		// statErr is ErrNotExist, continue to fetch from CDN
	}

	// Attempt to fetch from remote CDN
	body, headers, statusCode, fetchErr := s.fetchFromCDN(r.Context(), relPath)

	// For remote-first files, fallback to cache if remote fetch failed or returned non-2xx
	shouldFallback := fetchErr != nil || (statusCode < 200 || statusCode >= 300)
	if shouldFallback && remoteFirst && localPath != "" && !disableCache {
		_, statErr := os.Stat(localPath)
		if statErr == nil {
			if fetchErr != nil {
				s.cfg.Logger.Warnf("cdn fetch failed, serving from cache: %v", fetchErr)
			} else {
				s.cfg.Logger.Warnf("cdn returned status %d, serving from cache", statusCode)
			}
			http.ServeFile(w, r, localPath)
			return nil
		}
		// Cache miss as well, report the original fetch error or status
	}

	// If fetch failed and we have no fallback, report error
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
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("preview: write response body: %w", err)
	}

	if statusCode == http.StatusOK && localPath != "" && !disableCache {
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return fmt.Errorf("preview: ensure cache dir: %w", err)
		}
		if err := os.WriteFile(localPath, body, 0o644); err != nil {
			return fmt.Errorf("preview: write cache file: %w", err)
		}
	}

	return nil
}

// isRemoteFirstFile returns true for files that should be fetched from remote CDN first
// with cache as a fallback. This ensures we always get the latest version of critical files.
func isRemoteFirstFile(relPath string) bool {
	// bn-template-engine.esm.js should always try remote first to get latest version
	return strings.HasSuffix(relPath, "bn-template-engine.esm.js")
}

// fetchFromCDN attempts to fetch a file from the remote CDN.
// Returns the body, headers, status code, and any error.
func (s *Server) fetchFromCDN(ctx context.Context, relPath string) ([]byte, http.Header, int, error) {
	remoteURL := s.cfg.CDNBaseURL + relPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("preview: build upstream request: %w", err)
	}

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("preview: upstream request: %w", err)
	}
	defer resp.Body.Close()

	if limit := s.maxCDNBytes; limit > 0 && resp.ContentLength > limit {
		return nil, nil, 0, fmt.Errorf("preview: upstream body exceeded %d bytes", limit)
	}

	body, err := readUpstreamBody(resp.Body, s.maxCDNBytes)
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

type sseContentPayload struct {
	Path       string `json:"path"`
	HTMLBase64 string `json:"htmlBase64"`
}

type sseHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan []byte]struct{})}
}

func (h *sseHub) Subscribe() chan []byte {
	ch := make(chan []byte, 4)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) Unsubscribe(ch chan []byte) {
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

func (h *sseHub) Broadcast(msg []byte) {
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
func (h *sseHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}

func formatSSE(event string, data []byte) []byte {
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
