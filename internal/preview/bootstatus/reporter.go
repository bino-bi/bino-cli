// Package bootstatus reports the current phase of preview cold-start to
// multiple sinks (CLI spinner, SSE event hub) so the user can see progress
// while the server is still warming up.
package bootstatus

import (
	"sync"
)

// Phase identifies a discrete cold-start step.
type Phase string

const (
	PhaseStarting  Phase = "starting"
	PhaseDuckDB    Phase = "duckdb"
	PhaseExplorer  Phase = "explorer"
	PhaseManifests Phase = "manifests"
	PhaseGraph     Phase = "graph"
	PhaseRendering Phase = "rendering"
	PhaseReady     Phase = "ready"
	PhaseError     Phase = "error"
)

// Status captures everything the UI/CLI need to render a single boot snapshot.
type Status struct {
	Phase   Phase  `json:"phase"`
	Message string `json:"message"`
	Done    int    `json:"done,omitempty"`
	Total   int    `json:"total,omitempty"`
	Item    string `json:"item,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Reporter receives boot lifecycle events.
type Reporter interface {
	Begin(phase Phase, message string)
	Progress(done, total int, item string)
	End(phase Phase)
	Fail(phase Phase, err error)
	// Snapshot returns the latest status; useful for late-connecting clients.
	Snapshot() Status
}

// Multiplexer fans out lifecycle events to multiple reporters and keeps the
// last status snapshot for late subscribers.
type Multiplexer struct {
	mu        sync.RWMutex
	reporters []Reporter
	last      Status
}

// NewMultiplexer wraps zero or more reporters.
func NewMultiplexer(reporters ...Reporter) *Multiplexer {
	filtered := make([]Reporter, 0, len(reporters))
	for _, r := range reporters {
		if r != nil {
			filtered = append(filtered, r)
		}
	}
	return &Multiplexer{
		reporters: filtered,
		last:      Status{Phase: PhaseStarting, Message: "Starting preview"},
	}
}

// Begin records the start of a phase and forwards to each reporter.
func (m *Multiplexer) Begin(phase Phase, message string) {
	m.mu.Lock()
	m.last = Status{Phase: phase, Message: message}
	m.mu.Unlock()
	for _, r := range m.reporters {
		r.Begin(phase, message)
	}
}

// Progress reports incremental progress within the current phase.
func (m *Multiplexer) Progress(done, total int, item string) {
	m.mu.Lock()
	m.last.Done = done
	m.last.Total = total
	m.last.Item = item
	m.mu.Unlock()
	for _, r := range m.reporters {
		r.Progress(done, total, item)
	}
}

// End marks a phase as completed.
func (m *Multiplexer) End(phase Phase) {
	m.mu.Lock()
	if m.last.Phase == phase {
		m.last.Done = 0
		m.last.Total = 0
		m.last.Item = ""
	}
	m.mu.Unlock()
	for _, r := range m.reporters {
		r.End(phase)
	}
}

// Fail records a terminal error for the current phase.
func (m *Multiplexer) Fail(phase Phase, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	m.mu.Lock()
	m.last = Status{Phase: PhaseError, Message: string(phase), Error: msg}
	m.mu.Unlock()
	for _, r := range m.reporters {
		r.Fail(phase, err)
	}
}

// Snapshot returns the most recent status without hitting any sink.
func (m *Multiplexer) Snapshot() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.last
}

// nopReporter discards every event. Useful in tests.
type nopReporter struct{}

func (nopReporter) Begin(Phase, string)       {}
func (nopReporter) Progress(int, int, string) {}
func (nopReporter) End(Phase)                 {}
func (nopReporter) Fail(Phase, error)         {}
func (nopReporter) Snapshot() Status          { return Status{Phase: PhaseStarting} }

// Nop returns a reporter that silently drops every event.
func Nop() Reporter { return nopReporter{} }
