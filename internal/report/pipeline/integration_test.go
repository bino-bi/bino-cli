package pipeline

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/pdf"
)

// copySampleBundle copies testdata/sample-bundle into a temp dir so the build
// (dataset cache under .bino/) never writes into the repository tree.
func copySampleBundle(t *testing.T) string {
	t.Helper()

	srcDir := filepath.Join("testdata", "sample-bundle")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read sample bundle: %v", err)
	}

	workdir := t.TempDir()
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(srcDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(workdir, entry.Name()), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", entry.Name(), err)
		}
	}
	return workdir
}

// decodeInlinePayload decodes a "hash:base64(gzip(data))" inline payload from
// a bn-dataset/bn-datasource element body (see render.CompressContent).
func decodeInlinePayload(t *testing.T, payload string) string {
	t.Helper()

	_, encoded, found := strings.Cut(strings.TrimSpace(payload), ":")
	if !found {
		t.Fatalf("payload has no hash prefix: %q", payload)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode payload: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip open payload: %v", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip read payload: %v", err)
	}
	return string(decoded)
}

// TestIntegration_RenderSampleBundleHTML runs the full manifest-to-HTML
// pipeline on the sample bundle: load + validate, execute datasets against
// DuckDB, and render artefact HTML with the dataset payload embedded.
func TestIntegration_RenderSampleBundleHTML(t *testing.T) {
	ctx := context.Background()
	workdir := copySampleBundle(t)

	loadResult, err := LoadManifests(ctx, workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	if len(loadResult.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(loadResult.Artifacts))
	}
	artifact := loadResult.Artifacts[0]
	if artifact.Document.Name != "sample-report" {
		t.Fatalf("unexpected artifact name: %s", artifact.Document.Name)
	}

	result, err := RenderArtefactHTML(ctx, workdir, loadResult.Documents, artifact, RenderArtefactOptions{
		EngineVersion: "v1.0.0",
	})
	if err != nil {
		t.Fatalf("render artefact html: %v", err)
	}
	for _, diag := range result.Diagnostics {
		t.Errorf("unexpected diagnostic: %s/%s: %v", diag.Stage, diag.Datasource, diag.Err)
	}

	html := string(result.HTML)
	if !strings.Contains(html, "<bn-context") {
		t.Fatal("rendered HTML is missing <bn-context>")
	}
	if !strings.Contains(html, "<bn-chart-structure") || !strings.Contains(html, "datasets='revenue_by_region'") {
		t.Fatal("rendered HTML is missing the chart component bound to the dataset")
	}

	// Extract and decode the inline dataset payload, then verify the data
	// actually flowed from the inline datasource through DuckDB into the HTML.
	datasetRe := regexp.MustCompile(`<bn-dataset[^>]*name='revenue_by_region'[^>]*>([^<]*)</bn-dataset>`)
	m := datasetRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("rendered HTML is missing the revenue_by_region dataset element")
	}
	payload := decodeInlinePayload(t, m[1])
	for _, want := range []string{"DACH", "4250", "Nordics", "2870"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("dataset payload missing %q:\n%s", want, payload)
		}
	}
}

// TestIntegration_BuildSampleBundlePDF renders the sample bundle to PDF via
// Chrome headless shell and verifies the output, including byte-reproducible
// output under SOURCE_DATE_EPOCH. Skipped when Chrome is not installed.
func TestIntegration_BuildSampleBundlePDF(t *testing.T) {
	mgr, err := chrome.NewManager()
	if err != nil {
		t.Skipf("chrome manager unavailable: %v", err)
	}
	chromePath, err := mgr.ResolveExecPath()
	if err != nil {
		t.Skipf("chrome-headless-shell not installed: %v", err)
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
