package cli

import (
	"container/list"
	"context"
	"fmt"
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

// newServeCommand creates the serve subcommand for production serving.
// Unlike preview, serve:
//   - Does not watch for file changes
//   - Renders on-demand per request (with caching)
//   - Uses query parameters for dynamic variable substitution
//   - Serves a single LiveReportArtefact with navigation
func newServeCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
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
			resolvedDataMode, err := normalizeDataMode(dataMode)
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
			defer sharedSession.Close()

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

			// Validate the live artifact
			if err := config.ValidateLiveArtefact(*liveArtefact, artifacts, layoutPageNames); err != nil {
				return ConfigError(err)
			}

			// Build artifact lookup map
			artefactMap := make(map[string]config.Artifact, len(artifacts))
			for _, a := range artifacts {
				artefactMap[a.Document.Name] = a
			}

			// Create the server
			server, err := httpserver.New(httpserver.Config{
				ListenAddr: addr,
				CacheDir:   env.CacheDir,
				Logger:     logger.Channel("server"),
			})
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
			if resolvedDataMode == render.DataModeURL {
				if servePluginOpts == nil {
					servePluginOpts = &render.PluginOptions{}
				}
				servePluginOpts.DataMode = render.DataModeURL
				// Emit absolute URLs so older template-engine builds (which
				// only fetch http:// or https:// bodies) still resolve them.
				// NOTE: this has the same same-origin consideration as the
				// preview path — an absolute base pins the data fetch to one
				// host (e.g. 127.0.0.1), so a client loading the page from a
				// different host name would hit a cross-origin fetch (the data
				// route sends no CORS headers). Left as-is here on purpose;
				// revisit if serve grows an embed/iframe consumer.
				servePluginOpts.DataBaseURL = server.URL()
			}

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
			})
			if err != nil {
				return ConfigError(err)
			}
			server.SetContentRoutes(routeSetup.RouteMap)
			if routeSetup.RootContent != nil {
				server.SetContentFunc(routeSetup.RootContent)
			}
			server.SetLocalAssets(collectServeAssets(ctx, logger, *liveArtefact, artefactMap, env.ProjectRoot, docs, env.EngineVersion, sharedSession, servePluginOpts, servePostRenderHook, servePostDatasetHook))

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
	cmd.Flags().StringVar(&dataMode, "data-mode", "url",
		"Dataset/datasource delivery: 'url' fetches data via HTTP from the bino server (default), 'inline' embeds gzip+base64 in the HTML")

	return cmd
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

	// If there are missing required params, return missing params HTML
	if !validation.IsValid() {
		datasetOptions := serve.ResolveDatasetOptions(ctx, workdir, baseDocs, routeSpec, session)
		html := serve.BuildMissingParamsHTML(liveArtefact, routePath, routeSpec, reqInfo.RawQuery, validation.MissingNames, datasetOptions)
		return nil, html, nil
	}

	queryParams := validation.Params
	docs := baseDocs

	// If we have query params, reload documents with query params as variables
	if len(queryParams) > 0 {
		lookup := config.ChainLookup(config.MapLookup(queryParams), config.EnvLookup())
		reloadedDocs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{
			Lookup:       lookup,
			KindProvider: kindProvider,
		})
		if err != nil {
			logger.Errorf("Reload failed with query params: %v", err)
			return nil, nil, err
		}
		docs = reloadedDocs
	}

	return &serveRequestContext{
		ReqInfo:     reqInfo,
		QueryParams: queryParams,
		Docs:        docs,
	}, nil, nil
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
	// Server is used to register dataset/datasource payloads when the
	// renderer runs in url mode. May be nil in inline mode.
	Server *httpserver.Server
}

// serveRouteSetup holds the results of route setup.
type serveRouteSetup struct {
	RouteMap    map[string]httpserver.ContentFunc
	RootContent httpserver.ContentFunc // nil if "/" not in routes
}

