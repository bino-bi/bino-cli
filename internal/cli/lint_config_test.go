package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
)

// Fixtures for the [lint] wiring tests. reportA4 pairs with pageXGA to raise
// exactly one artefact-layoutpage-required finding (a4 artefact, xga page);
// reportXGA pairs with pageGhostRef to raise exactly one
// missing-required-reference finding, which carries severity "error" natively.
const (
	lintCfgReportA4 = `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: r
spec:
  format: a4
  orientation: portrait
  language: en
  filename: out.pdf
  title: Sample
  layoutPages:
    - p
`
	lintCfgReportXGA = `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: r
spec:
  format: xga
  orientation: portrait
  language: en
  filename: out.pdf
  title: Sample
  layoutPages:
    - p
`
	lintCfgPage = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: p
spec:
  children:
    - kind: Text
      spec:
        value: hi
`
	lintCfgPageGhostRef = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: p
spec:
  children:
    - kind: Text
      spec:
        value: hi
    - kind: Text
      ref: ghost_text
`
	lintCfgBadSchema = `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: r
spec:
  filename: 5
  title: Sample
`
)

// writeLintProject writes bino.toml plus the given manifests into a fresh
// temp dir and returns it.
func writeLintProject(t *testing.T, toml string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runLintCommand executes `bino lint` on dir and returns the combined
// stdout+stderr and its error.
func runLintCommand(t *testing.T, dir string, extraArgs ...string) (string, error) {
	t.Helper()
	cmd := newLintCommand()
	args := append([]string{"--work-dir", dir, "--out-dir", filepath.Join(dir, "out")}, extraArgs...)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	return stdout.String() + stderr.String(), err
}

// TestLintCommand_NoLintTable: the baseline every other case is measured
// against — a project without a [lint] table reports its finding and exits 0.
func TestLintCommand_NoLintTable(t *testing.T) {
	dir := writeLintProject(t, "report-id = \"t\"\n", map[string]string{
		"report.yaml": lintCfgReportA4,
		"page.yaml":   lintCfgPage,
	})
	out, err := runLintCommand(t, dir)
	if err != nil {
		t.Fatalf("expected exit 0, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "Found 1 lint warning(s):") {
		t.Errorf("summary wording changed for a project without a [lint] table, got:\n%s", out)
	}
	if !strings.Contains(out, "[artefact-layoutpage-required] report.yaml #1 (metadata.name):") {
		t.Errorf("finding line changed for a project without a [lint] table, got:\n%s", out)
	}
}

// TestLintCommand_SeverityShownInReport: the printed body must agree with the
// exit code — a rule raised to "error" is counted and labeled as one, and a
// rule lowered to "info" is not counted as a warning.
func TestLintCommand_SeverityShownInReport(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		want    []string
		notWant []string
	}{
		{
			name: "raised to error",
			toml: "report-id = \"t\"\n\n[lint.severity]\nartefact-layoutpage-required = \"error\"\n",
			want: []string{
				"Found 1 error(s) lint finding(s):",
				"[error] [artefact-layoutpage-required] report.yaml #1 (metadata.name):",
			},
			notWant: []string{"lint warning(s)"},
		},
		{
			name: "lowered to info",
			toml: "report-id = \"t\"\n\n[lint.severity]\nartefact-layoutpage-required = \"info\"\n",
			want: []string{
				"Found 1 info lint finding(s):",
				"[info] [artefact-layoutpage-required] report.yaml #1 (metadata.name):",
			},
			notWant: []string{"lint warning(s)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeLintProject(t, tc.toml, map[string]string{
				"report.yaml": lintCfgReportA4,
				"page.yaml":   lintCfgPage,
			})
			out, _ := runLintCommand(t, dir)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(out, notWant) {
					t.Errorf("unexpected %q in:\n%s", notWant, out)
				}
			}
		})
	}
}

// TestLintCommand_SeverityOnFixedExitIDWarns: severity cannot move the exit
// code of a bundle that will not load, so the entry is rejected loudly rather
// than accepted and ignored.
func TestLintCommand_SeverityOnFixedExitIDWarns(t *testing.T) {
	dir := writeLintProject(t, "report-id = \"t\"\n\n[lint.severity]\nschema-validation = \"info\"\n",
		map[string]string{"report.yaml": lintCfgReportA4, "page.yaml": lintCfgPage})
	out, err := runLintCommand(t, dir)
	if err != nil {
		t.Fatalf("expected exit 0, got %v\n%s", err, out)
	}
	want := `bino.toml: severity is not configurable for "schema-validation"; use [lint] disable to silence it`
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}
}

