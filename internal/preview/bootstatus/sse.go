package bootstatus

import (
	"encoding/json"
	"sync"
)

// Broadcaster is the subset of the preview HTTP server used by the SSE
// reporter. Defined here to avoid an import cycle with httpserver. The
// payload is the JSON-encoded Status so the server stays type-agnostic.
type Broadcaster interface {
	BroadcastBootStatus(payload []byte)
}

// SSEReporter forwards each lifecycle event to connected SSE clients via the
// preview server. It also caches the latest snapshot so a late-connecting
// loading page can fetch it via HTTP.
type SSEReporter struct {
	bus Broadcaster

	mu   sync.RWMutex
	last Status
}

// NewSSEReporter wraps a broadcaster. A nil bus produces a no-op reporter.
func NewSSEReporter(bus Broadcaster) *SSEReporter {
	return &SSEReporter{
		bus:  bus,
		last: Status{Phase: PhaseStarting, Message: "Starting preview"},
	}
}

// Begin sets the current phase and broadcasts it.
func (r *SSEReporter) Begin(phase Phase, message string) {
	r.mu.Lock()
	r.last = Status{Phase: phase, Message: message}
	snapshot := r.last
	r.mu.Unlock()
	r.broadcast(snapshot)
}

// Progress updates the in-flight counters and broadcasts.
func (r *SSEReporter) Progress(done, total int, item string) {
	r.mu.Lock()
	r.last.Done = done
	r.last.Total = total
	r.last.Item = item
	snapshot := r.last
	r.mu.Unlock()
	r.broadcast(snapshot)
}

// End clears progress counters for the finished phase but does not
// broadcast. Phase transitions are signaled by the next Begin call so the
// loading page does not flicker between two "complete" states.
func (r *SSEReporter) End(phase Phase) {
	r.mu.Lock()
	if r.last.Phase == phase {
		r.last.Done = 0
		r.last.Total = 0
		r.last.Item = ""
	}
	r.mu.Unlock()
}

// Fail broadcasts a terminal error for the current phase.
func (r *SSEReporter) Fail(phase Phase, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	r.mu.Lock()
	r.last = Status{Phase: PhaseError, Message: string(phase), Error: msg}
	snapshot := r.last
	r.mu.Unlock()
	r.broadcast(snapshot)
}

// Snapshot returns the latest status. Used by the boot-status HTTP endpoint
// so a browser opening after boot completes still sees the final state.
func (r *SSEReporter) Snapshot() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.last
}

func (r *SSEReporter) broadcast(s Status) {
	if r.bus == nil {
		return
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return
	}
	r.bus.BroadcastBootStatus(payload)
}
