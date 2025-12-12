package cli

import (
	"bytes"
	"fmt"
	"html"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/watchers"
)

const defaultPreviewPort = 45678

// newPreviewCommand creates the preview subcommand.
// The preview command respects context cancellation at multiple checkpoints:
//   - During initial content refresh
//   - During file watcher event loop
//   - During HTTP server operation
//   - During subsequent content refreshes
//
// On cancellation:
//   - The file watcher stops processing events
//   - The HTTP server performs graceful shutdown (5s timeout)
//   - The refresh goroutine exits
func newPreviewCommand() *cobra.Command {
	var (
		port    int
		workdir string
		logSQL  bool
	)

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Launch a minimal preview web server",
		Long: strings.TrimSpace(`Watch a workdir for manifest changes, rebuild data via DuckDB,
and serve the rendered report locally. Preview honors runtime env knobs:
  - BNR_MAX_QUERY_ROWS (default 100k)
  - BNR_MAX_QUERY_DURATION_MS (default 60s)
  - BNR_CDN_MAX_BYTES (default 50 MB)
  - BNR_CDN_TIMEOUT_MS (default 10s)

Use --debug for verbose watcher logs and CDN diagnostics.`),
		Example: strings.TrimSpace(`  bino preview
  bino preview --work-dir examples/coffee-report
  BNR_MAX_QUERY_ROWS=10000 bino preview --port 9000`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("preview")
			addr := fmt.Sprintf("127.0.0.1:%d", port)
			logger.Infof("Starting preview server on %s", addr)

			cacheDir, err := previewCacheDir()
			if err != nil {
				return RuntimeError(err)
			}
			logger.Debugf("Using cache directory %s", cacheDir)
			watchDir, err := pipeline.ResolveWorkdir(workdir)
			if err != nil {
				return ConfigError(err)
			}
			logger.Infof("Watching workdir %s", watchDir)

			// Set up SQL query logger if --log-sql is enabled
			var queryLogger func(string)
			if logSQL {
				queryLogger = func(query string) {
					if logx.DebugEnabled(ctx) {
						// Verbose mode: print with extra formatting
						logger.Infof("SQL query:\n%s", query)
					} else {
						// Normal mode: compact output
						logger.Infof("SQL: %s", strings.ReplaceAll(strings.TrimSpace(query), "\n", " "))
					}
				}
			}

			refreshMu := &sync.Mutex{}
			var server *previewhttp.Server

			refresh := func(reason string) error {
				// Check for cancellation before acquiring lock
				if err := ctx.Err(); err != nil {
					return err
				}

				refreshMu.Lock()
				defer refreshMu.Unlock()

				if server == nil {
					return RuntimeErrorf("preview: server not initialized")
				}

				// Check for cancellation after acquiring lock
				if err := ctx.Err(); err != nil {
					return err
				}

				logger.Infof("Rendering report (%s)", reason)
				docs, err := config.LoadDir(ctx, watchDir)
				if err != nil {
					logger.Errorf("Render failed (%s): %v", reason, err)
					return RuntimeError(err)
				}

				// Warn about unresolved environment variables (preview continues with empty values)
				for _, m := range config.CollectMissingEnvVars(docs) {
					logger.Warnf("unresolved environment variable %s in %s", m.VarName, m.File)
				}

				artefacts, err := config.CollectArtefacts(docs)
				if err != nil {
					logger.Errorf("Artefact scan failed (%s): %v", reason, err)
					return RuntimeError(err)
				}
				pipeline.LogArtefactWarnings(logger, artefacts)

				if len(artefacts) == 0 {
					renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{Language: "de", Mode: pipeline.RenderModePreview, QueryLogger: queryLogger})
					if err != nil {
						policy := pipeline.ClassifyInvalidLayout(err, pipeline.RenderModePreview)
						if policy.IsInvalidRoot {
							logger.Errorf("Render blocked (%s): %s", reason, policy.Message)
							setPreviewErrorPage(server, policy.Message, policy.Hint)
							return nil
						}
						logger.Errorf("Render failed (%s): %v", reason, err)
						return RuntimeError(err)
					}
					pipeline.LogDiagnostics(logger.Channel("datasource"), renderResult.Diagnostics)
					frameHTML := withPreviewStyles(renderResult.FrameHTML)
					contextHTML := withPreviewContextStyles(renderResult.ContextHTML)
					server.SetLocalAssets(pipeline.ConvertLocalAssets(renderResult.LocalAssets))
					server.SetContentRoutes(nil)
					server.SetContentFunc(previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8"))
					server.BroadcastContent("/", contextHTML)
					logger.Successf("Content refreshed (%s)", reason)
					return nil
				}

				if len(artefacts) == 1 {
					art := artefacts[0]
					renderResult, err := pipeline.RenderArtefactFrameAndContext(ctx, watchDir, docs, art, queryLogger)
					if err != nil {
						policy := pipeline.ClassifyInvalidLayout(err, pipeline.RenderModePreview)
						if policy.IsInvalidRoot {
							logger.Errorf("Render blocked for artefact %s (%s): %s", art.Document.Name, reason, policy.Message)
							setPreviewErrorPage(server, policy.Message, policy.Hint)
							return nil
						}
						logger.Errorf("Render failed (%s): %v", reason, err)
						return RuntimeError(err)
					}
					pipeline.LogDiagnostics(logger.Channel("datasource"), renderResult.Diagnostics)
					frameHTML := withPreviewStyles(renderResult.FrameHTML)
					contextHTML := withPreviewContextStyles(renderResult.ContextHTML)
					server.SetLocalAssets(pipeline.ConvertLocalAssets(renderResult.LocalAssets))
					server.SetContentRoutes(nil)
					server.SetContentFunc(previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8"))
					server.BroadcastContent("/", contextHTML)
					logger.Successf("Content refreshed (%s)", reason)
					return nil
				}

				routeMap := make(map[string]previewhttp.ContentFunc, len(artefacts))
				entries := make([]previewIndexEntry, 0, len(artefacts))
				allAssets := make([]previewhttp.LocalAsset, 0)
				type artefactPayload struct {
					path        string
					contextHTML []byte
				}
				payloads := make([]artefactPayload, 0, len(artefacts))
				for _, art := range artefacts {
					renderResult, err := pipeline.RenderArtefactFrameAndContext(ctx, watchDir, docs, art, queryLogger)
					if err != nil {
						if pipeline.IsInvalidRootError(err) {
							logger.Errorf("Render blocked for artefact %s (%s): %v", art.Document.Name, reason, err)
							continue
						}
						logger.Errorf("Render failed for %s (%s): %v", art.Document.Name, reason, err)
						return RuntimeError(err)
					}
					pipeline.LogDiagnostics(logger.Channel("datasource").Channel(art.Document.Name), renderResult.Diagnostics)
					frameHTML := withPreviewStyles(renderResult.FrameHTML)
					contextHTML := withPreviewContextStyles(renderResult.ContextHTML)
					allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
					path := "/" + art.Document.Name
					routeMap[path] = previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
					payloads = append(payloads, artefactPayload{path: path, contextHTML: contextHTML})
					entries = append(entries, previewIndexEntry{
						Name:        art.Document.Name,
						Title:       art.Spec.Title,
						Description: art.Spec.Description,
						Language:    art.Spec.Language,
						Author:      art.Spec.Author,
					})
				}

				server.SetLocalAssets(allAssets)
				server.SetContentRoutes(routeMap)
				server.SetContentFunc(previewhttp.StaticContent(buildPreviewIndex(entries), "text/html; charset=utf-8"))
				for _, payload := range payloads {
					server.BroadcastContent(payload.path, payload.contextHTML)
				}
				logger.Successf("Content refreshed (%s)", reason)
				return nil
			}

			refreshCh := make(chan string, 1)
			enqueue := func(reason string) {
				select {
				case refreshCh <- reason:
				default:
				}
			}

			watchLog := logger.Channel("watcher")
			watcher, err := watchers.NewWatcher(watchers.Config{
				Root:   watchDir,
				Logger: watchLog,
				Handler: func(evt watchers.Event) {
					watchLog.Infof("File updated %s (%s)", evt.RelativePath, evt.Op)
					enqueue(fmt.Sprintf("change %s", evt.RelativePath))
				},
			})
			if err != nil {
				return RuntimeError(err)
			}
			defer watcher.Close()
			go watcher.Run(ctx)

			server, err = previewhttp.New(previewhttp.Config{
				ListenAddr: addr,
				CacheDir:   cacheDir,
				Logger:     logger.Channel("server"),
			})
			if err != nil {
				return RuntimeError(err)
			}

			if err := refresh("initial load"); err != nil {
				return err
			}

			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case reason := <-refreshCh:
						if err := refresh(reason); err != nil {
							logger.Errorf("Refresh failed: %v", err)
						}
					}
				}
			}()

			url := server.URL()
			logger.Successf("Serving preview at %s", url)

			if err := openBrowser(url); err != nil {
				logger.Warnf("Unable to open browser automatically: %v", err)
			}

			logger.Infof("Preview running * press Ctrl+C to stop")
			if err := server.Start(ctx); err != nil {
				return RuntimeError(err)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", defaultPreviewPort, "Port to run the preview server on")
	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory to watch for changes")
	cmd.Flags().BoolVar(&logSQL, "log-sql", false, "Log all executed SQL queries to terminal")

	return cmd
}

