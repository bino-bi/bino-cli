//go:build integration

package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
)

// writePreviewPDFFixture builds a project with a report and a TOC document.
// Both read a dataset whose query fails, and the report asks for an absolute
// output file, which the preview PDF must not write.
func writePreviewPDFFixture(t *testing.T, absPDF string) string {
	t.Helper()
	workdir := t.TempDir()
	if err := os.CopyFS(workdir, os.DirFS(filepath.Join("..", "report", "pipeline", "testdata", "sample-bundle"))); err != nil {
		t.Fatalf("copy sample bundle: %v", err)
	}
	files := map[string]string{
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: sample-report
spec:
  format: xga
  orientation: landscape
  language: en
  filename: ` + absPDF + `
  title: "Sample Report"
  author: "Ops Team"
  layoutPages:
    - sample-page
    - broken-page
`,
		"broken.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: broken_ds
spec:
  query: SELECT * FROM table_that_does_not_exist
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: broken-page
spec:
  pageLayout: full
  children:
    - kind: ChartStructure
      metadata:
        name: broken_chart
      spec:
        dataset: broken_ds
        scenarios:
          - ac1
        level: category
`,
		"doc.yaml": `apiVersion: bino.bi/v1alpha1
kind: DocumentArtefact
metadata:
  name: handbook
spec:
  format: a4
  locale: en
  title: Handbook
  author: Ops Team
  tableOfContents: true
  displayHeaderFooter: true
  sources:
    - docs/handbook.md
---
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: doc_broken_chart
spec:
  dataset: broken_ds
  scenarios:
    - ac1
  level: category
`,
		"docs/handbook.md": "# Handbook\n\nIntro.\n\n## Data\n\n:ref[ChartStructure:doc_broken_chart]\n",
	}
	for name, content := range files {
		path := filepath.Join(workdir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return workdir
}

// unstampedPages returns the pages of the PDF that carry no pdfcpu stamp.
func unstampedPages(t *testing.T, pdfBytes []byte) (pages int, unstamped []int) {
	t.Helper()
	err := api.ExtractContent(bytes.NewReader(pdfBytes), nil, func(r io.Reader, pageNr int) error {
		content, rerr := io.ReadAll(r)
		if rerr != nil {
			return rerr
		}
		pages++
		if !bytes.Contains(content, []byte("/Artifact <</Subtype /Watermark")) {
			unstamped = append(unstamped, pageNr)
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("extract content: %v", err)
	}
	return pages, unstamped
}

func TestPreviewPDF_BuildsWatermarkedPDFs(t *testing.T) {
	requireRenderStack(t)
	ctx := context.Background()
	engineMgr, err := engine.NewManager()
	if err != nil {
		t.Fatalf("engine manager: %v", err)
	}
	engineInfo, err := engineMgr.EnsureVersion(ctx, "")
	if err != nil {
		t.Fatalf("engine version: %v", err)
	}
	cacheDir, err := pathutil.CacheDir("cdn")
	if err != nil {
		t.Fatalf("cache dir: %v", err)
	}

	absPDF := filepath.Join(t.TempDir(), "must-not-exist.pdf")
	h := &previewPDFHandler{
		base:   ctx,
		mu:     &sync.Mutex{},
		logger: logx.Nop(),
		builder: pipeline.Builder{
			Workdir:       writePreviewPDFFixture(t, absPDF),
			EngineVersion: engineInfo.Version,
			CacheDir:      cacheDir,
		},
	}

	for query, title := range map[string]string{"?kind=ReportArtefact&name=sample-report": "Sample Report", "?kind=DocumentArtefact&name=handbook": "Handbook"} {
		t.Run(query, func(t *testing.T) {
			status, resp := postPDF(ctx, t, h, query, nil)
			if status != http.StatusOK {
				t.Fatalf("status = %d, error = %q", status, resp.Error)
			}
			if !bytes.HasPrefix(resp.PDF, []byte("%PDF")) {
				t.Fatalf("body is not a PDF")
			}
			pages, unstamped := unstampedPages(t, resp.PDF)
			if pages == 0 || len(unstamped) > 0 {
				t.Errorf("pages = %d, pages without watermark = %v", pages, unstamped)
			}
			info := pdfInfo(t, resp.PDF)
			if info["Title"] != title || info["Author"] != "Ops Team" {
				t.Errorf("Title, Author = %q, %q; want %q, %q", info["Title"], info["Author"], title, "Ops Team")
			}
			if !strings.Contains(strings.Join(resp.Warnings, "\n"), "broken_ds") {
				t.Errorf("warnings do not name the failing dataset: %q", resp.Warnings)
			}
		})
	}

	if _, err := os.Stat(absPDF); !os.IsNotExist(err) {
		t.Errorf("the preview PDF wrote the artefact's own output file %s", absPDF)
	}
}

// failingPluginOptions stand in for a component plugin whose draw rejects.
func failingPluginOptions() *render.PluginOptions {
	return &render.PluginOptions{
		HasComponentKinds: true,
		ExtraHeadMarkup:   `<script>window.binoPlugin?.track(document.createElement("demo-chart"), Promise.reject(new Error("demo boom")));</script>`,
	}
}

const demoPluginFailure = "plugin component demo-chart failed to render: demo boom"

func TestPreviewPDF_PluginFailureIsAWarning(t *testing.T) {
	ctx := context.Background()
	engineVersion, cacheDir, _ := renderStack(t)
	h := &previewPDFHandler{
		base:   ctx,
		mu:     &sync.Mutex{},
		logger: logx.Nop(),
		builder: pipeline.Builder{
			Workdir:       writePreviewPDFFixture(t, filepath.Join(t.TempDir(), "report.pdf")),
			EngineVersion: engineVersion,
			CacheDir:      cacheDir,
			PluginOptions: failingPluginOptions(),
		},
	}
	for _, query := range []string{"?kind=ReportArtefact&name=sample-report", "?kind=DocumentArtefact&name=handbook"} {
		t.Run(query, func(t *testing.T) {
			status, resp := postPDF(ctx, t, h, query, nil)
			if status != http.StatusOK {
				t.Fatalf("status = %d, error = %q", status, resp.Error)
			}
			if !bytes.HasPrefix(resp.PDF, []byte("%PDF")) {
				t.Fatalf("body is not a PDF")
			}
			if !slices.Contains(resp.Warnings, demoPluginFailure) {
				t.Errorf("warnings = %q, want %q", resp.Warnings, demoPluginFailure)
			}
		})
	}
}

func TestBuild_PluginFailureFailsThePDF(t *testing.T) {
	ctx := context.Background()
	engineVersion, cacheDir, chromePath := renderStack(t)
	workdir := writePreviewPDFFixture(t, filepath.Join(t.TempDir(), "report.pdf"))
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	builder := &pipeline.Builder{
		Workdir:              workdir,
		EngineVersion:        engineVersion,
		CacheDir:             cacheDir,
		PluginOptions:        failingPluginOptions(),
		ContinueOnQueryError: true,
	}
	reports, err := config.CollectArtefacts(docs)
	if err != nil || len(reports) != 1 {
		t.Fatalf("report artefacts = %v, %v", reports, err)
	}
	documents, err := config.CollectDocumentArtefacts(docs)
	if err != nil || len(documents) != 1 {
		t.Fatalf("document artefacts = %v, %v", documents, err)
	}

	t.Run("report", func(t *testing.T) {
		_, err := buildArtefact(ctx, buildArtefactConfig{
			Builder: builder, Docs: docs, Artifact: reports[0], OutputDir: t.TempDir(), ChromePath: chromePath,
		})
		if err == nil || !strings.Contains(err.Error(), demoPluginFailure) {
			t.Fatalf("err = %v, want %q", err, demoPluginFailure)
		}
	})
	t.Run("document", func(t *testing.T) {
		_, err := buildDocumentArtefact(ctx, buildDocumentArtefactConfig{
			Builder: builder, Logger: logx.Nop(), Docs: docs, Artifact: documents[0], OutputDir: t.TempDir(), ChromePath: chromePath,
		})
		if err == nil || !strings.Contains(err.Error(), demoPluginFailure) {
			t.Fatalf("err = %v, want %q", err, demoPluginFailure)
		}
	})
}
