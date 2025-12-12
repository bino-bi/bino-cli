package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/spec"
)

// ErrorKind categorizes CLI errors for appropriate exit codes and user messaging.
type ErrorKind string

const (
	// ErrorKindConfig indicates user-fixable configuration issues (YAML, flags, paths).
	ErrorKindConfig ErrorKind = "config"
	// ErrorKindRuntime indicates errors during command execution (rendering, I/O).
	ErrorKindRuntime ErrorKind = "runtime"
	// ErrorKindExternal indicates failures in external dependencies (Playwright, network).
	ErrorKindExternal ErrorKind = "external"
	// ErrorKindUnknown is the fallback for unclassified errors.
	ErrorKindUnknown ErrorKind = "unknown"
)

// exitError wraps an error with a classification kind for exit code mapping.
type exitError struct {
	kind ErrorKind
	err  error
	hint string // optional override hint
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// wrapError creates an exitError with the given kind.
func wrapError(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{kind: kind, err: err}
}

// wrapErrorWithHint creates an exitError with a custom hint message.
func wrapErrorWithHint(kind ErrorKind, err error, hint string) error {
	if err == nil {
		return nil
	}
	return &exitError{kind: kind, err: err, hint: hint}
}

// ConfigError wraps an error as a configuration error (exit code 1).
// Use for: invalid YAML, missing required fields, bad flag values, invalid paths.
func ConfigError(err error) error {
	return wrapError(ErrorKindConfig, err)
}

// ConfigErrorf creates a formatted configuration error.
func ConfigErrorf(format string, args ...any) error {
	return wrapError(ErrorKindConfig, fmt.Errorf(format, args...))
}

// RuntimeError wraps an error as a runtime error (exit code 2).
// Use for: rendering failures, I/O errors, internal processing errors.
func RuntimeError(err error) error {
	return wrapError(ErrorKindRuntime, err)
}

// RuntimeErrorf creates a formatted runtime error.
func RuntimeErrorf(format string, args ...any) error {
	return wrapError(ErrorKindRuntime, fmt.Errorf(format, args...))
}

// ExternalError wraps an error as an external dependency error (exit code 3).
// Use for: Playwright failures, network errors, third-party tool issues.
func ExternalError(err error) error {
	return wrapError(ErrorKindExternal, err)
}

// ExternalErrorf creates a formatted external error.
func ExternalErrorf(format string, args ...any) error {
	return wrapError(ErrorKindExternal, fmt.Errorf(format, args...))
}

// ExternalErrorWithHint wraps an error as external with a custom hint.
func ExternalErrorWithHint(err error, hint string) error {
	return wrapErrorWithHint(ErrorKindExternal, err, hint)
}

// errorKind extracts the ErrorKind from an error chain.
func errorKind(err error) ErrorKind {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.kind
	}
	return ErrorKindUnknown
}

// errorHint extracts a custom hint from the error chain, if present.
func errorHint(err error) string {
	var ee *exitError
	if errors.As(err, &ee) && ee.hint != "" {
		return ee.hint
	}
	return ""
}

// exitCode returns the CLI exit code for a given error kind.
func exitCode(kind ErrorKind) int {
	switch kind {
	case ErrorKindConfig:
		return 1
	case ErrorKindRuntime:
		return 2
	case ErrorKindExternal:
		return 3
	default:
		return 2
	}
}

// prefix returns the error header label for a given kind.
func prefix(kind ErrorKind) string {
	switch kind {
	case ErrorKindConfig:
		return "Configuration Error"
	case ErrorKindRuntime:
		return "Runtime Error"
	case ErrorKindExternal:
		return "External Dependency Error"
	default:
		return "Unexpected Error"
	}
}

// kindColor returns the color scheme for a given error kind.
func kindColor(kind ErrorKind) *color.Color {
	switch kind {
	case ErrorKindConfig:
		return color.New(color.FgYellow, color.Bold)
	case ErrorKindRuntime:
		return color.New(color.FgRed, color.Bold)
	case ErrorKindExternal:
		return color.New(color.FgMagenta, color.Bold)
	default:
		return color.New(color.FgRed, color.Bold)
	}
}

// defaultHint returns the default hint message for a given error kind.
func defaultHint(kind ErrorKind) string {
	switch kind {
	case ErrorKindConfig:
		return "Check your YAML manifests for syntax errors or missing required fields"
	case ErrorKindRuntime:
		return "An error occurred during execution; check the logs for more details"
	case ErrorKindExternal:
		return "Ensure external dependencies (like Playwright) are properly installed"
	default:
		return ""
	}
}