type previewIndexEntry struct {
	Name        string
	Title       string
	Description string
	Language    string
	Author      string
}

func buildPreviewIndex(entries []previewIndexEntry) []byte {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n  <meta charset=\"utf-8\">\n  <title>Rainbow Preview Artefacts</title>\n  <style>\n    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; margin: 2rem auto; max-width: 720px; padding: 0 1rem; }\n    h1 { font-size: 1.75rem; margin-bottom: 0.5rem; }\n    p.description { color: #555; margin-top: 0; }\n    ul { list-style: none; padding: 0; }\n    li { border: 1px solid #e0e0e0; border-radius: 8px; margin-bottom: 1rem; padding: 1rem; }\n    a { color: #0366d6; text-decoration: none; font-weight: 600; }\n    a:hover { text-decoration: underline; }\n    .meta { color: #666; font-size: 0.9rem; margin-top: 0.35rem; }\n  </style>\n</head>\n<body>\n  <h1>Available Report Artefacts</h1>\n")
	if len(entries) == 0 {
		b.WriteString("  <p class=\"description\">No ReportArtefact manifests found. Define one to preview individual reports.</p>\n</body>\n</html>")
		return []byte(b.String())
	}
	b.WriteString("  <p class=\"description\">Select a report to preview. Each link maps to /&lt;metadata.name&gt;.</p>\n  <ul>\n")
	for _, entry := range entries {
		link := "/" + entry.Name
		b.WriteString("    <li>\n      <a href=\"")
		b.WriteString(html.EscapeString(link))
		b.WriteString("\">")
		b.WriteString(html.EscapeString(entry.Name))
		b.WriteString("</a>\n")
		if entry.Title != "" {
			b.WriteString("      <div class=\"meta\">")
			b.WriteString(html.EscapeString(entry.Title))
			b.WriteString("</div>\n")
		}
		if entry.Description != "" || entry.Language != "" || entry.Author != "" {
			b.WriteString("      <div class=\"meta\">")
			var parts []string
			if entry.Description != "" {
				parts = append(parts, entry.Description)
			}
			if entry.Language != "" {
				parts = append(parts, "lang: "+entry.Language)
			}
			if entry.Author != "" {
				parts = append(parts, "author: "+entry.Author)
			}
			b.WriteString(html.EscapeString(strings.Join(parts, " • ")))
			b.WriteString("</div>\n")
		}
		b.WriteString("    </li>\n")
	}
	b.WriteString("  </ul>\n</body>\n</html>")
	return []byte(b.String())
}

