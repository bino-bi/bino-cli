package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
)

// writeParityFixture writes a bundle that exercises every diagnostic surface:
// a syntax-broken file, a schema-invalid document, a missing ${VAR}, and a
// lint finding with error severity (dangling required ref). No DataSource
// documents, so the daemon state runs without a DuckDB session.
func writeParityFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"bino.toml": "report-id = \"parity\"\n",
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: report
spec:
  filename: report.pdf
  title: Report
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page_one
spec:
  children:
    - kind: Text
      spec:
        value: hello
    - kind: Text
      ref: ghost_text
`,
		"schema_bad.yaml": `apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: bad_text
spec:
  value: hi
  bogus: true
`,
		"env.yaml": `apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: env_text
spec:
  value: ${BINO_PARITY_UNSET_XYZ}
`,
		"broken.yaml": "kind: Table\nspec: [\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDaemonAndHelperValidationParity is the drift guard for DUP-02/QUAL-02:
// the daemon's validation pipeline (State.Refresh → validateDocs) and
// lsp-helper's validateDirectory must produce identical diagnostics for the
// same bundle. Both are thin diagnostics.Collect callers now, so any future
// divergence — kind providers, plugin linters, engine compat, severity
// mapping, nil-vs-[] — fails this test.
func TestDaemonAndHelperValidationParity(t *testing.T) {
	// Isolate the engine cache: engineCompatDiagnostic consults
	// $HOME/.bino, and a locally cached engine would make the fixture's
	// diagnostics machine-dependent.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	writeParityFixture(t, dir)
	ctx := context.Background()

	st, err := daemon.NewState(dir, nil, logx.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetEngineCompat(engineCompatDiagnostic)
	if err := st.Refresh(ctx); err != nil {
		t.Fatalf("daemon refresh: %v", err)
	}
	daemonDiags := st.Diagnostics()

	helperDiags := validateDirectory(ctx, dir, false)

	if !reflect.DeepEqual(daemonDiags, helperDiags) {
		t.Errorf("daemon and lsp-helper diagnostics diverge:\ndaemon: %+v\nhelper: %+v", daemonDiags, helperDiags)
	}

	// Guard against a vacuous pass: the fixture must actually exercise the
	// yaml-syntax, schema, env-var, and severity-preserving lint surfaces.
	wantCodes := []string{"yaml-syntax", "schema-validation", "missing-env-var", "missing-required-reference"}
	seen := make(map[string]bool, len(daemonDiags))
	for _, d := range daemonDiags {
		seen[d.Code] = true
	}
	for _, code := range wantCodes {
		if !seen[code] {
			t.Errorf("fixture did not produce a %q diagnostic; got %+v", code, daemonDiags)
		}
	}
}

// TestRunLSPValidate_CleanProjectSerializesEmptyArray: a clean bundle must
// serialize diagnostics as [] — validateDirectory used to return nil, which
// outputJSON rendered as JSON null (unlike the daemon's []).
func TestRunLSPValidate_CleanProjectSerializesEmptyArray(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("report-id = \"clean\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: r
spec:
  filename: out.pdf
  title: Sample
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: p
spec:
  children:
    - kind: Text
      spec:
        value: hi
`
	if err := os.WriteFile(filepath.Join(dir, "report.yaml"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runLSPValidate(context.Background(), dir, false, &buf); err != nil {
		t.Fatalf("runLSPValidate: %v", err)
	}
	var result struct {
		Valid       bool            `json:"valid"`
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.Valid {
		t.Errorf("Valid = false, want true; output: %s", buf.String())
	}
	if got := strings.TrimSpace(string(result.Diagnostics)); got != "[]" {
		t.Errorf("diagnostics serialized as %s, want []", got)
	}
}
