package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/version"
)

// HTTPBackend satisfies lsp.Backend by proxying a running bino daemon over
// loopback HTTP, subscribing to the daemon's SSE /events to invalidate caches.
// The full LSP server still runs locally; only the heavy data is remoted.
type HTTPBackend struct {
	base     string // http://127.0.0.1:<port>
	hc       *http.Client
	log      logx.Logger
	onChange atomic.Pointer[func()]
	stop     chan struct{}
}

// NewHTTPBackend builds a proxy backend and verifies the daemon answers
// /health AND actually serves the endpoints the LSP depends on. A stale daemon
// (spawned from an older binary) previously passed the bare health check and
// then 404'd /validate-draft — diagnostics silently vanished; erroring here
// makes the caller fall back to a working standalone backend instead.
func NewHTTPBackend(ctx context.Context, base string, log logx.Logger) (*HTTPBackend, error) {
	b := &HTTPBackend{
		base: strings.TrimRight(base, "/"),
		hc:   &http.Client{},
		log:  log,
		stop: make(chan struct{}),
	}
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(hctx, http.MethodGet, b.base+"/health", http.NoBody)
	resp, err := b.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var health struct {
		Version      string   `json:"version"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("daemon health: decode: %w", err)
	}
	if !slices.Contains(health.Capabilities, "validate-draft") {
		v := health.Version
		if v == "" {
			v = "unknown"
		}
		return nil, fmt.Errorf("daemon (version %s) predates /validate-draft — restart it with the current binary", v)
	}
	if health.Version != "" && health.Version != version.Version {
		log.Warnf("daemon version %s differs from bino %s; restart the daemon if behavior looks stale", health.Version, version.Version)
	}
	return b, nil
}

func (b *HTTPBackend) Start(ctx context.Context) error {
	go b.watchEvents(ctx)
	return nil
}

func (b *HTTPBackend) Close() error {
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
	return nil
}

func (b *HTTPBackend) OnProjectChange(fn func()) { b.onChange.Store(&fn) }

func (b *HTTPBackend) fireChange() {
	if fn := b.onChange.Load(); fn != nil {
		(*fn)()
	}
}

func (b *HTTPBackend) Index(ctx context.Context) ([]IndexDoc, error) {
	var r struct {
		Documents []struct {
			Kind, Name, File string
			Position         int
		} `json:"documents"`
	}
	if err := b.getJSON(ctx, "/index", &r); err != nil {
		return nil, err
	}
	out := make([]IndexDoc, len(r.Documents))
	for i, d := range r.Documents {
		out[i] = IndexDoc{Kind: d.Kind, Name: d.Name, File: d.File, Position: d.Position}
	}
	return out, nil
}

func (b *HTTPBackend) MergedSchema(ctx context.Context) (json.RawMessage, error) {
	body, err := b.getRaw(ctx, "/schema")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func (b *HTTPBackend) Columns(ctx context.Context, name string) ([]string, error) {
	var r struct {
		Columns []string `json:"columns"`
		Error   string   `json:"error"`
	}
	if err := b.getJSON(ctx, "/columns?name="+url.QueryEscape(name), &r); err != nil {
		return nil, err
	}
	if r.Error != "" {
		return nil, fmt.Errorf("%s", r.Error)
	}
	return r.Columns, nil
}

func (b *HTTPBackend) GraphDeps(ctx context.Context, kind, name, direction string, maxDepth int) (GraphResult, error) {
	q := url.Values{}
	q.Set("kind", kind)
	q.Set("name", name)
	q.Set("direction", direction)
	q.Set("max-depth", strconv.Itoa(maxDepth))
	var r struct {
		Nodes []struct{ ID, Kind, Name, File string }    `json:"nodes"`
		Edges []struct{ FromID, ToID, Direction string } `json:"edges"`
		Error string                                     `json:"error"`
	}
	if err := b.getJSON(ctx, "/graph-deps?"+q.Encode(), &r); err != nil {
		return GraphResult{}, err
	}
	out := GraphResult{Error: r.Error}
	for _, n := range r.Nodes {
		out.Nodes = append(out.Nodes, GraphNode{ID: n.ID, Kind: n.Kind, Name: n.Name, File: n.File})
	}
	for _, e := range r.Edges {
		out.Edges = append(out.Edges, GraphEdge{FromID: e.FromID, ToID: e.ToID, Direction: e.Direction})
	}
	return out, nil
}

func (b *HTTPBackend) ValidateDraft(ctx context.Context, yamlBytes []byte) ([]Diag, error) {
	var r validateResponse
	if err := b.postJSON(ctx, "/validate-draft", yamlBytes, &r); err != nil {
		return nil, err
	}
	return r.diags(), nil
}

func (b *HTTPBackend) ValidateProject(ctx context.Context, execQueries bool) (bool, []Diag, error) {
	var r validateResponse
	var err error
	if execQueries {
		err = b.postJSON(ctx, "/validate", nil, &r)
	} else {
		err = b.getJSON(ctx, "/validate", &r)
	}
	if err != nil {
		return false, nil, err
	}
	return r.Valid, r.diags(), nil
}

type validateResponse struct {
	Valid       bool `json:"valid"`
	Diagnostics []struct {
		File     string `json:"file"`
		Position int    `json:"position"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Code     string `json:"code"`
		Field    string `json:"field"`
		Hint     string `json:"hint"`
	} `json:"diagnostics"`
}

func (r validateResponse) diags() []Diag {
	out := make([]Diag, len(r.Diagnostics))
	for i, d := range r.Diagnostics {
		out[i] = Diag{
			File: d.File, Position: d.Position, Line: d.Line, Column: d.Column,
			Severity: d.Severity, Message: d.Message, Code: d.Code, Field: d.Field,
			Hint: d.Hint,
		}
	}
	return out
}

// --- HTTP plumbing ---

func (b *HTTPBackend) getRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (b *HTTPBackend) getJSON(ctx context.Context, path string, out any) error {
	body, err := b.getRaw(ctx, path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (b *HTTPBackend) postJSON(ctx context.Context, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %s", path, resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	return dec.Decode(out)
}

// watchEvents reads the daemon's SSE stream and fires the change callback on
// project-state events. It reconnects after a drop until Close.
func (b *HTTPBackend) watchEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.stop:
			return
		default:
		}
		if shut := b.readEventStream(ctx); shut {
			return // daemon announced shutdown
		}
		select {
		case <-ctx.Done():
			return
		case <-b.stop:
			return
		case <-time.After(time.Second):
		}
	}
}

// readEventStream consumes one SSE connection; returns true when the daemon sent
// a shutdown event (caller should stop reconnecting).
func (b *HTTPBackend) readEventStream(ctx context.Context) (shutdown bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+"/events", http.NoBody)
	if err != nil {
		return false
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var event string
	for scanner.Scan() {
		select {
		case <-b.stop:
			return false
		default:
		}
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case line == "":
			switch event {
			case "index-updated", "diagnostics", "build-complete":
				b.fireChange()
			case "shutdown":
				return true
			}
			event = ""
		}
	}
	if err := scanner.Err(); err != nil {
		b.log.Debugf("SSE stream ended: %v", err)
	}
	return false
}