func setPreviewErrorPage(server *previewhttp.Server, message, hint string) {
	if server == nil {
		return
	}
	content := buildPreviewErrorPage(message, hint)
	server.SetLocalAssets(nil)
	server.SetContentRoutes(nil)
	server.SetContentFunc(previewhttp.StaticContent(append([]byte(nil), content...), "text/html; charset=utf-8"))
	server.BroadcastContent("/", content)
}

func buildPreviewErrorPage(message, hint string) []byte {
	if message == "" {
		message = "An invalid layout configuration prevented preview rendering."
	}
	if hint == "" {
		hint = "Ensure at least one LayoutPage is defined and referenced by your report artefact."
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n  <meta charset=\"utf-8\">\n  <title>Rainbow Preview Error</title>\n  <style>body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background:#fef2f2; color:#7f1d1d; display:flex; align-items:center; justify-content:center; min-height:100vh; margin:0; } bn-context { display:flex; align-items:center; justify-content:center; width:100%; } .card { background:#fff; border:1px solid #fecaca; border-radius:12px; padding:2rem; max-width:520px; box-shadow:0 10px 30px rgba(185, 28, 28, 0.15);} h1 { margin-top:0; font-size:1.5rem;} p { line-height:1.5; } </style>\n</head>\n<body>\n  <bn-context>\n    <div class=\"card\">\n      <h1>Cannot Render Preview</h1>\n      <p>")
	b.WriteString(html.EscapeString(message))
	b.WriteString("</p>\n      <p>")
	b.WriteString(html.EscapeString(hint))
	b.WriteString("</p>\n    </div>\n  </bn-context>\n</body>\n</html>")
	return []byte(b.String())
}

