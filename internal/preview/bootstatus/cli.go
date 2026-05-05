package bootstatus

import (
	"fmt"
	"sync"
)

// CLISink renders boot phases on the terminal. The concrete implementation is
// in internal/logx so this package stays free of UI dependencies.
type CLISink interface {
	// SetMessage updates the visible status line to the given message.
	SetMessage(message string)
	// Done stops the active spinner (no-op when no phase is in progress).
	Done()
	// Stop releases any resources (called once at shutdown).
	Stop()
}

// CLIReporter drives a CLISink as boot phases progress.
type CLIReporter struct {
	sink CLISink

	mu   sync.RWMutex
	last Status
}

// NewCLIReporter binds a sink. A nil sink yields a no-op reporter.
func NewCLIReporter(sink CLISink) *CLIReporter {
	return &CLIReporter{
		sink: sink,
		last: Status{Phase: PhaseStarting, Message: "Starting preview"},
	}
}

// Begin shows the new phase message on the spinner.
func (r *CLIReporter) Begin(phase Phase, message string) {
	r.mu.Lock()
	r.last = Status{Phase: phase, Message: message}
	r.mu.Unlock()
	if r.sink != nil {
		r.sink.SetMessage(message)
	}
}

// Progress refreshes the spinner with a counter, e.g. "(3/12) postgres".
func (r *CLIReporter) Progress(done, total int, item string) {
	r.mu.Lock()
	r.last.Done = done
	r.last.Total = total
	r.last.Item = item
	msg := r.last.Message
	r.mu.Unlock()
	if r.sink == nil {
		return
	}
	r.sink.SetMessage(formatProgressMessage(msg, done, total, item))
}

// End closes out the spinner for the named phase.
func (r *CLIReporter) End(phase Phase) {
	r.mu.Lock()
	current := r.last.Phase
	r.mu.Unlock()
	if current != phase {
		return
	}
	if r.sink != nil {
		r.sink.Done()
	}
}

// Fail stops the spinner with an error indicator.
func (r *CLIReporter) Fail(phase Phase, err error) {
	r.mu.Lock()
	r.last = Status{Phase: PhaseError, Message: string(phase)}
	if err != nil {
		r.last.Error = err.Error()
	}
	r.mu.Unlock()
	if r.sink != nil {
		r.sink.Done()
	}
}

// Snapshot returns the latest status seen by the reporter.
func (r *CLIReporter) Snapshot() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.last
}

func formatProgressMessage(message string, done, total int, item string) string {
	switch {
	case total > 0 && item != "":
		return fmt.Sprintf("%s (%d/%d) %s", message, done, total, item)
	case total > 0:
		return fmt.Sprintf("%s (%d/%d)", message, done, total)
	case item != "":
		return fmt.Sprintf("%s %s", message, item)
	default:
		return message
	}
}
