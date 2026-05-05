package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/cli/web"
	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/preview/bootstatus"
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
		incremental    bool
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

			// Resolve data validation mode early — must happen before the boot
			// goroutine starts, since refreshCfg captures it.
			dataValidation = env.Resolver.ResolveString("data-validation", "data-validation", dataValidation)
			dataValidationMode, err := resolveDataValidationMode(dataValidation)
			if err != nil {
				return ConfigError(err)
			}
			dataValidationSampleSize := dataset.GetDataValidationSampleSize()

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
			}

			// Lazily-initialized explorer session; created in the boot
			// goroutine alongside the main DuckDB session so its extension
			// install does not extend cold-start latency. The HTTP handler
			// reads the slot and returns 503 until init completes.
			var explorerSlot atomic.Pointer[explorer.Session]
			lazyExplorer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sess := explorerSlot.Load()
				if sess == nil {
					http.Error(w, "explorer is still initializing", http.StatusServiceUnavailable)
					return
				}
				explorer.Handler(sess).ServeHTTP(w, r)
			})

			server, err := previewhttp.New(previewhttp.Config{
				ListenAddr:      addr,
				CacheDir:        env.CacheDir,
				Logger:          logger.Channel("server"),
				ExplorerHandler: lazyExplorer,
			})
			if err != nil {
				return RuntimeError(err)
			}

			if loadingPage, lerr := web.LoadingPageHTML(); lerr == nil && len(loadingPage) > 0 {
				server.SetContentFunc(previewhttp.StaticContent(loadingPage, "text/html; charset=utf-8"))
			}

			if resolvedDataMode == render.DataModeURL && pluginOpts != nil {
				pluginOpts.DataBaseURL = server.URL()
			}

			// Status reporter fans cold-start phases out to the CLI spinner
			// (TTY-aware, falls back to plain log lines on CI/piped output)
			// and to connected SSE clients via the loading page.
			cliSink := logx.NewStatus(logger, os.Stdout)
			defer cliSink.Stop()
			reporter := bootstatus.NewMultiplexer(
				bootstatus.NewSSEReporter(server),
				bootstatus.NewCLIReporter(cliSink),
			)

			// Start serving immediately; handleRoot returns the loading page
			// for any path until SetContentRoutes/SetContentFunc are updated
			// at the end of the first refresh.
			serverErrCh := make(chan error, 1)
			go func() { serverErrCh <- server.Start(ctx) }()

			previewURL := server.URL()
			logger.Successf("Serving preview at %s", previewURL)
			if err := openBrowser(ctx, previewURL); err != nil {
				logger.Warnf("Unable to open browser automatically: %v", err)
			}
			logger.Infof("Preview running * press Ctrl+C to stop")

			previewHookEnv.ListenAddr = addr
			if err := env.HookRunner.Run(ctx, "pre-preview", previewHookEnv); err != nil {
				cliSink.Stop()
				return RuntimeError(err)
			}

			// Phase 2 (background): heavy work that used to block before
			// `server.Start()`. We retain the duckdb session pointer so the
			// outer scope can close it on shutdown even if boot crashed mid-way.
			var (
				sessionMu     sync.Mutex
				sharedSession *duckdb.Session
			)
			defer func() {
				sessionMu.Lock()
				defer sessionMu.Unlock()
				if sharedSession != nil {
					_ = sharedSession.Close()
				}
				if es := explorerSlot.Load(); es != nil {
					_ = es.Close()
				}
			}()

			bootDone := make(chan struct{})
			go func() {
				defer close(bootDone)

				// 1. DuckDB engine + extensions
				reporter.Begin(bootstatus.PhaseDuckDB, "Initializing DuckDB engine")
				duckdbOpts, oerr := duckdb.DefaultOptions()
				if oerr != nil {
					reporter.Fail(bootstatus.PhaseDuckDB, oerr)
					logger.Errorf("DuckDB options: %v", oerr)
					return
				}
				duckdbOpts.QueryLogger = queryLogger
				sess, oerr := duckdb.OpenSession(ctx, duckdbOpts)
				if oerr != nil {
					reporter.Fail(bootstatus.PhaseDuckDB, oerr)
					logger.Errorf("DuckDB open: %v", oerr)
					return
				}
				sessionMu.Lock()
				sharedSession = sess
				sessionMu.Unlock()

				exts := duckdb.DefaultExtensions()
				progress := func(done, total int, name string) {
					reporter.Progress(done, total, name)
				}
				if eerr := sess.InstallAndLoadExtensionsWithProgress(ctx, exts, progress); eerr != nil {
					reporter.Fail(bootstatus.PhaseDuckDB, eerr)
					logger.Errorf("DuckDB extensions: %v", eerr)
					return
				}
				reporter.End(bootstatus.PhaseDuckDB)

				// 2. Build refresh plumbing
				refreshMu := &sync.Mutex{}
				refreshState := &previewRefreshState{
					perArtefactAssets: make(map[string][]previewhttp.LocalAsset),
				}
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
					Session:                  sess,
					KindProvider:             env.PluginRegistry,
					PluginOptions:            pluginOpts,
					PostRenderHTMLHook:       postRenderHTMLHook,
					PostDatasetHook:          postDatasetHook,
					PluginLinters:            pluginLinters,
					Reporter:                 reporter,
				}
				if env.PluginManager != nil {
					refreshCfg.HostService = env.PluginManager.HostService()
				}

				refresh := func(reason string, changed []string) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					refreshMu.Lock()
					defer refreshMu.Unlock()
					if err := ctx.Err(); err != nil {
						return err
					}
					server.BroadcastRefreshing(reason)
					broadcastPaths, err := refreshPreviewContent(ctx, reason, changed, server, explorerSlot.Load(), &refreshCfg, refreshState)
					if err != nil && ctx.Err() == nil {
						server.BroadcastRefreshError("", err.Error())
					}
					server.BroadcastRefreshDone(broadcastPaths)
					return err
				}

				// 3. Initial refresh — failure no longer aborts the server,
				// the loading page surfaces the error and the watcher will
				// retry once the user fixes the source.
				var visitedDirs []string
				refreshCfg.CollectedDirs = &visitedDirs
				initialErr := refresh("initial load", nil)
				refreshCfg.CollectedDirs = nil
				if initialErr != nil {
					reporter.Fail(bootstatus.PhaseRendering, initialErr)
				} else {
					reporter.Begin(bootstatus.PhaseReady, "Ready")
					reporter.End(bootstatus.PhaseReady)
				}

				// 4. Explorer session — kept off the cold-start critical path
				// by initializing AFTER the first refresh (so it doesn't add
				// to the "time to first paint"). Self-populates from the docs
				// the first refresh already loaded; failure is non-fatal.
				go func() {
					explorerLog := logger.Channel("explorer")
					reporter.Begin(bootstatus.PhaseExplorer, "Initializing data explorer")
					defer reporter.End(bootstatus.PhaseExplorer)
					es, eerr := explorer.NewSession(ctx, explorerLog)
					if eerr != nil {
						explorerLog.Warnf("Data explorer unavailable: %v", eerr)
						return
					}
					refreshMu.Lock()
					docs := refreshState.lastDocs
					refreshMu.Unlock()
					if len(docs) > 0 {
						if rerr := es.Refresh(ctx, docs); rerr != nil {
							explorerLog.Warnf("Initial explorer refresh: %v", rerr)
						}
					}
					explorerSlot.Store(es)
				}()

				// 5. File watcher + debounce loop. Must be set up after the
				// initial refresh so the watcher knows which dirs to register.
				watchLog := logger.Channel("watcher")
				refreshCh := make(chan refreshRequest, 16)
				enqueue := func(req refreshRequest) {
					select {
					case refreshCh <- req:
					default:
					}
				}
				watcher, werr := watchers.NewWatcher(watchers.Config{
					Root:   env.ProjectRoot,
					Dirs:   visitedDirs,
					Logger: watchLog,
					Handler: func(evt watchers.Event) {
						watchLog.Infof("File updated %s (%s)", evt.RelativePath, evt.Op)
						req := refreshRequest{reason: fmt.Sprintf("change %s", evt.RelativePath)}
						if incremental && evt.Path != "" {
							req.files = []string{evt.Path}
						}
						enqueue(req)
					},
				})
				if werr != nil {
					logger.Errorf("Watcher init failed: %v", werr)
					return
				}
				go watcher.Run(ctx)

				go func() {
					defer watcher.Close()
					debounce := time.NewTimer(0)
					if !debounce.Stop() {
						<-debounce.C
					}
					var pending []refreshRequest
					for {
						select {
						case <-ctx.Done():
							debounce.Stop()
							return
						case req := <-refreshCh:
							pending = append(pending, req)
							debounce.Reset(300 * time.Millisecond)
						case <-debounce.C:
							if len(pending) == 0 {
								continue
							}
							coalesced, files := mergeRefreshRequests(pending)
							pending = pending[:0]
							if err := refresh(coalesced, files); err != nil {
								logger.Errorf("Refresh failed: %v", err)
							}
						}
					}
				}()
			}()

			// Wait for either the server goroutine to exit (shutdown) or the
			// outer context to cancel. We do not block on bootDone — the
			// server should keep accepting requests even if boot is mid-flight
			// when Ctrl+C arrives.
			err = <-serverErrCh
			if err != nil {
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
	cmd.Flags().BoolVar(&incremental, "incremental", true,
		"Only re-render artefacts affected by the changed file(s); falls back to full rebuild for unknown files. Set --incremental=false to always rebuild everything")

	return cmd
}

