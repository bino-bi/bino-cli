package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/preview/bootstatus"
	"bino.bi/bino/internal/preview/explorer"
	"bino.bi/bino/internal/preview/refresh"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/watchers"
	"bino.bi/bino/internal/web"
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
func newPreviewCommand() *cobra.Command { //nolint:gocognit,funlen // grandfathered complexity — refactor before extending
	var (
		port           int
		workdir        string
		logSQL         bool
		enableLint     bool
		dataValidation string
		dataMode       string
		incremental    bool
		include        []string
		exclude        []string
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
			include = env.Resolver.ResolveStringSlice("artefact", "artefact", include)
			exclude = env.Resolver.ResolveStringSlice("exclude-artefact", "exclude-artefact", exclude)

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
				Include:  strings.Join(include, ","),
				Exclude:  strings.Join(exclude, ","),
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

			server, err := httpserver.New(httpserver.Config{
				ListenAddr:      addr,
				CacheDir:        env.CacheDir,
				Logger:          logger.Channel("server"),
				ExplorerHandler: lazyExplorer,
			})
			if err != nil {
				return RuntimeError(err)
			}

			if loadingPage, lerr := web.LoadingPageHTML(); lerr == nil && len(loadingPage) > 0 {
				server.SetContentFunc(httpserver.StaticContent(loadingPage, "text/html; charset=utf-8"))
			}

			// In url mode, deliberately leave pluginOpts.DataBaseURL empty so the
			// renderer emits relative, same-origin data URLs (/__bino/data/...).
			// The preview server serves both the embed HTML and its data, but it
			// binds 127.0.0.1 while clients (e.g. the VS Code webview iframe) load
			// it via localhost. Browsers treat 127.0.0.1 and localhost as different
			// origins, and the data route sends no CORS headers, so an absolute
			// 127.0.0.1 base would make the engine's cross-origin data fetch fail
			// ("No Data"). Relative URLs resolve against whatever host loaded the
			// document, eliminating the mismatch.

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
				refreshState := refresh.NewState()
				refreshCfg := refresh.Config{
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
					Include:                  include,
					Exclude:                  exclude,
				}
				if env.PluginManager != nil {
					refreshCfg.HostService = env.PluginManager.HostService()
				}

				// Wire the embedding endpoint to the refresh state and config so
				// /__embedding/{name} can render any named artefact, LayoutPage or
				// standalone component as build-equivalent isolated HTML.
				server.SetEmbeddingFunc(func(ctx context.Context, name, kind, language string) ([]byte, error) {
					return refresh.EmbedByName(ctx, name, kind, language, refreshMu, refreshState, &refreshCfg, server)
				})

				// Wire the buffer-override endpoint so the VS Code extension can
				// push unsaved editor content for a manifest. EmbedByName then
				// renders that file's component straight from the buffer (a fresh
				// overlaid load) instead of disk — no auto-save, no full refresh.
				projectRoot := env.ProjectRoot
				server.SetEmbeddingOverrideFunc(func(file, content string, remove bool) error {
					if !pathWithinRoot(projectRoot, file) {
						return httpserver.NewHTTPError(http.StatusForbidden, "file is outside the project root")
					}
					refreshMu.Lock()
					defer refreshMu.Unlock()
					if remove {
						refreshState.ClearLiveOverride(file)
					} else {
						refreshState.SetLiveOverride(file, content)
					}
					return nil
				})

				doRefresh := func(reason string, changed []string) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					refreshMu.Lock()
					defer refreshMu.Unlock()
					if err := ctx.Err(); err != nil {
						return err
					}
					server.BroadcastRefreshing(reason)
					broadcastPaths, err := refresh.Run(ctx, reason, changed, server, explorerSlot.Load(), &refreshCfg, refreshState)
					if err != nil && ctx.Err() == nil {
						server.BroadcastRefreshError("", err.Error())
					}
					server.BroadcastRefreshDone(broadcastPaths)
					if err != nil {
						return RuntimeError(err)
					}
					return nil
				}

				// 3. Initial refresh — failure no longer aborts the server,
				// the loading page surfaces the error and the watcher will
				// retry once the user fixes the source.
				var visitedDirs []string
				refreshCfg.CollectedDirs = &visitedDirs
				initialErr := doRefresh("initial load", nil)
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
					docs := refreshState.LastDocs()
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
				refreshCh := make(chan refresh.Request, 16)
				enqueue := func(req refresh.Request) {
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
						req := refresh.Request{Reason: fmt.Sprintf("change %s", evt.RelativePath)}
						if incremental && evt.Path != "" {
							req.Files = []string{evt.Path}
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
					var pending []refresh.Request
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
							coalesced, files := refresh.MergeRequests(pending)
							pending = pending[:0]
							// refresh.Run logs its own failures with reason
							// context; logging here would duplicate the output.
							_ = doRefresh(coalesced, files)
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
	cmd.Flags().StringSliceVar(&include, "artefact", nil, "metadata.name entries to preview (default: all)")
	cmd.Flags().StringSliceVar(&exclude, "exclude-artefact", nil, "metadata.name entries to skip")

	// Accept both UK and US spellings for artefact flags
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		switch name {
		case "artifact":
			return pflag.NormalizedName("artefact")
		case "exclude-artifact":
			return pflag.NormalizedName("exclude-artefact")
		}
		return pflag.NormalizedName(name)
	})

	return cmd
}

// pathWithinRoot reports whether file resolves to a location inside root.
// Both are made absolute and cleaned; a relative path that escapes via ".."
// (or an entirely different tree) is rejected. This guards the buffer-override
// endpoint so a client cannot push content for arbitrary files on disk.
func pathWithinRoot(root, file string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absFile, err := filepath.Abs(file)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absFile)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
