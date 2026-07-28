package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectWithEngine(t *testing.T, dir, engineVersion string) {
	t.Helper()
	pin := ""
	if engineVersion != "" {
		pin = "engine-version = \"" + engineVersion + "\"\n"
	}
	body := "report-id = \"test-compat\"\n" + pin
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCompatFinding_Unsupported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")
	finding, fatal := engineCompatFinding(dir, "v0.5.0")
	if !fatal {
		t.Fatal("expected fatal=true for unsupported pin")
	}
	if finding.RuleID != "engine-version-incompatible" {
		t.Errorf("RuleID = %q, want engine-version-incompatible", finding.RuleID)
	}
	if filepath.Base(finding.File) != "bino.toml" {
		t.Errorf("File = %q, want bino.toml", finding.File)
	}
	if finding.Line != 2 {
		t.Errorf("Line = %d, want 2", finding.Line)
	}
	if !strings.Contains(finding.Message, "v0.5.0") {
		t.Errorf("Message missing version: %s", finding.Message)
	}
}

func TestEngineCompatFinding_Supported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v1.0.0-alpha.19")
	_, fatal := engineCompatFinding(dir, "v1.0.0-alpha.19")
	if fatal {
		t.Error("expected fatal=false for supported pin")
	}
}

func TestEngineCompatFinding_NoPinNoCache(t *testing.T) {
	// Without a pin and (likely) no engine cached at the test runner's
	// HOME, the helper must skip silently rather than fail.
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeProjectWithEngine(t, dir, "")
	_, fatal := engineCompatFinding(dir, "")
	if fatal {
		t.Error("expected fatal=false when no engine is resolvable")
	}
}

func TestEngineCompatDiagnostic_Unsupported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")
	diag, ok := engineCompatDiagnostic(dir)
	if !ok {
		t.Fatal("expected diagnostic for unsupported pin")
	}
	if diag.Severity != "error" {
		t.Errorf("Severity = %q, want error", diag.Severity)
	}
	if diag.Code != "engine-version-incompatible" {
		t.Errorf("Code = %q, want engine-version-incompatible", diag.Code)
	}
	if filepath.Base(diag.File) != "bino.toml" {
		t.Errorf("File = %q, want bino.toml", diag.File)
	}
	if diag.Line != 2 {
		t.Errorf("Line = %d, want 2", diag.Line)
	}
}

func TestEngineCompatDiagnostic_Supported(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v1.0.0-alpha.19")
	_, ok := engineCompatDiagnostic(dir)
	if ok {
		t.Error("expected no diagnostic for supported pin")
	}
}

func TestRunLSPValidate_EmitsCompatDiagnostic(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")

	var buf bytes.Buffer
	if err := runLSPValidate(context.Background(), dir, false, &buf); err != nil {
		t.Fatalf("runLSPValidate: %v", err)
	}
	var result LSPValidateResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false")
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "engine-version-incompatible" {
			found = true
			if d.Severity != "error" {
				t.Errorf("Severity = %q, want error", d.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected an engine-version-incompatible diagnostic, got: %+v", result.Diagnostics)
	}
}

func TestLintCommand_FailsOnIncompatibleEngine(t *testing.T) {
	dir := t.TempDir()
	writeProjectWithEngine(t, dir, "v0.5.0")
	// A complete minimal manifest set so manifest loading succeeds and the
	// compat check is the only error surfaced.
	report := `apiVersion: bino.bi/v1alpha1
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
	page := `apiVersion: bino.bi/v1alpha1
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
	if err := os.WriteFile(filepath.Join(dir, "page.yaml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newLintCommand()
	cmd.SetArgs([]string{"--work-dir", dir, "--out-dir", filepath.Join(dir, "out")})
	cmd.SetContext(context.Background())
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected non-zero exit, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(err.Error(), "engine-version-incompatible") {
		t.Errorf("error message missing rule id: %v", err)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "engine-version-incompatible") {
		t.Errorf("output should mention rule id, got: %s", combined)
	}
	if !strings.Contains(combined, "bino.toml:2:1") {
		t.Errorf("output should include line:col, got: %s", combined)
	}
}
