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
	// OnLayoutState, when set, receives a getLayoutState() capture taken after
	// the page settles and before printing. It is not called when the engine
	// predates the API or the capture fails - a missing snapshot never fails a
	// build.
	OnLayoutState func(snapshot []byte)
}

// ComponentFailure is a component that threw while rendering. The engine lists
// these in window.componentRegisterFailures once the page has settled.
type ComponentFailure struct {
	// Tag is the element that threw, for example "bn-table-renderer".
	Tag string `json:"tag"`
	// ID is the id attribute of that element or of the public element around
	// it. Empty when neither has one.
	ID      string `json:"id"`
	Message string `json:"message"`
}

// String names the component and what it threw: "tag#id: message".
func (f ComponentFailure) String() string {
	name := f.Tag
	if name == "" {
		name = "component"
	}
	if f.ID != "" {
		name += "#" + f.ID
	}
	if f.Message == "" {
		return name
	}
	return name + ": " + f.Message
}

// ComponentFailuresError reports a page that settled with failed components.
// Such a page lacks their content, so RenderPDF writes nothing and
// RenderScreenshots captures nothing.
type ComponentFailuresError struct {
	Failures []ComponentFailure
}

func (e *ComponentFailuresError) Error() string {
	parts := make([]string, len(e.Failures))
	for i, f := range e.Failures {
		parts[i] = f.String()
	}
	noun := "components"
	if len(parts) == 1 {
		noun = "component"
	}
	return fmt.Sprintf("%d %s failed to render: %s", len(parts), noun, strings.Join(parts, "; "))
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
	var signals *readySignals
	if opts.WaitForComponentReady {
		signals = observeComponentReady(taskCtx, opts.ReadyConsolePrefix, logger)
	}

	// Navigate and wait for network idle
	if err := chromedp.Run(taskCtx,
		chromedp.Navigate(opts.URL),
		waitNetworkIdle(logger),
	); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("load %s: %w", opts.URL, err)
	}

	// Wait for component readiness signal
	if signals != nil {
		failures, err := waitForComponentReady(ctx, signals, timeout, pageComponentFailures(taskCtx, logger), logger)
		if err != nil {
			return err
		}
		if len(failures) > 0 {
			return &ComponentFailuresError{Failures: failures}
		}
	}

	// Capture the rendered layout before printing, while the page is still the
	// one that produced the PDF.
	if opts.OnLayoutState != nil {
		if snapshot, err := captureLayoutState(taskCtx, logger); err != nil {
			logger.Warnf("layout-state capture skipped: %v", err)
		} else if len(snapshot) > 0 {
			opts.OnLayoutState(snapshot)
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
			// Landscape is set - which would invert the intended orientation.
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
	var signals *readySignals
	if opts.WaitForComponentReady {
		signals = observeComponentReady(taskCtx, opts.ReadyConsolePrefix, logger)
	}

	// Navigate with viewport emulation and wait for network idle
	if err := chromedp.Run(taskCtx,
		chromedp.EmulateViewport(int64(viewportWidth), int64(viewportHeight), chromedp.EmulateScale(scaleFactor)),
		chromedp.Navigate(opts.URL),
		waitNetworkIdle(logger),
	); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("load %s: %w", opts.URL, err)
	}

	if signals != nil {
		failures, err := waitForComponentReady(ctx, signals, timeout, pageComponentFailures(taskCtx, logger), logger)
		if err != nil {
			return nil, err
		}
		if len(failures) > 0 {
			// A failed component keeps its layout box, so its screenshot would
			// be an empty image without an error. A failure cannot be mapped
			// to a ref, so no ref is captured and each one reports the failure.
			failed := &ComponentFailuresError{Failures: failures}
			results := make([]ScreenshotResult, len(opts.Refs))
			for i, ref := range opts.Refs {
				results[i] = ScreenshotResult{Ref: ref, Error: failed}
			}
			return results, nil
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
// A timeout is tolerated (some pages never fire networkIdle) but logged as a
// warning, since the rendered output may be incomplete.
func waitNetworkIdle(logger logx.Logger) chromedp.ActionFunc {
	if logger == nil {
		logger = logx.Nop()
	}
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
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				logger.Warnf("Timed out waiting for network idle; rendered output may be incomplete")
			}
			return nil
		}
	}
}

