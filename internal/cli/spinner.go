package cli

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

// Spinner provides progress indication for long-running operations.
// It automatically falls back to simple text output in CI environments or non-TTY terminals.
type Spinner struct {
	spinner   *spinner.Spinner
	stdout    io.Writer
	style     *Style
	isTTY     bool
	message   string
	startTime time.Time
	mu        sync.Mutex
	active    bool
}

// SpinnerConfig holds configuration for creating a Spinner.
type SpinnerConfig struct {
	Stdout  io.Writer
	NoColor bool
}

// NewSpinner creates a new Spinner with the given configuration.
func NewSpinner(cfg SpinnerConfig) *Spinner {
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	// Determine TTY status
	isTTY := false
	if f, ok := stdout.(*os.File); ok {
		isTTY = term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: fd value fits in int on all supported platforms
	}

	// Check CI environment - disable spinner in CI
	if os.Getenv("CI") != "" {
		isTTY = false
	}

	// Use the global style (already initialized by root command)
	style := GetStyle()

	s := &Spinner{
		stdout: stdout,
		style:  style,
		isTTY:  isTTY,
	}

	if isTTY {
		// Create spinner with dots pattern for modern look
		sp := spinner.New(spinner.CharSets[14], 80*time.Millisecond, spinner.WithWriter(stdout))
		if !style.NoColor {
			_ = sp.Color("cyan")
		}
		s.spinner = sp
	}

	return s
}

// Start begins the spinner with the given message.
func (s *Spinner) Start(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		return
	}

	s.message = message
	s.startTime = time.Now()
	s.active = true

	if s.isTTY && s.spinner != nil {
		s.spinner.Suffix = " " + message
		s.spinner.Start()
	} else {
		// Fallback for non-TTY: simple text output
		s.style.Cyan.Fprintf(s.stdout, "%s ", SymbolArrow)
		fmt.Fprintln(s.stdout, message+"...")
	}
}

// Update changes the spinner message while running.
func (s *Spinner) Update(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	s.message = message

	if s.isTTY && s.spinner != nil {
		s.spinner.Suffix = " " + message
	}
	// In non-TTY mode, we don't print updates to avoid spam
}

// Stop stops the spinner and shows a success message.
func (s *Spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	elapsed := time.Since(s.startTime)
	s.active = false

	if s.isTTY && s.spinner != nil {
		s.spinner.Stop()
		// Print success line with timing
		s.style.Green.Fprintf(s.stdout, "%s ", SymbolSuccess)
		fmt.Fprint(s.stdout, s.message)
		s.style.Dim.Fprintf(s.stdout, " (%s)\n", formatDuration(elapsed))
	}
	// In non-TTY mode, we printed the start message, success will be shown by caller
}

// StopWithError stops the spinner and shows an error.
func (s *Spinner) StopWithError(errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	s.active = false

	if s.isTTY && s.spinner != nil {
		s.spinner.Stop()
		// Print error line
		s.style.Red.Fprintf(s.stdout, "%s ", SymbolError)
		fmt.Fprintln(s.stdout, errMsg)
	} else {
		// Non-TTY: print error
		s.style.Red.Fprintf(s.stdout, "%s ", SymbolError)
		fmt.Fprintln(s.stdout, errMsg)
	}
}

// StopWithWarning stops the spinner and shows a warning.
func (s *Spinner) StopWithWarning(warnMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	elapsed := time.Since(s.startTime)
	s.active = false

	if s.isTTY && s.spinner != nil {
		s.spinner.Stop()
		// Print warning line
		s.style.Yellow.Fprintf(s.stdout, "%s ", SymbolWarning)
		fmt.Fprint(s.stdout, warnMsg)
		s.style.Dim.Fprintf(s.stdout, " (%s)\n", formatDuration(elapsed))
	} else {
		// Non-TTY: print warning
		s.style.Yellow.Fprintf(s.stdout, "%s ", SymbolWarning)
		fmt.Fprintln(s.stdout, warnMsg)
	}
}

// IsActive returns whether the spinner is currently running.
func (s *Spinner) IsActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// RunWithSpinner executes a function with spinner feedback.
func RunWithSpinner[T any](s *Spinner, message string, fn func() (T, error)) (T, error) {
	s.Start(message)
	result, err := fn()
	if err != nil {
		s.StopWithError(message + " failed")
		return result, err
	}
	s.Stop()
	return result, nil
}
