// Package pipeline — builder.go provides the Builder type that decouples
// CLI commands from the internal report orchestration packages (chrome, signing,
// preview/httpserver). CLI commands create a Builder once with session-level
// configuration and call its methods instead of importing and orchestrating
// chrome, signing, and ephemeral-server lifecycles directly.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/logx"
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/report/signing"
	"bino.bi/bino/pkg/duckdb"
)

// Builder orchestrates the report build pipeline. It holds per-session
// configuration and provides high-level methods that hide the internal
// orchestration details (ephemeral HTTP servers, Chrome headless shell,
// PDF signing) from CLI commands.
//
// Create one Builder per build/preview/serve session:
//
//	b := &pipeline.Builder{Workdir: absDir, EngineVersion: ver, ...}
//	html, assets, diags := ... // render via pipeline functions or Builder helpers
//	err := b.RenderPDF(ctx, html, assets, pdfOpts)
//	err = b.SignPDF(ctx, pdfPath, profile)
type Builder struct {
	// Workdir is the absolute path to the report project directory.
	Workdir string
	// EngineVersion is the template engine version to use (e.g., "v1.2.3").
	EngineVersion string
	// CacheDir is the directory for CDN/asset caching.
	CacheDir string
	// Logger for pipeline operations. May be nil (defaults to nop).
	Logger logx.Logger

	// QueryLogger is called for each SQL query executed. May be nil.
	QueryLogger func(string)
	// QueryExecLogger is called with detailed metadata per query. May be nil.
	QueryExecLogger duckdb.QueryExecLogger

	// EmbedOptions configures CSV embedding for build logs.
	EmbedOptions buildlog.EmbedOptions
	// ExecutionPlan tracks build execution steps. May be nil.
	ExecutionPlan *buildlog.ExecutionPlan

	// DataValidation controls how data validation errors are handled.
	DataValidation dataset.DataValidationMode
	// DataValidationSampleSize limits how many rows are validated.
	DataValidationSampleSize int
}

func (b *Builder) logger() logx.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return logx.Nop()
}

// ---------------------------------------------------------------------------
// Render helpers — convenience wrappers that use the Builder's session config
// ---------------------------------------------------------------------------

// RenderArtefactHTML generates HTML for a specific ReportArtefact.
func (b *Builder) RenderArtefactHTML(ctx context.Context, docs []config.Document, artefact config.Artefact) (RenderResult, error) {
	return RenderArtefactHTML(ctx, b.Workdir, docs, artefact, RenderArtefactOptions{
		EngineVersion:            b.EngineVersion,
		QueryLogger:              b.QueryLogger,
		QueryExecLogger:          b.QueryExecLogger,
		EmbedOptions:             b.EmbedOptions,
		ExecutionPlan:            b.ExecutionPlan,
		DataValidation:           b.DataValidation,
		DataValidationSampleSize: b.DataValidationSampleSize,
	})
}

// RenderScreenshotHTML generates HTML for a ScreenshotArtefact.
func (b *Builder) RenderScreenshotHTML(ctx context.Context, docs []config.Document, artefact config.ScreenshotArtefact) (RenderResult, error) {
	return RenderScreenshotArtefactHTML(ctx, b.Workdir, docs, artefact, RenderScreenshotArtefactOptions{
		EngineVersion:            b.EngineVersion,
		QueryLogger:              b.QueryLogger,
		QueryExecLogger:          b.QueryExecLogger,
		EmbedOptions:             b.EmbedOptions,
		ExecutionPlan:            b.ExecutionPlan,
		DataValidation:           b.DataValidation,
		DataValidationSampleSize: b.DataValidationSampleSize,
	})
}

// RenderDocumentHTML generates HTML for a DocumentArtefact.
func (b *Builder) RenderDocumentHTML(ctx context.Context, artefact config.DocumentArtefact, opts DocumentArtefactRenderOptions) (DocumentArtefactResult, error) {
	if opts.EngineVersion == "" {
		opts.EngineVersion = b.EngineVersion
	}
	return RenderDocumentArtefactHTML(ctx, b.Workdir, artefact, opts)
}

// RenderPreviewFrame generates a two-phase frame+context render for preview mode.
func (b *Builder) RenderPreviewFrame(ctx context.Context, docs []config.Document) (FrameRenderResult, error) {
	return RenderHTMLFrameAndContext(ctx, docs, RenderOptions{
		Workdir:                  b.Workdir,
		Language:                 "de",
		Mode:                     RenderModePreview,
		EngineVersion:            b.EngineVersion,
		QueryLogger:              b.QueryLogger,
		DataValidation:           b.DataValidation,
		DataValidationSampleSize: b.DataValidationSampleSize,
	})
}

