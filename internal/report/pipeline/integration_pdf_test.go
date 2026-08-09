//go:build integration

package pipeline

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/pdf"
)

// TestIntegration_BuildSampleBundlePDF renders the sample bundle to PDF via
// Chrome headless shell and verifies the output, including byte-reproducible
// output under SOURCE_DATE_EPOCH.
//
// This file is gated behind the `integration` build tag: a missing Chrome is a
// hard failure here, not a skip — the integration CI job installs it via
// `bino setup`, and anyone running `go test -tags=integration` locally is
// expected to have done the same.
func TestIntegration_BuildSampleBundlePDF(t *testing.T) {
	mgr, err := chrome.NewManager()
	if err != nil {
		t.Fatalf("chrome manager unavailable: %v", err)
	}
	chromePath, err := mgr.ResolveExecPath()
	if err != nil {
		t.Fatalf("chrome-headless-shell not installed (run 'bino setup' or set CHROME_PATH): %v", err)
	}

	ctx := context.Background()
	workdir := copySampleBundle(t)

	loadResult, err := LoadManifests(ctx, workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}

	builder := &Builder{
		Workdir:       workdir,
		EngineVersion: "v1.0.0",
		CacheDir:      t.TempDir(),
	}

	renderResult, err := builder.RenderArtefactHTML(ctx, loadResult.Documents, loadResult.Artifacts[0])
	if err != nil {
		t.Fatalf("render artefact html: %v", err)
	}

	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")

	renderPDF := func(t *testing.T, path string) []byte {
		t.Helper()
		err := builder.RenderPDFWithData(ctx, renderResult.HTML, renderResult.LocalAssets, renderResult.EmittedData, PDFRenderOptions{
			PDFPath:     path,
			ChromePath:  chromePath,
			Format:      loadResult.Artifacts[0].Spec.Format,
			Orientation: loadResult.Artifacts[0].Spec.Orientation,
		})
		if err != nil {
			t.Fatalf("render pdf: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read pdf: %v", err)
		}
		return data
	}

	outDir := t.TempDir()
	pdfPathA := filepath.Join(outDir, "a.pdf")
	dataA := renderPDF(t, pdfPathA)

	if len(dataA) == 0 || !bytes.HasPrefix(dataA, []byte("%PDF-")) {
		t.Fatal("output is not a PDF")
	}
	pages, err := pdf.PageCount(pdfPathA)
	if err != nil {
		t.Fatalf("page count: %v", err)
	}
	if pages < 1 {
		t.Fatalf("expected at least 1 page, got %d", pages)
	}

	// Reproducibility: a second render of the same HTML with the same
	// SOURCE_DATE_EPOCH must produce byte-identical output.
	dataB := renderPDF(t, filepath.Join(outDir, "b.pdf"))
	if !bytes.Equal(dataA, dataB) {
		t.Fatal("two renders with SOURCE_DATE_EPOCH set are not byte-identical")
	}

	// Normalized timestamps must reflect SOURCE_DATE_EPOCH (2023-11-14).
	if !bytes.Contains(dataA, []byte("D:20231114")) {
		t.Fatal("PDF timestamps were not normalized to SOURCE_DATE_EPOCH")
	}
}
