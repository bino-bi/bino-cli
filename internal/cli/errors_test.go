package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

// Regression: FormatError kept only the last two ": "-separated fragments of
// the message, so a deeply wrapped error lost the outer frames — the ones
// naming the artefact and dataset the user can actually act on.
func TestFormatErrorKeepsAllFrames(t *testing.T) {
	InitStyle(true)

	err := RuntimeError(fmt.Errorf("artefact sales: %w",
		fmt.Errorf("dataset revenue: %w",
			fmt.Errorf("execute query: %w",
				errors.New(`syntax error near "FROM"`)))))

	msg, code := FormatError(context.Background(), err)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (runtime)", code)
	}
	for _, frame := range []string{"artefact sales", "dataset revenue", "execute query", `syntax error near "FROM"`} {
		if !strings.Contains(msg, frame) {
			t.Errorf("formatted error lost frame %q:\n%s", frame, msg)
		}
	}
}

// Messages legitimately containing ": " inside one frame must not be cut at
// an arbitrary colon.
func TestFormatErrorDoesNotMisSplitColonSpace(t *testing.T) {
	InitStyle(true)

	err := ConfigError(errors.New(`invalid mapping: expected "key: value" near line 3: column 7`))
	msg, _ := FormatError(context.Background(), err)
	if !strings.Contains(msg, "invalid mapping") {
		t.Errorf("formatted error lost the leading frame:\n%s", msg)
	}
}

// buildErrorChain must traverse Unwrap() []error (errors.Join), giving each
// joined branch its own chain entry instead of rendering one blob.
func TestBuildErrorChainMultiError(t *testing.T) {
	InitStyle(true)

	err := fmt.Errorf("top: %w", errors.Join(
		fmt.Errorf("first branch: %w", errors.New("deep cause")),
		errors.New("second branch"),
	))
	chain := buildErrorChain(err)
	if got := strings.Count(chain, SymbolBullet); got < 2 {
		t.Errorf("chain has %d entries, want one per joined branch (>= 2):\n%s", got, chain)
	}
	if strings.Contains(chain, "first branch\nsecond") {
		t.Errorf("joined error rendered as one blob:\n%s", chain)
	}
}

// The verbose error chain renders under --verbose.
func TestFormatErrorVerboseShowsChain(t *testing.T) {
	InitStyle(true)

	ctx := logx.WithDebug(context.Background(), true)
	err := RuntimeError(fmt.Errorf("outer: %w", errors.New("inner cause")))
	msg, _ := FormatError(ctx, err)
	if !strings.Contains(msg, "Error chain:") {
		t.Errorf("verbose output has no error chain section:\n%s", msg)
	}
}

// Regression: registryProjectSetup replaced the ErrProjectRootNotFound
// sentinel with a fresh string, breaking errors.Is and dropping the
// "run 'bino init'" hint the sentinel carries.
func TestRegistryProjectSetupPreservesSentinel(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := registryProjectSetup()
	if err == nil {
		t.Fatal("expected an error outside a bino project")
	}
	if !errors.Is(err, pathutil.ErrProjectRootNotFound) {
		t.Errorf("errors.Is(err, pathutil.ErrProjectRootNotFound) = false, want true (err: %v)", err)
	}
	if !strings.Contains(err.Error(), "bino init") {
		t.Errorf("error dropped the 'bino init' hint: %v", err)
	}
}

// ExitCodeFromError maps a subprocess exit code carried in the error chain.
func TestExitCodeFromError(t *testing.T) {
	if _, ok := ExitCodeFromError(errors.New("plain")); ok {
		t.Error("plain error must not map to an exit code")
	}
	code, ok := ExitCodeFromError(fmt.Errorf("wrapped: %w", &ExitCodeError{Code: 7}))
	if !ok || code != 7 {
		t.Errorf("ExitCodeFromError = (%d, %v), want (7, true)", code, ok)
	}
}