// RenderArtefactPreviewFrame generates a two-phase artefact frame+context for preview.
func (b *Builder) RenderArtefactPreviewFrame(ctx context.Context, docs []config.Document, artefact config.Artefact) (FrameRenderResult, error) {
	return RenderArtefactFrameAndContextWithOptions(ctx, b.Workdir, docs, artefact, FrameRenderOptions{
		QueryLogger:              b.QueryLogger,
		EngineVersion:            b.EngineVersion,
		DataValidation:           b.DataValidation,
		DataValidationSampleSize: b.DataValidationSampleSize,
	})
}

// ---------------------------------------------------------------------------
// Chrome PDF rendering
// ---------------------------------------------------------------------------

// PDFRenderOptions configures a single Chrome PDF capture.
type PDFRenderOptions struct {
	PDFPath               string
	ChromePath            string
	Format                string
	Orientation           string
	Debug                 bool
	WaitForComponentReady bool
	ReadyConsolePrefix    string
	// Document-specific header/footer options.
	DisplayHeaderFooter bool
	HeaderTemplate      string
	FooterTemplate      string
	MarginTop           string
	MarginBottom        string
}

// RenderPDF starts an ephemeral HTTP server with the given HTML and assets,
// then uses Chrome headless shell to capture a PDF. The server is shut down
// automatically after the PDF is generated (or on error/cancellation).
func (b *Builder) RenderPDF(ctx context.Context, html []byte, assets []render.LocalAsset, opts PDFRenderOptions) error {
	srv, err := newEphemeralServer(ctx, b.CacheDir, b.logger(), html, ConvertLocalAssets(assets))
	if err != nil {
		return fmt.Errorf("start ephemeral server: %w", err)
	}

	pdfOpts := chrome.PDFOptions{
		URL:                   srv.URL(),
		PDFPath:               opts.PDFPath,
		ChromePath:            opts.ChromePath,
		Format:                opts.Format,
		Orientation:           opts.Orientation,
		Timeout:               2 * time.Minute,
		Debug:                 opts.Debug,
		WaitForComponentReady: opts.WaitForComponentReady,
		ReadyConsolePrefix:    opts.ReadyConsolePrefix,
		DisplayHeaderFooter:   opts.DisplayHeaderFooter,
		HeaderTemplate:        opts.HeaderTemplate,
		FooterTemplate:        opts.FooterTemplate,
		MarginTop:             opts.MarginTop,
		MarginBottom:          opts.MarginBottom,
	}
	pdfErr := chrome.RenderPDF(ctx, pdfOpts)
	closeErr := srv.Close()

	if pdfErr != nil {
		return pdfErr
	}
	if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
		return fmt.Errorf("stop ephemeral server: %w", closeErr)
	}
	return nil
}

