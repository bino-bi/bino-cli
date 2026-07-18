package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/engine"
)

// A self-contained report YAML: inline DataSource, DataSet, LayoutPage, and a
// ReportArtefact (PDF) plus a ScreenshotArtefact (PNG), all in one document.
const statelessSampleYAML = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: revenue_data
spec:
  type: inline
  content:
    - category: "DACH"
      categoryIndex: 1
      operation: "+"
      ac1: 4250
      pp1: 3800
    - category: "Nordics"
      categoryIndex: 2
      operation: "+"
      ac1: 2870
      pp1: 2650
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: revenue_by_region
spec:
  query: SELECT * FROM revenue_data ORDER BY categoryIndex
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: sample-page
spec:
  pageLayout: 2x2
  titleBusinessUnit: "Test Corp"
  titleMeasures:
    - name: "Revenue"
      unit: "EUR k"
  titleScenarios: "AC, PY"
  footerText: "Test Corp"
  children:
    - kind: ChartStructure
      metadata:
        name: revenue_chart
      spec:
        dataset: revenue_by_region
        scenarios:
          - ac1
          - pp1
        variances:
          - dac1_pp1_pos
        level: category
        order: ac1
        orderDirection: desc
        unitScaling: 0.001
        chartTitle: "Revenue by Region"
---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: sample-report
spec:
  format: xga
  orientation: landscape
  language: en
  filename: sample-report.pdf
  title: "Test Corp"
  layoutPages:
    - sample-page
---
apiVersion: bino.bi/v1alpha1
kind: ScreenshotArtefact
metadata:
  name: sample-shot
spec:
  format: xga
  orientation: landscape
  language: en
  filenamePrefix: shot
  refs:
    - kind: ChartStructure
      name: revenue_chart
`

// requireRenderStack skips the test when Chrome or the template engine are not
// available (e.g. CI without `bino setup`). Stateless rendering needs both.
func requireRenderStack(t *testing.T) {
	t.Helper()
	mgr, err := chrome.NewManager()
	if err != nil {
		t.Skipf("chrome manager unavailable: %v", err)
	}
	if _, err := mgr.ResolveExecPath(); err != nil {
		t.Skipf("chrome-headless-shell not installed: %v", err)
	}
	em, err := engine.NewManager()
	if err != nil {
		t.Skipf("engine manager unavailable: %v", err)
	}
	if _, err := em.EnsureVersion(context.Background(), ""); err != nil {
		t.Skipf("template engine unavailable: %v", err)
	}
}

// runStatelessForTest drives runStatelessBuild in-process and returns the bytes
// written to stdout along with any structured error.
func runStatelessForTest(t *testing.T, format, inputArg, stdin string) ([]byte, *statelessError) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(stdin))
	serr := runStatelessBuild(cmd, format, inputArg)
	return out.Bytes(), serr
}

func TestStatelessBuild_PDFFromStdin(t *testing.T) {
	requireRenderStack(t)
	out, serr := runStatelessForTest(t, "pdf", "-", statelessSampleYAML)
	if serr != nil {
		t.Fatalf("unexpected error: %v", serr)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatalf("stdout is not a PDF (len=%d, prefix=%q)", len(out), firstBytes(out))
	}
}

func TestStatelessBuild_PDFFromFile(t *testing.T) {
	requireRenderStack(t)
	path := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(path, []byte(statelessSampleYAML), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	out, serr := runStatelessForTest(t, "pdf", path, "")
	if serr != nil {
		t.Fatalf("unexpected error: %v", serr)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatalf("stdout is not a PDF (len=%d, prefix=%q)", len(out), firstBytes(out))
	}
}

func TestStatelessBuild_PNGFromStdin(t *testing.T) {
	requireRenderStack(t)
	out, serr := runStatelessForTest(t, "png", "-", statelessSampleYAML)
	if serr != nil {
		t.Fatalf("unexpected error: %v", serr)
	}
	if !bytes.HasPrefix(out, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("stdout is not a PNG (len=%d, prefix=%q)", len(out), firstBytes(out))
	}
}

func TestStatelessBuild_InvalidYAML(t *testing.T) {
	requireRenderStack(t)
	out, serr := runStatelessForTest(t, "pdf", "-", "this: is: not: valid: [\n")
	if serr == nil {
		t.Fatal("expected an error for invalid YAML")
	}
	if serr.Code != statelessErrInvalidYAML {
		t.Fatalf("expected code %q, got %q (%s)", statelessErrInvalidYAML, serr.Code, serr.Message)
	}
	if len(out) != 0 {
		t.Fatalf("expected no bytes on stdout, got %d", len(out))
	}
}

func TestStatelessBuild_BadFormat(t *testing.T) {
	requireRenderStack(t)
	_, serr := runStatelessForTest(t, "gif", "-", statelessSampleYAML)
	if serr == nil {
		t.Fatal("expected an error for bad format")
	}
	if serr.Code != statelessErrInvalidInput {
		t.Fatalf("expected code %q, got %q (%s)", statelessErrInvalidInput, serr.Code, serr.Message)
	}
}

func firstBytes(b []byte) string {
	if len(b) > 8 {
		b = b[:8]
	}
	return string(b)
}