// readySignals carries the engine's "componentRegisterIsRendered: <value>"
// console lines. The engine writes one each time the page has been quiet for a
// moment: true when every component rendered, false when one is still working
// or has failed.
type readySignals struct {
	ready    chan struct{} // a line with true arrived
	notReady chan struct{} // a line with any other value arrived
}

// observeComponentReady sets up a listener for console messages that signal
// component readiness. The returned channels receive when such a line arrives.
func observeComponentReady(ctx context.Context, prefix string, logger logx.Logger) *readySignals {
	signals := &readySignals{ready: make(chan struct{}, 1), notReady: make(chan struct{}, 1)}
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
			rendered, ok := classifyReadyLine(joined, prefix)
			if !ok {
				return
			}
			ch := signals.notReady
			if rendered {
				ch = signals.ready
			}
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	})
	return signals
}

// classifyReadyLine reports whether a console line is the engine's readiness
// line and, if it is, whether it says that the page is rendered. prefix must
// be lower case.
func classifyReadyLine(line, prefix string) (rendered, ok bool) {
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return false, false
	}
	value := strings.TrimSpace(line[len(prefix):])
	value = strings.Trim(value, "\"'")
	return isTruthy(value), true
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

// waitForComponentReady blocks until the engine reports the page as rendered,
// reports failed components, or a timeout/cancellation occurs.
//
// From v1.0.0-next.28 on the engine never reports a page with a failed
// component as rendered. So on each "not rendered" line the failures are read:
// a non-empty list means the page has settled and waiting longer cannot help.
// The failures are returned, they are not an error. An empty list means a
// component is still working. An older engine has no such list and reports a
// page with a failed component as rendered.
//
// A timeout is tolerated (the page may never emit the readiness signal) but logged
// as a warning, since the rendered output may be incomplete.
func waitForComponentReady(ctx context.Context, signals *readySignals, timeout time.Duration, failures func() []ComponentFailure, logger logx.Logger) ([]ComponentFailure, error) {
	if logger == nil {
		logger = logx.Nop()
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		select {
		case <-signals.ready:
			return nil, nil
		case <-signals.notReady:
			// A later line may already say the page is rendered.
			select {
			case <-signals.ready:
				return nil, nil
			default:
			}
			if failed := failures(); len(failed) > 0 {
				return failed, nil
			}
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				logger.Warnf("Component readiness signal not received within %s; rendered output may be incomplete", timeout)
				return nil, nil
			}
			return nil, waitCtx.Err()
		}
	}
}

// componentFailuresExpression reads the engine's failure list as plain
// strings. A page without the list gives an empty result.
const componentFailuresExpression = `(Array.isArray(window.componentRegisterFailures) ? window.componentRegisterFailures : []).map(function (f) {
  f = f || {};
  return { tag: String(f.tag || ''), id: String(f.id || ''), message: String(f.message || '') };
})`

// pageComponentFailures returns a reader of the failure list of the page in
// ctx. A page that cannot be asked counts as having no failures, so the
// caller keeps waiting as it did before.
func pageComponentFailures(ctx context.Context, logger logx.Logger) func() []ComponentFailure {
	return func() []ComponentFailure {
		var failures []ComponentFailure
		if err := chromedp.Run(ctx, chromedp.Evaluate(componentFailuresExpression, &failures)); err != nil {
			logger.Debugf("read component failures: %v", err)
			return nil
		}
		return failures
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
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "in"), "%f", &value) //nolint:errcheck // zero on parse failure is the documented fallback
		return value
	case strings.HasSuffix(s, "mm"):
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "mm"), "%f", &value) //nolint:errcheck // zero on parse failure is the documented fallback
		return mmToInches(value)
	case strings.HasSuffix(s, "cm"):
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "cm"), "%f", &value) //nolint:errcheck // zero on parse failure is the documented fallback
		return cmToInches(value)
	case strings.HasSuffix(s, "px"):
		_, _ = fmt.Sscanf(strings.TrimSuffix(s, "px"), "%f", &value) //nolint:errcheck // zero on parse failure is the documented fallback
		return value / 96.0
	default:
		// Default: treat as millimeters
		_, _ = fmt.Sscanf(s, "%f", &value) //nolint:errcheck // zero on parse failure is the documented fallback
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
