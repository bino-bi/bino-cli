package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/cli/web"
	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/preview/explorer"
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	reportgraph "bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/watchers"
	"bino.bi/bino/pkg/duckdb"
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
		dataMode       string
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

			env, err := initCommandEnv(ctx, cmd, workdir, "preview", logger)
			if err != nil {
				return err
			}
			if env.PluginManager != nil {
				defer env.PluginManager.ShutdownAll(ctx)
			}

			port = env.Resolver.ResolveInt("port", "port", port)
			logSQL = env.Resolver.ResolveBool("log-sql", "log-sql", logSQL)
			enableLint = env.Resolver.ResolveBool("lint", "lint", enableLint)

			if !env.EngineVersionPinned {
				logger.Warnf("No engine-version set in bino.toml - using latest local version. Pin a version for reproducible builds.")
			}

			addr := fmt.Sprintf("127.0.0.1:%d", port)
			logger.Infof("Starting preview server on %s", addr)
			logger.Infof("Watching workdir %s", env.ProjectRoot)

			queryLogger := newQueryLogger(ctx, logger, logSQL)

			// Create a shared DuckDB session for the lifetime of the preview process.
			// Extensions are loaded once; views are re-registered on each refresh
			// via CREATE OR REPLACE VIEW.
			logger.Infof("Initializing DuckDB engine...")
			duckdbOpts, err := duckdb.DefaultOptions()
			if err != nil {
				return RuntimeError(err)
			}
			duckdbOpts.QueryLogger = queryLogger
			sharedSession, err := duckdb.OpenSession(ctx, duckdbOpts)
			if err != nil {
				return RuntimeError(err)
			}
			defer sharedSession.Close()

			if err := sharedSession.InstallAndLoadExtensions(ctx, duckdb.DefaultExtensions()); err != nil {
				return RuntimeError(err)
			}

			// Resolve data validation mode
			dataValidation = env.Resolver.ResolveString("data-validation", "data-validation", dataValidation)
			dataValidationMode, err := resolveDataValidationMode(dataValidation)
			if err != nil {
				return ConfigError(err)
			}
			dataValidationSampleSize := dataset.GetDataValidationSampleSize()

			// Resolve data delivery mode
			dataMode = env.Resolver.ResolveString("data-mode", "data-mode", dataMode)
			resolvedDataMode, err := normalizeDataMode(dataMode)
			if err != nil {
				return RuntimeError(err)
			}

			previewHookEnv := hooks.HookEnv{
				Mode:     "preview",
				Workdir:  env.ProjectRoot,
				ReportID: env.ProjectCfg.ReportID,
				Verbose:  logx.DebugEnabled(ctx),
			}

			// Set up plugin integration for the preview pipeline.
			var pluginOpts *render.PluginOptions
			var postRenderHTMLHook func(context.Context, []byte) ([]byte, error)
			var postDatasetHook func(context.Context, []pipeline.DatasetPayload) error
			var pluginLinters lint.PluginLinterRegistry
			if env.PluginRegistry != nil {
				pluginOpts = plugin.BuildRenderOptions(ctx, env.PluginRegistry, env.ProjectRoot, "preview")
				hookBus := plugin.NewHookBus(env.PluginRegistry, logger.Channel("plugin-hooks"))
				postRenderHTMLHook = func(hookCtx context.Context, htmlData []byte) ([]byte, error) {
					modified, _, err := hookBus.DispatchPostRenderHTML(hookCtx, htmlData)
					return modified, err
				}
				var hostSvc *plugin.BinoHostServer
				if env.PluginManager != nil {
					hostSvc = env.PluginManager.HostService()
					hostSvc.SetDefaultDuckDBOpener()
				}
				postDatasetHook = func(hookCtx context.Context, datasets []pipeline.DatasetPayload) error {
					pluginDatasets := make([]plugin.DatasetPayload, len(datasets))
					for i, ds := range datasets {
						pluginDatasets[i] = plugin.DatasetPayload{Name: ds.Name, JSONRows: ds.JSONRows, Columns: ds.Columns}
					}
					if hostSvc != nil {
						hostSvc.SetDatasets(pluginDatasets)
					}
					_, _, err := hookBus.DispatchPostDatasetExecute(hookCtx, pluginDatasets)
					return err
				}
				pluginLinters = plugin.NewLinterRegistry(env.PluginRegistry)
			}
			if resolvedDataMode == render.DataModeURL {
				if pluginOpts == nil {
					pluginOpts = &render.PluginOptions{}
				}
				pluginOpts.DataMode = render.DataModeURL
				// Same-origin relative URLs work for the long-lived preview server.
			}

			refreshMu := &sync.Mutex{}
			var server *previewhttp.Server
			var explorerSession *explorer.Session

			refreshCfg := previewRefreshConfig{
				Logger:                   logger,
				Workdir:                  env.ProjectRoot,
				EnableLint:               enableLint,
				EngineVersion:            env.EngineVersion,
				QueryLogger:              queryLogger,
				DataValidationMode:       dataValidationMode,
				DataValidationSampleSize: dataValidationSampleSize,
				HookRunner:               env.HookRunner,
				HookEnv:                  previewHookEnv,
				Session:                  sharedSession,
				KindProvider:             env.PluginRegistry,
				PluginOptions:            pluginOpts,
				PostRenderHTMLHook:       postRenderHTMLHook,
				PostDatasetHook:          postDatasetHook,
				PluginLinters:            pluginLinters,
			}
			if env.PluginManager != nil {
				refreshCfg.HostService = env.PluginManager.HostService()
			}

			refresh := func(reason string) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				refreshMu.Lock()
				defer refreshMu.Unlock()
				if server == nil {
					return RuntimeErrorf("preview: server not initialized")
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				server.BroadcastRefreshing(reason)
				defer server.BroadcastRefreshDone()
				return refreshPreviewContent(ctx, reason, server, explorerSession, &refreshCfg)
			}

			// Create explorer session for data exploration
			explorerSession, err = explorer.NewSession(ctx, logger.Channel("explorer"))
			if err != nil {
				logger.Warnf("Data explorer unavailable: %v", err)
			}
			if explorerSession != nil {
				defer explorerSession.Close()
			}

			server, err = previewhttp.New(previewhttp.Config{
				ListenAddr:      addr,
				CacheDir:        env.CacheDir,
				Logger:          logger.Channel("server"),
				ExplorerHandler: explorerHandler(explorerSession),
			})
			if err != nil {
				return RuntimeError(err)
			}

			// In url mode, emit absolute URLs so older template-engine builds
			// (which only treat http:// or https:// bodies as URLs) still
			// fetch them correctly. Same-origin relative paths would also
			// work, but only with template-engine builds that include the
			// "/" prefix in isUrl().
			if resolvedDataMode == render.DataModeURL && refreshCfg.PluginOptions != nil {
				refreshCfg.PluginOptions.DataBaseURL = server.URL()
			}

			// Run pre-preview hook (once, before initial refresh)
			previewHookEnv.ListenAddr = addr
			if err := env.HookRunner.Run(ctx, "pre-preview", previewHookEnv); err != nil {
				return RuntimeError(err)
			}

			// Initial refresh collects visited directories so the watcher can
			// register them without a redundant filesystem walk.
			logger.Infof("Loading manifests and executing datasets...")
			var visitedDirs []string
			refreshCfg.CollectedDirs = &visitedDirs
			if err := refresh("initial load"); err != nil {
				return err
			}
			refreshCfg.CollectedDirs = nil // subsequent refreshes skip collection

			refreshCh := make(chan string, 16)
			enqueue := func(reason string) {
				select {
				case refreshCh <- reason:
				default:
				}
			}

			watchLog := logger.Channel("watcher")
			watcher, err := watchers.NewWatcher(watchers.Config{
				Root:   env.ProjectRoot,
				Dirs:   visitedDirs,
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

			go func() {
				debounce := time.NewTimer(0)
				if !debounce.Stop() {
					<-debounce.C
				}
				var reasons []string
				for {
					select {
					case <-ctx.Done():
						debounce.Stop()
						return
					case reason := <-refreshCh:
						reasons = append(reasons, reason)
						debounce.Reset(300 * time.Millisecond)
					case <-debounce.C:
						if len(reasons) == 0 {
							continue
						}
						coalesced := coalesceReasons(reasons)
						reasons = reasons[:0]
						if err := refresh(coalesced); err != nil {
							logger.Errorf("Refresh failed: %v", err)
						}
					}
				}
			}()

			url := server.URL()
			logger.Successf("Serving preview at %s", url)

			if err := openBrowser(ctx, url); err != nil {
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
	cmd.Flags().StringVar(&dataMode, "data-mode", "url",
		"Dataset/datasource delivery: 'url' fetches data via HTTP from the bino server (default), 'inline' embeds gzip+base64 in the HTML")

	return cmd
}

// previewRefreshConfig holds configuration for a preview content refresh.
type previewRefreshConfig struct {
	Logger                   logx.Logger
	Workdir                  string
	EnableLint               bool
	EngineVersion            string
	QueryLogger              func(string)
	DataValidationMode       dataset.DataValidationMode
	DataValidationSampleSize int
	HookRunner               *hooks.Runner
	HookEnv                  hooks.HookEnv

	// CollectedDirs, when non-nil, receives directories visited during LoadDir.
	// This is set for the initial refresh and cleared afterwards so subsequent
	// refreshes skip the overhead of collecting directories.
	CollectedDirs *[]string

	// Session is a shared DuckDB session reused across refreshes.
	// Extensions are loaded once; views are re-registered on each refresh.
	Session *duckdb.Session

	// KindProvider supplies plugin-registered kinds for document validation.
	KindProvider config.KindProvider

	// PluginOptions carries plugin integration state. May be nil.
	PluginOptions *render.PluginOptions
	// PostRenderHTMLHook is called after HTML generation. May be nil.
	PostRenderHTMLHook func(ctx context.Context, html []byte) ([]byte, error)
	// PostDatasetHook is called after dataset execution. May be nil.
	PostDatasetHook func(ctx context.Context, datasets []pipeline.DatasetPayload) error
	// PluginLinters runs plugin lint rules alongside built-in rules. May be nil.
	PluginLinters lint.PluginLinterRegistry
	// HostService is the shared BinoHost server for updating documents. May be nil.
	HostService *plugin.BinoHostServer
}

// refreshPreviewContent loads manifests, renders all artifacts, and updates the
// preview server. It is the core logic extracted from the preview refresh closure.
func refreshPreviewContent(ctx context.Context, reason string, server *previewhttp.Server, explorerSession *explorer.Session, cfg *previewRefreshConfig) error {
	logger := cfg.Logger
	watchDir := cfg.Workdir

	// Run pre-refresh hook (on failure: log and continue)
	refreshHookEnv := cfg.HookEnv
	refreshHookEnv.RefreshReason = reason
	if err := cfg.HookRunner.Run(ctx, "pre-refresh", refreshHookEnv); err != nil {
		logger.Errorf("pre-refresh hook failed: %v", err)
		return nil
	}

	logger.Infof("Rendering report (%s)", reason)
	loadOpts := config.LoadOptions{CollectedDirs: cfg.CollectedDirs, KindProvider: cfg.KindProvider}
	docs, err := config.LoadDirWithOptions(ctx, watchDir, loadOpts)
	if err != nil {
		logger.Errorf("Render failed (%s): %v", reason, err)
		return RuntimeError(err)
	}

	// Update host service with loaded documents for bidirectional plugin queries.
	if cfg.HostService != nil {
		cfg.HostService.SetDocuments(plugin.DocumentsFromConfig(docs))
	}

	// Warn about unresolved environment variables (preview continues with empty values)
	for _, m := range config.CollectMissingEnvVars(docs) {
		logger.Warnf("unresolved environment variable %s in %s", m.VarName, m.File)
	}

	// Refresh explorer session with latest documents (non-fatal on error)
	if explorerSession != nil {
		if err := explorerSession.Refresh(ctx, docs); err != nil {
			logger.Warnf("Explorer refresh: %v", err)
		}
	}

	// Run lint rules if enabled
	if cfg.EnableLint {
		lintDocs := configDocsToLintDocs(docs)
		runner := lint.NewDefaultRunner()
		findings := runner.Run(ctx, lintDocs)
		if cfg.PluginLinters != nil {
			pluginFindings := lint.RunPluginLinters(ctx, lintDocs, cfg.PluginLinters)
			findings = append(findings, pluginFindings...)
		}
		for _, f := range findings {
			relPath := pathutil.RelPath(watchDir, f.File)
			loc := relPath
			if f.DocIdx > 0 {
				loc = fmt.Sprintf("%s #%d", relPath, f.DocIdx)
			}
			logger.Warnf("[%s] %s: %s", f.RuleID, loc, f.Message)
		}
	}

	artifacts, err := config.CollectArtefacts(docs)
	if err != nil {
		logger.Errorf("Artifact scan failed (%s): %v", reason, err)
		return RuntimeError(err)
	}
	pipeline.LogArtefactWarnings(logger, artifacts)

	documentArtefacts, err := config.CollectDocumentArtefacts(docs)
	if err != nil {
		logger.Errorf("DocumentArtefact scan failed (%s): %v", reason, err)
		return RuntimeError(err)
	}
	pipeline.LogDocumentArtefactWarnings(logger, documentArtefacts)

	// Build artifact info list for header dropdown
	artefactInfos := make([]previewArtefactInfo, 0, len(artifacts)+len(documentArtefacts))
	for _, art := range artifacts {
		artefactInfos = append(artefactInfos, previewArtefactInfo{
			Name:   art.Document.Name,
			Title:  art.Spec.Title,
			Format: art.Spec.Format,
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

	// Build document info list for assets modal
	documentInfos := make([]previewDocumentInfo, 0, len(docs))
	for _, doc := range docs {
		var cs []string
		for _, c := range doc.Constraints {
			cs = append(cs, formatConstraint(c))
		}
		documentInfos = append(documentInfos, previewDocumentInfo{
			Kind:        doc.Kind,
			Name:        doc.Name,
			File:        pathutil.RelPath(watchDir, doc.File),
			Labels:      doc.Labels,
			Constraints: cs,
		})
	}

	// Build dependency graph for artifact graph visualization
	g, graphErr := reportgraph.Build(ctx, docs)
	if graphErr != nil {
		logger.Warnf("Graph build skipped: %v", graphErr)
	}

	// Always render "All Pages" view for "/" route - this is the default view
	// that shows all LayoutPages without any artifact filtering
	allPagesResult, err := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{
		Workdir:                  watchDir,
		Language:                 "de",
		Mode:                     pipeline.RenderModePreview,
		EngineVersion:            cfg.EngineVersion,
		QueryLogger:              cfg.QueryLogger,
		DataValidation:           cfg.DataValidationMode,
		DataValidationSampleSize: cfg.DataValidationSampleSize,
		Session:                  cfg.Session,
		PluginOptions:            cfg.PluginOptions,
		PostRenderHTMLHook:       cfg.PostRenderHTMLHook,
		PostDatasetHook:          cfg.PostDatasetHook,
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
	registerEmittedData(server, allPagesResult.EmittedData)

	routeMap := make(map[string]previewhttp.ContentFunc, len(artifacts)+len(documentArtefacts)+1)
	allAssets := make([]previewhttp.LocalAsset, 0)
	type artefactPayload struct {
		path        string
		contextHTML []byte
	}
	payloads := make([]artefactPayload, 0, len(artifacts)+1)

	// Add "All Pages" route (default "/" view)
	allPagesFrameHTML := withPreviewHeader(withPreviewStyles(allPagesResult.FrameHTML), artefactInfos, documentInfos, "/", nil)
	pageMeta := buildPageMetadata(docs, artifacts)
	allPagesContextHTML := withPreviewPageMetadata(withPreviewContextStyles(allPagesResult.ContextHTML), pageMeta)
	allAssets = append(allAssets, pipeline.ConvertLocalAssets(allPagesResult.LocalAssets)...)
	payloads = append(payloads, artefactPayload{path: "/", contextHTML: allPagesContextHTML})

	// Render each ReportArtefact
	for _, art := range artifacts {
		renderResult, err := pipeline.RenderArtefactFrameAndContextWithOptions(ctx, watchDir, docs, art, pipeline.FrameRenderOptions{
			QueryLogger:              cfg.QueryLogger,
			EngineVersion:            cfg.EngineVersion,
			DataValidation:           cfg.DataValidationMode,
			DataValidationSampleSize: cfg.DataValidationSampleSize,
			Session:                  cfg.Session,
			PluginOptions:            cfg.PluginOptions,
			PostRenderHTMLHook:       cfg.PostRenderHTMLHook,
			PostDatasetHook:          cfg.PostDatasetHook,
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
		registerEmittedData(server, renderResult.EmittedData)
		artPath := "/" + art.Document.Name
		var artGraph *previewGraphData
		if g != nil {
			if rootNode, ok := g.ReportArtefactByName(art.Document.Name); ok {
				artGraph = buildPreviewGraphData(g, rootNode)
			}
		}
		frameHTML := withPreviewHeader(withPreviewStyles(renderResult.FrameHTML), artefactInfos, documentInfos, artPath, artGraph)
		contextHTML := withPreviewContextStyles(renderResult.ContextHTML)
		allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
		routeMap[artPath] = previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
		payloads = append(payloads, artefactPayload{path: artPath, contextHTML: contextHTML})
	}

	// Render each DocumentArtefact
	for _, docArt := range documentArtefacts {
		renderResult, err := pipeline.RenderDocumentArtefactHTML(ctx, watchDir, docArt, pipeline.DocumentArtefactRenderOptions{
			EngineVersion:      cfg.EngineVersion,
			Session:            cfg.Session,
			PluginOptions:      cfg.PluginOptions,
			KindProvider:       cfg.KindProvider,
			PostRenderHTMLHook: cfg.PostRenderHTMLHook,
			PostDatasetHook:    cfg.PostDatasetHook,
		})
		if err != nil {
			logger.Errorf("Render failed for DocumentArtefact %s (%s): %v", docArt.Document.Name, reason, err)
			continue
		}
		registerEmittedData(server, renderResult.EmittedData)
		allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
		docPath := "/doc/" + docArt.Document.Name
		var docGraph *previewGraphData
		if g != nil {
			if rootNode, ok := g.DocumentArtefactByName(docArt.Document.Name); ok {
				docGraph = buildPreviewGraphData(g, rootNode)
			}
		}
		// DocumentArtefacts get header injected too
		styledHTML := withPreviewStyles(withDocumentPageWidth(renderResult.HTML, docArt.Spec.Format, docArt.Spec.Orientation))
		frameHTML := withPreviewHeader(styledHTML, artefactInfos, documentInfos, docPath, docGraph)
		routeMap[docPath] = previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
	}

	// Register lazy presentation view for each ReportArtefact at /pres/{name}.
	// Presentation rendering re-executes datasets, so defer it until first access.
	for _, art := range artifacts {
		presPath := "/pres/" + art.Document.Name
		routeMap[presPath] = lazyPresentationContent(watchDir, docs, art, cfg, server, presPath)
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

// lazyPresentationContent returns a ContentFunc that renders the presentation view
// on first access, caching the result for subsequent requests.
func lazyPresentationContent(workdir string, docs []config.Document, art config.Artifact, cfg *previewRefreshConfig, server *previewhttp.Server, presPath string) previewhttp.ContentFunc {
	var once sync.Once
	var cachedBody []byte
	var cachedCT string
	var cachedErr error

	return func(ctx context.Context) ([]byte, string, error) {
		once.Do(func() {
			renderResult, err := pipeline.RenderPresentationFrameAndContext(ctx, workdir, docs, art, pipeline.PresentationArtefactRenderOptions{
				EngineVersion:            cfg.EngineVersion,
				QueryLogger:              cfg.QueryLogger,
				DataValidation:           cfg.DataValidationMode,
				DataValidationSampleSize: cfg.DataValidationSampleSize,
				PluginOptions:            cfg.PluginOptions,
				PostDatasetHook:          cfg.PostDatasetHook,
				Session:                  cfg.Session,
			})
			if err != nil {
				cachedErr = err
				return
			}
			registerEmittedData(server, renderResult.EmittedData)
			frameHTML := withPreviewStyles(renderResult.FrameHTML)
			cachedBody = append([]byte(nil), frameHTML...)
			cachedCT = "text/html; charset=utf-8"
			server.BroadcastContent(presPath, renderResult.ContextHTML)
		})
		return cachedBody, cachedCT, cachedErr
	}
}

// previewArtefactInfo holds metadata about an artifact for the preview header dropdown.
type previewArtefactInfo struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	Format string `json:"format"`
	IsDoc  bool   `json:"isDoc"` // true for DocumentArtefact
}

// previewDocumentInfo holds metadata about a manifest document for the assets modal.
type previewDocumentInfo struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	File        string            `json:"file"`
	Labels      map[string]string `json:"labels,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
}

// previewPageMeta holds metadata about a LayoutPage for the "All Pages" preview overlay.
type previewPageMeta struct {
	Name        string   `json:"name"`
	Constraints []string `json:"constraints,omitempty"`
	Artifacts   []string `json:"artifacts,omitempty"`
}

// previewGraphNode is a serializable graph node for the frontend dependency graph.
type previewGraphNode struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

// previewGraphData holds the dependency subgraph for a single artifact.
type previewGraphData struct {
	Nodes  map[string]previewGraphNode `json:"nodes"`
	RootID string                      `json:"rootId"`
}

// buildPreviewGraphData extracts the reachable subgraph from root and serializes it for the frontend.
func buildPreviewGraphData(g *reportgraph.Graph, root *reportgraph.Node) *previewGraphData {
	if g == nil || root == nil {
		return nil
	}
	reachable := collectReachableNodes(g, []*reportgraph.Node{root})

	nodes := make(map[string]previewGraphNode, len(reachable))
	for id, node := range reachable {
		var deps []string
		for _, dep := range node.DependsOn {
			if _, ok := reachable[dep]; ok {
				deps = append(deps, dep)
			}
		}
		nodes[id] = previewGraphNode{
			ID:        node.ID,
			Kind:      string(node.Kind),
			Name:      displayName(node),
			DependsOn: deps,
		}
	}

	return &previewGraphData{
		Nodes:  nodes,
		RootID: root.ID,
	}
}

// buildPageMetadata computes per-page metadata (constraints and artifact usage) for the "All Pages" view.
func buildPageMetadata(docs []config.Document, artifacts []config.Artifact) []previewPageMeta {
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

	// Build page-name → artifact-names mapping
	pageArtefacts := make(map[string][]string)
	for _, art := range artifacts {
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
			Artifacts:   pageArtefacts[p.name],
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

// coalesceReasons merges multiple file-change reasons into a single human-readable string.
func coalesceReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "unknown"
	}
	if len(reasons) == 1 {
		return reasons[0]
	}
	return fmt.Sprintf("%s (+%d more)", reasons[0], len(reasons)-1)
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// buildPreviewHeader generates the HTML for the sticky preview toolbar and error panel Web Components.
func buildPreviewHeader(artifacts []previewArtefactInfo, documents []previewDocumentInfo, currentPath string, graphData *previewGraphData) string {
	artefactsJSON, _ := json.Marshal(artifacts)
	documentsJSON, _ := json.Marshal(documents)

	var b strings.Builder
	b.WriteString(`<bino-toolbar artifacts='`)
	b.WriteString(html.EscapeString(string(artefactsJSON)))
	b.WriteString(`' documents='`)
	b.WriteString(html.EscapeString(string(documentsJSON)))
	b.WriteString(`' current-path='`)
	b.WriteString(html.EscapeString(currentPath))
	if graphData != nil {
		graphJSON, _ := json.Marshal(graphData)
		b.WriteString(`' graph='`)
		b.WriteString(html.EscapeString(string(graphJSON)))
	}
	b.WriteString(`'><bino-search></bino-search></bino-toolbar>`)
	b.WriteString(`<bino-error-panel></bino-error-panel>`)
	b.WriteString(`<bino-assets-modal></bino-assets-modal>`)
	b.WriteString(`<bino-graph-modal></bino-graph-modal>`)
	b.WriteString(`<bino-data-explorer></bino-data-explorer>`)

	return b.String()
}

func withPreviewHeader(doc []byte, artifacts []previewArtefactInfo, documents []previewDocumentInfo, currentPath string, graphData *previewGraphData) []byte {
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

	header := buildPreviewHeader(artifacts, documents, currentPath, graphData)

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

var previewStyleMarker = []byte("bn-preview-style")

func previewStyleBlock() []byte {
	var b strings.Builder
	b.WriteString("\n\t<link id=\"bn-preview-style\" rel=\"stylesheet\" href=\"/__bino/shared/tokens.css\">\n")
	b.WriteString("\t<link rel=\"stylesheet\" href=\"/__bino/preview/preview.css\">\n")
	b.WriteString("\t<script type=\"module\" src=\"/__bino/preview/preview-app.js\"></script>\n")
	return []byte(b.String())
}

// withPreviewStyles injects a lightweight set of layout styles so preview pages are centered
// and readable without relying on external assets. The import map is placed before the first
// <script> tag so that Firefox (which strictly enforces the HTML spec) processes it before
// any module scripts begin loading.
func withPreviewStyles(doc []byte) []byte {
	if len(doc) == 0 || bytes.Contains(doc, previewStyleMarker) {
		return doc
	}
	headClose := []byte("</head>")
	idx := bytes.Index(doc, headClose)
	if idx == -1 {
		return doc
	}
	block := previewStyleBlock()
	importMap := []byte(web.ImportMapScript() + "\n  ")
	updated := make([]byte, 0, len(doc)+len(block)+len(importMap))

	// Inject the import map before the first <script> tag so it is parsed
	// before any module scripts. Firefox requires this ordering.
	scriptIdx := bytes.Index(doc, []byte("<script"))
	if scriptIdx != -1 {
		updated = append(updated, doc[:scriptIdx]...)
		updated = append(updated, importMap...)
		updated = append(updated, doc[scriptIdx:idx]...)
	} else {
		updated = append(updated, doc[:idx]...)
		updated = append(updated, importMap...)
	}
	updated = append(updated, block...)
	updated = append(updated, doc[idx:]...)
	return updated
}

// withDocumentPageWidth injects a CSS custom property with the page width
// derived from the document's format and orientation so the preview can
// size the page container accordingly.
func withDocumentPageWidth(doc []byte, format, orientation string) []byte {
	width := documentPageWidth(format, orientation)
	tag := []byte(fmt.Sprintf(`<style>:root{--bn-doc-page-width:%s}</style>`, width))
	headClose := []byte("</head>")
	idx := bytes.Index(doc, headClose)
	if idx == -1 {
		return doc
	}
	out := make([]byte, 0, len(doc)+len(tag))
	out = append(out, doc[:idx]...)
	out = append(out, tag...)
	out = append(out, doc[idx:]...)
	return out
}

// documentPageWidth returns the CSS width for the given page format and orientation.
func documentPageWidth(format, orientation string) string {
	type dims struct{ portrait, landscape string }
	formats := map[string]dims{
		"a4":     {"210mm", "297mm"},
		"a5":     {"148mm", "210mm"},
		"letter": {"215.9mm", "279.4mm"},
		"legal":  {"215.9mm", "355.6mm"},
	}
	d, ok := formats[format]
	if !ok {
		d = formats["a4"]
	}
	if orientation == "landscape" {
		return d.landscape
	}
	return d.portrait
}

// withPreviewContextStyles returns the context HTML as-is for SSE delivery.
// The context HTML is a standalone <bn-context> block that replaces the existing
// one in the DOM. Preview styles are already in the frame's <head>, so no
// additional injection is needed here.
func withPreviewContextStyles(ctx []byte) []byte {
	return ctx
}

// withPreviewPageMetadata injects page metadata (constraints and artifact usage) into
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

func openBrowser(ctx context.Context, url string) error {
	// Validate URL to prevent command injection
	if err := validateBrowserURL(url); err != nil {
		return err
	}

	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", url)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.CommandContext(ctx, "xdg-open", url)
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

// explorerHandler returns the explorer HTTP handler if the session is available.
func explorerHandler(session *explorer.Session) http.Handler {
	if session == nil {
		return nil
	}
	return explorer.Handler(session)
}