// CollectHeadingPages starts an ephemeral server with the given HTML and uses
// Chrome to collect heading page numbers for table-of-contents generation.
// Returns a map from heading ID to page number.
func (b *Builder) CollectHeadingPages(ctx context.Context, html []byte, assets []render.LocalAsset, opts PDFRenderOptions) (map[string]int, error) {
	srv, err := newEphemeralServer(ctx, b.CacheDir, b.logger(), html, ConvertLocalAssets(assets))
	if err != nil {
		return nil, fmt.Errorf("start ephemeral server: %w", err)
	}
	defer srv.Close()

	pdfOpts := chrome.PDFOptions{
		URL:                 srv.URL(),
		ChromePath:          opts.ChromePath,
		Format:              opts.Format,
		Orientation:         opts.Orientation,
		Timeout:             2 * time.Minute,
		Debug:               opts.Debug,
		DisplayHeaderFooter: opts.DisplayHeaderFooter,
		HeaderTemplate:      opts.HeaderTemplate,
		FooterTemplate:      opts.FooterTemplate,
		MarginTop:           opts.MarginTop,
		MarginBottom:        opts.MarginBottom,
	}
	headings, err := chrome.CollectHeadingPages(ctx, pdfOpts)
	if err != nil {
		return nil, err
	}

	result := make(map[string]int, len(headings))
	for _, h := range headings {
		result[h.ID] = h.PageNum
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Chrome screenshot capture
// ---------------------------------------------------------------------------

// ScreenshotRef identifies a component to capture a screenshot of.
type ScreenshotRef struct {
	Kind string
	Name string
}

// ScreenshotCaptureResult holds the result of capturing a single screenshot.
type ScreenshotCaptureResult struct {
	Ref      ScreenshotRef
	FilePath string
	Error    error
}

// ScreenshotRenderOptions configures a Chrome screenshot capture session.
type ScreenshotRenderOptions struct {
	OutputDir             string
	ChromePath            string
	Format                string
	Orientation           string
	Debug                 bool
	WaitForComponentReady bool
	ReadyConsolePrefix    string
	Refs                  []ScreenshotRef
	FilenamePrefix        string
	FilenamePattern       string
	Scale                 float64
}

// CaptureScreenshots starts an ephemeral HTTP server with the given HTML and
// assets, then uses Chrome headless shell to capture screenshots of the
// specified elements. The server is shut down automatically.
func (b *Builder) CaptureScreenshots(ctx context.Context, html []byte, assets []render.LocalAsset, opts ScreenshotRenderOptions) ([]ScreenshotCaptureResult, error) {
	srv, err := newEphemeralServer(ctx, b.CacheDir, b.logger(), html, ConvertLocalAssets(assets))
	if err != nil {
		return nil, fmt.Errorf("start ephemeral server: %w", err)
	}

	// Convert our refs to chrome refs
	chromeRefs := make([]chrome.ScreenshotRef, len(opts.Refs))
	for i, ref := range opts.Refs {
		chromeRefs[i] = chrome.ScreenshotRef{Kind: ref.Kind, Name: ref.Name}
	}

	chromeOpts := chrome.ScreenshotOptions{
		URL:                   srv.URL(),
		OutputDir:             opts.OutputDir,
		ChromePath:            opts.ChromePath,
		Format:                opts.Format,
		Orientation:           opts.Orientation,
		Timeout:               2 * time.Minute,
		Debug:                 opts.Debug,
		WaitForComponentReady: opts.WaitForComponentReady,
		ReadyConsolePrefix:    opts.ReadyConsolePrefix,
		Refs:                  chromeRefs,
		FilenamePrefix:        opts.FilenamePrefix,
		FilenamePattern:       opts.FilenamePattern,
		Scale:                 opts.Scale,
	}

	chromeResults, screenshotErr := chrome.RenderScreenshots(ctx, chromeOpts)
	closeErr := srv.Close()

	if screenshotErr != nil {
		return nil, screenshotErr
	}
	if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
		return nil, fmt.Errorf("stop ephemeral server: %w", closeErr)
	}

	// Convert chrome results to our type
	results := make([]ScreenshotCaptureResult, len(chromeResults))
	for i, r := range chromeResults {
		results[i] = ScreenshotCaptureResult{
			Ref:      ScreenshotRef{Kind: r.Ref.Kind, Name: r.Ref.Name},
			FilePath: r.FilePath,
			Error:    r.Error,
		}
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// PDF signing
// ---------------------------------------------------------------------------

// SignPDF applies a digital signature to a PDF file using the given signing profile.
func (b *Builder) SignPDF(ctx context.Context, pdfPath string, profile config.SigningProfile) error {
	return signing.Apply(ctx, profile, pdfPath)
}

// ---------------------------------------------------------------------------
// Ephemeral server — internal helper for Builder methods
// ---------------------------------------------------------------------------

// ephemeralServer is a short-lived HTTP server used to serve rendered HTML
// to Chrome headless shell during PDF generation and screenshot capture.
type ephemeralServer struct {
	server *previewhttp.Server
	cancel context.CancelFunc
	errCh  chan error
}

func newEphemeralServer(ctx context.Context, cacheDir string, logger logx.Logger, html []byte, assets []previewhttp.LocalAsset) (*ephemeralServer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	srv, err := previewhttp.New(previewhttp.Config{
		ListenAddr: "127.0.0.1:0",
		CacheDir:   cacheDir,
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}
	srv.SetLocalAssets(assets)
	srv.SetContentFunc(previewhttp.StaticContent(append([]byte(nil), html...), "text/html; charset=utf-8"))

	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(runCtx)
	}()
	return &ephemeralServer{server: srv, cancel: cancel, errCh: errCh}, nil
}

func (s *ephemeralServer) URL() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.URL()
}

func (s *ephemeralServer) Close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	select {
	case err := <-s.errCh:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("server shutdown timed out")
	}
}