var (
	previewStyleMarker = []byte("bn-preview-style")
	previewStyleBlock  = []byte(`
	<style id="bn-preview-style">
		:root {
			color-scheme: light;
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
			background-color: #f5f6fb;
		}
		body {
			margin: 0;
			min-height: 100vh;
			background: #f5f6fb;
			font-family: inherit;
			color: #111827;
			display: flex;
			justify-content: center;
			box-sizing: border-box;
		}
		bn-context {
			display: flex;
			flex-direction: column;
			align-items: center;
			gap: 1.75rem;
		}
		
	bn-layout-page {
			box-sizing: border-box;
			background: #ffffff;
			box-shadow: 0 14px 40px rgba(15, 23, 42, 0.12);
			
		}
		@media (max-width: 768px) {
			body {
				padding: 1.5rem;
			}
		}
	</style>
	<script id="bn-preview-runtime">
	(function () {
		if (!window.EventSource || window.__bnPreviewRuntime) {
			return;
		}
		window.__bnPreviewRuntime = true;

		var parser = new DOMParser();
		var normalizedPath = normalizePath(window.location.pathname || "/");
		var source = new EventSource("/__preview/events");
		var sseReady = false;
		var engineReady = false;

		function normalizePath(value) {
			if (!value) {
				return "/";
			}
			return value.charAt(0) === "/" ? value : "/" + value;
		}

		function decodeBase64(input) {
			if (!input) {
				return "";
			}
			try {
				return window.atob(input);
			} catch (err) {
				console.error("bn preview: decode failed", err);
				return "";
			}
		}

		function swapContext(html) {
			if (!html) {
				return;
			}
			var doc = parser.parseFromString(html, "text/html");
			var nextCtx = doc.querySelector("bn-context");
			var currentCtx = document.querySelector("bn-context");
			if (!nextCtx || !currentCtx) {
				return;
			}
			currentCtx.replaceWith(nextCtx);
			try {
				var detail = { path: normalizedPath };
				document.dispatchEvent(new CustomEvent("bn-preview:content-updated", { detail: detail }));
			} catch (eventErr) {
				console.debug("bn preview: custom event skipped", eventErr);
			}
		}

		function fetchInitialContext() {
			fetch("/__preview/context?path=" + encodeURIComponent(normalizedPath))
				.then(function (resp) {
					if (!resp.ok) {
						console.debug("bn preview: context not available yet");
						return null;
					}
					return resp.text();
				})
				.then(function (html) {
					if (html) {
						swapContext(html);
					}
				})
				.catch(function (err) {
					console.error("bn preview: fetch context failed", err);
				});
		}

		function tryFetchContext() {
			if (sseReady && engineReady) {
				fetchInitialContext();
			}
		}

		// Wait for template engine to be ready (bn-context custom element defined)
		function waitForEngine() {
			if (customElements.get("bn-context")) {
				engineReady = true;
				tryFetchContext();
				return;
			}
			// Poll until bn-context is defined
			customElements.whenDefined("bn-context").then(function () {
				engineReady = true;
				tryFetchContext();
			});
		}

		// Start waiting for engine immediately
		waitForEngine();

		// Mark SSE ready when connection is established
		source.addEventListener("ready", function () {
			sseReady = true;
			tryFetchContext();
		});

		source.addEventListener("content", function (event) {
			try {
				var payload = JSON.parse(event.data || "{}");
				if (!payload || normalizePath(payload.path) !== normalizedPath) {
					return;
				}
				var html = decodeBase64(payload.htmlBase64);
				swapContext(html);
			} catch (err) {
				console.error("bn preview: apply failed", err);
			}
		});

		window.addEventListener("beforeunload", function () {
			source.close();
		});
	})();
	</script>
`)
)

