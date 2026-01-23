// Package playwright provides HTML-to-PDF rendering using Playwright browsers.
//
// # Context and Cancellation
//
// The package respects context cancellation at the following points:
//   - RenderPDF() checks ctx.Err() at function entry
//   - waitForComponentReady() respects context for early termination
//   - Page navigation has a separate timeout (not tied to context)
//
// Note: The Playwright driver itself does not directly support context cancellation
// for browser operations. The timeout-based approach is used for page navigation
// and PDF generation. However, context cancellation will:
//   - Prevent new operations from starting
//   - Allow waitForComponentReady() to return early
//   - The browser and page resources are cleaned up via defer
//
// On context cancellation:
//   - Browser and context handles are closed via defer
//   - The Playwright client is stopped via defer
//   - Partial PDF files may be left on disk (caller should handle cleanup)
package playwright

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pw "github.com/playwright-community/playwright-go"

	"bino.bi/bino/internal/logx"
)

// PDFOptions controls the HTML-to-PDF export pipeline using Playwright.
type PDFOptions struct {
	URL                   string
	PDFPath               string
	Browser               string
	DriverDirectory       string
	Format                string
	Orientation           string
	Timeout               time.Duration
	Debug                 bool
	WaitForComponentReady bool
	ReadyConsolePrefix    string
	// Header/footer options for document PDFs
	DisplayHeaderFooter bool
	HeaderTemplate      string
	FooterTemplate      string
	MarginTop           string
	MarginBottom        string
}

// RenderPDF loads the provided URL in a headless browser and exports it to PDF.
// It checks ctx.Err() at entry and propagates context to waitForComponentReady.
// Browser operations use timeout-based cancellation via PDFOptions.Timeout.
//
// On context cancellation, resources are cleaned up but partial work may remain.
func RenderPDF(ctx context.Context, opts PDFOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	logger := logx.FromContext(ctx).Channel("playwright")
	if opts.URL == "" {
		return fmt.Errorf("render pdf: url is required")
	}
	if opts.PDFPath == "" {
		return fmt.Errorf("render pdf: pdf path is required")
	}

	if err := os.MkdirAll(filepath.Dir(opts.PDFPath), 0o755); err != nil {
		return fmt.Errorf("render pdf: create output dir: %w", err)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	browserName := opts.Browser
	if browserName == "" {
		browserName = "chromium"
	}

	runOpts := &pw.RunOptions{DriverDirectory: opts.DriverDirectory}
	if opts.Debug {
		runOpts.Verbose = true
	}

	client, err := pw.Run(runOpts)
	if err != nil {
		return fmt.Errorf("launch playwright: %w (run 'bino setup' if this is the first run)", err)
	}
	defer client.Stop()

	browser, err := launchBrowser(client, browserName)
	if err != nil {
		return err
	}
	defer browser.Close()

	contextHandle, err := browser.NewContext()
	if err != nil {
		return fmt.Errorf("create browser context: %w", err)
	}
	defer contextHandle.Close()

	page, err := contextHandle.NewPage()
	if err != nil {
		return fmt.Errorf("create page: %w", err)
	}

	var readyCh <-chan struct{}
	if opts.WaitForComponentReady {
		readyCh = observeComponentReady(page, opts.ReadyConsolePrefix, logger)
	}

	timeoutMs := float64(timeout.Milliseconds())
	if timeoutMs <= 0 {
		timeoutMs = 120000
	}
	gotoOpts := pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateNetworkidle,
		Timeout:   &timeoutMs,
	}
	if _, err := page.Goto(opts.URL, gotoOpts); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("load %s: %w", opts.URL, err)
	}

	if readyCh != nil {
		if err := waitForComponentReady(ctx, readyCh, timeout); err != nil {
			return err
		}
	}

	pdfOpts := pw.PagePdfOptions{
		Path:            pw.String(opts.PDFPath),
		PrintBackground: pw.Bool(true),
	}
	format := strings.TrimSpace(opts.Format)
	if format != "" {
		if w, h, ok := customFormatDimensions(format); ok {
			pdfOpts.Width = pw.String(fmt.Sprintf("%dpx", w))
			pdfOpts.Height = pw.String(fmt.Sprintf("%dpx", h))
		} else {
			upper := strings.ToUpper(format)
			pdfOpts.Format = &upper
		}
	}
	// Set margins - use larger margins if header/footer is enabled
	marginTop := "0"
	marginBottom := "0"
	if opts.DisplayHeaderFooter {
		marginTop = "20mm"
		marginBottom = "15mm"
		if opts.MarginTop != "" {
			marginTop = opts.MarginTop
		}
		if opts.MarginBottom != "" {
			marginBottom = opts.MarginBottom
		}
	}
	pdfOpts.Margin = &pw.Margin{
		Top:    pw.String(marginTop),
		Right:  pw.String("0"),
		Bottom: pw.String(marginBottom),
		Left:   pw.String("0"),
	}
	if opts.Orientation != "" {
		landscape := strings.EqualFold(opts.Orientation, "landscape")
		pdfOpts.Landscape = pw.Bool(landscape)
	}
	// Header/footer support
	if opts.DisplayHeaderFooter {
		pdfOpts.DisplayHeaderFooter = pw.Bool(true)
		if opts.HeaderTemplate != "" {
			pdfOpts.HeaderTemplate = pw.String(opts.HeaderTemplate)
		}
		if opts.FooterTemplate != "" {
			pdfOpts.FooterTemplate = pw.String(opts.FooterTemplate)
		}
	}

	if _, err := page.PDF(pdfOpts); err != nil {
		return fmt.Errorf("generate pdf: %w", err)
	}

	return nil
}

