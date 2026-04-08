package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/go-json-experiment/json/jsontext"

	"bino.bi/bino/internal/logx"
)

// PDFOptions controls the HTML-to-PDF export pipeline using chromedp.
type PDFOptions struct {
	URL                   string
	PDFPath               string
	ChromePath            string
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

// RenderPDF loads the provided URL in a headless Chrome and exports it to PDF.
// It checks ctx.Err() at entry and propagates context to waitForComponentReady.
func RenderPDF(ctx context.Context, opts PDFOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	logger := logx.FromContext(ctx).Channel("chrome")
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

	allocCtx, allocCancel := newExecAllocator(ctx, opts.ChromePath, opts.Debug)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	taskCtx, timeoutCancel := context.WithTimeout(taskCtx, timeout)
	defer timeoutCancel()

	// Set up console listener for component readiness before navigation
	var readyCh <-chan struct{}
	if opts.WaitForComponentReady {
		readyCh = observeComponentReady(taskCtx, opts.ReadyConsolePrefix, logger)
	}

	// Navigate and wait for network idle
	if err := chromedp.Run(taskCtx,
		chromedp.Navigate(opts.URL),
		waitNetworkIdle(),
	); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("load %s: %w", opts.URL, err)
	}

	// Wait for component readiness signal
	if readyCh != nil {
		if err := waitForComponentReady(ctx, readyCh, timeout); err != nil {
			return err
		}
	}

	// Build PrintToPDF parameters
	printParams := page.PrintToPDF().
		WithPrintBackground(true).
		WithPreferCSSPageSize(true)

	format := strings.TrimSpace(opts.Format)
	customFormat := false
	if format != "" {
		if w, h, ok := customFormatDimensions(format); ok {
			customFormat = true
			// Custom formats define landscape dimensions (width > height).
			// Orientation is handled by swapping dimensions rather than using
			// the Landscape flag, because Chrome swaps Width/Height when
			// Landscape is set — which would invert the intended orientation.
			if strings.EqualFold(opts.Orientation, "portrait") {
				w, h = h, w
			}
			printParams = printParams.
				WithPaperWidth(pxToInches(w)).
				WithPaperHeight(pxToInches(h))
		} else {
			// Standard paper format
			pw, ph := paperSizeInches(format)
			if pw > 0 && ph > 0 {
				printParams = printParams.
					WithPaperWidth(pw).
					WithPaperHeight(ph)
			}
		}
	}

	// Set margins
	marginTop := 0.0
	marginBottom := 0.0
	if opts.DisplayHeaderFooter {
		marginTop = mmToInches(20)    // 20mm default
		marginBottom = mmToInches(15) // 15mm default
		if opts.MarginTop != "" {
			marginTop = parseMargin(opts.MarginTop)
		}
		if opts.MarginBottom != "" {
			marginBottom = parseMargin(opts.MarginBottom)
		}
	}
	printParams = printParams.
		WithMarginTop(marginTop).
		WithMarginRight(0).
		WithMarginBottom(marginBottom).
		WithMarginLeft(0)

	// Only set Landscape for standard paper formats.
	// Custom formats handle orientation via dimension swapping above.
	if !customFormat && opts.Orientation != "" {
		landscape := strings.EqualFold(opts.Orientation, "landscape")
		printParams = printParams.WithLandscape(landscape)
	}

	// Header/footer support
	if opts.DisplayHeaderFooter {
		printParams = printParams.WithDisplayHeaderFooter(true)
		if opts.HeaderTemplate != "" {
			printParams = printParams.WithHeaderTemplate(opts.HeaderTemplate)
		}
		if opts.FooterTemplate != "" {
			printParams = printParams.WithFooterTemplate(opts.FooterTemplate)
		}
	}

	// Generate PDF
	var buf []byte
	if err := chromedp.Run(taskCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, _, err = printParams.Do(ctx)
			return err
		}),
	); err != nil {
		return fmt.Errorf("generate pdf: %w", err)
	}

	if err := os.WriteFile(opts.PDFPath, buf, 0o644); err != nil { //nolint:gosec // G306: PDF output files need standard read perms
		return fmt.Errorf("write pdf: %w", err)
	}

	return nil
}

