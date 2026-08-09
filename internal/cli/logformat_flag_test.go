package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// Regression: build and lint registered a LOCAL --log-format flag with a
// different meaning (build-log file format). pflag skips duplicate names when
// merging persistent flags, so the root's --log-format (logger format) never
// reached these commands — `bino build --log-format json` was accepted and
// did nothing for logging, and the root's validation never fired.
func TestBuildLogFormatReachesRootLogger(t *testing.T) {
	t.Setenv("CI", "1")
	t.Chdir(t.TempDir())

	root := newRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"build", "--log-format", "bogus"})
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid --log-format") {
		t.Fatalf("--log-format on build does not reach the root logger validation; err = %v", err)
	}
}

func TestLintLogFormatReachesRootLogger(t *testing.T) {
	t.Setenv("CI", "1")
	t.Chdir(t.TempDir())

	root := newRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"lint", "--log-format", "bogus"})
	err := root.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid --log-format") {
		t.Fatalf("--log-format on lint does not reach the root logger validation; err = %v", err)
	}
}

// The build-log file format keeps its own flag under an unambiguous name.
func TestBuildLogFileFormatFlagRenamed(t *testing.T) {
	if newBuildCommand().Flags().Lookup("build-log-format") == nil {
		t.Error("build has no --build-log-format flag")
	}
	if newBuildCommand().Flags().Lookup("log-format") != nil {
		t.Error("build still registers a local --log-format, shadowing the root flag")
	}
	if newLintCommand().Flags().Lookup("lint-log-format") == nil {
		t.Error("lint has no --lint-log-format flag")
	}
	if newLintCommand().Flags().Lookup("log-format") != nil {
		t.Error("lint still registers a local --log-format, shadowing the root flag")
	}
}

// Regression: the spinner reported errors and warnings on stdout, so
// `bino build > file` hid render failures while `2>/dev/null` hid nothing.
func TestSpinnerErrorsAndWarningsGoToStderr(t *testing.T) {
	InitStyle(true)

	var out, errB bytes.Buffer
	s := NewSpinner(SpinnerConfig{Stdout: &out, Stderr: &errB, NoColor: true})
	s.Start("working")
	s.StopWithError("boom failure")
	if !strings.Contains(errB.String(), "boom failure") {
		t.Errorf("spinner error not on stderr; stdout=%q stderr=%q", out.String(), errB.String())
	}
	if strings.Contains(out.String(), "boom failure") {
		t.Errorf("spinner error leaked to stdout: %q", out.String())
	}

	out.Reset()
	errB.Reset()
	s2 := NewSpinner(SpinnerConfig{Stdout: &out, Stderr: &errB, NoColor: true})
	s2.Start("working")
	s2.StopWithWarning("careful now")
	if !strings.Contains(errB.String(), "careful now") {
		t.Errorf("spinner warning not on stderr; stdout=%q stderr=%q", out.String(), errB.String())
	}
}