// withPreviewStyles injects a lightweight set of layout styles so preview pages are centered
// and readable without relying on external assets.
func withPreviewStyles(doc []byte) []byte {
	if len(doc) == 0 || bytes.Contains(doc, previewStyleMarker) {
		return doc
	}
	headClose := []byte("</head>")
	idx := bytes.Index(doc, headClose)
	if idx == -1 {
		return doc
	}
	updated := make([]byte, 0, len(doc)+len(previewStyleBlock))
	updated = append(updated, doc[:idx]...)
	updated = append(updated, previewStyleBlock...)
	updated = append(updated, doc[idx:]...)
	return updated
}

// withPreviewContextStyles returns the context HTML as-is for SSE delivery.
// The context HTML is a standalone <bn-context> block that replaces the existing
// one in the DOM. Preview styles are already in the frame's <head>, so no
// additional injection is needed here.
func withPreviewContextStyles(ctx []byte) []byte {
	return ctx
}

func openBrowser(url string) error {
	// Validate URL to prevent command injection
	if err := validateBrowserURL(url); err != nil {
		return err
	}

	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}

	return command.Start()
}

// validateBrowserURL ensures the URL is safe to pass to system browser commands.
// This prevents potential command injection attacks.
func validateBrowserURL(url string) error {
	if url == "" {
		return fmt.Errorf("url cannot be empty")
	}

	// Only allow http and https schemes for browser opening
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("url must use http or https scheme")
	}

	// Reject URLs with potentially dangerous characters that could be
	// interpreted as shell metacharacters
	dangerousChars := []string{";", "|", "&", "`", "$", "(", ")", "<", ">", "\n", "\r"}
	for _, char := range dangerousChars {
		if strings.Contains(url, char) {
			return fmt.Errorf("url contains invalid character: %q", char)
		}
	}

	return nil
}

func previewCacheDir() (string, error) {
	return pathutil.CacheDir("cdn")
}