// refreshRequest carries one debounce input from the file watcher.
// files holds the absolute paths that changed; nil signals a full rebuild
// (e.g. initial load, or --incremental=false). The debounce loop merges
// multiple requests into a single call to refresh.
type refreshRequest struct {
	reason string
	files  []string
}

// previewRefreshState carries cached output from the previous refresh so
// selective rebuilds can re-broadcast unchanged routes without re-rendering.
// The state is mutated under refreshMu; no extra synchronization is needed.
type previewRefreshState struct {
	allPagesFrameHTML   []byte
	allPagesContextHTML []byte
	allPagesAssets      []previewhttp.LocalAsset

	// perArtefactAssets keys are route paths ("/name", "/doc/name") so the
	// asset union can be rebuilt across full and selective refreshes.
	perArtefactAssets map[string][]previewhttp.LocalAsset

	// lastDocs is the most recent successfully-loaded manifest set. Replaced
	// (never mutated) on every refresh, so a goroutine that grabbed the
	// slice header earlier can keep using it without holding refreshMu. The
	// boot's explorer-init goroutine reads this so a freshly-created
	// explorer session can be populated even when it finishes initializing
	// after the first refresh.
	lastDocs []config.Document
}

// mergeRefreshRequests collapses a debounce window into a single
// (reason, files) pair. If any input had nil files, the result is nil
// (full rebuild) — mixing partial signals with a full-rebuild signal must
// not lose information.
func mergeRefreshRequests(reqs []refreshRequest) (reason string, files []string) {
	if len(reqs) == 0 {
		return "unknown", nil
	}
	reasons := make([]string, 0, len(reqs))
	fullRebuild := false
	for _, r := range reqs {
		reasons = append(reasons, r.reason)
		if r.files == nil {
			fullRebuild = true
			continue
		}
		files = append(files, r.files...)
	}
	if fullRebuild {
		return coalesceReasons(reasons), nil
	}
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, f := range files {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return coalesceReasons(reasons), out
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
	// Reporter receives phase events for the CLI spinner and the loading-page
	// SSE stream. May be nil for callers that do not surface progress (tests).
	Reporter bootstatus.Reporter
}

// refreshPreviewContent loads manifests, renders affected artifacts, and
// updates the preview server. When changed is nil, every artefact is
// re-rendered (full rebuild). When changed lists file paths and every path
// maps to a node in the dependency graph, only the artefacts that
// transitively depend on those files are re-rendered; unaffected routes keep
// their cached content. The returned slice lists every route that received a
// fresh content broadcast so the caller can forward it to
// BroadcastRefreshDone — clients viewing a path not in the slice know their
// view was not part of this refresh (failure or simply unaffected).
func refreshPreviewContent(ctx context.Context, reason string, changed []string, server *previewhttp.Server, explorerSession *explorer.Session, cfg *previewRefreshConfig, state *previewRefreshState) ([]string, error) {
	logger := cfg.Logger
	watchDir := cfg.Workdir
	report := cfg.Reporter
	if report == nil {
		report = bootstatus.Nop()
	}

	refreshHookEnv := cfg.HookEnv
	refreshHookEnv.RefreshReason = reason
	if err := cfg.HookRunner.Run(ctx, "pre-refresh", refreshHookEnv); err != nil {
		logger.Errorf("pre-refresh hook failed: %v", err)
		return nil, nil
	}

	report.Begin(bootstatus.PhaseManifests, fmt.Sprintf("Loading manifests (%s)", reason))
	loadOpts := config.LoadOptions{CollectedDirs: cfg.CollectedDirs, KindProvider: cfg.KindProvider}
	docs, err := config.LoadDirWithOptions(ctx, watchDir, loadOpts)
	if err != nil {
		report.Fail(bootstatus.PhaseManifests, err)
		logger.Errorf("Render failed (%s): %v", reason, err)
		return nil, RuntimeError(err)
	}
	report.End(bootstatus.PhaseManifests)
	// Snapshot for late-arriving consumers (e.g. the explorer init goroutine
	// when it finishes after the first refresh).
	state.lastDocs = docs

	if cfg.HostService != nil {
		cfg.HostService.SetDocuments(plugin.DocumentsFromConfig(docs))
	}

	for _, m := range config.CollectMissingEnvVars(docs) {
		logger.Warnf("unresolved environment variable %s in %s", m.VarName, m.File)
	}

	if explorerSession != nil {
		if err := explorerSession.Refresh(ctx, docs); err != nil {
			logger.Warnf("Explorer refresh: %v", err)
		}
	}

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
		return nil, RuntimeError(err)
	}
	pipeline.LogArtefactWarnings(logger, artifacts)

	documentArtefacts, err := config.CollectDocumentArtefacts(docs)
	if err != nil {
		logger.Errorf("DocumentArtefact scan failed (%s): %v", reason, err)
		return nil, RuntimeError(err)
	}
	pipeline.LogDocumentArtefactWarnings(logger, documentArtefacts)

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

	report.Begin(bootstatus.PhaseGraph, "Building dependency graph")
	g, graphErr := reportgraph.Build(ctx, docs)
	if graphErr != nil {
		logger.Warnf("Graph build skipped: %v", graphErr)
	}
	report.End(bootstatus.PhaseGraph)

	// Decide selective vs full. Selective requires: changed != nil, the graph
	// built successfully, and every changed file maps to at least one graph
	// node. Anything else falls back to full rebuild — safe behavior for
	// new files, deletions, .bnignore changes, plugin assets, etc. Note: on
	// the first refresh state.allPagesFrameHTML is empty, so even if changed
	// is non-nil we must do a full rebuild.
	selective := false
	var affectedReports map[string]struct{}
	var affectedDocArts map[string]struct{}
	renderAllPages := true
	if changed != nil && g != nil && len(state.allPagesFrameHTML) > 0 {
		fileSet := make(map[string]struct{}, len(changed))
		for _, f := range changed {
			fileSet[f] = struct{}{}
		}
		seeds := g.NodesByFile(fileSet)
		seenFiles := make(map[string]struct{}, len(seeds))
		for _, n := range seeds {
			seenFiles[n.File] = struct{}{}
		}
		allMapped := true
		for f := range fileSet {
			if _, ok := seenFiles[f]; !ok {
				allMapped = false
				break
			}
		}
		if allMapped && len(seeds) > 0 {
			reports, dArts := g.AffectedArtefacts(seeds)
			affectedReports = make(map[string]struct{}, len(reports))
			for _, n := range reports {
				affectedReports[n] = struct{}{}
			}
			affectedDocArts = make(map[string]struct{}, len(dArts))
			for _, n := range dArts {
				affectedDocArts[n] = struct{}{}
			}
			renderAllPages = needsAllPagesRerender(seeds)
			selective = true
			logger.Infof("Selective refresh: %d affected artefact(s), %d affected document(s), all-pages=%t", len(affectedReports), len(affectedDocArts), renderAllPages)
		}
	}

	totalRender := 0
	if renderAllPages {
		totalRender++
	}
	totalRender += len(artifacts) + len(documentArtefacts)
	if totalRender > 0 {
		report.Begin(bootstatus.PhaseRendering, fmt.Sprintf("Rendering report (%s)", reason))
	}
	rendered := 0

	// Render "All Pages" if needed. Cache the frame and context HTML so we
	// can reuse them on selective refreshes that don't touch the layout.
	if renderAllPages {
		report.Progress(rendered, totalRender, "All Pages")
		allPagesResult, rerr := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{
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
		if rerr != nil {
			policy := pipeline.ClassifyInvalidLayout(rerr, pipeline.RenderModePreview)
			if policy.IsInvalidRoot {
				logger.Errorf("Render blocked (%s): %s", reason, policy.Message)
				setPreviewErrorPage(server, policy.Message, policy.Hint)
				return nil, nil
			}
			logger.Errorf("Render failed (%s): %v", reason, rerr)
			return nil, RuntimeError(rerr)
		}
		pipeline.LogDiagnostics(logger.Channel("datasource"), allPagesResult.Diagnostics)
		registerEmittedData(server, allPagesResult.EmittedData)
		allPagesFrameHTML := withPreviewHeader(withPreviewStyles(allPagesResult.FrameHTML), artefactInfos, documentInfos, "/", nil)
		pageMeta := buildPageMetadata(docs, artifacts)
		allPagesContextHTML := withPreviewPageMetadata(withPreviewContextStyles(allPagesResult.ContextHTML), pageMeta)
		state.allPagesFrameHTML = allPagesFrameHTML
		state.allPagesContextHTML = allPagesContextHTML
		state.allPagesAssets = pipeline.ConvertLocalAssets(allPagesResult.LocalAssets)
	}

	routeMap := make(map[string]previewhttp.ContentFunc, len(artifacts)+len(documentArtefacts)+1)
	broadcastPaths := make([]string, 0, len(artifacts)+len(documentArtefacts)+1)

	// Broadcast "/" early so any client viewing All Pages sees the new
	// content while we render individual artefacts.
	if renderAllPages {
		server.BroadcastContent("/", state.allPagesContextHTML)
		broadcastPaths = append(broadcastPaths, "/")
		rendered++
		report.Progress(rendered, totalRender, "All Pages")
	}

	// On a full rebuild, drop stale per-artefact assets so deleted artefacts
	// stop leaking into the asset union. Selective rebuilds preserve them
	// because the artefact set is unchanged (deletions fall back to full).
	if !selective {
		for k := range state.perArtefactAssets {
			delete(state.perArtefactAssets, k)
		}
	}

	// Render ReportArtefacts. In selective mode skip those not in the
	// affected set; their cached route entry stays via UpdateContentRoutes.
	for _, art := range artifacts {
		artPath := "/" + art.Document.Name
		if selective {
			if _, ok := affectedReports[art.Document.Name]; !ok {
				continue
			}
		}
		report.Progress(rendered, totalRender, art.Document.Name)
		renderResult, rerr := pipeline.RenderArtefactFrameAndContextWithOptions(ctx, watchDir, docs, art, pipeline.FrameRenderOptions{
			QueryLogger:              cfg.QueryLogger,
			EngineVersion:            cfg.EngineVersion,
			DataValidation:           cfg.DataValidationMode,
			DataValidationSampleSize: cfg.DataValidationSampleSize,
			Session:                  cfg.Session,
			PluginOptions:            cfg.PluginOptions,
			PostRenderHTMLHook:       cfg.PostRenderHTMLHook,
			PostDatasetHook:          cfg.PostDatasetHook,
		})
		if rerr != nil {
			if pipeline.IsInvalidRootError(rerr) {
				logger.Errorf("Render blocked for artefact %s (%s): %v", art.Document.Name, reason, rerr)
				server.BroadcastRefreshError(artPath, rerr.Error())
				continue
			}
			logger.Errorf("Render failed for %s (%s): %v", art.Document.Name, reason, rerr)
			server.BroadcastRefreshError(artPath, rerr.Error())
			return nil, RuntimeError(rerr)
		}
		pipeline.LogDiagnostics(logger.Channel("datasource").Channel(art.Document.Name), renderResult.Diagnostics)
		registerEmittedData(server, renderResult.EmittedData)
		var artGraph *previewGraphData
		if g != nil {
			if rootNode, ok := g.ReportArtefactByName(art.Document.Name); ok {
				artGraph = buildPreviewGraphData(g, rootNode)
			}
		}
		frameHTML := withPreviewHeader(withPreviewStyles(renderResult.FrameHTML), artefactInfos, documentInfos, artPath, artGraph)
		contextHTML := withPreviewContextStyles(renderResult.ContextHTML)
		state.perArtefactAssets[artPath] = pipeline.ConvertLocalAssets(renderResult.LocalAssets)
		routeMap[artPath] = previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
		// Broadcast immediately so the browser can fetch new context HTML
		// while later artefacts are still rendering.
		server.BroadcastContent(artPath, contextHTML)
		broadcastPaths = append(broadcastPaths, artPath)
		rendered++
		report.Progress(rendered, totalRender, art.Document.Name)
	}

	for _, docArt := range documentArtefacts {
		docPath := "/doc/" + docArt.Document.Name
		if selective {
			if _, ok := affectedDocArts[docArt.Document.Name]; !ok {
				continue
			}
		}
		report.Progress(rendered, totalRender, docArt.Document.Name)
		renderResult, rerr := pipeline.RenderDocumentArtefactHTML(ctx, watchDir, docArt, pipeline.DocumentArtefactRenderOptions{
			EngineVersion:      cfg.EngineVersion,
			Session:            cfg.Session,
			PluginOptions:      cfg.PluginOptions,
			KindProvider:       cfg.KindProvider,
			PostRenderHTMLHook: cfg.PostRenderHTMLHook,
			PostDatasetHook:    cfg.PostDatasetHook,
		})
		if rerr != nil {
			logger.Errorf("Render failed for DocumentArtefact %s (%s): %v", docArt.Document.Name, reason, rerr)
			server.BroadcastRefreshError(docPath, rerr.Error())
			continue
		}
		registerEmittedData(server, renderResult.EmittedData)
		state.perArtefactAssets[docPath] = pipeline.ConvertLocalAssets(renderResult.LocalAssets)
		var docGraph *previewGraphData
		if g != nil {
			if rootNode, ok := g.DocumentArtefactByName(docArt.Document.Name); ok {
				docGraph = buildPreviewGraphData(g, rootNode)
			}
		}
		styledHTML := withPreviewStyles(withDocumentPageWidth(renderResult.HTML, docArt.Spec.Format, docArt.Spec.Orientation))
		frameHTML := withPreviewHeader(styledHTML, artefactInfos, documentInfos, docPath, docGraph)
		routeMap[docPath] = previewhttp.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
		rendered++
		report.Progress(rendered, totalRender, docArt.Document.Name)
	}

	// Register lazy presentation routes. On a full rebuild we register all
	// of them (the previous closures captured stale docs). On a selective
	// rebuild we only re-register the affected ones; unaffected /pres/X
	// routes still serve cached output keyed on docs that were correct at
	// last full rebuild — fine because nothing in their dependency tree
	// changed.
	for _, art := range artifacts {
		if selective {
			if _, ok := affectedReports[art.Document.Name]; !ok {
				continue
			}
		}
		presPath := "/pres/" + art.Document.Name
		routeMap[presPath] = lazyPresentationContent(watchDir, docs, art, cfg, server, presPath)
	}

	// Rebuild the asset union and push it to the server. SetLocalAssets
	// replaces the table, so we always send the full union — selective
	// refreshes preserve unchanged entries via the per-artefact cache.
	allAssets := make([]previewhttp.LocalAsset, 0, len(state.allPagesAssets)+len(state.perArtefactAssets)*4)
	allAssets = append(allAssets, state.allPagesAssets...)
	for _, assets := range state.perArtefactAssets {
		allAssets = append(allAssets, assets...)
	}
	server.SetLocalAssets(allAssets)
	if selective {
		server.UpdateContentRoutes(routeMap)
	} else {
		server.SetContentRoutes(routeMap)
	}
	server.SetContentFunc(previewhttp.StaticContent(append([]byte(nil), state.allPagesFrameHTML...), "text/html; charset=utf-8"))
	if totalRender > 0 {
		report.End(bootstatus.PhaseRendering)
	}
	logger.Successf("Content refreshed (%s)", reason)
	return broadcastPaths, nil
}

// needsAllPagesRerender returns true when any seed node is a LayoutPage or a
// ReportArtefact — both affect what the "All Pages" view shows. Other kinds
// (DataSource, DataSet, Component, MarkdownFile, LayoutCard, DocumentArtefact)
// only change what renders inside an artefact, so the All-Pages frame stays
// valid.
func needsAllPagesRerender(seeds []*reportgraph.Node) bool {
	for _, n := range seeds {
		if n == nil {
			continue
		}
		switch n.Kind {
		case reportgraph.NodeLayoutPage, reportgraph.NodeReportArtefact:
			return true
		case reportgraph.NodeDocumentArtefact, reportgraph.NodeLayoutCard, reportgraph.NodeComponent,
			reportgraph.NodeDataSet, reportgraph.NodeDataSource, reportgraph.NodeMarkdownFile:
			// These kinds only change content within artefacts, not the
			// All-Pages frame itself.
		}
	}
	return false
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
	return []byte(
		"\n\t<link id=\"bn-preview-style\" rel=\"stylesheet\" href=\"/__bino/shared/tokens.css\">\n" +
			"\t<link rel=\"stylesheet\" href=\"/__bino/preview/preview.css\">\n" +
			"\t<script type=\"module\" src=\"/__bino/static/preview.js\"></script>\n",
	)
}

// withPreviewStyles injects layout styles and the preview module bundle before </head>.
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
	extra := len(block)
	if len(doc) > math.MaxInt-extra {
		return doc
	}
	updated := make([]byte, 0, len(doc)+extra)
	updated = append(updated, doc[:idx]...)
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
	extra := len(tag)
	if len(doc) > math.MaxInt-extra {
		return doc
	}
	out := make([]byte, 0, len(doc)+extra)
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
