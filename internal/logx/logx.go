package logx

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/fatih/color"
)

type Logger interface {
	Infof(format string, args ...any)
	Successf(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Debugf(format string, args ...any)
	Channel(name string) Logger
}

type ctxKey struct{}
type debugKey struct{}
type runIDKey struct{}
type noColorKey struct{}

type nopLogger struct{}

func (nopLogger) Infof(string, ...any)    {}
func (nopLogger) Successf(string, ...any) {}
func (nopLogger) Warnf(string, ...any)    {}
func (nopLogger) Errorf(string, ...any)   {}
func (nopLogger) Debugf(string, ...any)   {}
func (nopLogger) Channel(string) Logger   { return nopLogger{} }

func Nop() Logger {
	return nopLogger{}
}

func WithLogger(ctx context.Context, logger Logger) context.Context {
	if logger == nil {
		logger = Nop()
	}
	return context.WithValue(ctx, ctxKey{}, logger)
}

func WithDebug(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, debugKey{}, enabled)
}

func DebugEnabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if enabled, ok := ctx.Value(debugKey{}).(bool); ok {
		return enabled
	}
	return false
}

func WithRunID(ctx context.Context, runID string) context.Context {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, runIDKey{}, runID)
}

func RunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if runID, ok := ctx.Value(runIDKey{}).(string); ok {
		return runID
	}
	return ""
}

func WithNoColor(ctx context.Context, noColor bool) context.Context {
	return context.WithValue(ctx, noColorKey{}, noColor)
}

func NoColorEnabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if noColor, ok := ctx.Value(noColorKey{}).(bool); ok {
		return noColor
	}
	return false
}

// IsNoColorEnv checks if NO_COLOR environment variable is set.
// This is a convenience function for checking the environment variable directly.
func IsNoColorEnv() bool {
	return os.Getenv("NO_COLOR") != ""
}

func FromContext(ctx context.Context) Logger {
	if ctx == nil {
		return Nop()
	}
	if logger, ok := ctx.Value(ctxKey{}).(Logger); ok && logger != nil {
		return logger
	}
	return Nop()
}

type Terminal struct {
	core   *terminalCore
	prefix string
}

type terminalCore struct {
	stdout   io.Writer
	stderr   io.Writer
	noColor  bool
	verbose  bool
	mu       sync.Mutex
	infoC    *color.Color
	successC *color.Color
	warnC    *color.Color
	errorC   *color.Color
	debugC   *color.Color
	dimC     *color.Color
}

// Terminal symbols for visual feedback
const (
	symbolInfo    = "ℹ"
	symbolSuccess = "✓"
	symbolWarning = "⚠"
	symbolError   = "✗"
	symbolDebug   = "●"
)

// NewTerminal creates a new Terminal logger with the given writers and verbosity.
// The noColor parameter controls whether ANSI colors are disabled.
// Pass true if either --no-color flag was specified or NO_COLOR env is set.
func NewTerminal(stdout, stderr io.Writer, verbose bool) *Terminal {
	return NewTerminalWithColor(stdout, stderr, verbose, IsNoColorEnv())
}

// NewTerminalWithColor creates a new Terminal logger with explicit color control.
// Use this when you need to pass the noColor setting from the CLI flags.
func NewTerminalWithColor(stdout, stderr io.Writer, verbose, noColor bool) *Terminal {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if noColor {
		color.NoColor = true
	}
	core := &terminalCore{
		stdout:   stdout,
		stderr:   stderr,
		noColor:  noColor,
		verbose:  verbose,
		infoC:    color.New(color.FgCyan),
		successC: color.New(color.FgGreen),
		warnC:    color.New(color.FgYellow),
		errorC:   color.New(color.FgRed),
		debugC:   color.New(color.Faint),
		dimC:     color.New(color.Faint),
	}
	return &Terminal{core: core}
}

func (t *Terminal) Channel(name string) Logger {
	if t == nil || t.core == nil {
		return Nop()
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return t
	}
	prefix := name
	if t.prefix != "" {
		prefix = t.prefix + "/" + name
	}
	return &Terminal{core: t.core, prefix: prefix}
}

func (t *Terminal) Infof(format string, args ...any) {
	t.write(t.core.stdout, t.core.infoC, symbolInfo, "INFO", format, args...)
}

func (t *Terminal) Successf(format string, args ...any) {
	t.write(t.core.stdout, t.core.successC, symbolSuccess, "OK", format, args...)
}

func (t *Terminal) Warnf(format string, args ...any) {
	t.write(t.core.stderr, t.core.warnC, symbolWarning, "WARN", format, args...)
}

func (t *Terminal) Errorf(format string, args ...any) {
	t.write(t.core.stderr, t.core.errorC, symbolError, "ERR", format, args...)
}

func (t *Terminal) Debugf(format string, args ...any) {
	if t == nil || t.core == nil || !t.core.verbose {
		return
	}
	t.write(t.core.stdout, t.core.debugC, symbolDebug, "DBG", format, args...)
}

func (t *Terminal) write(out io.Writer, c *color.Color, symbol, label, format string, args ...any) {
	if t == nil || t.core == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	msg = strings.TrimRight(msg, "\n")
	if strings.Contains(msg, "\n") {
		msg = indentMultiline(msg)
	}

	var prefix string
	if t.prefix != "" {
		prefix = t.core.dimC.Sprintf("%s: ", t.prefix)
	}

	labelText := c.Sprintf("%s %s", symbol, label)
	line := fmt.Sprintf("%s %s%s\n", labelText, prefix, msg)

	t.core.mu.Lock()
	defer t.core.mu.Unlock()
	fmt.Fprint(out, line)
}

func indentMultiline(msg string) string {
	lines := strings.Split(msg, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "           " + lines[i]
	}
	return strings.Join(lines, "\n")
}