// ScreenshotOptions controls the HTML-to-screenshot export pipeline using chromedp.
type ScreenshotOptions struct {
	URL                   string
	OutputDir             string
	ChromePath            string
	Format                string
	Orientation           string
	Timeout               time.Duration
	Debug                 bool
	WaitForComponentReady bool
	ReadyConsolePrefix    string
	Refs                  []ScreenshotRef
	FilenamePrefix        string
	FilenamePattern       string  // "index" or "ref"
	Scale                 float64 // device scale factor (e.g. 2.0 for retina)
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

// RenderScreenshots loads the provided URL in a headless Chrome and captures screenshots of specified elements.
func RenderScreenshots(ctx context.Context, opts ScreenshotOptions) ([]ScreenshotResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger := logx.FromContext(ctx).Channel("chrome")
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

	// Set viewport size based on format and orientation
	viewportWidth, viewportHeight := 1024, 768 // default XGA
	if w, h, ok := customFormatDimensions(opts.Format); ok {
		viewportWidth, viewportHeight = w, h
	}
	if strings.EqualFold(opts.Orientation, "portrait") {
		viewportWidth, viewportHeight = viewportHeight, viewportWidth
	}

	// Apply device scale factor for high-DPI screenshots
	scaleFactor := opts.Scale
	if scaleFactor <= 0 {
		scaleFactor = 1.0
	}

	allocCtx, allocCancel := newExecAllocator(ctx, opts.ChromePath, opts.Debug)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	taskCtx, timeoutCancel := context.WithTimeout(taskCtx, timeout)
	defer timeoutCancel()

	// Set up console listener before navigation
	var readyCh <-chan struct{}
	if opts.WaitForComponentReady {
		readyCh = observeComponentReady(taskCtx, opts.ReadyConsolePrefix, logger)
	}

	// Navigate with viewport emulation and wait for network idle
	if err := chromedp.Run(taskCtx,
		chromedp.EmulateViewport(int64(viewportWidth), int64(viewportHeight), chromedp.EmulateScale(scaleFactor)),
		chromedp.Navigate(opts.URL),
		waitNetworkIdle(),
	); err != nil {
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

		// Build output filename (always PNG)
		var filename string
		if opts.FilenamePattern == "index" {
			filename = fmt.Sprintf("%s-%03d.png", opts.FilenamePrefix, i+1)
		} else {
			filename = fmt.Sprintf("%s-%s.png", opts.FilenamePrefix, ref.Name)
		}
		result.FilePath = filepath.Join(opts.OutputDir, filename)

		// Take screenshot of element
		var buf []byte
		if err := chromedp.Run(taskCtx,
			chromedp.Screenshot(selector, &buf, chromedp.ByQuery),
		); err != nil {
			result.Error = fmt.Errorf("capture screenshot of %s: %w", selector, err)
			results = append(results, result)
			continue
		}

		if err := os.WriteFile(result.FilePath, buf, 0o644); err != nil { //nolint:gosec // G306: screenshot output files need standard read perms
			result.Error = fmt.Errorf("write screenshot %s: %w", result.FilePath, err)
			results = append(results, result)
			continue
		}

		results = append(results, result)
	}

	return results, nil
}

// newExecAllocator creates a chromedp ExecAllocator with the appropriate flags.
func newExecAllocator(parentCtx context.Context, chromePath string, debug bool) (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("font-render-hinting", "none"),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("disable-device-discovery-notifications", true),
	)

	if chromePath != "" {
		opts = append(opts, chromedp.ExecPath(chromePath))
	}

	if debug {
		return chromedp.NewExecAllocator(parentCtx, opts...)
	}

	return chromedp.NewExecAllocator(parentCtx, opts...)
}