// setupServeRoutes builds the route map and root content function from a LiveReportArtefact.
func setupServeRoutes(cfg serveRouteConfig) (*serveRouteSetup, error) {
	renderCache := newServeRenderCache()
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
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService, cfg.Server,
				)
			}
		} else {
			routeLayoutPages := route.LayoutPages

			routeMap[routePath] = func(reqCtx context.Context) ([]byte, string, error) {
				if err := cfg.HookRunner.Run(reqCtx, "pre-request", cfg.HookEnv); err != nil {
					return nil, "", err
				}
				renderMu.Lock()
				defer renderMu.Unlock()
				return serveLayoutPagesHandler(
					reqCtx, cfg.Logger, renderCache, cfg.Workdir, cfg.BaseDocs, routeLayoutPages,
					cfg.LiveArtefact, routePath, routeSpec, cfg.QueryLogger, cfg.EngineVersion, cfg.Session,
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService, cfg.Server,
				)
			}
		}
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
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService, cfg.Server,
				)
			}
		} else {
			rootLayoutPages := rootRoute.LayoutPages
			setup.RootContent = func(reqCtx context.Context) ([]byte, string, error) {
				if err := cfg.HookRunner.Run(reqCtx, "pre-request", cfg.HookEnv); err != nil {
					return nil, "", err
				}
				renderMu.Lock()
				defer renderMu.Unlock()
				return serveLayoutPagesHandler(
					reqCtx, cfg.Logger, renderCache, cfg.Workdir, cfg.BaseDocs, rootLayoutPages,
					cfg.LiveArtefact, "/", rootSpec, cfg.QueryLogger, cfg.EngineVersion, cfg.Session,
					cfg.KindProvider, cfg.PluginOptions, cfg.PostRenderHTMLHook, cfg.PostDatasetHook, cfg.HostService, cfg.Server,
				)
			}
		}
	}

	return setup, nil
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
	// emitted is the set of dataset/datasource bodies that need to be
	// registered on the httpserver.Server's data store for url-mode fetches.
	// Re-registered on every cache hit because the store retains only the
	// last N hashes per (kind,name).
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
	server *httpserver.Server,
) (body []byte, contentType string, err error) {
	// Extract query parameters from request context
	reqInfo := httpserver.GetRequestInfo(ctx)

	// Validate and merge query parameters
	validation := serve.ValidateAndMergeQueryParams(routeSpec, reqInfo.Query)

	// If there are missing required params, show the sidebar with error indicators
	if !validation.IsValid() {
		// Resolve dataset options for select parameters (needed for sidebar)
		datasetOptions := serve.ResolveDatasetOptions(ctx, workdir, baseDocs, routeSpec, session)
		return serve.BuildMissingParamsHTML(liveArtefact, routePath, routeSpec, reqInfo.RawQuery, validation.MissingNames, datasetOptions), "text/html; charset=utf-8", nil
	}

	queryParams := validation.Params

	// Build cache key from artifact name + sorted query params
	cacheKey := buildCacheKey(artifact.Document.Name, queryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		// Re-register payloads on every hit: the data store retains only the
		// last N hashes per (kind,name), so a long-lived cached HTML can
		// outlive its data registration.
		pipeline.RegisterEmittedData(server, entry.emitted)
		return serve.BuildHTML(ctx, entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery, workdir, baseDocs, session), "text/html; charset=utf-8", nil
	}

	// If we have query params, reload documents with query params as variables
	docs := baseDocs
	currentArtefact := artifact
	if len(queryParams) > 0 {
		// Create a lookup that checks query params first, then falls back to env vars
		lookup := config.ChainLookup(config.MapLookup(queryParams), config.EnvLookup())

		// Reload documents with the custom lookup
		reloadedDocs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{
			Lookup:       lookup,
			KindProvider: kindProvider,
		})
		if err != nil {
			logger.Errorf("Reload failed for %s with query params: %v", artifact.Document.Name, err)
			return nil, "", err
		}
		docs = reloadedDocs

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
	pipeline.RegisterEmittedData(server, renderResult.EmittedData)

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
	server *httpserver.Server,
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

	// Build cache key from layout pages + sorted query params
	cacheKey := buildLayoutPagesCacheKey(layoutPages, reqCtx.QueryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		pipeline.RegisterEmittedData(server, entry.emitted)
		return serve.BuildHTML(ctx, entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqCtx.ReqInfo.RawQuery, workdir, baseDocs, session), "text/html; charset=utf-8", nil
	}

	// Filter documents to include only the specified LayoutPages (plus dependencies)
	filteredDocs := filterDocsForLayoutPages(reqCtx.Docs, layoutPages)

	// Render the layout pages directly, passing query params as LayoutPage param overrides
	renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, filteredDocs, pipeline.RenderOptions{
		Workdir:            workdir,
		Mode:               pipeline.RenderModeServe,
		EngineVersion:      engineVersion,
		QueryLogger:        queryLogger,
		LayoutPageParams:   reqCtx.QueryParams,
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
	pipeline.RegisterEmittedData(server, renderResult.EmittedData)

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

// filterDocsForLayoutPages filters documents to include only LayoutPages with matching names
// and all other document types (DataSets, DataSources, etc.) needed for rendering.
func filterDocsForLayoutPages(docs []config.Document, layoutPages config.LayoutPagesOrRefs) []config.Document {
	// Build a set of requested layout page names
	requestedPages := make(map[string]struct{})
	for _, ref := range layoutPages {
		requestedPages[ref.Page] = struct{}{}
	}

	// Filter documents: keep all non-LayoutPage docs, and only matching LayoutPages
	filtered := make([]config.Document, 0, len(docs))
	for _, doc := range docs {
		if doc.Kind == "LayoutPage" {
			if _, ok := requestedPages[doc.Name]; ok {
				filtered = append(filtered, doc)
			}
		} else {
			// Keep all other document types (DataSets, DataSources, ThemeStyle, etc.)
			filtered = append(filtered, doc)
		}
	}

	return filtered
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
		parts = append(parts, k+"="+params[k])
	}

	return key + "?" + strings.Join(parts, "&")
}

// buildCacheKey creates a cache key from artifact name and sorted query params.
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
		sb.WriteString(params[k])
	}
	return sb.String()
}
