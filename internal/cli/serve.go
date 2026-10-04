package cli

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/report/serve"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/pkg/duckdb"
)

const defaultServePort = 8080

// defaultServeDataMode differs from preview and build: a served page carries
// its rows itself, so it does not depend on a second request.
const defaultServeDataMode = render.DataModeInline

// newServeCommand creates the serve subcommand for production serving.
// Unlike preview, serve:
//   - Does not watch for file changes
//   - Renders on-demand per request (with caching)
//   - Uses query parameters for dynamic variable substitution
//   - Serves a single LiveReportArtefact with navigation
func newServeCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity - refactor before extending
	var (
		port     int
		workdir  string
		live     string
		logSQL   bool
		addr     string
		dataMode string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve a live report application for production",
		Long: strings.TrimSpace(`Serve a LiveReportArtefact as a production web application.
Unlike preview, serve does not watch for file changes and renders on-demand
per request. Query parameters defined in the LiveReportArtefact spec are
substituted into report documents using ${VAR} syntax.

Environment knobs:
  - BNR_MAX_QUERY_ROWS (default 100k)
  - BNR_MAX_QUERY_DURATION_MS (default 60s)
  - BNR_CDN_MAX_BYTES (default 50 MB)
  - BNR_CDN_TIMEOUT_MS (default 10s)`),
		Example: strings.TrimSpace(`  bino serve --live my-dashboard
  bino serve --live my-dashboard --port 8080
  bino serve --live my-dashboard --work-dir ./reports --addr 0.0.0.0:8080`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("serve")

			env, err := initCommandEnv(ctx, cmd, workdir, "serve", logger)
			if err != nil {
				return err
			}
			if env.PluginManager != nil {
				defer env.PluginManager.ShutdownAll(ctx)
			}

			port = env.Resolver.ResolveInt("port", "port", port)
			logSQL = env.Resolver.ResolveBool("log-sql", "log-sql", logSQL)
			live = env.Resolver.ResolveString("live", "live", live)
			dataMode = env.Resolver.ResolveString("data-mode", "data-mode", dataMode)
			resolvedDataMode, err := resolveServeDataMode(dataMode)
			if err != nil {
				return RuntimeError(err)
			}

			// Determine listen address
			if addr == "" {
				addr = fmt.Sprintf("127.0.0.1:%d", port)
			}

			// Validate --live flag is provided
			if live == "" {
				return ConfigErrorf("--live flag is required: specify the name of a LiveReportArtefact to serve")
			}

			logger.Infof("Starting serve on %s", addr)
			logger.Infof("Project directory %s", env.ProjectRoot)
			logger.Infof("Serving LiveReportArtefact %q", live)

			queryLogger := newQueryLogger(ctx, logger, logSQL)

			// Create a shared DuckDB session for the lifetime of the serve process.
			// Extensions are loaded once; views and queries reuse the session.
			duckdbOpts, err := duckdb.DefaultOptions()
			if err != nil {
				return RuntimeError(err)
			}
			duckdbOpts.QueryLogger = queryLogger
			sharedSession, err := duckdb.OpenSession(ctx, duckdbOpts)
			if err != nil {
				return RuntimeError(err)
			}
			defer sharedSession.Close() //nolint:errcheck // in-memory session teardown at shutdown

			if err := sharedSession.InstallAndLoadExtensions(ctx, duckdb.DefaultExtensions()); err != nil {
				return RuntimeError(err)
			}

			// Load documents once at startup
			docs, err := config.LoadDirWithOptions(ctx, env.ProjectRoot, config.LoadOptions{KindProvider: env.PluginRegistry})
			if err != nil {
				return ConfigError(err)
			}

			// Collect live artifacts and find the requested one
			liveArtefacts, err := config.CollectLiveArtefacts(docs)
			if err != nil {
				return ConfigError(err)
			}

			liveArtefact := config.FindLiveArtefact(liveArtefacts, live)
			if liveArtefact == nil {
				var available []string
				for _, la := range liveArtefacts {
					available = append(available, la.Document.Name)
				}
				if len(available) == 0 {
					return ConfigErrorf("LiveReportArtefact %q not found; no LiveReportArtefact documents exist", live)
				}
				return ConfigErrorf("LiveReportArtefact %q not found; available: %s", live, strings.Join(available, ", "))
			}

			// Collect all query param names from the live artifact to exclude from env var check
			// For select type params with static items, also exclude {name}_LABEL
			excludeNames := make(map[string]struct{})
			for _, route := range liveArtefact.Spec.Routes {
				for _, p := range route.QueryParams {
					excludeNames[p.Name] = struct{}{}
					// For select params with static items, also exclude the _LABEL variant
					if p.Type == "select" && p.Options != nil && len(p.Options.Items) > 0 {
						excludeNames[p.Name+"_LABEL"] = struct{}{}
					}
				}
			}

			// Also exclude LayoutPage param names (they're resolved at render time)
			for name := range config.CollectLayoutPageParamNames(docs) {
				excludeNames[name] = struct{}{}
			}

			// Check for missing env vars - exclude query params and layout page params
			if err := config.CheckMissingEnvVarsExcluding(docs, excludeNames); err != nil {
				return ConfigError(err)
			}

			// Collect report artifacts for validation
			artifacts, err := config.CollectArtefacts(docs)
			if err != nil {
				return ConfigError(err)
			}

			// Collect LayoutPage names for validation
			layoutPageNames := make(map[string]struct{})
			for _, doc := range docs {
				if doc.Kind == "LayoutPage" {
					layoutPageNames[doc.Name] = struct{}{}
				}
			}

			// Collect Asset info for PWA icon validation
			assetInfos, err := collectAssetInfos(docs)
			if err != nil {
				return ConfigError(err)
			}

			// Validate the live artifact
			if err := config.ValidateLiveArtefact(*liveArtefact, artifacts, layoutPageNames, assetInfos); err != nil {
				return ConfigError(err)
			}

			// Build artifact lookup map
			artefactMap := make(map[string]config.Artifact, len(artifacts))
			for _, a := range artifacts {
				artefactMap[a.Document.Name] = a
			}

			// Generate PWA payloads (manifest, service worker, icon assets)
			// when the artefact opts in via spec.pwa. Icon files are resolved
			// here so a missing file fails at startup, not at request time.
			pwaContent, err := serve.BuildPWAContent(*liveArtefact, docs, env.EngineVersion)
			if err != nil {
				return ConfigError(err)
			}

			// Create the server
			server, err := httpserver.New(serveServerConfig(addr, env.CacheDir, logger.Channel("server")))
			if err != nil {
				return RuntimeError(err)
			}

			// Run pre-serve hook (once, before route setup)
			serveHookEnv := hooks.HookEnv{
				Mode:         "serve",
				Workdir:      env.ProjectRoot,
				ReportID:     env.ProjectCfg.ReportID,
				Verbose:      logx.DebugEnabled(ctx),
				ListenAddr:   addr,
				LiveArtefact: live,
			}
			if err := env.HookRunner.Run(ctx, "pre-serve", serveHookEnv); err != nil {
				return RuntimeError(err)
			}

			// Set up plugin integration for serve pipeline.
			var servePluginOpts *render.PluginOptions
			var servePostRenderHook func(context.Context, []byte) ([]byte, error)
			var servePostDatasetHook func(context.Context, []pipeline.DatasetPayload) error
			var serveHostSvcRef *plugin.BinoHostServer
			if env.PluginManager != nil {
				serveHostSvcRef = env.PluginManager.HostService()
				serveHostSvcRef.SetDocuments(plugin.DocumentsFromConfig(docs))
				serveHostSvcRef.SetDefaultDuckDBOpener()
			}
			if env.PluginRegistry != nil {
				servePluginOpts = plugin.BuildRenderOptions(ctx, env.PluginRegistry, env.ProjectRoot, "preview")
				hookBus := plugin.NewHookBus(env.PluginRegistry, logger.Channel("plugin-hooks"))
				servePostRenderHook = func(hookCtx context.Context, htmlData []byte) ([]byte, error) {
					modified, _, err := hookBus.DispatchPostRenderHTML(hookCtx, htmlData)
					return modified, err
				}
				servePostDatasetHook = func(hookCtx context.Context, datasets []pipeline.DatasetPayload) error {
					pluginDatasets := make([]plugin.DatasetPayload, len(datasets))
					for i, ds := range datasets {
						pluginDatasets[i] = plugin.DatasetPayload{Name: ds.Name, JSONRows: ds.JSONRows, Columns: ds.Columns}
					}
					if serveHostSvcRef != nil {
						serveHostSvcRef.SetDatasets(pluginDatasets)
					}
					_, _, err := hookBus.DispatchPostDatasetExecute(hookCtx, pluginDatasets)
					return err
				}
			}
			servePluginOpts = applyServeDataMode(servePluginOpts, resolvedDataMode)

			// Set up routes and assets
			routeSetup, err := setupServeRoutes(serveRouteConfig{
				LiveArtefact:       *liveArtefact,
				ArtefactMap:        artefactMap,
				HookRunner:         env.HookRunner,
				HookEnv:            serveHookEnv,
				Logger:             logger,
				Workdir:            env.ProjectRoot,
				BaseDocs:           docs,
				QueryLogger:        queryLogger,
				EngineVersion:      env.EngineVersion,
				Session:            sharedSession,
				KindProvider:       env.PluginRegistry,
				PluginOptions:      servePluginOpts,
				PostRenderHTMLHook: servePostRenderHook,
				PostDatasetHook:    servePostDatasetHook,
				HostService:        serveHostSvcRef,
				Server:             server,
				PWA:                pwaContent,

				LayoutPageTemplates: loadLayoutPageTemplates(ctx, logger, env.ProjectRoot, env.PluginRegistry, *liveArtefact),
			})
			if err != nil {
				return ConfigError(err)
			}
			server.SetContentRoutes(routeSetup.RouteMap)
			if routeSetup.RootContent != nil {
				server.SetContentFunc(routeSetup.RootContent)
			}
			serveAssets := collectServeAssets(ctx, logger, *liveArtefact, artefactMap, env.ProjectRoot, docs, env.EngineVersion, sharedSession, servePluginOpts, servePostRenderHook, servePostDatasetHook)
			if pwaContent != nil {
				// Icons referenced only by the pwa block are not harvested by
				// the pre-render pass above, so register them explicitly.
				serveAssets = append(serveAssets, pipeline.ConvertLocalAssets(pwaContent.LocalAssets)...)
			}
			server.SetLocalAssets(serveAssets)

			url := server.URL()
			logger.Successf("Serving at %s", url)
			logger.Infof("Press Ctrl+C to stop")

			if err := server.Start(ctx); err != nil {
				return RuntimeError(err)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", defaultServePort, "Port to run the server on")
	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory containing bino manifests")
	cmd.Flags().StringVar(&live, "live", "", "Name of the LiveReportArtefact to serve (required)")
	cmd.Flags().BoolVar(&logSQL, "log-sql", false, "Log all executed SQL queries to terminal")
	cmd.Flags().StringVar(&addr, "addr", "", "Full listen address (overrides --port, e.g. 0.0.0.0:8080)")
	cmd.Flags().StringVar(&dataMode, "data-mode", defaultServeDataMode,
		"Dataset/datasource delivery: 'inline' embeds gzip+base64 in the HTML (default), 'url' fetches data via HTTP from the bino server")

	return cmd
}

// serveServerConfig is the HTTP server configuration of serve. Its pages and
// data bodies are one viewer's result, so no cache may store them.
func serveServerConfig(addr, cacheDir string, logger logx.Logger) httpserver.Config {
	return httpserver.Config{
		ListenAddr: addr,
		CacheDir:   cacheDir,
		Logger:     logger,
		NoStore:    true,
	}
}

// serveRequestContext holds the result of processing query parameters for a serve request.
type serveRequestContext struct {
	ReqInfo     httpserver.RequestInfo
	QueryParams map[string]string
	Docs        []config.Document // Documents reloaded with query params (or baseDocs if no params)
}

// prepareServeRequest processes query parameters for a serve request.
// Returns nil and missing params HTML if validation fails.
// Returns the request context with reloaded documents if successful.
func prepareServeRequest(
	ctx context.Context,
	logger logx.Logger,
	workdir string,
	baseDocs []config.Document,
	routeSpec config.LiveRouteSpec,
	liveArtefact config.LiveArtefact,
	routePath string,
	session *duckdb.Session,
	kindProvider config.KindProvider,
) (*serveRequestContext, []byte, error) {
	reqInfo := httpserver.GetRequestInfo(ctx)

	// Validate and merge query parameters
	validation := serve.ValidateAndMergeQueryParams(routeSpec, reqInfo.Query)

	queryParams := validation.Params

	// If there are missing required params, return missing params HTML
	if !validation.IsValid() {
		optionDocs, err := selectOptionDocs(ctx, logger, workdir, baseDocs, routeSpec, queryParams, kindProvider)
		if err != nil {
			return nil, nil, err
		}
		datasetOptions := serve.ResolveDatasetOptions(ctx, workdir, optionDocs, routeSpec, session)
		html := serve.BuildMissingParamsHTML(liveArtefact, routePath, routeSpec, reqInfo.RawQuery, validation.MissingNames, datasetOptions)
		return nil, html, nil
	}

	docs, err := reloadServeDocs(ctx, logger, workdir, baseDocs, queryParams, kindProvider)
	if err != nil {
		return nil, nil, err
	}

	return &serveRequestContext{
		ReqInfo:     reqInfo,
		QueryParams: queryParams,
		Docs:        docs,
	}, nil, nil
}

// reloadServeDocs returns the documents of a request: the project reloaded
// with queryParams as variables, or baseDocs when there are none. A rejected
// request value is not in queryParams.
func reloadServeDocs(ctx context.Context, logger logx.Logger, workdir string, baseDocs []config.Document, queryParams map[string]string, kindProvider config.KindProvider) ([]config.Document, error) {
	if len(queryParams) == 0 {
		return baseDocs, nil
	}
	lookup := config.ChainLookup(config.MapLookup(queryParams), config.EnvLookup())
	docs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{
		Lookup:       lookup,
		KindProvider: kindProvider,
	})
	if err != nil {
		logger.Errorf("Reload failed with query params: %v", err)
		return nil, err
	}
	return docs, nil
}

// selectOptionDocs returns the documents to resolve the select options of a
// route with. Options that come from a DataSet must be resolved with the
// params of the request, so they need the reloaded documents. A route without
// such options never reads the documents, and the reload is skipped. This
// keeps a cached page from reading the manifests again.
func selectOptionDocs(ctx context.Context, logger logx.Logger, workdir string, baseDocs []config.Document, routeSpec config.LiveRouteSpec, queryParams map[string]string, kindProvider config.KindProvider) ([]config.Document, error) {
	for _, p := range routeSpec.QueryParams {
		if p.Options != nil && p.Options.Dataset != "" {
			return reloadServeDocs(ctx, logger, workdir, baseDocs, queryParams, kindProvider)
		}
	}
	return baseDocs, nil
}

// serveRouteConfig holds configuration for setting up serve routes.
type serveRouteConfig struct {
	LiveArtefact       config.LiveArtefact
	ArtefactMap        map[string]config.Artifact
	HookRunner         *hooks.Runner
	HookEnv            hooks.HookEnv
	Logger             logx.Logger
	Workdir            string
	BaseDocs           []config.Document
	QueryLogger        func(string)
	EngineVersion      string
	Session            *duckdb.Session
	KindProvider       config.KindProvider
	PluginOptions      *render.PluginOptions
	PostRenderHTMLHook func(ctx context.Context, html []byte) ([]byte, error)
	PostDatasetHook    func(ctx context.Context, datasets []pipeline.DatasetPayload) error
	HostService        *plugin.BinoHostServer
	// Server serves the dataset/datasource payloads of the rendered pages
	// when the renderer runs in url mode. May be nil in inline mode.
	Server *httpserver.Server
	// PWA holds the generated manifest and service worker; nil when the
	// artefact has no spec.pwa block.
	PWA *serve.PWAContent
	// LayoutPageTemplates holds, per route path, the layoutPages with the
	// query param references in entry params not yet filled in. See
	// loadLayoutPageTemplates. May be nil.
	LayoutPageTemplates map[string]config.LayoutPagesOrRefs
}

// serveRouteSetup holds the results of route setup.
type serveRouteSetup struct {
	RouteMap    map[string]httpserver.ContentFunc
	RootContent httpserver.ContentFunc // nil if "/" not in routes
}

// setupServeRoutes builds the route map and root content function from a LiveReportArtefact.
func setupServeRoutes(cfg serveRouteConfig) (*serveRouteSetup, error) {
	renderCache := newServeRenderCache()
	if cfg.Server != nil {
		cfg.Server.SetDataFunc(renderCache.data)
	}
	routeMap := make(map[string]httpserver.ContentFunc)

	// renderMu serializes request handling across the shared DuckDB session:
	// renders execute ATTACH and CREATE OR REPLACE VIEW with request-scoped
	// ${VAR} values, so concurrent renders would race on session bookkeeping
	// and could read another request's views. See pkg/duckdb.Session docs.
	var renderMu sync.Mutex

	for path, route := range cfg.LiveArtefact.Spec.Routes {
		routePath := path
		routeSpec := route

		if route.Artifact != "" {
			art, ok := cfg.ArtefactMap[route.Artifact]
			if !ok {
				return nil, fmt.Errorf("route %q references unknown artefact %q", path, route.Artifact)
			}
			routeArt := art

			routeMap[routePath] = func(reqCtx context.Context) ([]byte, string, error) {
				if err := cfg.HookRunner.Run(reqCtx, "pre-request", cfg.HookEnv); err != nil {
					return nil, "", err
				}
				renderMu.Lock()
				defer renderMu.Unlock()
				return serveRenderHandler(
					reqCtx, cfg.Logger, renderCache, cfg.Workdir, cfg.BaseDocs, routeArt,
					cfg.LiveArtefact, routePath, routeSpec, cfg.QueryLogger, cfg.EngineVersion, cfg.Session,
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService,
				)
			}
		} else {
			routeLayoutPages := route.LayoutPages
			routeTemplates := cfg.LayoutPageTemplates[path]

			routeMap[routePath] = func(reqCtx context.Context) ([]byte, string, error) {
				if err := cfg.HookRunner.Run(reqCtx, "pre-request", cfg.HookEnv); err != nil {
					return nil, "", err
				}
				renderMu.Lock()
				defer renderMu.Unlock()
				return serveLayoutPagesHandler(
					reqCtx, cfg.Logger, renderCache, cfg.Workdir, cfg.BaseDocs, routeLayoutPages, routeTemplates,
					cfg.LiveArtefact, routePath, routeSpec, cfg.QueryLogger, cfg.EngineVersion, cfg.Session,
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService,
				)
			}
		}
	}

	// PWA serving paths. These are reserved route paths
	// (config.ReservedLiveRoutePaths), so they can never collide with a
	// tenant route. They must live in the route map: once the map is
	// non-empty, lookupContentFunc hard-404s any path not present.
	if cfg.PWA != nil {
		routeMap["/manifest.webmanifest"] = httpserver.StaticContent(cfg.PWA.Manifest, "application/manifest+json")
		routeMap["/sw.js"] = httpserver.StaticContent(cfg.PWA.ServiceWorker, "text/javascript; charset=utf-8")
	}

	setup := &serveRouteSetup{RouteMap: routeMap}

	// Set default content function for root if "/" is in routes
	if rootRoute, ok := cfg.LiveArtefact.Spec.Routes["/"]; ok {
		rootSpec := rootRoute
		if rootRoute.Artifact != "" {
			rootArt := cfg.ArtefactMap[rootRoute.Artifact]
			setup.RootContent = func(reqCtx context.Context) ([]byte, string, error) {
				if err := cfg.HookRunner.Run(reqCtx, "pre-request", cfg.HookEnv); err != nil {
					return nil, "", err
				}
				renderMu.Lock()
				defer renderMu.Unlock()
				return serveRenderHandler(
					reqCtx, cfg.Logger, renderCache, cfg.Workdir, cfg.BaseDocs, rootArt,
					cfg.LiveArtefact, "/", rootSpec, cfg.QueryLogger, cfg.EngineVersion, cfg.Session,
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService,
				)
			}
		} else {
			rootLayoutPages := rootRoute.LayoutPages
			rootTemplates := cfg.LayoutPageTemplates["/"]
			setup.RootContent = func(reqCtx context.Context) ([]byte, string, error) {
				if err := cfg.HookRunner.Run(reqCtx, "pre-request", cfg.HookEnv); err != nil {
					return nil, "", err
				}
				renderMu.Lock()
				defer renderMu.Unlock()
				return serveLayoutPagesHandler(
					reqCtx, cfg.Logger, renderCache, cfg.Workdir, cfg.BaseDocs, rootLayoutPages, rootTemplates,
					cfg.LiveArtefact, "/", rootSpec, cfg.QueryLogger, cfg.EngineVersion, cfg.Session,
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService,
				)
			}
		}
	}

	return setup, nil
}

// resolveServeDataMode validates the --data-mode value of serve. An empty
// value, from the flag or from bino.toml, means the serve default.
func resolveServeDataMode(s string) (string, error) {
	if strings.TrimSpace(s) == "" {
		s = defaultServeDataMode
	}
	return normalizeDataMode(s)
}

// applyServeDataMode configures url-mode data emission on the serve plugin
// options; any other mode returns opts unchanged. DataBaseURL is deliberately
// left empty so the renderer emits relative, same-origin data URLs
// (/__bino/data/...): serve binds one host (e.g. 127.0.0.1) but clients may
// load the page via another (localhost, a reverse-proxy hostname), and the
// data route sends no CORS headers, so an absolute base pinned to the bind
// address makes every data fetch fail cross-origin ("No Data"). Relative
// bodies are fetched by every supported engine (the >=1.0.0-alpha.19 floor
// postdates same-origin path support, added in alpha.14).
func applyServeDataMode(opts *render.PluginOptions, resolvedDataMode string) *render.PluginOptions {
	if resolvedDataMode != render.DataModeURL {
		return opts
	}
	if opts == nil {
		opts = &render.PluginOptions{}
	}
	opts.DataMode = render.DataModeURL
	return opts
}

// collectAssetInfos summarizes the project's Asset documents for
// ValidateLiveArtefact's PWA icon checks (spec.type and whether the source is
// a local file).
func collectAssetInfos(docs []config.Document) (map[string]config.AssetInfo, error) {
	assetInfos := make(map[string]config.AssetInfo)
	for _, doc := range docs {
		if doc.Kind != "Asset" {
			continue
		}
		var payload struct {
			Spec struct {
				Type   string `json:"type"`
				Source struct {
					LocalPath string `json:"localPath"`
				} `json:"source"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("parse Asset %s: %w", doc.Name, err)
		}
		assetInfos[doc.Name] = config.AssetInfo{
			Type:         payload.Spec.Type,
			HasLocalPath: payload.Spec.Source.LocalPath != "",
		}
	}
	return assetInfos, nil
}

// collectServeAssets pre-renders routes to collect all local assets needed for serving.
func collectServeAssets(
	ctx context.Context, logger logx.Logger, liveArtefact config.LiveArtefact,
	artefactMap map[string]config.Artifact, watchDir string, docs []config.Document,
	engineVersion string, session *duckdb.Session, pluginOpts *render.PluginOptions,
	postRenderHook func(context.Context, []byte) ([]byte, error),
	postDatasetHook func(context.Context, []pipeline.DatasetPayload) error,
) []httpserver.LocalAsset {
	allAssets := make([]httpserver.LocalAsset, 0)
	for _, route := range liveArtefact.Spec.Routes {
		if route.Artifact != "" {
			art := artefactMap[route.Artifact]
			renderResult, err := pipeline.RenderArtefactFrameAndContextWithModeAndOptions(ctx, watchDir, docs, art, spec.ModeServe, pipeline.FrameRenderOptions{
				EngineVersion:      engineVersion,
				Session:            session,
				PluginOptions:      pluginOpts,
				PostRenderHTMLHook: postRenderHook,
				PostDatasetHook:    postDatasetHook,
			})
			if err != nil {
				logger.Warnf("Could not pre-render artefact %s for asset collection: %v", art.Document.Name, err)
				continue
			}
			allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
		} else {
			renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{
				Workdir:            watchDir,
				Mode:               pipeline.RenderModeServe,
				EngineVersion:      engineVersion,
				Session:            session,
				PluginOptions:      pluginOpts,
				PostRenderHTMLHook: postRenderHook,
				PostDatasetHook:    postDatasetHook,
			})
			if err != nil {
				logger.Warnf("Could not pre-render layoutPages route for asset collection: %v", err)
				continue
			}
			allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
		}
	}
	return allAssets
}

// maxServeRenderCacheEntries bounds the render cache. Every distinct
// query-param combination creates an entry and params arrive from untrusted
// clients, so an unbounded map is a memory-growth vector on the production
// serve surface.
const maxServeRenderCacheEntries = 100

// serveRenderCache provides thread-safe caching for rendered content with
// LRU eviction once maxServeRenderCacheEntries is exceeded.
type serveRenderCache struct {
	mu    sync.Mutex
	cache map[string]*list.Element
	lru   *list.List // front=oldest, back=most recently used
}

// serveRenderCacheItem is the LRU list element value; it stores its own key
// so eviction can delete the map entry in O(1).
type serveRenderCacheItem struct {
	key   string
	entry *serveRenderEntry
}

type serveRenderEntry struct {
	frameHTML   []byte
	contextHTML []byte
	assets      []render.LocalAsset
	// emitted is the set of dataset/datasource bodies the page fetches in url
	// mode. The data route serves them from here (see data), so a body is
	// available for as long as the page that points at it is cached.
	emitted []render.EmittedData
}

func newServeRenderCache() *serveRenderCache {
	return &serveRenderCache{
		cache: make(map[string]*list.Element),
		lru:   list.New(),
	}
}

func (c *serveRenderCache) Get(key string) (*serveRenderEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.cache[key]
	if !ok {
		return nil, false
	}
	c.lru.MoveToBack(elem)
	item, _ := elem.Value.(*serveRenderCacheItem)
	return item.entry, true
}

func (c *serveRenderCache) Set(key string, entry *serveRenderEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.cache[key]; ok {
		item, _ := elem.Value.(*serveRenderCacheItem)
		item.entry = entry
		c.lru.MoveToBack(elem)
		return
	}
	c.cache[key] = c.lru.PushBack(&serveRenderCacheItem{key: key, entry: entry})
	for c.lru.Len() > maxServeRenderCacheEntries {
		oldest := c.lru.Front()
		item, _ := oldest.Value.(*serveRenderCacheItem)
		delete(c.cache, item.key)
		c.lru.Remove(oldest)
	}
}

// data returns the body of a dataset/datasource payload that a cached page
// references. A page holds few payloads and the cache is small, so it scans,
// newest page first.
func (c *serveRenderCache) data(kind, name, hash string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for elem := c.lru.Back(); elem != nil; elem = elem.Prev() {
		item, _ := elem.Value.(*serveRenderCacheItem)
		for _, e := range item.entry.emitted {
			if e.Kind == kind && e.Name == name && e.Hash == hash {
				return e.Body, true
			}
		}
	}
	return nil, false
}

// serveRenderHandler handles on-demand rendering for a route with query param substitution.
func serveRenderHandler(
	ctx context.Context,
	logger logx.Logger,
	cache *serveRenderCache,
	workdir string,
	baseDocs []config.Document,
	artifact config.Artifact,
	liveArtefact config.LiveArtefact,
	routePath string,
	routeSpec config.LiveRouteSpec,
	queryLogger func(string),
	engineVersion string,
	session *duckdb.Session,
	kindProvider config.KindProvider,
	pluginOpts *render.PluginOptions,
	postRenderHook func(context.Context, []byte) ([]byte, error),
	postDatasetHook func(context.Context, []pipeline.DatasetPayload) error,
	hostService *plugin.BinoHostServer,
) (body []byte, contentType string, err error) {
	// Extract query parameters from request context
	reqInfo := httpserver.GetRequestInfo(ctx)

	// Validate and merge query parameters
	validation := serve.ValidateAndMergeQueryParams(routeSpec, reqInfo.Query)
	queryParams := validation.Params

	// If there are missing required params, show the sidebar with error indicators
	if !validation.IsValid() {
		// Resolve dataset options for select parameters (needed for sidebar)
		optionDocs, err := selectOptionDocs(ctx, logger, workdir, baseDocs, routeSpec, queryParams, kindProvider)
		if err != nil {
			return nil, "", err
		}
		datasetOptions := serve.ResolveDatasetOptions(ctx, workdir, optionDocs, routeSpec, session)
		return serve.BuildMissingParamsHTML(liveArtefact, routePath, routeSpec, reqInfo.RawQuery, validation.MissingNames, datasetOptions), "text/html; charset=utf-8", nil
	}

	// Build cache key from artifact name + sorted query params
	cacheKey := buildCacheKey(artifact.Document.Name, queryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		optionDocs, err := selectOptionDocs(ctx, logger, workdir, baseDocs, routeSpec, queryParams, kindProvider)
		if err != nil {
			return nil, "", err
		}
		return serve.BuildHTML(ctx, entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery, workdir, optionDocs, session), "text/html; charset=utf-8", nil
	}

	// If we have query params, reload documents with query params as variables
	docs, err := reloadServeDocs(ctx, logger, workdir, baseDocs, queryParams, kindProvider)
	if err != nil {
		return nil, "", err
	}
	currentArtefact := artifact
	if len(queryParams) > 0 {
		// Update host service with reloaded documents.
		if hostService != nil {
			hostService.SetDocuments(plugin.DocumentsFromConfig(docs))
		}

		// Re-collect artifacts to get the one with expanded query params
		artifacts, err := config.CollectArtefacts(docs)
		if err != nil {
			logger.Errorf("Collect artefacts failed for %s: %v", artifact.Document.Name, err)
			return nil, "", err
		}

		// Find the matching artifact by name
		found := false
		for _, a := range artifacts {
			if a.Document.Name == artifact.Document.Name {
				currentArtefact = a
				found = true
				break
			}
		}
		if !found {
			logger.Errorf("Artefact %s not found after reload", artifact.Document.Name)
			return nil, "", fmt.Errorf("artefact %s not found after reload", artifact.Document.Name)
		}
	}

	// Render the artifact with serve mode for constraint evaluation
	renderResult, err := pipeline.RenderArtefactFrameAndContextWithModeAndOptions(ctx, workdir, docs, currentArtefact, spec.ModeServe, pipeline.FrameRenderOptions{
		QueryLogger:        queryLogger,
		EngineVersion:      engineVersion,
		Session:            session,
		PluginOptions:      pluginOpts,
		PostRenderHTMLHook: postRenderHook,
		PostDatasetHook:    postDatasetHook,
	})
	if err != nil {
		logger.Errorf("Render failed for %s: %v", artifact.Document.Name, err)
		return nil, "", err
	}

	pipeline.LogDiagnostics(logger.Channel("datasource").Channel(artifact.Document.Name), renderResult.Diagnostics)

	// Apply serve styles
	frameHTML := serve.WithStyles(renderResult.FrameHTML)
	contextHTML := renderResult.ContextHTML

	// Cache the result
	cache.Set(cacheKey, &serveRenderEntry{
		frameHTML:   frameHTML,
		contextHTML: contextHTML,
		assets:      renderResult.LocalAssets,
		emitted:     renderResult.EmittedData,
	})

	return serve.BuildHTML(ctx, frameHTML, contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery, workdir, docs, session), "text/html; charset=utf-8", nil
}

// serveLayoutPagesHandler handles on-demand rendering for a route with layoutPages.
func serveLayoutPagesHandler(
	ctx context.Context,
	logger logx.Logger,
	cache *serveRenderCache,
	workdir string,
	baseDocs []config.Document,
	layoutPages config.LayoutPagesOrRefs,
	templates config.LayoutPagesOrRefs,
	liveArtefact config.LiveArtefact,
	routePath string,
	routeSpec config.LiveRouteSpec,
	queryLogger func(string),
	engineVersion string,
	session *duckdb.Session,
	kindProvider config.KindProvider,
	pluginOpts *render.PluginOptions,
	postRenderHook func(context.Context, []byte) ([]byte, error),
	postDatasetHook func(context.Context, []pipeline.DatasetPayload) error,
	hostService *plugin.BinoHostServer,
) (body []byte, contentType string, err error) {
	// Process query parameters and reload documents if needed
	reqCtx, missingParamsHTML, err := prepareServeRequest(ctx, logger, workdir, baseDocs, routeSpec, liveArtefact, routePath, session, kindProvider)
	if err != nil {
		return nil, "", err
	}
	if missingParamsHTML != nil {
		return missingParamsHTML, "text/html; charset=utf-8", nil
	}

	// Update host service with reloaded documents (when query params caused a reload).
	if hostService != nil && len(reqCtx.QueryParams) > 0 {
		hostService.SetDocuments(plugin.DocumentsFromConfig(reqCtx.Docs))
	}

	// Build cache key from route + layout pages + sorted query params. Two
	// routes can list the same pages and still render differently: in another
	// order, or with entry params that take different query params.
	cacheKey := routePath + " " + buildLayoutPagesCacheKey(layoutPages, reqCtx.QueryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		return serve.BuildHTML(ctx, entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqCtx.ReqInfo.RawQuery, workdir, reqCtx.Docs, session), "text/html; charset=utf-8", nil
	}

	// Select the route's pages like a ReportArtefact does: globs, entry params,
	// a page listed more than once, and route order.
	pageParams := plainQueryParams(reqCtx.QueryParams, routeSpec.GetQueryParamDefaults())
	docs := withQueryParamDefaults(selectablePages(reqCtx.Docs, baseDocs), pageParams)
	refs := resolveLayoutPageParams(layoutPages, templates, pageParams)
	// An entry param has no effect on a page that declares no params, but it
	// makes the selector expand the page. Such a page was never expanded, and
	// its text can hold request values from the reload.
	declaresParams := make(map[string]bool)
	for _, doc := range docs {
		if doc.Kind == "LayoutPage" && len(doc.Params) > 0 {
			declaresParams[doc.Name] = true
		}
	}
	for i := range refs {
		if !declaresParams[refs[i].Page] {
			refs[i].Params = nil
		}
	}
	selectedDocs, err := pipeline.SelectLayoutPages(docs, refs)
	if err != nil {
		logger.Errorf("Select layoutPages failed for route %s: %v", routePath, err)
		return nil, "", err
	}
	// The selector expanded the page params. Without their declarations the
	// renderer does not expand the pages a second time. That pass would read
	// a request value next to a "$" of the page text as a variable reference.
	for i := range selectedDocs {
		if selectedDocs[i].Kind == "LayoutPage" {
			selectedDocs[i].Params = nil
		}
	}

	// Render the selected layout pages directly
	renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, selectedDocs, pipeline.RenderOptions{
		Workdir:            workdir,
		Mode:               pipeline.RenderModeServe,
		EngineVersion:      engineVersion,
		QueryLogger:        queryLogger,
		Session:            session,
		PluginOptions:      pluginOpts,
		PostRenderHTMLHook: postRenderHook,
		PostDatasetHook:    postDatasetHook,
	})
	if err != nil {
		logger.Errorf("Render failed for layoutPages: %v", err)
		return nil, "", err
	}

	pipeline.LogDiagnostics(logger.Channel("datasource"), renderResult.Diagnostics)

	// Apply serve styles
	frameHTML := serve.WithStyles(renderResult.FrameHTML)
	contextHTML := renderResult.ContextHTML

	// Cache the result
	cache.Set(cacheKey, &serveRenderEntry{
		frameHTML:   frameHTML,
		contextHTML: contextHTML,
		assets:      renderResult.LocalAssets,
		emitted:     renderResult.EmittedData,
	})

	return serve.BuildHTML(ctx, frameHTML, contextHTML, liveArtefact, routePath, routeSpec, reqCtx.ReqInfo.RawQuery, workdir, reqCtx.Docs, session), "text/html; charset=utf-8", nil
}

// loadLayoutPageTemplates loads the project once more, with every reference to
// a query param of the LiveReportArtefact kept, and returns the layoutPages of
// each route from that load, with the references as ${NAME} text. A request
// fills its query params into these entry params. The startup load has already
// replaced the references. The documents reloaded for a request are not used
// for this: request values are put into their text and could add or change
// entries.
// Returns nil when no entry has params or the load does not match the routes.
// The entry params then stay as the startup load resolved them.
func loadLayoutPageTemplates(ctx context.Context, logger logx.Logger, workdir string, kindProvider config.KindProvider, live config.LiveArtefact) map[string]config.LayoutPagesOrRefs {
	// A marker stands in for each reference while the YAML is parsed: ${NAME}
	// itself is not valid in a flow mapping. A name cannot hold the "." that
	// ends the marker, so no marker starts like another.
	markers := make(map[string]string)
	var toReference []string
	hasParams := false
	for _, route := range live.Spec.Routes {
		for _, p := range route.QueryParams {
			for _, name := range []string{p.Name, p.Name + "_LABEL"} {
				markers[name] = "bino-query-param." + name + "."
				toReference = append(toReference, markers[name], "${"+name+"}")
			}
		}
		for _, ref := range route.LayoutPages {
			hasParams = hasParams || len(ref.Params) > 0
		}
	}
	if !hasParams {
		return nil
	}
	unavailable := func(reason any) map[string]config.LayoutPagesOrRefs {
		logger.Warnf("LiveReportArtefact %s: query params are not passed to layoutPages entry params: %v", live.Document.Name, reason)
		return nil
	}

	// Lenient: a marker is not valid where the schema wants a number.
	docs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{
		Lookup:       config.ChainLookup(config.MapLookup(markers), config.EnvLookup()),
		KindProvider: kindProvider,
		Lenient:      true,
	})
	if err != nil {
		return unavailable(err)
	}
	i := slices.IndexFunc(docs, func(doc config.Document) bool {
		return doc.Kind == "LiveReportArtefact" && doc.Name == live.Document.Name
	})
	// Only the routes are read: elsewhere a marker may sit where a number is wanted.
	var loaded struct {
		Spec struct {
			Routes map[string]struct {
				LayoutPages config.LayoutPagesOrRefs `json:"layoutPages"`
			} `json:"routes"`
		} `json:"spec"`
	}
	if i < 0 {
		return unavailable("the second load of the manifests does not have it")
	}
	if err := json.Unmarshal(docs[i].Raw, &loaded); err != nil {
		return unavailable(err)
	}
	references := strings.NewReplacer(toReference...)
	templates := make(map[string]config.LayoutPagesOrRefs, len(live.Spec.Routes))
	for path, route := range live.Spec.Routes {
		if len(loaded.Spec.Routes[path].LayoutPages) != len(route.LayoutPages) {
			return unavailable("the second load of the manifests does not match the routes")
		}
		templates[path] = loaded.Spec.Routes[path].LayoutPages
		for _, ref := range templates[path] {
			for name, value := range ref.Params {
				ref.Params[name] = references.Replace(value)
			}
		}
	}
	return templates
}

// plainQueryParams returns the query params that may become page params, as
// text for the page JSON: a page param is put into the JSON text of the page,
// so a quote in a request value would end the string it sits in.
// The page selector also expands entry params against the server environment.
// A request value that such an expansion would change is not used: one with a
// variable reference, or with the marker the expansion uses for an escaped
// "${". The default of the query param takes its place, as if the value was
// not sent. Query params that the reload puts into the manifest text do not
// pass through here.
func plainQueryParams(queryParams, defaults map[string]string) map[string]string {
	plain := make(map[string]string, len(queryParams))
	for name, value := range queryParams {
		if !expandsToItself(value) {
			var hasDefault bool
			if value, hasDefault = defaults[name]; !hasDefault {
				continue
			}
		}
		plain[name] = jsonText(value)
	}
	return plain
}

// jsonText returns s as the content of a JSON string, without the quotes.
// "&", "<" and ">" stay as they are: a select param finds its label by
// comparing this text with the option values.
func jsonText(s string) string {
	var quoted strings.Builder
	encoder := json.NewEncoder(&quoted)
	encoder.SetEscapeHTML(false)
	encoder.Encode(s) //nolint:errcheck // a string always encodes
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(quoted.String()), `"`), `"`)
}

// selectablePages returns docs without the LayoutPages that a route must not
// select.
// A page that the startup load does not have: the reload for a request puts
// request values into the manifest text, and a value can add a document
// there. Such a page must not match a glob of the route.
// A page whose constraints rule out serve mode: pages may share a name when
// their constraints differ, and the selector keeps one page per name. A
// constraint on labels or spec cannot be evaluated, a route has neither, so
// such a page stays.
func selectablePages(docs, baseDocs []config.Document) []config.Document {
	known := make(map[string]struct{})
	for _, doc := range baseDocs {
		if doc.Kind == "LayoutPage" {
			known[doc.Name] = struct{}{}
		}
	}
	serveMode := &spec.ConstraintContext{Mode: spec.ModeServe}
	return slices.DeleteFunc(slices.Clone(docs), func(doc config.Document) bool {
		if doc.Kind != "LayoutPage" {
			return false
		}
		if _, ok := known[doc.Name]; !ok {
			return true
		}
		match, err := spec.EvaluateParsedConstraints(doc.Constraints, serveMode)
		return err == nil && !match
	})
}

// resolveLayoutPageParams returns the route entries with the params for this
// request: the ${NAME} references of the templates are filled with the query
// params, then the environment. Page names, count and order are always those
// of layoutPages, the startup load. Without templates its entry params are
// used, with the references the loader kept as text.
func resolveLayoutPageParams(layoutPages, templates config.LayoutPagesOrRefs, queryParams map[string]string) config.LayoutPagesOrRefs {
	noTemplates := templates == nil
	if noTemplates {
		templates = layoutPages
	}
	lookup := config.ChainLookup(config.MapLookup(queryParams), config.EnvLookup())
	refs := slices.Clone(layoutPages)
	for i, ref := range refs {
		if len(templates[i].Params) == 0 {
			continue
		}
		params := make(map[string]string, len(templates[i].Params))
		for name, template := range templates[i].Params {
			// A reference without a value is empty next to one that has a
			// value. Its inline default is not known here.
			found := false
			value, missing := config.ExpandVars(template, func(name string) (string, bool) {
				v, ok := lookup(name)
				found = found || ok
				return v, ok
			})
			resolved := (len(missing) == 0 || found) && expandsToItself(value)
			if !resolved {
				// No reference has a value, or request values join to a
				// reference: the startup load holds the inline default.
				value = ref.Params[name]
			}
			// An empty startup value can be a reference that the startup
			// load replaced. The page default applies then.
			if (!resolved || noTemplates) && (value == "" || !expandsToItself(value)) {
				continue
			}
			params[name] = value
		}
		refs[i].Params = params
	}
	return refs
}

// expandsToItself reports whether another variable expansion leaves s as it is.
func expandsToItself(s string) bool {
	expanded, _ := config.ExpandVars(s, func(string) (string, bool) { return "", false })
	return expanded == s
}

// withQueryParamDefaults makes each query param the default of the LayoutPage
// param with the same name. The page selector then uses it wherever a
// layoutPages entry does not set that param itself.
func withQueryParamDefaults(docs []config.Document, queryParams map[string]string) []config.Document {
	if len(queryParams) == 0 {
		return docs
	}
	result := slices.Clone(docs)
	for i, doc := range result {
		if doc.Kind != "LayoutPage" || len(doc.Params) == 0 {
			continue
		}
		params := slices.Clone(doc.Params)
		for j, param := range params {
			if value, ok := queryParams[param.Name]; ok {
				params[j].Default = &value
			}
		}
		result[i].Params = params
	}
	return result
}

// buildLayoutPagesCacheKey creates a cache key from layout page refs and sorted query params.
func buildLayoutPagesCacheKey(layoutPages config.LayoutPagesOrRefs, params map[string]string) string {
	// Build page+params strings and sort for consistent key
	pageKeys := make([]string, 0, len(layoutPages))
	for _, ref := range layoutPages {
		pageKey := ref.Page
		if len(ref.Params) > 0 {
			// Include params in the key
			paramParts := make([]string, 0, len(ref.Params))
			for k, v := range ref.Params {
				paramParts = append(paramParts, k+"="+v)
			}
			sort.Strings(paramParts)
			pageKey += "#" + strings.Join(paramParts, ",")
		}
		pageKeys = append(pageKeys, pageKey)
	}
	sort.Strings(pageKeys)
	key := "layoutPages:" + strings.Join(pageKeys, ";")

	if len(params) == 0 {
		return key
	}

	// Sort keys for consistent cache key
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+url.QueryEscape(params[k]))
	}

	return key + "?" + strings.Join(parts, "&")
}

// buildCacheKey creates a cache key from artifact name and sorted query params.
// Values come from the request and are escaped, so a value cannot imitate the
// separators of the key and collide with another parameter set.
func buildCacheKey(artefactName string, params map[string]string) string {
	if len(params) == 0 {
		return artefactName
	}

	// Sort keys for consistent cache key
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(artefactName)
	for _, k := range keys {
		sb.WriteByte('?')
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(params[k]))
	}
	return sb.String()
}