// HeadingPageInfo contains page number information for a heading.
type HeadingPageInfo struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	PageNum  int    `json:"pageNum"`
}

// CollectHeadingPages loads a page and calculates which page each heading appears on.
// This is used for TOC page number generation in a two-pass rendering approach.
func CollectHeadingPages(ctx context.Context, opts PDFOptions) ([]HeadingPageInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.URL == "" {
		return nil, fmt.Errorf("collect heading pages: url is required")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	browserName := opts.Browser
	if browserName == "" {
		browserName = "chromium"
	}

	runOpts := &pw.RunOptions{DriverDirectory: opts.DriverDirectory}
	if opts.Debug {
		runOpts.Verbose = true
	}

	client, err := pw.Run(runOpts)
	if err != nil {
		return nil, fmt.Errorf("launch playwright: %w", err)
	}
	defer client.Stop()

	browser, err := launchBrowser(client, browserName)
	if err != nil {
		return nil, err
	}
	defer browser.Close()

	contextHandle, err := browser.NewContext()
	if err != nil {
		return nil, fmt.Errorf("create browser context: %w", err)
	}
	defer contextHandle.Close()

	page, err := contextHandle.NewPage()
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}

	// Emulate print media for accurate page calculations
	if err := page.EmulateMedia(pw.PageEmulateMediaOptions{
		Media: pw.MediaPrint,
	}); err != nil {
		return nil, fmt.Errorf("emulate print media: %w", err)
	}

	timeoutMs := float64(timeout.Milliseconds())
	gotoOpts := pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateNetworkidle,
		Timeout:   &timeoutMs,
	}
	if _, err := page.Goto(opts.URL, gotoOpts); err != nil {
		return nil, fmt.Errorf("load %s: %w", opts.URL, err)
	}

	// Calculate page dimensions based on format
	pageHeightPx := 1123.0 // A4 default at 96 DPI (297mm)
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	switch format {
	case "a4":
		pageHeightPx = 1123.0 // 297mm at 96 DPI
	case "a5":
		pageHeightPx = 794.0 // 210mm at 96 DPI
	case "letter":
		pageHeightPx = 1056.0 // 11in at 96 DPI
	case "legal":
		pageHeightPx = 1344.0 // 14in at 96 DPI
	}

	// Swap for landscape
	if strings.EqualFold(opts.Orientation, "landscape") {
		// In landscape, height becomes the shorter dimension
		switch format {
		case "a4":
			pageHeightPx = 794.0 // 210mm
		case "a5":
			pageHeightPx = 559.0 // 148mm
		case "letter":
			pageHeightPx = 816.0 // 8.5in
		case "legal":
			pageHeightPx = 816.0 // 8.5in
		}
	}

	// Account for margins in header/footer mode
	marginTopPx := 0.0
	marginBottomPx := 0.0
	if opts.DisplayHeaderFooter {
		marginTopPx = 75.6   // 20mm default
		marginBottomPx = 56.7 // 15mm default
	}
	effectivePageHeight := pageHeightPx - marginTopPx - marginBottomPx

	// JavaScript to collect heading positions and account for CSS page breaks
	jsCode := fmt.Sprintf(`
		(() => {
			const headings = document.querySelectorAll('h1[id], h2[id], h3[id], h4[id], h5[id], h6[id]');
			const pageHeight = %f;

			// Collect elements that force page breaks (TOC, explicit page breaks)
			const pageBreakElements = [];
			document.querySelectorAll('.bn-toc, .bn-page-break').forEach(el => {
				const rect = el.getBoundingClientRect();
				const style = window.getComputedStyle(el);
				// Check for page-break-after or break-after
				if (style.pageBreakAfter === 'always' || style.breakAfter === 'page') {
					pageBreakElements.push({
						top: rect.top + window.scrollY,
						bottom: rect.bottom + window.scrollY
					});
				}
			});

			const results = [];
			for (const h of headings) {
				const rect = h.getBoundingClientRect();
				const absoluteTop = rect.top + window.scrollY;

				// Find the last forced break before this heading
				let lastBreakBottom = 0;
				for (const pb of pageBreakElements) {
					if (pb.bottom < absoluteTop) {
						lastBreakBottom = Math.max(lastBreakBottom, pb.bottom);
					}
				}

				let pageNum;
				if (lastBreakBottom > 0) {
					// Heading is after a forced page break
					// Calculate pages the content before break takes
					const pagesBeforeBreak = Math.ceil(lastBreakBottom / pageHeight);
					// Position relative to the break
					const relativePos = absoluteTop - lastBreakBottom;
					// Pages from relative position (0-indexed)
					const pagesAfterBreak = Math.floor(relativePos / pageHeight);
					// Total: pages before + 1 (for the break) + pages after
					pageNum = pagesBeforeBreak + 1 + pagesAfterBreak;
				} else {
					// No break before this heading, simple calculation
					pageNum = Math.floor(absoluteTop / pageHeight) + 1;
				}

				results.push({
					id: h.id,
					text: h.textContent.trim(),
					pageNum: pageNum
				});
			}
			return results;
		})()
	`, effectivePageHeight)

	result, err := page.Evaluate(jsCode)
	if err != nil {
		return nil, fmt.Errorf("evaluate heading positions: %w", err)
	}

	// Parse result
	var headings []HeadingPageInfo
	if arr, ok := result.([]any); ok {
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				info := HeadingPageInfo{}
				if id, ok := m["id"].(string); ok {
					info.ID = id
				}
				if text, ok := m["text"].(string); ok {
					info.Text = text
				}
				// Handle pageNum - could be float64 or int depending on JS engine
				switch pn := m["pageNum"].(type) {
				case float64:
					info.PageNum = int(pn)
				case int:
					info.PageNum = pn
				case int64:
					info.PageNum = int(pn)
				}
				if info.ID != "" {
					headings = append(headings, info)
				}
			}
		}
	}

	return headings, nil
}