// FormatError prepares a user-facing error message and exit code based on the wrapped error kind.
// Enhanced formatting is always enabled; colors respect NO_COLOR env and --no-color flag.
func FormatError(ctx context.Context, err error) (string, int) {
	if err == nil {
		return "", 0
	}

	// Use the global style (already initialized by root command)
	style := GetStyle()

	kind := errorKind(err)
	code := exitCode(kind)

	var b strings.Builder

	// Error header with symbol and colored prefix
	errColor := kindColor(kind)

	b.WriteString("\n")
	style.RedBold.Fprint(&b, SymbolError)
	b.WriteString(" ")
	errColor.Fprint(&b, prefix(kind))
	b.WriteString("\n\n")

	// Check for schema validation error (special formatting)
	var schemaErr *spec.SchemaValidationError
	if errors.As(err, &schemaErr) {
		b.WriteString(formatSchemaValidationError(schemaErr))
	} else {
		// Regular error message
		message := extractCoreMessage(err)
		b.WriteString("  ")
		b.WriteString(message)
		b.WriteString("\n")
	}

	// Hint section: prefer custom hint from error, fall back to kind default
	h := errorHint(err)
	if h == "" {
		h = defaultHint(kind)
	}
	if h != "" {
		b.WriteString("\n")
		style.Dim.Fprint(&b, "  "+SymbolArrow+" ")
		style.Dim.Fprint(&b, h)
		b.WriteString("\n")
	}

	// Verbose mode: show error chain
	if logx.DebugEnabled(ctx) {
		if detail := buildErrorChain(err); detail != "" {
			b.WriteString("\n")
			style.Dim.Fprint(&b, "  Error chain:\n")
			b.WriteString(detail)
		}
	}

	b.WriteString("\n")

	return b.String(), code
}

// formatSchemaValidationError formats schema errors with colors and structure.
func formatSchemaValidationError(schemaErr *spec.SchemaValidationError) string {
	var b strings.Builder

	style := GetStyle()

	for i, err := range schemaErr.Errors {
		if i > 0 {
			b.WriteString("\n")
		}

		// Field path
		b.WriteString("  ")
		style.Red.Fprint(&b, SymbolError)
		b.WriteString(" ")
		if err.Field != "" && err.Field != "(root)" {
			style.Cyan.Fprint(&b, err.Field)
		} else {
			style.Dim.Fprint(&b, "(document root)")
		}
		b.WriteString("\n")

		// Description
		b.WriteString("    ")
		b.WriteString(err.Description)
		b.WriteString("\n")

		// Value if present and short
		if err.Value != nil {
			valStr := fmt.Sprintf("%v", err.Value)
			if len(valStr) <= 50 && valStr != "" && valStr != "<nil>" {
				b.WriteString("    ")
				style.Dim.Fprint(&b, "got: ")
				style.Yellow.Fprint(&b, valStr)
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// extractCoreMessage extracts the most relevant part of an error message.
func extractCoreMessage(err error) string {
	if err == nil {
		return "no additional details"
	}

	msg := err.Error()

	// Try to extract the innermost meaningful message
	// Skip generic prefixes like "artefact X: " or "build: "
	parts := strings.Split(msg, ": ")
	if len(parts) > 2 {
		// Return the last two parts for context
		return strings.Join(parts[len(parts)-2:], ": ")
	}

	return strings.TrimSpace(msg)
}

func buildErrorChain(err error) string {
	var chain []string
	current := errors.Unwrap(err)
	for current != nil {
		msg := strings.TrimSpace(current.Error())
		if msg != "" && !containsAny(chain, msg) {
			chain = append(chain, msg)
		}
		current = errors.Unwrap(current)
	}
	if len(chain) == 0 {
		return ""
	}

	style := GetStyle()

	var b strings.Builder
	for _, msg := range chain {
		b.WriteString("    ")
		style.Dim.Fprint(&b, SymbolBullet)
		b.WriteString(" ")
		style.Dim.Fprint(&b, msg)
		b.WriteString("\n")
	}
	return b.String()
}

func containsAny(slice []string, s string) bool {
	for _, item := range slice {
		if strings.Contains(item, s) || strings.Contains(s, item) {
			return true
		}
	}
	return false
}
