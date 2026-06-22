package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/version"
)

// Diagnostic represents a single diagnostic message for a file/document.
type Diagnostic struct {
	File     string `json:"file"`
	Position int    `json:"position"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
	Field    string `json:"field,omitempty"`
}

// Server is the daemon HTTP server.
type Server struct {
	state          *State
	sse            *httpserver.SSEHub
	listener       net.Listener
	httpServer     *http.Server
	logger         logx.Logger
	startedAt      time.Time
	shutdownCh     chan struct{}
	shutdownOnce   sync.Once
	pluginRegistry *plugin.PluginRegistry

	// Preview subprocess management
	previewMu     sync.Mutex
	previewCmd    *exec.Cmd
	previewPort   int
	previewStatus string // "stopped", "starting", "running", "error"
	previewURL    string

	// Build state
	buildMu  sync.Mutex
	building bool
}

// ServerConfig controls daemon server construction.
type ServerConfig struct {
	ListenAddr     string
	State          *State
	Logger         logx.Logger
	PluginRegistry *plugin.PluginRegistry
	// MCPHandler, when non-nil, is mounted at /mcp to serve the Model Context
	// Protocol over Streamable HTTP, sharing the daemon's loaded State. Built by
	// the CLI layer (internal/cli) so the daemon package avoids importing
	// internal/mcp (which imports this package).
	MCPHandler http.Handler
}

// NewServer constructs a daemon HTTP server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if cfg.Logger == nil {
		cfg.Logger = logx.Nop()
	}

	listener, err := net.Listen("tcp", cfg.ListenAddr) //nolint:noctx // server startup, no context needed for listen
	if err != nil {
		return nil, fmt.Errorf("daemon: listen on %s: %w", cfg.ListenAddr, err)
	}

	srv := &Server{
		state:          cfg.State,
		sse:            httpserver.NewSSEHub(),
		listener:       listener,
		logger:         cfg.Logger,
		startedAt:      time.Now(),
		shutdownCh:     make(chan struct{}),
		pluginRegistry: cfg.PluginRegistry,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", srv.handleHealth)
	mux.HandleFunc("GET /schema", srv.handleSchema)
	mux.HandleFunc("GET /index", srv.handleIndex)
	mux.HandleFunc("GET /validate", srv.handleValidateGet)
	mux.HandleFunc("POST /validate", srv.handleValidatePost)
	mux.HandleFunc("POST /validate-draft", srv.handleValidateDraft)
	mux.HandleFunc("GET /columns", srv.handleColumns)
	mux.HandleFunc("GET /rows", srv.handleRows)
	mux.HandleFunc("POST /introspect-draft", srv.handleIntrospectDraft)
	mux.HandleFunc("POST /sqlgen/typed-select", srv.handleTypedSelect)
	mux.HandleFunc("POST /preview-dataset", srv.handlePreviewDataSet)
	mux.HandleFunc("GET /dataset-schema", srv.handleDatasetSchema)
	mux.HandleFunc("GET /graph-deps", srv.handleGraphDeps)
	mux.HandleFunc("POST /preview/start", srv.handlePreviewStart)
	mux.HandleFunc("POST /preview/stop", srv.handlePreviewStop)
	mux.HandleFunc("GET /preview/status", srv.handlePreviewStatus)
	mux.HandleFunc("POST /build", srv.handleBuild)
	mux.HandleFunc("GET /events", srv.handleEvents)
	mux.HandleFunc("POST /shutdown", srv.handleShutdown)

	// The MCP server (Streamable HTTP) is mounted here when supplied by the CLI.
	// Both "/mcp" and "/mcp/" are routed so subpath requests reach the handler.
	if cfg.MCPHandler != nil {
		mux.Handle("/mcp", cfg.MCPHandler)
		mux.Handle("/mcp/", cfg.MCPHandler)
	}

	srv.httpServer = &http.Server{Handler: mux} //nolint:gosec // G112: local dev server on localhost
	return srv, nil
}

// Port returns the port the server is listening on.
func (s *Server) Port() int {
	addr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return addr.Port
}

// ClientCount returns the number of connected SSE clients.
func (s *Server) ClientCount() int {
	return s.sse.ClientCount()
}

// BroadcastEvent sends a typed SSE event to all connected clients.
func (s *Server) BroadcastEvent(event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		s.logger.Warnf("daemon: marshal sse payload: %v", err)
		return
	}
	s.sse.Broadcast(httpserver.FormatSSE(event, payload))
}

// ShutdownCh returns a channel that is closed when a shutdown is requested via the API.
func (s *Server) ShutdownCh() <-chan struct{} {
	return s.shutdownCh
}

// Start begins serving until the context is done.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.Serve(s.listener)
	}()

	select {
	case <-ctx.Done():
		if s.sse != nil {
			s.sse.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("daemon: shutdown failed: %w", err)
		}
		return nil
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("daemon: server error: %w", err)
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{
		"status":       "ok",
		"uptime":       time.Since(s.startedAt).String(),
		"startedAt":    s.startedAt.UTC(),
		"clients":      s.sse.ClientCount(),
		"version":      version.Version,
		"capabilities": daemonCapabilities,
	}
	if s.state != nil && s.state.Session() != nil {
		resp["session"] = true
	}
	s.writeJSON(w, resp)
}

func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	// Build merged schema (built-in + plugin kinds).
	registry := s.pluginRegistry
	if registry == nil {
		registry = plugin.NewRegistry()
	}
	aggregator := plugin.NewSchemaAggregator(registry)
	if err := aggregator.Build(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("building schema: %v", err), http.StatusInternalServerError)
		return
	}

	kindFilter := r.URL.Query().Get("kind")
	var schemaBytes json.RawMessage
	if kindFilter != "" {
		kindSchema, ok := aggregator.SchemaForKind(kindFilter)
		if !ok {
			http.Error(w, fmt.Sprintf("unknown kind: %s", kindFilter), http.StatusNotFound)
			return
		}
		schemaBytes = kindSchema
	} else {
		schemaBytes = aggregator.MergedSchema()
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(schemaBytes) //nolint:errcheck,gosec // G705: schema is generated internally, not user tainted
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, s.state.Index())
}

func (s *Server) handleValidateGet(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, s.state.Validate(context.Background(), false))
}

func (s *Server) handleValidatePost(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, s.state.Validate(r.Context(), true))
}

// handleValidateDraft validates a raw in-memory YAML buffer (schema + constraints,
// no disk). It backs the LSP proxy's live per-keystroke diagnostics. Loopback-only
// and stateless w.r.t. the shared session (ValidateDraft uses its own temp dir).
func (s *Server) handleValidateDraft(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	diags, err := s.state.ValidateDraft(ctx, body)
	if err != nil {
		s.writeJSON(w, ValidateResult{Valid: false, Diagnostics: []Diagnostic{{
			Severity: "error", Message: err.Error(), Code: "validate-draft-error",
		}}})
		return
	}
	s.writeJSON(w, ValidateResult{Valid: len(diags) == 0, Diagnostics: diags})
}

func (s *Server) handleColumns(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, `{"error":"missing name parameter"}`, http.StatusBadRequest)
		return
	}
	s.writeJSON(w, s.state.Columns(r.Context(), name))
}

func (s *Server) handleRows(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, `{"error":"missing name parameter"}`, http.StatusBadRequest)
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	s.writeJSON(w, s.state.Rows(r.Context(), name, limit))
}

func (s *Server) handleGraphDeps(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	name := r.URL.Query().Get("name")
	direction := r.URL.Query().Get("direction")

	maxDepth := 0
	if md := r.URL.Query().Get("max-depth"); md != "" {
		if parsed, err := strconv.Atoi(md); err == nil {
			maxDepth = parsed
		}
	}
	s.writeJSON(w, s.state.GraphDeps(r.Context(), kind, name, direction, maxDepth))
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}

	clientCh := s.sse.Subscribe()
	defer s.sse.Unsubscribe(clientCh)

	if _, err := w.Write(httpserver.FormatSSE("ready", []byte(`{}`))); err != nil {
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
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// RequestShutdown signals the daemon to shut down gracefully.
// Safe to call multiple times.
func (s *Server) RequestShutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdownCh)
	})
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, map[string]string{"status": "shutting down"})
	s.RequestShutdown()
}

// --- Preview subprocess management ---

const defaultPreviewPort = 45678

func (s *Server) handlePreviewStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port int `json:"port"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	s.previewMu.Lock()
	defer s.previewMu.Unlock()

	// Already running — return current state
	if s.previewCmd != nil && s.previewStatus != "stopped" {
		s.writeJSON(w, map[string]any{
			"status": s.previewStatus,
			"url":    s.previewURL,
			"port":   s.previewPort,
		})
		return
	}

	exe, err := os.Executable()
	if err != nil {
		s.writeJSON(w, map[string]any{"status": "error", "error": fmt.Sprintf("resolve executable: %v", err)})
		return
	}

	port := req.Port
	if port == 0 {
		port = defaultPreviewPort
	}

	cmd := exec.CommandContext(context.Background(), exe, "preview", "--port", strconv.Itoa(port), "--work-dir", s.state.ProjectRoot()) //nolint:gosec // G204: exe is our own binary
	cmd.Env = append(os.Environ(), "BINO_DISABLE_UPDATE_CHECK=1", "NO_COLOR=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.writeJSON(w, map[string]any{"status": "error", "error": err.Error()})
		return
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout pipe

	if err := cmd.Start(); err != nil {
		s.writeJSON(w, map[string]any{"status": "error", "error": err.Error()})
		return
	}

	s.previewCmd = cmd
	s.previewPort = port
	s.previewStatus = "starting"
	s.previewURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	// Watch stdout for ready signal in background
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			s.logger.Debugf("[preview] %s", line)
			if strings.Contains(line, "Serving preview at") || strings.Contains(line, "http://") {
				s.previewMu.Lock()
				if s.previewStatus == "starting" {
					s.previewStatus = "running"
				}
				s.previewMu.Unlock()
				s.BroadcastEvent("preview-status", map[string]any{
					"status": "running",
					"url":    fmt.Sprintf("http://127.0.0.1:%d", port),
					"port":   port,
				})
			}
		}

		// Process exited
		_ = cmd.Wait()
		s.previewMu.Lock()
		s.previewCmd = nil
		s.previewStatus = "stopped"
		s.previewURL = ""
		s.previewPort = 0
		s.previewMu.Unlock()
		s.BroadcastEvent("preview-status", map[string]any{"status": "stopped"})
	}()

	s.writeJSON(w, map[string]any{
		"status": "starting",
		"url":    s.previewURL,
		"port":   port,
	})
}

