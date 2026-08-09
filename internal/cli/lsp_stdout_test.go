package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const optionalRefPage = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: main_page
spec:
  children:
    - kind: Text
      ref: ghost_text
      optional: true
`

// runLSPHelperGraphDeps executes `bino lsp-helper graph-deps` through the real
// root command (so the root PersistentPreRunE binds the logger exactly as in
// production) against a bundle whose optional layout ref is missing — the
// case that makes graph.Build log a skip message.
func runLSPHelperGraphDeps(t *testing.T, extraArgs ...string) bytes.Buffer {
	t.Helper()
	t.Setenv("CI", "1") // suppress the background update check

	dir := t.TempDir()
	writeProjectConfig(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "page.yaml"), []byte(optionalRefPage), 0o644); err != nil {
		t.Fatalf("write page.yaml: %v", err)
	}

	var stdout, stderr bytes.Buffer
	root := newRootCommand()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"lsp-helper", "graph-deps", dir, "--kind", "LayoutPage", "--name", "main_page"}, extraArgs...))
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute lsp-helper graph-deps: %v", err)
	}
	return stdout
}

// Regression: lsp-helper subcommands emit machine-consumed JSON on stdout,
// but the root command bound an stdout-writing logger into the context — a
// bundle with an optional missing layout ref made graph.Build log an Info
// line into the JSON stream, and the extension's JSON.parse of the whole
// stdout failed.
func TestLSPHelperGraphDepsEmitsPureJSON(t *testing.T) {
	stdout := runLSPHelperGraphDeps(t)
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("lsp-helper stdout is not pure JSON: %v\nstdout:\n%s", err, stdout.String())
	}
}

// The stdout contract must hold under --verbose too: Debug chatter belongs on
// stderr for every lsp-helper subcommand.
func TestLSPHelperStdoutStaysPureUnderVerbose(t *testing.T) {
	stdout := runLSPHelperGraphDeps(t, "--verbose")
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("lsp-helper stdout is not pure JSON under --verbose: %v\nstdout:\n%s", err, stdout.String())
	}
}
