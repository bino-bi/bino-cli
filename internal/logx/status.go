package logx

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mattn/go-isatty"
)

// Status renders a single, replaced-in-place activity line on a TTY. When the
// output is not a terminal (CI=1, piped output, NO_COLOR-only environments),
// it falls back to plain log lines on the supplied logger.
type Status interface {
	// SetMessage replaces the visible status line. Safe to call from any
	// goroutine.
	SetMessage(message string)
	// Done freezes/clears the active line. Subsequent SetMessage calls start
	// a new render.
	Done()
	// Stop releases any resources. Safe to call multiple times.
	Stop()
}

// NewStatus picks the best status sink for the given output. When `out` is a
// TTY (and CI/NO_COLOR are not forcing plain mode) it returns a spinning
// status line; otherwise it logs each transition through `logger.Infof`.
func NewStatus(logger Logger, out io.Writer) Status {
	if useSpinner(out) {
		return newSpinnerStatus(out)
	}
	return &plainStatus{logger: logger}
}

func useSpinner(out io.Writer) bool {
	if out == nil {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	if IsNoColorEnv() {
		return false
	}
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

type spinnerStatus struct {
	mu  sync.Mutex
	sp  *spinner.Spinner
	out io.Writer
}

func newSpinnerStatus(out io.Writer) *spinnerStatus {
	s := spinner.New(spinner.CharSets[14], 90*time.Millisecond, spinner.WithWriter(out))
	s.HideCursor = true
	return &spinnerStatus{sp: s, out: out}
}

func (s *spinnerStatus) SetMessage(message string) {
	if s == nil || s.sp == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sp.Suffix = " " + strings.TrimRight(message, "\n")
	if !s.sp.Active() {
		s.sp.Start()
	}
}

func (s *spinnerStatus) Done() {
	if s == nil || s.sp == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sp.Active() {
		s.sp.Stop()
	}
}

func (s *spinnerStatus) Stop() {
	if s == nil || s.sp == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sp.Active() {
		s.sp.Stop()
	}
}

type plainStatus struct {
	mu     sync.Mutex
	logger Logger
	last   string
}

func (p *plainStatus) SetMessage(message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if message == "" || message == p.last {
		return
	}
	p.last = message
	if p.logger != nil {
		p.logger.Infof("%s", message)
	}
}

func (p *plainStatus) Done() { /* nothing to clear in plain mode */ }
func (p *plainStatus) Stop() { /* nothing to release in plain mode */ }
