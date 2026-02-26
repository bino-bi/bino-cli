package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/spec"
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
		port           int
		workdir        string
		logSQL         bool
		enableLint     bool
		dataValidation string
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

Use --verbose (-v) for verbose watcher logs and CDN diagnostics.`),
		Example: strings.TrimSpace(`  bino preview
  bino preview --work-dir examples/coffee-report
  BNR_MAX_QUERY_ROWS=10000 bino preview --port 9000`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("preview")

			cacheDir, err := previewCacheDir()
			if err != nil {
				return RuntimeError(err)
			}
			logger.Debugf("Using cache directory %s", cacheDir)

			// Find project root (directory containing bino.toml)
			watchDir, err := pipeline.ResolveProjectRoot(workdir)
			if err != nil {
				return ConfigError(err)
			}

			// Load project config for defaults
			projectCfg, cfgErr := pathutil.LoadProjectConfig(watchDir)
			if cfgErr != nil {
				logger.Debugf("Could not load bino.toml defaults: %v", cfgErr)
				projectCfg = &pathutil.ProjectConfig{}
			}

			// Apply environment variables from TOML (actual env vars take precedence)
			projectCfg.Preview.Env.Apply(func(key, tomlVal, envVal string) {
				logger.Infof("Environment variable %s overrides bino.toml (%q -> %q)", key, tomlVal, envVal)
			})

			// Create hook runner
			hookRunner := hooks.NewRunner(
				hooks.Resolve(projectCfg.Hooks, projectCfg.Preview.Hooks, logger.Channel("hooks")),
				logger.Channel("hooks"), watchDir,
			)

			// Resolve arguments with TOML defaults
			resolver := pathutil.NewArgResolver(cmd, projectCfg.Preview.Args, func(format string, args ...any) {
				logger.Infof(format, args...)
			})

			port = resolver.ResolveInt("port", "port", port)
			logSQL = resolver.ResolveBool("log-sql", "log-sql", logSQL)
			enableLint = resolver.ResolveBool("lint", "lint", enableLint)

			// Resolve template engine version
			engineVersion := projectCfg.EngineVersion
			engineVersionPinned := engineVersion != ""
			engineMgr, err := engine.NewManager()
			if err != nil {
				return RuntimeError(fmt.Errorf("initialize engine manager: %w", err))
			}
			engineInfo, err := engineMgr.EnsureVersion(ctx, engineVersion)
			if err != nil {
				return ConfigError(fmt.Errorf("template engine: %w", err))
			}
			engineVersion = engineInfo.Version
			logger.Infof("Using template engine %s", engineVersion)
			if !engineVersionPinned {
				logger.Warnf("No engine-version set in bino.toml - using latest local version. Pin a version for reproducible builds.")
			}

			addr := fmt.Sprintf("127.0.0.1:%d", port)
			logger.Infof("Starting preview server on %s", addr)
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

			// Resolve data validation mode
			dataValidation = resolver.ResolveString("data-validation", "data-validation", dataValidation)
			dataValidationMode := dataset.DataValidationWarn // default
			switch dataValidation {
			case "fail":
				dataValidationMode = dataset.DataValidationFail
			case "warn":
				dataValidationMode = dataset.DataValidationWarn
			case "off":
				dataValidationMode = dataset.DataValidationOff
			default:
				return ConfigErrorf("invalid --data-validation value %q, expected 'fail', 'warn', or 'off'", dataValidation)
			}
			dataValidationSampleSize := dataset.GetDataValidationSampleSize()

			previewHookEnv := hooks.HookEnv{
				Mode:     "preview",
				Workdir:  watchDir,
				ReportID: projectCfg.ReportID,
				Verbose:  logx.DebugEnabled(ctx),
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

				// Run pre-refresh hook (on failure: log and continue)
				refreshHookEnv := previewHookEnv
				refreshHookEnv.RefreshReason = reason
				if err := hookRunner.Run(ctx, "pre-refresh", refreshHookEnv); err != nil {
					logger.Errorf("pre-refresh hook failed: %v", err)
					return nil
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

				// Run lint rules if enabled
				if enableLint {
					lintDocs := configDocsToLintDocs(docs)
					runner := lint.NewDefaultRunner()
					findings := runner.Run(ctx, lintDocs)
					for _, f := range findings {
						relPath := pathutil.RelPath(watchDir, f.File)
						loc := relPath
						if f.DocIdx > 0 {
							loc = fmt.Sprintf("%s #%d", relPath, f.DocIdx)
						}
						logger.Warnf("[%s] %s: %s", f.RuleID, loc, f.Message)
					}
				}

				artefacts, err := config.CollectArtefacts(docs)
				if err != nil {
					logger.Errorf("Artefact scan failed (%s): %v", reason, err)
					return RuntimeError(err)
				}
				pipeline.LogArtefactWarnings(logger, artefacts)

				documentArtefacts, err := config.CollectDocumentArtefacts(docs)
				if err != nil {
					logger.Errorf("DocumentArtefact scan failed (%s): %v", reason, err)
					return RuntimeError(err)
				}
				pipeline.LogDocumentArtefactWarnings(logger, documentArtefacts)

				// Build artefact info list for header dropdown
				artefactInfos := make([]previewArtefactInfo, 0, len(artefacts)+len(documentArtefacts))
				for _, art := range artefacts {
					artefactInfos = append(artefactInfos, previewArtefactInfo{
						Name:   art.Document.Name,
						Title:  art.Spec.Title,
						Format: art.Spec.Format,
						IsDoc:  false,
					})
				}
				for _, docArt := range documentArtefacts {
					artefactInfos = append(artefactInfos, previewArtefactInfo{
						Name:   docArt.Document.Name,
						Title:  docArt.Spec.Title,
						Format: docArt.Spec.Format,
						IsDoc:  true,
					})
				}

				// Always render "All Pages" view for "/" route - this is the default view
				// that shows all LayoutPages without any artefact filtering
				allPagesResult, err := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{
					Workdir:                  watchDir,
					Language:                 "de",
					Mode:                     pipeline.RenderModePreview,
					EngineVersion:            engineVersion,
					QueryLogger:              queryLogger,
					DataValidation:           dataValidationMode,
					DataValidationSampleSize: dataValidationSampleSize,
				})
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
				pipeline.LogDiagnostics(logger.Channel("datasource"), allPagesResult.Diagnostics)

				routeMap := make(map[string]previewhttp.ContentFunc, len(artefacts)+len(documentArtefacts)+1)
				allAssets := make([]previewhttp.LocalAsset, 0)
				type artefactPayload struct {
					path        string
					contextHTML []byte
				}
				payloads := make([]artefactPayload, 0, len(artefacts)+1)

				// Add "All Pages" route (default "/" view)
				allPagesFrameHTML := withPreviewHeader(withPreviewStyles(allPagesResult.FrameHTML), artefactInfos, "/")
				pageMeta := buildPageMetadata(docs, artefacts)
				allPagesContextHTML := withPreviewPageMetadata(withPreviewContextStyles(allPagesResult.ContextHTML), pageMeta)
				allAssets = append(allAssets, pipeline.ConvertLocalAssets(allPagesResult.LocalAssets)...)
				payloads = append(payloads, artefactPayload{path: "/", contextHTML: allPagesContextHTML})

				// Render each ReportArtefact
				for _, art := range artefacts {
					renderResult, err := pipeline.RenderArtefactFrameAndContextWithOptions(ctx, watchDir, docs, art, pipeline.FrameRenderOptions{
						QueryLogger:              queryLogger,
						EngineVersion:            engineVersion,
						DataValidation:           dataValidationMode,
						DataValidationSampleSize: dataValidationSampleSize,
					})
					if err != nil {
						if pipeline.IsInvalidRootError(err) {
							logger.Errorf("Render blocked for artefact %s (%s): %v", art.Document.Name, reason, err)
							continue
						}
						logger.Errorf("Render failed for %s (%s): %v", art.Document.Name, reason, err)
						return RuntimeError(err)
					}
					pipeline.LogDiagnostics(logger.Channel("datasource").Channel(art.Document.Name), renderResult.Diagnostics)
					path := "/" + art.Document.Name
					frameHTML := withPreviewHeader(withPreviewStyles(renderResult.FrameHTML), artefactInfos, path)
					contextHTML := withPreviewContextStyles(renderResult.ContextHTML)
					allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
					routeMap[path] = previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
					payloads = append(payloads, artefactPayload{path: path, contextHTML: contextHTML})
				}

				// Render each DocumentArtefact
				for _, docArt := range documentArtefacts {
					renderResult, err := pipeline.RenderDocumentArtefactHTML(ctx, watchDir, docArt, pipeline.DocumentArtefactRenderOptions{
						EngineVersion: engineVersion,
					})
					if err != nil {
						logger.Errorf("Render failed for DocumentArtefact %s (%s): %v", docArt.Document.Name, reason, err)
						continue
					}
					allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
					path := "/doc/" + docArt.Document.Name
					// DocumentArtefacts get header injected too
					frameHTML := withPreviewHeader(renderResult.HTML, artefactInfos, path)
					routeMap[path] = previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
				}

				server.SetLocalAssets(allAssets)
				server.SetContentRoutes(routeMap)
				server.SetContentFunc(previewhttp.StaticContent(append([]byte(nil), allPagesFrameHTML...), "text/html; charset=utf-8"))
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

			// Run pre-preview hook (once, before initial refresh)
			previewHookEnv.ListenAddr = addr
			if err := hookRunner.Run(ctx, "pre-preview", previewHookEnv); err != nil {
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
	cmd.Flags().BoolVar(&enableLint, "lint", false, "Run lint rules on each refresh")
	cmd.Flags().StringVar(&dataValidation, "data-validation", "warn",
		"Data validation mode: 'fail' treats errors as fatal, 'warn' logs and continues, 'off' skips validation")

	return cmd
}

// previewArtefactInfo holds metadata about an artefact for the preview header dropdown.
type previewArtefactInfo struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	Format string `json:"format"`
	IsDoc  bool   `json:"isDoc"` // true for DocumentArtefact
}

// previewPageMeta holds metadata about a LayoutPage for the "All Pages" preview overlay.
type previewPageMeta struct {
	Name        string   `json:"name"`
	Constraints []string `json:"constraints,omitempty"`
	Artefacts   []string `json:"artefacts,omitempty"`
}

// buildPageMetadata computes per-page metadata (constraints and artefact usage) for the "All Pages" view.
func buildPageMetadata(docs []config.Document, artefacts []config.Artefact) []previewPageMeta {
	// Collect LayoutPage names and their constraints
	type pageInfo struct {
		name        string
		constraints []string
	}
	var pages []pageInfo
	for _, doc := range docs {
		if doc.Kind != "LayoutPage" {
			continue
		}
		var cs []string
		for _, c := range doc.Constraints {
			cs = append(cs, formatConstraint(c))
		}
		pages = append(pages, pageInfo{name: doc.Name, constraints: cs})
	}

	// Build page-name → artefact-names mapping
	pageArtefacts := make(map[string][]string)
	for _, art := range artefacts {
		refs := art.Spec.LayoutPages
		if len(refs) == 0 {
			// No layoutPages specified means all pages are included
			for _, p := range pages {
				pageArtefacts[p.name] = appendUnique(pageArtefacts[p.name], art.Document.Name)
			}
			continue
		}
		for _, ref := range refs {
			pageName := strings.TrimSpace(ref.Page)
			if pageName == "" {
				continue
			}
			if pageName == "*" || strings.ContainsAny(pageName, "*?[") {
				// Glob pattern: match against all page names
				for _, p := range pages {
					matched, _ := path.Match(pageName, p.name)
					if matched {
						pageArtefacts[p.name] = appendUnique(pageArtefacts[p.name], art.Document.Name)
					}
				}
			} else {
				pageArtefacts[pageName] = appendUnique(pageArtefacts[pageName], art.Document.Name)
			}
		}
	}

	// Build result
	result := make([]previewPageMeta, 0, len(pages))
	for _, p := range pages {
		result = append(result, previewPageMeta{
			Name:        p.name,
			Constraints: p.constraints,
			Artefacts:   pageArtefacts[p.name],
		})
	}
	return result
}

// formatConstraint formats a parsed constraint as a human-readable string.
func formatConstraint(c *spec.Constraint) string {
	if c.Raw != "" {
		return c.Raw
	}
	switch c.Operator {
	case "in", "not-in":
		return c.Left + " " + c.Operator + " [" + strings.Join(c.Values, ", ") + "]"
	default:
		return c.Left + " " + c.Operator + " " + c.Right
	}
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// buildPreviewHeader generates the HTML for the sticky preview header with artefact dropdown.
func buildPreviewHeader(artefacts []previewArtefactInfo, currentPath string) string {
	var b strings.Builder

	b.WriteString(`<div id='bn-preview-header'>`)
	b.WriteString(`<span id='bn-preview-title'>bino preview</span>`)
	b.WriteString(`<select id='bn-preview-artefact-select'>`)

	// "All Pages" option
	allSelected := ""
	if currentPath == "/" {
		allSelected = ` selected`
	}
	b.WriteString(`<option value='/'` + allSelected + `>All Pages</option>`)

	// Separate ReportArtefacts and DocumentArtefacts
	var reportArts, docArts []previewArtefactInfo
	for _, art := range artefacts {
		if art.IsDoc {
			docArts = append(docArts, art)
		} else {
			reportArts = append(reportArts, art)
		}
	}

	// ReportArtefacts
	if len(reportArts) > 0 {
		b.WriteString(`<optgroup label='Report Artefacts'>`)
		for _, art := range reportArts {
			path := "/" + art.Name
			selected := ""
			if path == currentPath {
				selected = ` selected`
			}
			label := art.Name
			if art.Title != "" {
				label = art.Title + " (" + art.Name + ")"
			}
			b.WriteString(`<option value='`)
			b.WriteString(html.EscapeString(path))
			b.WriteString(`'`)
			b.WriteString(selected)
			b.WriteString(`>`)
			b.WriteString(html.EscapeString(label))
			b.WriteString(`</option>`)
		}
		b.WriteString(`</optgroup>`)
	}

	// DocumentArtefacts
	if len(docArts) > 0 {
		b.WriteString(`<optgroup label='Document Artefacts'>`)
		for _, art := range docArts {
			path := "/doc/" + art.Name
			selected := ""
			if path == currentPath {
				selected = ` selected`
			}
			label := art.Name
			if art.Title != "" {
				label = art.Title + " (" + art.Name + ")"
			}
			b.WriteString(`<option value='`)
			b.WriteString(html.EscapeString(path))
			b.WriteString(`'`)
			b.WriteString(selected)
			b.WriteString(`>`)
			b.WriteString(html.EscapeString(label))
			b.WriteString(`</option>`)
		}
		b.WriteString(`</optgroup>`)
	}

	b.WriteString(`</select>`)
	b.WriteString(`</div>`)

	return b.String()
}

