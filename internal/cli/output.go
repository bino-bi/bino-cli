package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Output provides structured, Vite-style terminal output with colors and formatting.
type Output struct {
	stdout    io.Writer
	stderr    io.Writer
	style     *Style
	isTTY     bool
	startTime time.Time
	mu        sync.Mutex
}

// OutputConfig holds configuration for creating an Output instance.
type OutputConfig struct {
	Stdout  io.Writer
	Stderr  io.Writer
	NoColor bool
}

// NewOutput creates a new Output instance with the given configuration.
func NewOutput(cfg OutputConfig) *Output {
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
		isTTY = term.IsTerminal(int(f.Fd()))
	}

	// Check CI environment
	if os.Getenv("CI") != "" {
		isTTY = false
	}

	// Use the global style (already initialized by root command)
	style := GetStyle()

	return &Output{
		stdout:    stdout,
		stderr:    stderr,
		style:     style,
		isTTY:     isTTY,
		startTime: time.Now(),
	}
}

// IsTTY returns whether the output is a TTY.
func (o *Output) IsTTY() bool {
	return o.isTTY
}

// Header prints a prominent header line (e.g., "BINO v1.0.0").
func (o *Output) Header(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintln(o.stdout)
	o.style.CyanBold.Fprintln(o.stdout, text)
	fmt.Fprintln(o.stdout)
}

// Step prints a step/section header (e.g., "Validating manifests...").
func (o *Output) Step(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Cyan.Fprintf(o.stdout, "%s ", SymbolArrow)
	o.style.Bold.Fprintln(o.stdout, text)
}

// StepDone prints a completed step with timing.
func (o *Output) StepDone(text string, duration time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Green.Fprintf(o.stdout, "%s ", SymbolSuccess)
	fmt.Fprint(o.stdout, text)
	o.style.Dim.Fprintf(o.stdout, " (%s)\n", formatDuration(duration))
}

// Success prints a success message.
func (o *Output) Success(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Green.Fprintf(o.stdout, "%s ", SymbolSuccess)
	fmt.Fprintln(o.stdout, text)
}

// Info prints an informational message.
func (o *Output) Info(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Cyan.Fprintf(o.stdout, "%s ", SymbolInfo)
	fmt.Fprintln(o.stdout, text)
}

// Warning prints a warning message.
func (o *Output) Warning(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Yellow.Fprintf(o.stderr, "%s ", SymbolWarning)
	fmt.Fprintln(o.stderr, text)
}

// Error prints an error message.
func (o *Output) Error(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Red.Fprintf(o.stderr, "%s ", SymbolError)
	fmt.Fprintln(o.stderr, text)
}

// ErrorWithHint prints an error message with an actionable hint.
func (o *Output) ErrorWithHint(text, hint string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Red.Fprintf(o.stderr, "%s ", SymbolError)
	fmt.Fprintln(o.stderr, text)
	if hint != "" {
		o.style.Dim.Fprintf(o.stderr, "  %s %s\n", SymbolArrow, hint)
	}
}

// List prints a list item.
func (o *Output) List(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Dim.Fprintf(o.stdout, "  %s ", SymbolBullet)
	fmt.Fprintln(o.stdout, text)
}

// ListColored prints a list item with colored key-value pairs.
func (o *Output) ListColored(prefix string, keyVals ...string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.style.Dim.Fprintf(o.stdout, "  %s ", SymbolBullet)
	fmt.Fprint(o.stdout, prefix)
	for i := 0; i+1 < len(keyVals); i += 2 {
		key := keyVals[i]
		val := keyVals[i+1]
		o.style.Dim.Fprintf(o.stdout, " %s=", key)
		o.style.Magenta.Fprint(o.stdout, val)
	}
	fmt.Fprintln(o.stdout)
}

// Summary prints a summary box.
func (o *Output) Summary(title string, items []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintln(o.stdout)
	o.style.Bold.Fprintln(o.stdout, title)
	for _, item := range items {
		o.style.Dim.Fprintf(o.stdout, "  %s ", SymbolBullet)
		fmt.Fprintln(o.stdout, item)
	}
}

// Done prints a final success summary with total time.
func (o *Output) Done(text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	elapsed := time.Since(o.startTime)
	fmt.Fprintln(o.stdout)
	o.style.GreenBold.Fprintf(o.stdout, "%s %s ", SymbolSuccess, text)
	o.style.Dim.Fprintf(o.stdout, "in %s\n", formatDuration(elapsed))
	fmt.Fprintln(o.stdout)
}

// Blank prints a blank line.
func (o *Output) Blank() {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintln(o.stdout)
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

// FormatPath formats a file path for display (dims the directory part).
func FormatPath(path string) string {
	style := GetStyle()
	// Find the last path separator
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		idx = strings.LastIndex(path, "\\")
	}
	if idx == -1 {
		return path
	}
	dir := path[:idx+1]
	file := path[idx+1:]
	return style.Dim.Sprint(dir) + file
}

// FormatKind formats a document kind for display.
func FormatKind(kind string) string {
	return GetStyle().Magenta.Sprint(kind)
}

// FormatName formats a document name for display.
func FormatName(name string) string {
	return GetStyle().Cyan.Sprint(name)
}

// Box prints content inside a box (for errors, etc.).
func (o *Output) Box(title string, content []string, boxColor *Style) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Use red style as default
	bc := o.style.Red
	if boxColor != nil {
		bc = boxColor.Red
	}

	// Find max width
	maxLen := len(title)
	for _, line := range content {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	width := maxLen + 4

	// Top border
	bc.Fprintf(o.stderr, "╭%s╮\n", strings.Repeat("─", width))

	// Title
	bc.Fprint(o.stderr, "│ ")
	o.style.Bold.Fprint(o.stderr, title)
	bc.Fprintf(o.stderr, "%s │\n", strings.Repeat(" ", width-len(title)-2))

	// Separator
	bc.Fprintf(o.stderr, "├%s┤\n", strings.Repeat("─", width))

	// Content
	for _, line := range content {
		bc.Fprint(o.stderr, "│ ")
		fmt.Fprint(o.stderr, line)
		bc.Fprintf(o.stderr, "%s │\n", strings.Repeat(" ", width-len(line)-2))
	}

	// Bottom border
	bc.Fprintf(o.stderr, "╰%s╯\n", strings.Repeat("─", width))
}