// waitNetworkIdle returns a chromedp action that enables lifecycle events and
// waits for the "networkIdle" event, indicating no pending network requests.
func waitNetworkIdle() chromedp.ActionFunc {
	return func(ctx context.Context) error {
		// Enable lifecycle events
		if err := page.SetLifecycleEventsEnabled(true).Do(ctx); err != nil {
			return err
		}

		ch := make(chan struct{}, 1)
		chromedp.ListenTarget(ctx, func(ev interface{}) {
			if le, ok := ev.(*page.EventLifecycleEvent); ok {
				if le.Name == "networkIdle" {
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		})

		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			// Timeout = success (same as current behavior — page may not fire networkIdle)
			return nil
		}
	}
}

// observeComponentReady sets up a listener for console messages that signal
// component readiness. Returns a channel that receives when the component is ready.
func observeComponentReady(ctx context.Context, prefix string, logger logx.Logger) <-chan struct{} {
	ready := make(chan struct{}, 1)
	if prefix == "" {
		prefix = "componentregisterisrendered:"
	} else {
		prefix = strings.ToLower(prefix)
	}
	if logger == nil {
		logger = logx.Nop()
	}

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if ev, ok := ev.(*runtime.EventConsoleAPICalled); ok {
			// Join all console.log arguments into a single string,
			// matching Playwright's behavior. In CDP, console.log("prefix:", value)
			// arrives as separate args rather than a single concatenated string.
			var parts []string
			for _, arg := range ev.Args {
				text := strings.TrimSpace(unquoteJSValue(arg.Value))
				if text != "" {
					parts = append(parts, text)
				}
			}
			joined := strings.Join(parts, " ")
			logger.Debugf("Console log: %q", joined)
			if joined == "" {
				return
			}
			lower := strings.ToLower(joined)
			if !strings.HasPrefix(lower, prefix) {
				return
			}
			value := strings.TrimSpace(joined[len(prefix):])
			value = strings.Trim(value, "\"'")
			if isTruthy(value) {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}
	})
	return ready
}

// unquoteJSValue extracts a string from a JSON-encoded runtime.RemoteObject value.
func unquoteJSValue(raw jsontext.Value) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		// If it's not a JSON string, return the raw bytes
		return string(raw)
	}
	return s
}

// waitForComponentReady blocks until the component signals readiness or a timeout/cancellation occurs.
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

// formatDimensionsPx returns the pixel dimensions (width, height) for a given
// page format and orientation. It supports both custom screen formats and
// standard paper sizes at 96 DPI.
func formatDimensionsPx(format, orientation string) (width, height int, ok bool) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return 0, 0, false
	}

	var w, h int

	if cw, ch, cok := customFormatDimensions(format); cok {
		w, h = cw, ch
	} else {
		switch format {
		case "a3":
			w, h = 1123, 1587
		case "a4":
			w, h = 794, 1123
		case "a5":
			w, h = 559, 794
		case "letter":
			w, h = 816, 1056
		case "legal":
			w, h = 816, 1344
		case "tabloid":
			w, h = 1056, 1632
		default:
			return 0, 0, false
		}

		if strings.EqualFold(orientation, "landscape") {
			w, h = h, w
		}
		return w, h, true
	}

	if strings.EqualFold(orientation, "portrait") {
		w, h = h, w
	}
	return w, h, true
}

// Unit conversion helpers for chromedp PrintToPDF (which uses inches).

// pxToInches converts CSS pixels (96 DPI) to inches.
func pxToInches(px int) float64 {
	return float64(px) / 96.0
}

// mmToInches converts millimeters to inches.
func mmToInches(mm float64) float64 {
	return mm / 25.4
}

// cmToInches converts centimeters to inches.
func cmToInches(cm float64) float64 {
	return cm / 2.54
}

// parseMargin parses a margin string with unit suffix (e.g., "20mm", "1in", "2cm", "96px")
// and returns the value in inches. Defaults to treating bare numbers as millimeters.
func parseMargin(s string) float64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}

	var value float64
	switch {
	case strings.HasSuffix(s, "in"):
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "in"), "%f", &value)
		return value
	case strings.HasSuffix(s, "mm"):
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "mm"), "%f", &value)
		return mmToInches(value)
	case strings.HasSuffix(s, "cm"):
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "cm"), "%f", &value)
		return cmToInches(value)
	case strings.HasSuffix(s, "px"):
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "px"), "%f", &value)
		return value / 96.0
	default:
		// Default: treat as millimeters
		_, _ = fmt.Sscanf(s, "%f", &value)
		return mmToInches(value)
	}
}

// paperSizeInches returns the paper dimensions in inches for standard formats.
func paperSizeInches(format string) (width, height float64) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "a3":
		return mmToInches(297), mmToInches(420)
	case "a4":
		return mmToInches(210), mmToInches(297)
	case "a5":
		return mmToInches(148), mmToInches(210)
	case "letter":
		return 8.5, 11
	case "legal":
		return 8.5, 14
	case "tabloid":
		return 11, 17
	default:
		return 0, 0
	}
}
