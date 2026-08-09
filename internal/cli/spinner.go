package cli

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mattn/go-isatty"
)

// Spinner provides progress indication for long-running operations.
// It automatically falls back to simple text output in CI environments or non-TTY terminals.
type Spinner struct {
	spinner   *spinner.Spinner
	stdout    io.Writer
	stderr    io.Writer
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
	Stderr  io.Writer
	NoColor bool
}

// NewSpinner creates a new Spinner with the given configuration.
func NewSpinner(cfg SpinnerConfig) *Spinner {
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	// Determine TTY status
	isTTY := false
	if f, ok := stdout.(*os.File); ok {
		isTTY = isatty.IsTerminal(f.Fd())
	}

	// Check CI environment - disable spinner in CI
	if os.Getenv("CI") != "" {
		isTTY = false
	}

	// Use the global style (already initialized by root command)
	style := GetStyle()

	s := &Spinner{
		stdout: stdout,
		stderr: stderr,
		style:  style,
		isTTY:  isTTY,
	}

	if isTTY {
		// Create spinner with dots pattern for modern look
		sp := spinner.New(spinner.CharSets[14], 80*time.Millisecond, spinner.WithWriter(stdout))
		if !style.NoColor {
			_ = sp.Color("cyan") //nolint:errcheck // constant color name the spinner library accepts
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
	}
	// Errors belong on stderr — `bino build > file` must not hide render
	// failures, and stdout may carry machine-consumed data.
	s.style.Red.Fprintf(s.stderr, "%s ", SymbolError)
	fmt.Fprintln(s.stderr, errMsg)
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
		s.style.Yellow.Fprintf(s.stderr, "%s ", SymbolWarning)
		fmt.Fprint(s.stderr, warnMsg)
		s.style.Dim.Fprintf(s.stderr, " (%s)\n", formatDuration(elapsed))
	} else {
		s.style.Yellow.Fprintf(s.stderr, "%s ", SymbolWarning)
		fmt.Fprintln(s.stderr, warnMsg)
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
