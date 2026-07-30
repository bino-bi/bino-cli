package lsp

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"bino.bi/bino/internal/logx"
)

// publishFunc pushes diagnostics for a document version to the client.
type publishFunc func(u uri.URI, version int32, diags []protocol.Diagnostic)

// Analyzer debounces per-document analysis and cancels superseded in-flight work,
// so rapid keystrokes coalesce and a stale result never overwrites a newer one.
type Analyzer struct {
	base     context.Context
	backend  Backend
	docs     *DocumentStore
	publish  publishFunc
	log      logx.Logger
	debounce time.Duration

	mu      sync.Mutex
	pending map[uri.URI]*inflight
}

type inflight struct {
	timer  *time.Timer
	cancel context.CancelFunc
}

// NewAnalyzer builds an analyzer; debounce defaults to 250ms when non-positive.
func NewAnalyzer(base context.Context, backend Backend, docs *DocumentStore, publish publishFunc, log logx.Logger, debounce time.Duration) *Analyzer {
	if debounce <= 0 {
		debounce = 250 * time.Millisecond
	}
	return &Analyzer{
		base:     base,
		backend:  backend,
		docs:     docs,
		publish:  publish,
		log:      log,
		debounce: debounce,
		pending:  make(map[uri.URI]*inflight),
	}
}

// Schedule (re)arms analysis for a document: it resets the debounce timer and
// cancels any analysis already running for that document.
func (a *Analyzer) Schedule(u uri.URI) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if f := a.pending[u]; f != nil {
		if f.timer != nil {
			f.timer.Stop()
		}
		if f.cancel != nil {
			f.cancel()
		}
	}
	f := &inflight{}
	f.timer = time.AfterFunc(a.debounce, func() { a.run(u) })
	a.pending[u] = f
}

// Cancel stops any pending/running analysis for a document (on didClose).
func (a *Analyzer) Cancel(u uri.URI) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if f := a.pending[u]; f != nil {
		if f.timer != nil {
			f.timer.Stop()
		}
		if f.cancel != nil {
			f.cancel()
		}
		delete(a.pending, u)
	}
}

// Shutdown cancels everything in flight.
func (a *Analyzer) Shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range a.pending {
		if f.timer != nil {
			f.timer.Stop()
		}
		if f.cancel != nil {
			f.cancel()
		}
	}
	a.pending = make(map[uri.URI]*inflight)
}

func (a *Analyzer) run(u uri.URI) {
	// This runs in a background timer goroutine; a panic here would crash the
	// whole LSP process, so contain it.
	defer func() {
		if r := recover(); r != nil {
			a.log.Errorf("analysis panic for %s: %v\n%s", u, r, debug.Stack())
		}
	}()
	ctx, cancel := context.WithCancel(a.base)
	a.mu.Lock()
	f := a.pending[u]
	if f == nil {
		a.mu.Unlock()
		cancel()
		return
	}
	f.cancel = cancel
	a.mu.Unlock()
	defer cancel()

	doc, ok := a.docs.Get(u)
	if !ok {
		return
	}
	version := doc.Version

	diags, err := a.backend.ValidateDraft(ctx, []byte(doc.Text))
	if ctx.Err() != nil {
		return // superseded by a newer keystroke — that run owns the document
	}
	if err != nil {
		a.log.Warnf("validate-draft failed for %s: %v", u, err)
		// The previously published draft diagnostics describe text that has
		// since changed; leaving them unpublished keeps stale squiggles on
		// screen. Clear them for this version — when the backend recovers, the
		// project-change path re-schedules and republishes real results.
		if cur, ok := a.docs.Get(u); ok && cur.Version == version {
			a.publish(u, version, nil)
		}
		return
	}

	// Drop the result if the buffer advanced while we were analyzing.
	if cur, ok := a.docs.Get(u); !ok || cur.Version != version {
		return
	}
	a.publish(u, version, backfillDiagnostics(doc, diags))
}
