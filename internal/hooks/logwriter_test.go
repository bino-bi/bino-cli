package hooks

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

// Regression: logWriter buffered until it saw a newline and was never
// flushed, so a hook whose last output line lacked a trailing \n lost that
// line — often the one explaining why the hook failed.
func TestHookFinalUnterminatedLineIsLogged(t *testing.T) {
	var out, errOut bytes.Buffer
	log := logx.NewTerminalWithColor(&out, &errOut, false, true)

	// The runner echoes the command line itself, so the marker must be
	// produced by the command, not spelled inside it: printf of a lowercase
	// string piped through tr yields TAILMARKER only in the output.
	r := NewRunner(&Config{Hooks: pathutil.HooksConfig{
		"pre-build": {"printf tailmarker | tr a-z A-Z"},
	}}, log, t.TempDir())

	if err := r.Run(context.Background(), "pre-build", HookEnv{}); err != nil {
		t.Fatalf("hook run failed: %v", err)
	}

	logged := out.String() + errOut.String()
	if !strings.Contains(logged, "TAILMARKER") {
		t.Errorf("final unterminated hook output line was lost; logged: %q", logged)
	}
}