func launchBrowser(client *pw.Playwright, name string) (pw.Browser, error) {
	headless := pw.Bool(true)
	launchOpts := pw.BrowserTypeLaunchOptions{Headless: headless}
	if args := browserArgs(name); len(args) > 0 {
		launchOpts.Args = args
	}

	switch strings.ToLower(name) {
	case "chromium", "chrome", "edge":
		return client.Chromium.Launch(launchOpts)
	case "webkit":
		return client.WebKit.Launch(launchOpts)
	case "firefox":
		return client.Firefox.Launch(launchOpts)
	default:
		return nil, fmt.Errorf("unsupported browser %q", name)
	}
}

func browserArgs(name string) []string {
	switch strings.ToLower(name) {
	case "chromium", "chrome", "edge":
		args := make([]string, len(defaultChromiumArgs))
		copy(args, defaultChromiumArgs)
		return args
	default:
		return nil
	}
}

// defaultChromiumArgs contains command-line flags for headless Chromium PDF rendering.
//
// Security Note: These flags are specifically configured for trusted, internal PDF
// generation workflows where the HTML content is generated by the application itself.
// DO NOT use these settings for rendering untrusted external content.
//
// Flags explained:
//   - --no-sandbox, --disable-setuid-sandbox: Required for running in containerized
//     environments (Docker, CI). The sandbox is not needed since we only render
//     trusted internal content.
//   - --disable-dev-shm-usage: Prevents /dev/shm exhaustion in container environments.
//   - --font-render-hinting=none: Ensures consistent font rendering across platforms.
//   - --disable-web-security: Allows loading local assets without CORS restrictions.
//     This is safe because we only render internally-generated HTML.
//   - --disable-device-discovery-notifications: Suppresses unnecessary notifications.
var defaultChromiumArgs = []string{
	"--no-sandbox",
	"--disable-setuid-sandbox",
	"--disable-dev-shm-usage",
	"--font-render-hinting=none",
	"--disable-web-security",
	"--disable-device-discovery-notifications",
}

