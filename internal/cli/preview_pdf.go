package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"sync"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pdf"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/pipeline"
)

const (
	previewPDFKindReport   = "ReportArtefact"
	previewPDFKindDocument = "DocumentArtefact"
	// previewPDFFilename replaces spec.filename, which may be absolute or
	// derived from a scoped name with slashes.
	previewPDFFilename  = "preview.pdf"
	previewPDFWatermark = "PREVIEW"
)

// previewPDFResponse is the JSON body of POST /__preview/pdf.
type previewPDFResponse struct {
	PDF      []byte   `json:"pdf,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// previewPDFHandler builds one artefact to a watermarked PDF for the preview
// UI. It runs the same per-artefact code as `bino build`, without hooks,
// signing and graph output, and with query errors downgraded to warnings.
type previewPDFHandler struct {
	// base is the command context; server shutdown does not cancel request contexts.
	base context.Context
	// mu is the refresh mutex. A build removes the inline-datasource dir a
	// running refresh reads, so the two must not overlap.
	mu      *sync.Mutex
	logger  logx.Logger
	builder pipeline.Builder
	kinds   config.KindProvider
	filter  pipeline.FilterOptions
	// chromePath is the bino.toml build setting; empty means auto-detect.
	chromePath string
	// render builds the PDF into outDir and returns its path. Nil means renderArtefact.
	render func(ctx context.Context, kind, name, outDir string) (string, error)
}

func (h *previewPDFHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The route starts Chrome, so another site must not be able to trigger it.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writePDFResponse(w, http.StatusForbidden, previewPDFResponse{Error: "cross-site requests are not allowed"})
		return
	}
	kind, name := r.URL.Query().Get("kind"), r.URL.Query().Get("name")
	if (kind != previewPDFKindReport && kind != previewPDFKindDocument) || name == "" {
		writePDFResponse(w, http.StatusBadRequest, previewPDFResponse{Error: "kind must be ReportArtefact or DocumentArtefact, and name must be set"})
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(h.base, cancel)
	defer stop()

	status, resp := h.build(ctx, kind, name)
	writePDFResponse(w, status, resp)
}

// build holds the refresh mutex until the PDF is in memory. The response is
// written after it returns, so a slow client cannot block live reload.
func (h *previewPDFHandler) build(ctx context.Context, kind, name string) (int, previewPDFResponse) {
	h.mu.Lock()
	defer h.mu.Unlock()
	canceled := previewPDFResponse{Error: "the PDF build was canceled"}
	if ctx.Err() != nil {
		return http.StatusServiceUnavailable, canceled
	}

	capture := warnCapture{Logger: h.logger, sink: &warnSink{}}
	ctx = logx.WithLogger(ctx, capture)

	outDir, err := os.MkdirTemp("", "bino-preview-pdf-*")
	if err != nil {
		return http.StatusInternalServerError, previewPDFResponse{Error: err.Error()}
	}
	defer os.RemoveAll(outDir)

	render := h.render
	if render == nil {
		render = h.renderArtefact
	}
	pdfPath, err := render(ctx, kind, name, outDir)
	if ctx.Err() != nil {
		return http.StatusServiceUnavailable, canceled
	}
	if err == nil {
		// A PDF without the watermark is never served.
		err = pdf.StampWatermark(pdfPath, previewPDFWatermark)
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(pdfPath)
	}
	if err != nil {
		status := http.StatusInternalServerError
		var httpErr *httpserver.HTTPError
		if errors.As(err, &httpErr) {
			status = httpErr.Code
		} else {
			h.logger.Errorf("Preview PDF failed: %v", err)
		}
		return status, previewPDFResponse{Error: previewPDFErrorText(err), Warnings: capture.sink.snapshot()}
	}
	return http.StatusOK, previewPDFResponse{PDF: data, Warnings: capture.sink.snapshot()}
}

// renderArtefact loads the manifests from disk and builds the named artefact
// into outDir with the build command's per-artefact functions.
func (h *previewPDFHandler) renderArtefact(ctx context.Context, kind, name, outDir string) (string, error) {
	logger := logx.FromContext(ctx)
	docs, err := config.LoadDirWithOptions(ctx, h.builder.Workdir, config.LoadOptions{KindProvider: h.kinds})
	if err != nil {
		return "", err
	}
	// The build command fails on these; here they are warnings.
	for _, m := range config.CollectMissingEnvVarsExcluding(docs, config.CollectLayoutPageParamNames(docs)) {
		logger.Warnf("unresolved environment variable %s in %s", m.VarName, m.File)
	}

	b := h.builder
	b.Logger = logger
	b.ContinueOnQueryError = true
	if b.DataValidation == dataset.DataValidationFail {
		b.DataValidation = dataset.DataValidationWarn
	}
	chromePath := resolveChromePath(h.chromePath)

	if kind == previewPDFKindDocument {
		arts, err := config.CollectDocumentArtefacts(docs)
		if err != nil {
			return "", err
		}
		for _, art := range pipeline.FilterDocumentArtefacts(arts, h.filter) {
			if art.Document.Name != name {
				continue
			}
			art.Spec.Filename = previewPDFFilename
			art.Spec.SigningProfile = ""
			logger.Infof("Building preview PDF for document %s", art.Document.Name)
			res, err := buildDocumentArtefact(ctx, buildDocumentArtefactConfig{
				Builder: &b, Logger: logger, Docs: docs, Artifact: art, OutputDir: outDir, ChromePath: chromePath,
			})
			return res.PDFPath, err
		}
	} else {
		arts, err := config.CollectArtefacts(docs)
		if err != nil {
			return "", err
		}
		for _, art := range pipeline.FilterArtefacts(arts, h.filter) {
			if art.Document.Name != name {
				continue
			}
			art.Spec.Filename = previewPDFFilename
			art.Spec.SigningProfile = ""
			logger.Infof("Building preview PDF for report %s", art.Document.Name)
			res, err := buildArtefact(ctx, buildArtefactConfig{
				Builder: &b, Logger: logger, Docs: docs, Artifact: art, OutputDir: outDir, ChromePath: chromePath,
			})
			return res.PDFPath, err
		}
	}
	return "", httpserver.NewHTTPError(http.StatusNotFound, fmt.Sprintf("no %s named %q", kind, name))
}

// previewPDFErrorText turns the two failures a user can act on into plain text.
func previewPDFErrorText(err error) string {
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return "Chrome was not found. Run `bino setup` or set CHROME_PATH."
	case errors.Is(err, context.DeadlineExceeded):
		return "Rendering timed out: " + err.Error()
	}
	return err.Error()
}

func writePDFResponse(w http.ResponseWriter, status int, resp previewPDFResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp) //nolint:errcheck // writing the response body to a client that may be gone
}

// warnCapture records the warnings and errors logged during one build, so the
// preview UI can list them. They go to the UI only; the other levels still
// reach the terminal through the embedded logger.
type warnCapture struct {
	logx.Logger
	sink *warnSink
}

func (c warnCapture) Warnf(format string, args ...any) {
	c.sink.add(fmt.Sprintf(format, args...))
}

func (c warnCapture) Errorf(format string, args ...any) {
	c.sink.add(fmt.Sprintf(format, args...))
}

func (c warnCapture) Channel(name string) logx.Logger {
	return warnCapture{Logger: c.Logger.Channel(name), sink: c.sink}
}

// warnSink is shared by a warnCapture and its channels. Chrome listeners and
// the ephemeral server log from other goroutines.
type warnSink struct {
	mu    sync.Mutex
	lines []string
}

// add drops repeats: the renderer and the build both log render warnings, and
// a TOC document renders twice.
func (s *warnSink) add(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(s.lines, line) {
		s.lines = append(s.lines, line)
	}
}

func (s *warnSink) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.lines)
}