func (s *Server) handlePreviewStop(w http.ResponseWriter, _ *http.Request) {
	s.StopPreview()
	s.writeJSON(w, map[string]any{"status": "stopped"})
}

func (s *Server) handlePreviewStatus(w http.ResponseWriter, _ *http.Request) {
	s.previewMu.Lock()
	status := s.previewStatus
	url := s.previewURL
	port := s.previewPort
	s.previewMu.Unlock()

	if status == "" {
		status = "stopped"
	}
	s.writeJSON(w, map[string]any{
		"status": status,
		"url":    url,
		"port":   port,
	})
}

// StopPreview kills the preview subprocess if running.
func (s *Server) StopPreview() {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()

	if s.previewCmd == nil || s.previewCmd.Process == nil {
		return
	}

	_ = s.previewCmd.Process.Signal(os.Interrupt)
	// Force kill after 3 seconds
	go func(cmd *exec.Cmd) {
		time.Sleep(3 * time.Second)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}(s.previewCmd)

	s.previewCmd = nil
	s.previewStatus = "stopped"
	s.previewURL = ""
	s.previewPort = 0
}

// --- Build subprocess management ---

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	s.buildMu.Lock()
	if s.building {
		s.buildMu.Unlock()
		s.writeJSON(w, map[string]any{"status": "error", "error": "build already in progress"})
		return
	}
	s.building = true
	s.buildMu.Unlock()
	defer func() {
		s.buildMu.Lock()
		s.building = false
		s.buildMu.Unlock()
	}()

	var req struct {
		Artefact string `json:"artefact"` //nolint:misspell // UK spelling is intentional
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	exe, err := os.Executable()
	if err != nil {
		s.writeJSON(w, map[string]any{"status": "error", "error": err.Error()})
		return
	}

	args := []string{"build", "--work-dir", s.state.ProjectRoot()}
	if req.Artefact != "" {
		args = append(args, "--artefact", req.Artefact)
	}

	s.BroadcastEvent("build-progress", map[string]string{"status": "started"})

	cmd := exec.CommandContext(r.Context(), exe, args...) //nolint:gosec // G204: exe is our own binary
	cmd.Env = append(os.Environ(), "BINO_DISABLE_UPDATE_CHECK=1", "NO_COLOR=1")
	output, cmdErr := cmd.CombinedOutput()

	exitCode := 0
	if cmdErr != nil {
		var exitErr *exec.ExitError
		if errors.As(cmdErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	result := map[string]any{
		"status":   "completed",
		"exitCode": exitCode,
		"output":   string(output),
	}
	if exitCode != 0 {
		result["status"] = "failed"
	}

	s.BroadcastEvent("build-complete", result)
	s.writeJSON(w, result)
}