func observeComponentReady(page pw.Page, prefix string, logger logx.Logger) <-chan struct{} {
	ready := make(chan struct{}, 1)
	if prefix == "" {
		prefix = "componentregisterisrendered:"
	} else {
		prefix = strings.ToLower(prefix)
	}
	if logger == nil {
		logger = logx.Nop()
	}
	page.On("console", func(msg pw.ConsoleMessage) {
		text := strings.TrimSpace(msg.Text())
		logger.Debugf("Console log: %q", text)
		if text == "" {
			return
		}
		lower := strings.ToLower(text)
		if !strings.HasPrefix(lower, prefix) {
			return
		}
		value := strings.TrimSpace(text[len(prefix):])
		value = strings.Trim(value, "\"'")

		if isTruthy(value) {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	return ready
}

// waitForComponentReady blocks until the component signals readiness or a timeout/cancellation occurs.
// It creates a child context with the specified timeout to bound the wait time.
//
// Returns nil in the following cases:
//   - The ready channel receives a signal (component is ready)
//   - The timeout expires (assumes component is ready, returns nil)
//
// Returns an error only if the parent context is canceled (context.Canceled).
// This allows builds to be interrupted while still completing gracefully on timeout.
func waitForComponentReady(ctx context.Context, ready <-chan struct{}, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-ready:
		return nil
	case <-waitCtx.Done():
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return nil
		}
		return waitCtx.Err()
	}
}

func isTruthy(value string) bool {
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func customFormatDimensions(name string) (width, height int, ok bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "xga":
		return 1024, 768, true
	case "hd":
		return 1280, 720, true
	case "full_hd", "full-hd", "fullhd":
		return 1920, 1080, true
	case "4k":
		return 3840, 2160, true
	case "4k2k":
		return 4096, 2160, true
	default:
		return 0, 0, false
	}
}

// ScreenshotOptions controls the HTML-to-screenshot export pipeline using Playwright.
type ScreenshotOptions struct {
	URL                   string
	OutputDir             string
	Browser               string
	DriverDirectory       string
	Format                string
	Orientation           string
	Timeout               time.Duration
	Debug                 bool
	WaitForComponentReady bool
	ReadyConsolePrefix    string
	Refs                  []ScreenshotRef
	FilenamePrefix        string
	FilenamePattern       string // "index" or "ref"
	ImageFormat           string // "png" or "jpeg"
	Quality               *int   // JPEG quality 1-100
	OmitBackground        bool
	Scale                 string // "css" or "device"
}

// ScreenshotRef identifies a component to capture a screenshot of.
type ScreenshotRef struct {
	Kind string
	Name string
}

// ScreenshotResult contains the result of a single screenshot capture.
type ScreenshotResult struct {
	Ref      ScreenshotRef
	FilePath string
	Error    error
}

// RenderScreenshots loads the provided URL in a headless browser and captures screenshots of specified elements.
// It checks ctx.Err() at entry and propagates context to waitForComponentReady.
// Browser operations use timeout-based cancellation via ScreenshotOptions.Timeout.
//
// On context cancellation, resources are cleaned up but partial work may remain.
func RenderScreenshots(ctx context.Context, opts ScreenshotOptions) ([]ScreenshotResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger := logx.FromContext(ctx).Channel("playwright")
	if opts.URL == "" {
		return nil, fmt.Errorf("render screenshots: url is required")
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("render screenshots: output dir is required")
	}
	if len(opts.Refs) == 0 {
		return nil, fmt.Errorf("render screenshots: at least one ref is required")
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("render screenshots: create output dir: %w", err)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	browserName := opts.Browser
	if browserName == "" {
		browserName = "chromium"
	}

	runOpts := &pw.RunOptions{DriverDirectory: opts.DriverDirectory}
	if opts.Debug {
		runOpts.Verbose = true
	}

	client, err := pw.Run(runOpts)
	if err != nil {
		return nil, fmt.Errorf("launch playwright: %w (run 'bino setup' if this is the first run)", err)
	}
	defer client.Stop()

	browser, err := launchBrowser(client, browserName)
	if err != nil {
		return nil, err
	}
	defer browser.Close()

	// Set viewport size based on format and orientation
	viewportWidth, viewportHeight := 1024, 768 // default XGA
	if w, h, ok := customFormatDimensions(opts.Format); ok {
		viewportWidth, viewportHeight = w, h
	}
	if strings.EqualFold(opts.Orientation, "portrait") {
		viewportWidth, viewportHeight = viewportHeight, viewportWidth
	}

	contextHandle, err := browser.NewContext(pw.BrowserNewContextOptions{
		Viewport: &pw.Size{
			Width:  viewportWidth,
			Height: viewportHeight,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create browser context: %w", err)
	}
	defer contextHandle.Close()

	page, err := contextHandle.NewPage()
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}

	var readyCh <-chan struct{}
	if opts.WaitForComponentReady {
		readyCh = observeComponentReady(page, opts.ReadyConsolePrefix, logger)
	}

	timeoutMs := float64(timeout.Milliseconds())
	if timeoutMs <= 0 {
		timeoutMs = 120000
	}
	gotoOpts := pw.PageGotoOptions{
		WaitUntil: pw.WaitUntilStateNetworkidle,
		Timeout:   &timeoutMs,
	}
	if _, err := page.Goto(opts.URL, gotoOpts); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("load %s: %w", opts.URL, err)
	}

	if readyCh != nil {
		if err := waitForComponentReady(ctx, readyCh, timeout); err != nil {
			return nil, err
		}
	}

	// Capture screenshots for each ref
	results := make([]ScreenshotResult, 0, len(opts.Refs))
	for i, ref := range opts.Refs {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		result := ScreenshotResult{Ref: ref}

		// Build element ID selector
		elementID := "bino-" + strings.ToLower(ref.Kind) + "-" + ref.Name
		selector := "#" + elementID

		// Build output filename
		var filename string
		ext := opts.ImageFormat
		if ext == "" {
			ext = "png"
		}
		if opts.FilenamePattern == "index" {
			filename = fmt.Sprintf("%s-%03d.%s", opts.FilenamePrefix, i+1, ext)
		} else {
			// Default to "ref" pattern
			filename = fmt.Sprintf("%s-%s.%s", opts.FilenamePrefix, ref.Name, ext)
		}
		result.FilePath = filepath.Join(opts.OutputDir, filename)

		// Locate the element
		locator := page.Locator(selector)
		count, err := locator.Count()
		if err != nil {
			result.Error = fmt.Errorf("locate element %s: %w", selector, err)
			results = append(results, result)
			continue
		}
		if count == 0 {
			result.Error = fmt.Errorf("element %s not found", selector)
			results = append(results, result)
			continue
		}

		// Build screenshot options
		screenshotOpts := pw.LocatorScreenshotOptions{
			Path: pw.String(result.FilePath),
		}
		if strings.EqualFold(ext, "jpeg") || strings.EqualFold(ext, "jpg") {
			screenshotOpts.Type = pw.ScreenshotTypeJpeg
			if opts.Quality != nil {
				screenshotOpts.Quality = opts.Quality
			}
		} else {
			screenshotOpts.Type = pw.ScreenshotTypePng
		}
		if opts.OmitBackground {
			screenshotOpts.OmitBackground = pw.Bool(true)
		}
		if opts.Scale != "" {
			switch strings.ToLower(opts.Scale) {
			case "css":
				screenshotOpts.Scale = pw.ScreenshotScaleCss
			case "device":
				screenshotOpts.Scale = pw.ScreenshotScaleDevice
			}
		}

		// Take the screenshot
		if _, err := locator.Screenshot(screenshotOpts); err != nil {
			result.Error = fmt.Errorf("capture screenshot of %s: %w", selector, err)
			results = append(results, result)
			continue
		}

		results = append(results, result)
	}

	return results, nil
}