// withPreviewHeader injects the preview header into the HTML document after <body>.
func withPreviewHeader(doc []byte, artefacts []previewArtefactInfo, currentPath string) []byte {
	if len(doc) == 0 {
		return doc
	}

	// Find <body> or <body ...> tag
	bodyIdx := bytes.Index(doc, []byte("<body>"))
	insertAt := -1
	if bodyIdx != -1 {
		insertAt = bodyIdx + len("<body>")
	} else {
		// Try <body with attributes
		bodyIdx = bytes.Index(doc, []byte("<body "))
		if bodyIdx != -1 {
			// Find the closing >
			closeIdx := bytes.Index(doc[bodyIdx:], []byte(">"))
			if closeIdx != -1 {
				insertAt = bodyIdx + closeIdx + 1
			}
		}
	}

	if insertAt == -1 {
		return doc
	}

	header := buildPreviewHeader(artefacts, currentPath)

	updated := make([]byte, 0, len(doc)+len(header))
	updated = append(updated, doc[:insertAt]...)
	updated = append(updated, []byte(header)...)
	updated = append(updated, doc[insertAt:]...)

	return updated
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
		*, *::before, *::after {
			box-sizing: border-box;
		}
		html {
			display: block !important;
			overflow-x: auto;
		}
		body {
			display: block !important;
			justify-content: initial !important;
			margin: 0;
			min-height: 100vh;
			min-width: fit-content;
			background: #f5f6fb;
			font-family: inherit;
			color: #111827;
			padding: 1.5rem;
			padding-top: 3.5rem;
		}
		bn-context {
			display: flex;
			flex-direction: column;
			align-items: center;
			gap: 1.75rem;
			width: fit-content;
			min-width: 100%;
			margin: 0 auto;
		}
		bn-layout-page {
			box-sizing: border-box;
			background: #ffffff;
			box-shadow: 0 14px 40px rgba(15, 23, 42, 0.12);
			flex-shrink: 0;
		}
		@media (min-width: 769px) {
			body {
				padding: 2rem;
				padding-top: 3.5rem;
			}
		}
		/* Preview header styles */
		#bn-preview-header {
			position: fixed;
			top: 0;
			left: 0;
			right: 0;
			z-index: 10000;
			background: #ffffff;
			border-bottom: 1px solid #e5e7eb;
			padding: 0.5rem 1rem;
			display: flex;
			align-items: center;
			gap: 1rem;
			font-size: 0.875rem;
			box-shadow: 0 1px 3px rgba(0,0,0,0.05);
		}
		#bn-preview-title {
			font-weight: 600;
			color: #374151;
		}
		#bn-preview-artefact-select {
			padding: 0.375rem 0.625rem;
			border-radius: 6px;
			border: 1px solid #d1d5db;
			background: #f9fafb;
			font-size: 0.875rem;
			color: #374151;
			cursor: pointer;
			min-width: 200px;
		}
		#bn-preview-artefact-select:hover {
			border-color: #9ca3af;
		}
		#bn-preview-artefact-select:focus {
			outline: none;
			border-color: #3b82f6;
			box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
		}
		/* Page info bar for "All Pages" view - placed as sibling before bn-layout-page */
		.bn-page-info {
			display: flex;
			align-items: center;
			gap: 0.5rem;
			flex-wrap: wrap;
			padding: 5px 10px;
			background: #f8fafc;
			border: 1px solid #e2e8f0;
			border-radius: 6px 6px 0 0;
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
			font-size: 11px;
			line-height: 1.4;
			margin-bottom: -1.75rem;
			align-self: center;
		}
		.bn-page-info-name {
			font-weight: 600;
			color: #374151;
			margin-right: 2px;
		}
		.bn-page-info-pill {
			display: inline-block;
			padding: 1px 6px;
			border-radius: 4px;
			font-size: 10px;
			font-weight: 500;
			white-space: nowrap;
		}
		.bn-page-info-pill.constraint {
			background: #ede9fe;
			color: #5b21b6;
			border: 1px solid #c4b5fd;
		}
		.bn-page-info-pill.artefact {
			background: #dbeafe;
			color: #1e40af;
			border: 1px solid #93c5fd;
		}
		.bn-page-info-label {
			color: #6b7280;
			font-size: 10px;
			margin-left: 4px;
		}
		/* Preview-only error indicator styles */
		[has-error]::before, [has-errors]::before {
			content: "⚠";
			position: absolute;
			top: 2px;
			right: 2px;
			width: 18px;
			height: 18px;
			background: #f59e0b;
			color: #fff;
			font-size: 12px;
			border-radius: 50%;
			display: flex;
			align-items: center;
			justify-content: center;
			z-index: 10000;
			cursor: pointer;
		}
		#bn-error-panel {
			position: fixed;
			bottom: 0;
			left: 0;
			right: 0;
			max-height: 200px;
			overflow-y: auto;
			background: #fffbeb;
			border-top: 2px solid #f59e0b;
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
			font-size: 13px;
			z-index: 10001;
			box-shadow: 0 -4px 12px rgba(0,0,0,0.1);
		}
		#bn-error-panel-header {
			display: flex;
			justify-content: space-between;
			align-items: center;
			padding: 8px 12px;
			background: #fef3c7;
			border-bottom: 1px solid #fcd34d;
			font-weight: 600;
			color: #92400e;
		}
		#bn-error-panel-close {
			background: none;
			border: none;
			font-size: 18px;
			cursor: pointer;
			color: #92400e;
			padding: 0 4px;
		}
		#bn-error-panel-close:hover {
			color: #78350f;
		}
		#bn-error-list {
			list-style: none;
			margin: 0;
			padding: 0;
		}
		.bn-error-item {
			padding: 8px 12px;
			border-bottom: 1px solid #fde68a;
			cursor: pointer;
			display: flex;
			align-items: flex-start;
			gap: 8px;
		}
		.bn-error-item:hover {
			background: #fef3c7;
		}
		.bn-error-item:last-child {
			border-bottom: none;
		}
		.bn-error-badge {
			flex-shrink: 0;
			padding: 2px 6px;
			border-radius: 4px;
			font-size: 11px;
			font-weight: 600;
			text-transform: uppercase;
		}
		.bn-error-badge.warning {
			background: #fcd34d;
			color: #78350f;
		}
		.bn-error-badge.error {
			background: #fca5a5;
			color: #7f1d1d;
		}
		.bn-error-message {
			color: #78350f;
		}
		.bn-error-highlight {
			animation: bn-error-pulse 0.6s ease-out;
		}
		@keyframes bn-error-pulse {
			0% { outline-color: #f59e0b; outline-width: 2px; }
			50% { outline-color: #dc2626; outline-width: 4px; }
			100% { outline-color: #f59e0b; outline-width: 2px; }
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

		// Click-to-source: when user clicks on a component with data-bino-kind,
		// post a message to parent window (VS Code webview) to reveal the source.
		document.addEventListener("click", function (e) {
			// Only handle clicks with Cmd (Mac) or Ctrl (Windows/Linux) held
			if (!e.metaKey && !e.ctrlKey) {
				return;
			}
			var el = e.target.closest("[data-bino-kind]");
			if (!el) {
				return;
			}
			var kind = el.getAttribute("data-bino-kind");
			var name = el.getAttribute("data-bino-name") || "";
			var ref = el.getAttribute("data-bino-ref") || "";
			var msg = {
				type: "bino:revealSource",
				kind: kind,
				name: name,
				ref: ref
			};
			// Post to parent window (for iframe in VS Code webview)
			if (window.parent && window.parent !== window) {
				window.parent.postMessage(msg, "*");
			}
			e.preventDefault();
			e.stopPropagation();
		});

		// Error panel functionality for preview-only validation feedback
		var errorPanelVisible = false;
		var errorPanelElement = null;
		var scanDebounceTimer = null;

		function parseErrors(attrValue) {
			if (!attrValue) return [];
			try {
				var parsed = JSON.parse(attrValue);
				return Array.isArray(parsed) ? parsed : [];
			} catch (e) {
				console.debug("bn preview: failed to parse error attribute", e);
				return [];
			}
		}

		function collectAllErrors() {
			var results = [];
			var elements = document.querySelectorAll("[has-error], [has-errors]");
			elements.forEach(function (el) {
				var attrValue = el.getAttribute("has-error") || el.getAttribute("has-errors");
				var errors = parseErrors(attrValue);
				errors.forEach(function (err) {
					results.push({ element: el, error: err });
				});
			});
			return results;
		}

		function createErrorPanel() {
			if (errorPanelElement) return errorPanelElement;
			var panel = document.createElement("div");
			panel.id = "bn-error-panel";
			panel.innerHTML = '<div id="bn-error-panel-header"><span id="bn-error-count"></span><button id="bn-error-panel-close" title="Close">&times;</button></div><ul id="bn-error-list"></ul>';
			document.body.appendChild(panel);
			panel.querySelector("#bn-error-panel-close").addEventListener("click", function () {
				hideErrorPanel();
			});
			errorPanelElement = panel;
			return panel;
		}

		function showErrorPanel(errors) {
			var panel = createErrorPanel();
			var countEl = panel.querySelector("#bn-error-count");
			var listEl = panel.querySelector("#bn-error-list");
			countEl.textContent = errors.length + " warning" + (errors.length !== 1 ? "s" : "") + " found";
			listEl.innerHTML = "";
			errors.forEach(function (item, idx) {
				var li = document.createElement("li");
				li.className = "bn-error-item";
				li.innerHTML = '<span class="bn-error-badge ' + (item.error.type || "warning") + '">' + (item.error.type || "warning") + '</span><span class="bn-error-message">' + escapeHtml(item.error.message || item.error.id || "Unknown error") + '</span>';
				li.addEventListener("click", function () {
					scrollToElement(item.element);
				});
				listEl.appendChild(li);
			});
			panel.style.display = "block";
			errorPanelVisible = true;
		}

		function hideErrorPanel() {
			if (errorPanelElement) {
				errorPanelElement.style.display = "none";
			}
			errorPanelVisible = false;
		}

		function scrollToElement(el) {
			if (!el) return;
			el.scrollIntoView({ behavior: "smooth", block: "center" });
			el.classList.remove("bn-error-highlight");
			void el.offsetWidth; // force reflow
			el.classList.add("bn-error-highlight");
			setTimeout(function () {
				el.classList.remove("bn-error-highlight");
			}, 700);
		}

		function escapeHtml(str) {
			var div = document.createElement("div");
			div.textContent = str;
			return div.innerHTML;
		}

		function scanForErrors() {
			var errors = collectAllErrors();
			if (errors.length > 0) {
				showErrorPanel(errors);
			} else {
				hideErrorPanel();
			}
		}

		function debouncedScan() {
			if (scanDebounceTimer) {
				clearTimeout(scanDebounceTimer);
			}
			scanDebounceTimer = setTimeout(function () {
				scanForErrors();
			}, 100);
		}

		// Observe DOM for dynamically added/modified has-error attributes
		var errorObserver = new MutationObserver(function (mutations) {
			var shouldScan = false;
			mutations.forEach(function (m) {
				if (m.type === "attributes" && (m.attributeName === "has-error" || m.attributeName === "has-errors")) {
					shouldScan = true;
				}
				if (m.type === "childList" && m.addedNodes.length > 0) {
					m.addedNodes.forEach(function (node) {
						if (node.nodeType === 1 && (node.hasAttribute && (node.hasAttribute("has-error") || node.hasAttribute("has-errors")))) {
							shouldScan = true;
						}
						if (node.nodeType === 1 && node.querySelector && node.querySelector("[has-error], [has-errors]")) {
							shouldScan = true;
						}
					});
				}
			});
			if (shouldScan) {
				debouncedScan();
			}
		});

		// Start observing when DOM is ready
		function startErrorObserver() {
			errorObserver.observe(document.body, {
				childList: true,
				subtree: true,
				attributes: true,
				attributeFilter: ["has-error", "has-errors"]
			});
			// Initial scan
			debouncedScan();
		}

		// Listen for content updates to rescan
		document.addEventListener("bn-preview:content-updated", function () {
			debouncedScan();
		});

		// Start observer when document is ready
		if (document.readyState === "loading") {
			document.addEventListener("DOMContentLoaded", startErrorObserver);
		} else {
			startErrorObserver();
		}

		// Preview header artefact dropdown navigation
		function initHeaderNavigation() {
			var select = document.getElementById("bn-preview-artefact-select");
			if (!select) return;
			select.addEventListener("change", function(e) {
				var newPath = e.target.value;
				if (newPath && newPath !== normalizedPath) {
					window.location.href = newPath;
				}
			});
		}
		if (document.readyState === "loading") {
			document.addEventListener("DOMContentLoaded", initHeaderNavigation);
		} else {
			initHeaderNavigation();
		}

		// Page info overlays for "All Pages" view
		function applyPageInfoOverlays() {
			// Only show on "All Pages" route
			if (normalizedPath !== "/") return;

			// Remove existing overlays
			document.querySelectorAll(".bn-page-info").forEach(function (el) {
				el.remove();
			});

			// Read page metadata from bn-context data attribute
			var bnContext = document.querySelector("bn-context");
			if (!bnContext) return;
			var metaJSON = bnContext.getAttribute("data-page-meta");
			if (!metaJSON) return;
			var pageMeta;
			try {
				pageMeta = JSON.parse(metaJSON);
			} catch (e) {
				return;
			}
			if (!Array.isArray(pageMeta)) return;

			// Build lookup by page name
			var metaByName = {};
			pageMeta.forEach(function (m) {
				metaByName[m.name] = m;
			});

			// Inject info bar before each bn-layout-page as a sibling element.
			// bn-layout-page uses Shadow DOM so we cannot inject children into it.
			// Search in both shadow root and light DOM.
			var searchRoot = bnContext.shadowRoot || bnContext;
			var pages = searchRoot.querySelectorAll("bn-layout-page[data-bino-page]");
			if (pages.length === 0) {
				pages = document.querySelectorAll("bn-layout-page[data-bino-page]");
			}
			pages.forEach(function (pageEl) {
				var pageName = pageEl.getAttribute("data-bino-page");
				if (!pageName) return;
				// Strip param suffix for lookup (e.g. "page#REGION=EU" -> "page")
				var baseName = pageName.split("#")[0];
				var meta = metaByName[baseName] || metaByName[pageName];
				if (!meta) return;

				var overlay = document.createElement("div");
				overlay.className = "bn-page-info";

				// Page name
				var nameSpan = document.createElement("span");
				nameSpan.className = "bn-page-info-name";
				nameSpan.textContent = pageName;
				overlay.appendChild(nameSpan);

				// Constraints
				if (meta.constraints && meta.constraints.length > 0) {
					var clabel = document.createElement("span");
					clabel.className = "bn-page-info-label";
					clabel.textContent = "constraints:";
					overlay.appendChild(clabel);
					meta.constraints.forEach(function (c) {
						var pill = document.createElement("span");
						pill.className = "bn-page-info-pill constraint";
						pill.textContent = c;
						overlay.appendChild(pill);
					});
				}

				// Artefacts
				if (meta.artefacts && meta.artefacts.length > 0) {
					var alabel = document.createElement("span");
					alabel.className = "bn-page-info-label";
					alabel.textContent = "used in:";
					overlay.appendChild(alabel);
					meta.artefacts.forEach(function (a) {
						var pill = document.createElement("span");
						pill.className = "bn-page-info-pill artefact";
						pill.textContent = a;
						overlay.appendChild(pill);
					});
				}

				// Insert as sibling before the page element
				pageEl.parentNode.insertBefore(overlay, pageEl);
			});
		}

		// Apply overlays after content updates
		document.addEventListener("bn-preview:content-updated", function () {
			applyPageInfoOverlays();
		});

		// Also apply on initial load
		function initPageInfoOverlays() {
			applyPageInfoOverlays();
		}
		if (document.readyState === "loading") {
			document.addEventListener("DOMContentLoaded", initPageInfoOverlays);
		} else {
			initPageInfoOverlays();
		}
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

// withPreviewPageMetadata injects page metadata (constraints and artefact usage) into
// the "All Pages" context HTML. The metadata is stored as a data-page-meta attribute
// on the <bn-context> element itself. This ensures it survives the DOM replacement
// performed by swapContext and is accessible even if bn-context uses Shadow DOM.
func withPreviewPageMetadata(ctx []byte, pageMeta []previewPageMeta) []byte {
	if len(pageMeta) == 0 {
		return ctx
	}
	data, err := json.Marshal(pageMeta)
	if err != nil {
		return ctx
	}
	// Insert data-page-meta attribute into the <bn-context ...> opening tag
	attr := []byte(` data-page-meta="` + html.EscapeString(string(data)) + `"`)
	openTag := []byte("<bn-context")
	idx := bytes.Index(ctx, openTag)
	if idx == -1 {
		return ctx
	}
	insertAt := idx + len(openTag)
	updated := make([]byte, 0, len(ctx)+len(attr))
	updated = append(updated, ctx[:insertAt]...)
	updated = append(updated, attr...)
	updated = append(updated, ctx[insertAt:]...)
	return updated
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