// TestLintCommand_DisableSilencesRule: disable removes the finding from the
// report; the exit code stays 0.
func TestLintCommand_DisableSilencesRule(t *testing.T) {
	dir := writeLintProject(t, "report-id = \"t\"\n\n[lint]\ndisable = [\"artefact-layoutpage-required\"]\n", map[string]string{
		"report.yaml": lintCfgReportA4,
		"page.yaml":   lintCfgPage,
	})
	out, err := runLintCommand(t, dir)
	if err != nil {
		t.Fatalf("expected exit 0, got %v\n%s", err, out)
	}
	if strings.Contains(out, "artefact-layoutpage-required") {
		t.Errorf("disabled rule still reported:\n%s", out)
	}
	if !strings.Contains(out, "No lint warnings found") {
		t.Errorf("expected a clean report, got:\n%s", out)
	}
}

// TestLintCommand_DisableEngineCompat: the compat check's fatal exit exists
// only to escalate its finding, so disabling the rule removes both.
func TestLintCommand_DisableEngineCompat(t *testing.T) {
	dir := writeLintProject(t,
		"report-id = \"t\"\nengine-version = \"v0.5.0\"\n\n[lint]\ndisable = [\"engine-version-incompatible\"]\n",
		map[string]string{"report.yaml": lintCfgReportXGA, "page.yaml": lintCfgPage})
	out, err := runLintCommand(t, dir)
	if err != nil {
		t.Fatalf("expected exit 0, got %v\n%s", err, out)
	}
	if strings.Contains(out, "engine-version-incompatible") {
		t.Errorf("disabled compat rule still reported:\n%s", out)
	}
}

// TestLintCommand_DisableSchemaValidationStaysFatal: disable hides the
// finding lines, but lint must never claim success on a bundle it could not
// read — and the reported issue count stays the honest pre-filter one.
func TestLintCommand_DisableSchemaValidationStaysFatal(t *testing.T) {
	cases := []struct {
		name       string
		toml       string
		wantInText bool
	}{
		{
			name:       "control without a lint table",
			toml:       "report-id = \"t\"\n",
			wantInText: true,
		},
		{
			name:       "schema-validation disabled",
			toml:       "report-id = \"t\"\n\n[lint]\ndisable = [\"schema-validation\"]\n",
			wantInText: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeLintProject(t, tc.toml, map[string]string{"bad.yaml": lintCfgBadSchema})
			out, err := runLintCommand(t, dir)
			if err == nil {
				t.Fatalf("expected a non-zero exit, got nil\n%s", out)
			}
			if !strings.Contains(err.Error(), "schema validation failed: 1 issue(s)") {
				t.Errorf("error = %v, want the pre-filter issue count", err)
			}
			if got := strings.Contains(out, "[schema-validation]"); got != tc.wantInText {
				t.Errorf("finding lines present = %v, want %v; output:\n%s", got, tc.wantInText, out)
			}
		})
	}
}

