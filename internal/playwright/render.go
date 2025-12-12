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
		return fmt.Errorf("launch playwright: %w (run 'bino playwright install' if this is the first run)", err)
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
	pdfOpts.Margin = &pw.Margin{
		Top:    pw.String("0"),
		Right:  pw.String("0"),
		Bottom: pw.String("0"),
		Left:   pw.String("0"),
	}
	if opts.Orientation != "" {
		landscape := strings.EqualFold(opts.Orientation, "landscape")
		pdfOpts.Landscape = pw.Bool(landscape)
	}

	if _, err := page.PDF(pdfOpts); err != nil {
		return fmt.Errorf("generate pdf: %w", err)
	}

	return nil
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