// TestLintCommand_SeverityDrivesExitCode: only a severity the project set in
// bino.toml changes the exit code.
func TestLintCommand_SeverityDrivesExitCode(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		args    []string
		wantErr string // "" means exit 0
	}{
		{
			name:    "override to error fails without --fail-on-warnings",
			toml:    "report-id = \"t\"\n\n[lint.severity]\nartefact-layoutpage-required = \"error\"\n",
			wantErr: "lint found 1 error(s)",
		},
		{
			name: "override to warning does not fail on its own",
			toml: "report-id = \"t\"\n\n[lint.severity]\nartefact-layoutpage-required = \"warning\"\n",
		},
		{
			name:    "no override still fails with --fail-on-warnings",
			toml:    "report-id = \"t\"\n",
			args:    []string{"--fail-on-warnings"},
			wantErr: "lint found 1 warning(s)",
		},
		{
			name: "override to info is exempt from --fail-on-warnings",
			toml: "report-id = \"t\"\n\n[lint.severity]\nartefact-layoutpage-required = \"info\"\n",
			args: []string{"--fail-on-warnings"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeLintProject(t, tc.toml, map[string]string{
				"report.yaml": lintCfgReportA4,
				"page.yaml":   lintCfgPage,
			})
			out, err := runLintCommand(t, dir, tc.args...)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected exit 0, got %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected a non-zero exit, got nil\n%s", out)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestLintCommand_NativeErrorSeverityStaysAdvisory: missing-required-reference
// carries severity "error" in code. Without a [lint] override it must not
// change the exit code, exactly as before.
func TestLintCommand_NativeErrorSeverityStaysAdvisory(t *testing.T) {
	dir := writeLintProject(t, "report-id = \"t\"\n", map[string]string{
		"report.yaml": lintCfgReportXGA,
		"page.yaml":   lintCfgPageGhostRef,
	})
	out, err := runLintCommand(t, dir)
	if err != nil {
		t.Fatalf("expected exit 0, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "missing-required-reference") {
		t.Fatalf("expected the finding to be reported, got:\n%s", out)
	}
}

// TestLintCommand_UnknownRuleIDWarns: an unknown id is reported and silences
// nothing.
func TestLintCommand_UnknownRuleIDWarns(t *testing.T) {
	dir := writeLintProject(t, "report-id = \"t\"\n\n[lint]\ndisable = [\"no-such-rule\"]\n", map[string]string{
		"report.yaml": lintCfgReportA4,
		"page.yaml":   lintCfgPage,
	})
	out, err := runLintCommand(t, dir)
	if err != nil {
		t.Fatalf("expected exit 0, got %v\n%s", err, out)
	}
	if !strings.Contains(out, `bino.toml: unknown rule id "no-such-rule" in [lint] disable`) {
		t.Errorf("missing the config warning, got:\n%s", out)
	}
	if !strings.Contains(out, "artefact-layoutpage-required") {
		t.Errorf("real finding was lost, got:\n%s", out)
	}
}

// TestLoadBuildManifests_AppliesLintConfig: `bino build` prints its lint
// report through the same [lint] filter as `bino lint`, so a rule the project
// disabled does not reappear in a build log.
func TestLoadBuildManifests_AppliesLintConfig(t *testing.T) {
	cases := []struct {
		name     string
		toml     string
		reported bool
	}{
		{
			name:     "no lint table",
			toml:     "report-id = \"t\"\n",
			reported: true,
		},
		{
			name:     "rule disabled",
			toml:     "report-id = \"t\"\n\n[lint]\ndisable = [\"artefact-layoutpage-required\"]\n",
			reported: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeLintProject(t, tc.toml, map[string]string{
				"report.yaml": lintCfgReportA4,
				"page.yaml":   lintCfgPage,
			})
			var buf bytes.Buffer
			out := NewOutput(OutputConfig{Stdout: &buf, Stderr: &buf, NoColor: true})
			if _, err := loadBuildManifests(context.Background(), out, logx.Nop(), dir, nil, nil, false, true, nil, nil); err != nil {
				t.Fatalf("loadBuildManifests: %v\n%s", err, buf.String())
			}
			if got := strings.Contains(buf.String(), "[artefact-layoutpage-required]"); got != tc.reported {
				t.Errorf("finding reported = %v, want %v; output:\n%s", got, tc.reported, buf.String())
			}
		})
	}
}

// TestLoadBuildManifests_LintCannotHideLoadFailure is the boundary: [lint]
// governs the lint report, never what `bino build` accepts.
func TestLoadBuildManifests_LintCannotHideLoadFailure(t *testing.T) {
	dir := writeLintProject(t,
		"report-id = \"t\"\n\n[lint]\ndisable = [\"schema-validation\", \"manifest-load\"]\n",
		map[string]string{"bad.yaml": lintCfgBadSchema})

	out := NewOutput(OutputConfig{Stdout: io.Discard, Stderr: io.Discard, NoColor: true})
	_, err := loadBuildManifests(context.Background(), out, logx.Nop(), dir, nil, nil, false, true, nil, nil)
	if err == nil {
		t.Fatal("expected loadBuildManifests to reject a schema-invalid bundle")
	}
}
