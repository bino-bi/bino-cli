package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/cli/web"
	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/report/spec"
)

const defaultServePort = 8080

// newServeCommand creates the serve subcommand for production serving.
// Unlike preview, serve:
//   - Does not watch for file changes
//   - Renders on-demand per request (with caching)
//   - Uses query parameters for dynamic variable substitution
//   - Serves a single LiveReportArtefact with navigation
func newServeCommand() *cobra.Command {
	var (
		port    int
		workdir string
		live    string
		logSQL  bool
		addr    string
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

			// Apply environment variables from TOML for serve section
			projectCfg.Serve.Env.Apply(func(key, tomlVal, envVal string) {
				logger.Infof("Environment variable %s overrides bino.toml (%q -> %q)", key, tomlVal, envVal)
			})

			// Create hook runner
			hookRunner := hooks.NewRunner(
				hooks.Resolve(projectCfg.Hooks, projectCfg.Serve.Hooks, logger.Channel("hooks")),
				logger.Channel("hooks"), watchDir,
			)

			// Resolve arguments with TOML defaults
			resolver := pathutil.NewArgResolver(cmd, projectCfg.Serve.Args, func(format string, args ...any) {
				logger.Infof(format, args...)
			})

			port = resolver.ResolveInt("port", "port", port)
			logSQL = resolver.ResolveBool("log-sql", "log-sql", logSQL)
			live = resolver.ResolveString("live", "live", live)

			// Determine listen address
			if addr == "" {
				addr = fmt.Sprintf("127.0.0.1:%d", port)
			}

			// Validate --live flag is provided
			if live == "" {
				return ConfigErrorf("--live flag is required: specify the name of a LiveReportArtefact to serve")
			}

			// Resolve template engine version
			engineVersion := projectCfg.EngineVersion
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

			logger.Infof("Starting serve on %s", addr)
			logger.Infof("Project directory %s", watchDir)
			logger.Infof("Serving LiveReportArtefact %q", live)

			// Set up SQL query logger if --log-sql is enabled
			var queryLogger func(string)
			if logSQL {
				queryLogger = func(query string) {
					if logx.DebugEnabled(ctx) {
						logger.Infof("SQL query:\n%s", query)
					} else {
						logger.Infof("SQL: %s", strings.ReplaceAll(strings.TrimSpace(query), "\n", " "))
					}
				}
			}

			// Load documents once at startup
			docs, err := config.LoadDir(ctx, watchDir)
			if err != nil {
				return ConfigError(err)
			}

			// Collect live artefacts and find the requested one
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

			// Collect all query param names from the live artefact to exclude from env var check
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

			// Collect report artefacts for validation
			artefacts, err := config.CollectArtefacts(docs)
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

			// Validate the live artefact
			if err := config.ValidateLiveArtefact(*liveArtefact, artefacts, layoutPageNames); err != nil {
				return ConfigError(err)
			}

			// Build artefact lookup map
			artefactMap := make(map[string]config.Artefact, len(artefacts))
			for _, a := range artefacts {
				artefactMap[a.Document.Name] = a
			}

			// Create the server
			server, err := previewhttp.New(previewhttp.Config{
				ListenAddr: addr,
				CacheDir:   cacheDir,
				Logger:     logger.Channel("server"),
			})
			if err != nil {
				return RuntimeError(err)
			}

			// Run pre-serve hook (once, before route setup)
			serveHookEnv := hooks.HookEnv{
				Mode:         "serve",
				Workdir:      watchDir,
				ReportID:     projectCfg.ReportID,
				Verbose:      logx.DebugEnabled(ctx),
				ListenAddr:   addr,
				LiveArtefact: live,
			}
			if err := hookRunner.Run(ctx, "pre-serve", serveHookEnv); err != nil {
				return RuntimeError(err)
			}

			// Create render cache for on-demand rendering
			renderCache := newServeRenderCache()

			// Set up routes based on LiveReportArtefact spec
			routeMap := make(map[string]previewhttp.ContentFunc)
			for path, route := range liveArtefact.Spec.Routes {
				// Capture variables for closure
				routePath := path
				routeSpec := route

				if route.Artefact != "" {
					// Route references an artefact
					art, ok := artefactMap[route.Artefact]
					if !ok {
						// This should have been caught by validation, but be safe
						return ConfigErrorf("route %q references unknown artefact %q", path, route.Artefact)
					}
					routeArt := art

					routeMap[routePath] = func(reqCtx context.Context) ([]byte, string, error) {
						if err := hookRunner.Run(reqCtx, "pre-request", serveHookEnv); err != nil {
							return nil, "", err
						}
						return serveRenderHandler(
							reqCtx,
							logger,
							renderCache,
							watchDir,
							docs,
							routeArt,
							*liveArtefact,
							routePath,
							routeSpec,
							queryLogger,
							engineVersion,
						)
					}
				} else {
					// Route references layoutPages directly
					routeLayoutPages := route.LayoutPages

					routeMap[routePath] = func(reqCtx context.Context) ([]byte, string, error) {
						if err := hookRunner.Run(reqCtx, "pre-request", serveHookEnv); err != nil {
							return nil, "", err
						}
						return serveLayoutPagesHandler(
							reqCtx,
							logger,
							renderCache,
							watchDir,
							docs,
							routeLayoutPages,
							*liveArtefact,
							routePath,
							routeSpec,
							queryLogger,
							engineVersion,
						)
					}
				}
			}

			// Set up the server routes
			server.SetContentRoutes(routeMap)

			// Set default content function for root if "/" is in routes
			if rootRoute, ok := liveArtefact.Spec.Routes["/"]; ok {
				rootSpec := rootRoute // Capture route spec
				if rootRoute.Artefact != "" {
					rootArt := artefactMap[rootRoute.Artefact]
					server.SetContentFunc(func(reqCtx context.Context) ([]byte, string, error) {
						if err := hookRunner.Run(reqCtx, "pre-request", serveHookEnv); err != nil {
							return nil, "", err
						}
						return serveRenderHandler(
							reqCtx,
							logger,
							renderCache,
							watchDir,
							docs,
							rootArt,
							*liveArtefact,
							"/",
							rootSpec,
							queryLogger,
							engineVersion,
						)
					})
				} else {
					rootLayoutPages := rootRoute.LayoutPages
					server.SetContentFunc(func(reqCtx context.Context) ([]byte, string, error) {
						if err := hookRunner.Run(reqCtx, "pre-request", serveHookEnv); err != nil {
							return nil, "", err
						}
						return serveLayoutPagesHandler(
							reqCtx,
							logger,
							renderCache,
							watchDir,
							docs,
							rootLayoutPages,
							*liveArtefact,
							"/",
							rootSpec,
							queryLogger,
							engineVersion,
						)
					})
				}
			}

			// Collect all assets from all referenced routes
			allAssets := make([]previewhttp.LocalAsset, 0)
			for _, route := range liveArtefact.Spec.Routes {
				if route.Artefact != "" {
					art := artefactMap[route.Artefact]
					renderResult, err := pipeline.RenderArtefactFrameAndContextWithMode(ctx, watchDir, docs, art, nil, spec.ModeServe, engineVersion)
					if err != nil {
						logger.Warnf("Could not pre-render artefact %s for asset collection: %v", art.Document.Name, err)
						continue
					}
					allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
				} else {
					// For layoutPages routes, render with default settings to collect assets
					renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{
						Workdir:       watchDir,
						Mode:          pipeline.RenderModeServe,
						EngineVersion: engineVersion,
						QueryLogger:   nil,
					})
					if err != nil {
						logger.Warnf("Could not pre-render layoutPages route for asset collection: %v", err)
						continue
					}
					allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
				}
			}
			server.SetLocalAssets(allAssets)

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

	return cmd
}

// serveRequestContext holds the result of processing query parameters for a serve request.
type serveRequestContext struct {
	ReqInfo     previewhttp.RequestInfo
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
) (*serveRequestContext, []byte, error) {
	reqInfo := previewhttp.GetRequestInfo(ctx)

	// Validate and merge query parameters
	validation := validateAndMergeQueryParams(routeSpec, reqInfo.Query)

	// If there are missing required params, return missing params HTML
	if !validation.IsValid() {
		datasetOptions := resolveDatasetOptions(ctx, workdir, baseDocs, routeSpec)
		html := buildMissingParamsHTML(liveArtefact, routePath, routeSpec, reqInfo.RawQuery, validation.MissingNames, datasetOptions)
		return nil, html, nil
	}

	queryParams := validation.Params
	docs := baseDocs

	// If we have query params, reload documents with query params as variables
	if len(queryParams) > 0 {
		lookup := config.ChainLookup(config.MapLookup(queryParams), config.EnvLookup())
		reloadedDocs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{
			Lookup: lookup,
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

// serveRenderCache provides thread-safe caching for rendered content.
type serveRenderCache struct {
	mu    sync.RWMutex
	cache map[string]*serveRenderEntry
}

type serveRenderEntry struct {
	frameHTML   []byte
	contextHTML []byte
	assets      []render.LocalAsset
}

func newServeRenderCache() *serveRenderCache {
	return &serveRenderCache{
		cache: make(map[string]*serveRenderEntry),
	}
}

func (c *serveRenderCache) Get(key string) (*serveRenderEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[key]
	return entry, ok
}

func (c *serveRenderCache) Set(key string, entry *serveRenderEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = entry
}

// serveRenderHandler handles on-demand rendering for a route with query param substitution.
func serveRenderHandler(
	ctx context.Context,
	logger logx.Logger,
	cache *serveRenderCache,
	workdir string,
	baseDocs []config.Document,
	artefact config.Artefact,
	liveArtefact config.LiveArtefact,
	routePath string,
	routeSpec config.LiveRouteSpec,
	queryLogger func(string),
	engineVersion string,
) ([]byte, string, error) {
	// Extract query parameters from request context
	reqInfo := previewhttp.GetRequestInfo(ctx)

	// Validate and merge query parameters
	validation := validateAndMergeQueryParams(routeSpec, reqInfo.Query)

	// If there are missing required params, show the sidebar with error indicators
	if !validation.IsValid() {
		// Resolve dataset options for select parameters (needed for sidebar)
		datasetOptions := resolveDatasetOptions(ctx, workdir, baseDocs, routeSpec)
		return buildMissingParamsHTML(liveArtefact, routePath, routeSpec, reqInfo.RawQuery, validation.MissingNames, datasetOptions), "text/html; charset=utf-8", nil
	}

	queryParams := validation.Params

	// Build cache key from artefact name + sorted query params
	cacheKey := buildCacheKey(artefact.Document.Name, queryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		return buildServeHTML(ctx, entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery, workdir, baseDocs), "text/html; charset=utf-8", nil
	}

	// If we have query params, reload documents with query params as variables
	docs := baseDocs
	currentArtefact := artefact
	if len(queryParams) > 0 {
		// Create a lookup that checks query params first, then falls back to env vars
		lookup := config.ChainLookup(config.MapLookup(queryParams), config.EnvLookup())

		// Reload documents with the custom lookup
		reloadedDocs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{
			Lookup: lookup,
		})
		if err != nil {
			logger.Errorf("Reload failed for %s with query params: %v", artefact.Document.Name, err)
			return nil, "", err
		}
		docs = reloadedDocs

		// Re-collect artefacts to get the one with expanded query params
		artefacts, err := config.CollectArtefacts(docs)
		if err != nil {
			logger.Errorf("Collect artefacts failed for %s: %v", artefact.Document.Name, err)
			return nil, "", err
		}

		// Find the matching artefact by name
		found := false
		for _, a := range artefacts {
			if a.Document.Name == artefact.Document.Name {
				currentArtefact = a
				found = true
				break
			}
		}
		if !found {
			logger.Errorf("Artefact %s not found after reload", artefact.Document.Name)
			return nil, "", fmt.Errorf("artefact %s not found after reload", artefact.Document.Name)
		}
	}

	// Render the artefact with serve mode for constraint evaluation
	renderResult, err := pipeline.RenderArtefactFrameAndContextWithMode(ctx, workdir, docs, currentArtefact, queryLogger, spec.ModeServe, engineVersion)
	if err != nil {
		logger.Errorf("Render failed for %s: %v", artefact.Document.Name, err)
		return nil, "", err
	}

	pipeline.LogDiagnostics(logger.Channel("datasource").Channel(artefact.Document.Name), renderResult.Diagnostics)

	// Apply serve styles
	frameHTML := withServeStyles(renderResult.FrameHTML)
	contextHTML := renderResult.ContextHTML

	// Cache the result
	cache.Set(cacheKey, &serveRenderEntry{
		frameHTML:   frameHTML,
		contextHTML: contextHTML,
		assets:      renderResult.LocalAssets,
	})

	return buildServeHTML(ctx, frameHTML, contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery, workdir, docs), "text/html; charset=utf-8", nil
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
) ([]byte, string, error) {
	// Process query parameters and reload documents if needed
	reqCtx, missingParamsHTML, err := prepareServeRequest(ctx, logger, workdir, baseDocs, routeSpec, liveArtefact, routePath)
	if err != nil {
		return nil, "", err
	}
	if missingParamsHTML != nil {
		return missingParamsHTML, "text/html; charset=utf-8", nil
	}

	// Build cache key from layout pages + sorted query params
	cacheKey := buildLayoutPagesCacheKey(layoutPages, reqCtx.QueryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		return buildServeHTML(ctx, entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqCtx.ReqInfo.RawQuery, workdir, baseDocs), "text/html; charset=utf-8", nil
	}

	// Filter documents to include only the specified LayoutPages (plus dependencies)
	filteredDocs := filterDocsForLayoutPages(reqCtx.Docs, layoutPages)

	// Render the layout pages directly
	renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, filteredDocs, pipeline.RenderOptions{
		Workdir:       workdir,
		Mode:          pipeline.RenderModeServe,
		EngineVersion: engineVersion,
		QueryLogger:   queryLogger,
	})
	if err != nil {
		logger.Errorf("Render failed for layoutPages: %v", err)
		return nil, "", err
	}

	pipeline.LogDiagnostics(logger.Channel("datasource"), renderResult.Diagnostics)

	// Apply serve styles
	frameHTML := withServeStyles(renderResult.FrameHTML)
	contextHTML := renderResult.ContextHTML

	// Cache the result
	cache.Set(cacheKey, &serveRenderEntry{
		frameHTML:   frameHTML,
		contextHTML: contextHTML,
		assets:      renderResult.LocalAssets,
	})

	return buildServeHTML(ctx, frameHTML, contextHTML, liveArtefact, routePath, routeSpec, reqCtx.ReqInfo.RawQuery, workdir, reqCtx.Docs), "text/html; charset=utf-8", nil
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
	var pageKeys []string
	for _, ref := range layoutPages {
		pageKey := ref.Page
		if len(ref.Params) > 0 {
			// Include params in the key
			var paramParts []string
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

	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}

	return key + "?" + strings.Join(parts, "&")
}

// queryParamValidationResult holds the result of query parameter validation.
type queryParamValidationResult struct {
	Params       map[string]string // Merged parameters (request values + defaults)
	MissingNames []string          // Names of missing required parameters
}

// IsValid returns true if there are no missing required parameters.
func (r queryParamValidationResult) IsValid() bool {
	return len(r.MissingNames) == 0
}

// validateAndMergeQueryParams validates query parameters against route spec.
// Returns merged params (request values + defaults) and list of missing required params.
// Unlike before, this does NOT return an error - missing params are reported in the result.
// For select type params with static items, also adds {name}_LABEL with the label from the option item.
func validateAndMergeQueryParams(routeSpec config.LiveRouteSpec, requestQuery map[string][]string) queryParamValidationResult {
	result := queryParamValidationResult{
		Params:       make(map[string]string),
		MissingNames: nil,
	}

	// Build param spec lookup for label resolution
	paramSpecs := make(map[string]config.LiveQueryParamSpec)
	for _, p := range routeSpec.QueryParams {
		paramSpecs[p.Name] = p
	}

	// Apply defaults first
	defaults := routeSpec.GetQueryParamDefaults()
	for name, defaultVal := range defaults {
		result.Params[name] = defaultVal
		// Add _LABEL for select params with static items
		if spec, ok := paramSpecs[name]; ok && spec.Type == "select" && spec.Options != nil && len(spec.Options.Items) > 0 {
			result.Params[name+"_LABEL"] = lookupLiveSelectLabel(spec.Options.Items, defaultVal)
		}
	}

	// Override with request values (only for declared params)
	declaredParams := make(map[string]struct{})
	for _, p := range routeSpec.QueryParams {
		declaredParams[p.Name] = struct{}{}
	}

	for name := range declaredParams {
		if values, ok := requestQuery[name]; ok && len(values) > 0 {
			result.Params[name] = values[0]
			// Add _LABEL for select params with static items
			if spec, ok := paramSpecs[name]; ok && spec.Type == "select" && spec.Options != nil && len(spec.Options.Items) > 0 {
				result.Params[name+"_LABEL"] = lookupLiveSelectLabel(spec.Options.Items, values[0])
			}
		}
	}

	// Check for missing required params (params with no default)
	for _, requiredName := range routeSpec.GetRequiredQueryParams() {
		if _, ok := result.Params[requiredName]; !ok {
			result.MissingNames = append(result.MissingNames, requiredName)
		}
	}

	return result
}

// lookupLiveSelectLabel finds the label for a given value in a list of live select option items.
// If the value is not found or has no label, the value itself is returned.
func lookupLiveSelectLabel(items []config.LiveQueryParamOptionItem, value string) string {
	for _, item := range items {
		if item.Value == value {
			if item.Label != "" {
				return item.Label
			}
			return value // No label defined, use value
		}
	}
	return value // Value not found in items, use value as-is
}

// buildCacheKey creates a cache key from artefact name and sorted query params.
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

// buildServeHTML combines frame and context HTML with seamless navigation support.
// Instead of replacing the placeholder, it keeps the loading state and embeds context
// as data to be injected after the template engine is ready.
func buildServeHTML(ctx context.Context, frameHTML, contextHTML []byte, liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, workdir string, docs []config.Document) []byte {
	frameStr := string(frameHTML)

	// Encode context HTML as base64 for safe embedding
	contextBase64 := base64.StdEncoding.EncodeToString(contextHTML)

	// Resolve dataset options for select parameters
	datasetOptions := resolveDatasetOptions(ctx, workdir, docs, routeSpec)

	// Inject the navigation script and embedded context before </head>
	return injectServeScript([]byte(frameStr), liveArtefact, currentPath, routeSpec, rawQuery, contextBase64, datasetOptions, nil)
}

// buildMissingParamsHTML generates a full HTML page with sidebar showing error indicators
// for missing required parameters and a message instead of the report content.
func buildMissingParamsHTML(liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery string, missingParams []string, datasetOptions map[string][]queryParamOptionItem) []byte {
	// Build a minimal HTML frame with the serve styles and the control panel
	// Context is empty since we don't render the report
	contextBase64 := ""

	// Create a set of missing params for quick lookup
	missingSet := make(map[string]struct{}, len(missingParams))
	for _, name := range missingParams {
		missingSet[name] = struct{}{}
	}

	// Apply serve styles to the frame HTML first, then inject script
	frameHTML := withServeStyles([]byte(buildMissingParamsFrameHTML()))
	return injectServeScript(frameHTML, liveArtefact, currentPath, routeSpec, rawQuery, contextBase64, datasetOptions, missingSet)
}

// buildMissingParamsFrameHTML generates a minimal HTML frame for the missing params page.
// It includes the template engine so that navigation to other routes works properly.
func buildMissingParamsFrameHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Parameters Required</title>
<script type="module" src="/cdn/bn-template-engine/SNAPSHOT/bn-template-engine.esm.js"></script>
<script nomodule src="/cdn/bn-template-engine/SNAPSHOT/bn-template-engine.esm.js"></script>
</head>
<body>
</body>
</html>`
}

// injectServeScript adds the navigation script and embedded context before </head>.
// If missingParams is non-nil, it indicates which parameters are missing and should be highlighted with errors.
func injectServeScript(htmlBytes []byte, liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, contextBase64 string, datasetOptions map[string][]queryParamOptionItem, missingParams map[string]struct{}) []byte {
	htmlStr := string(htmlBytes)
	script := buildServeScript(liveArtefact, currentPath, routeSpec, rawQuery, contextBase64, datasetOptions, missingParams)

	// Find </head> and inject the script
	headClose := strings.Index(htmlStr, "</head>")
	if headClose != -1 {
		var b strings.Builder
		b.WriteString(htmlStr[:headClose])
		b.WriteString(script)
		b.WriteString(htmlStr[headClose:])
		return []byte(b.String())
	}

	return htmlBytes
}

// queryParamInfo holds info about a query parameter for JSON serialization.
type queryParamInfo struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"` // string, number, number_range, select, date, date_time
	Default     *string            `json:"default,omitempty"`
	Description string             `json:"description,omitempty"`
	Required    bool               `json:"required"`
	Options     *queryParamOptions `json:"options,omitempty"`
}

// queryParamOptions holds options for select, number, and number_range type parameters.
type queryParamOptions struct {
	Items []queryParamOptionItem `json:"items,omitempty"`
	Min   *float64               `json:"min,omitempty"`
	Max   *float64               `json:"max,omitempty"`
	Step  *float64               `json:"step,omitempty"`
}

// queryParamOptionItem holds a single option for select type parameters.
type queryParamOptionItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// resolveDatasetOptions resolves select options from datasets for a route's query parameters.
// Returns a map from parameter name to resolved options.
func resolveDatasetOptions(ctx context.Context, workdir string, docs []config.Document, routeSpec config.LiveRouteSpec) map[string][]queryParamOptionItem {
	result := make(map[string][]queryParamOptionItem)

	// Find parameters that need dataset resolution
	datasetsNeeded := make(map[string]config.LiveQueryParamSpec)
	for _, p := range routeSpec.QueryParams {
		if p.Options != nil && p.Options.Dataset != "" {
			datasetsNeeded[p.Options.Dataset] = p
		}
	}

	if len(datasetsNeeded) == 0 {
		return result
	}

	// Execute datasets
	datasetResults, _, err := dataset.Execute(ctx, workdir, docs, nil)
	if err != nil {
		// Log error but continue - options will be empty
		return result
	}

	// Build lookup of dataset results
	datasetResultMap := make(map[string]json.RawMessage)
	for _, r := range datasetResults {
		datasetResultMap[r.Name] = r.Data
	}

	// Resolve options for each parameter
	for datasetName, paramSpec := range datasetsNeeded {
		data, ok := datasetResultMap[datasetName]
		if !ok {
			continue
		}

		// Parse dataset result as array of objects
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			continue
		}

		valueCol := paramSpec.Options.ValueColumn
		labelCol := paramSpec.Options.LabelColumn
		if labelCol == "" {
			labelCol = valueCol
		}

		items := make([]queryParamOptionItem, 0, len(rows))
		for _, row := range rows {
			valueRaw, ok := row[valueCol]
			if !ok {
				continue
			}
			value := fmt.Sprintf("%v", valueRaw)

			label := value
			if labelRaw, ok := row[labelCol]; ok {
				label = fmt.Sprintf("%v", labelRaw)
			}

			items = append(items, queryParamOptionItem{
				Value: value,
				Label: label,
			})
		}

		result[paramSpec.Name] = items
	}

	return result
}

// buildRoutesJSON builds the routes map for navigation and returns JSON.
func buildRoutesJSON(liveArtefact config.LiveArtefact) []byte {
	routes := make(map[string]string)
	for path, route := range liveArtefact.Spec.Routes {
		title := route.Title
		if title == "" {
			title = route.Artefact
		}
		routes[path] = title
	}
	routesJSON, _ := json.Marshal(routes)
	return routesJSON
}

// buildMissingParamsJSON builds a sorted list of missing parameter names and returns JSON.
func buildMissingParamsJSON(missingParams map[string]struct{}) []byte {
	missingList := make([]string, 0, len(missingParams))
	for name := range missingParams {
		missingList = append(missingList, name)
	}
	sort.Strings(missingList) // for consistent output
	missingParamsJSON, _ := json.Marshal(missingList)
	return missingParamsJSON
}

// buildQueryParamsJSON builds the query params array for the control panel and returns JSON.
func buildQueryParamsJSON(routeSpec config.LiveRouteSpec, datasetOptions map[string][]queryParamOptionItem) []byte {
	queryParams := make([]queryParamInfo, 0, len(routeSpec.QueryParams))
	for _, p := range routeSpec.QueryParams {
		paramType := p.Type
		if paramType == "" {
			paramType = "string"
		}

		info := queryParamInfo{
			Name:        p.Name,
			Type:        paramType,
			Default:     p.Default,
			Description: p.Description,
			Required:    p.Default == nil && !p.Optional,
		}

		// Add options if present
		if p.Options != nil {
			opts := &queryParamOptions{
				Min:  p.Options.Min,
				Max:  p.Options.Max,
				Step: p.Options.Step,
			}
			// Use dataset options if available, otherwise use static items
			if dsItems, ok := datasetOptions[p.Name]; ok && len(dsItems) > 0 {
				opts.Items = dsItems
			} else if len(p.Options.Items) > 0 {
				// Convert static items
				opts.Items = make([]queryParamOptionItem, 0, len(p.Options.Items))
				for _, item := range p.Options.Items {
					label := item.Label
					if label == "" {
						label = item.Value
					}
					opts.Items = append(opts.Items, queryParamOptionItem{
						Value: item.Value,
						Label: label,
					})
				}
			}
			info.Options = opts
		}

		queryParams = append(queryParams, info)
	}
	queryParamsJSON, _ := json.Marshal(queryParams)
	return queryParamsJSON
}

// buildServeScript generates an inline config script and a reference to the external serve runtime.
// If missingParams is non-nil, it indicates which parameters are missing and should be highlighted with errors.
func buildServeScript(liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, contextBase64 string, datasetOptions map[string][]queryParamOptionItem, missingParams map[string]struct{}) string {
	routesJSON := buildRoutesJSON(liveArtefact)
	missingParamsJSON := buildMissingParamsJSON(missingParams)
	queryParamsJSON := buildQueryParamsJSON(routeSpec, datasetOptions)

	// Build full URL with query string for initial state
	currentURL := currentPath
	if rawQuery != "" {
		currentURL = currentPath + "?" + rawQuery
	}

	// Build the config object as JSON. The contextBase64 field is emitted
	// separately (not via json.Marshal) because it can be very large and
	// we want to avoid double-escaping.
	type serveConfig struct {
		Routes        json.RawMessage `json:"routes"`
		QueryParams   json.RawMessage `json:"queryParams"`
		MissingParams json.RawMessage `json:"missingParams"`
		CurrentPath   string          `json:"currentPath"`
		CurrentURL    string          `json:"currentURL"`
	}
	cfg := serveConfig{
		Routes:        routesJSON,
		QueryParams:   queryParamsJSON,
		MissingParams: missingParamsJSON,
		CurrentPath:   currentPath,
		CurrentURL:    currentURL,
	}
	cfgJSON, _ := json.Marshal(cfg)

	// Strip the closing "}" so we can append the contextBase64 field manually.
	// This avoids JSON-encoding the (potentially huge) base64 string twice.
	cfgStr := string(cfgJSON[:len(cfgJSON)-1])

	var sb strings.Builder
	sb.WriteString(`<script id="bino-serve-config">window.__binoServeConfig = `)
	sb.WriteString(cfgStr)
	sb.WriteString(`,"initialContextBase64":"`)
	sb.WriteString(contextBase64)
	sb.WriteString(`"};</script>`)
	sb.WriteString("\n")
	sb.WriteString(web.ImportMapScript())
	sb.WriteString("\n")
	sb.WriteString(`<script type="module" src="/__bino/serve/serve-app.js"></script>`)
	return sb.String()
}

// withServeStyles applies production-appropriate styles to the frame HTML.
func withServeStyles(frameHTML []byte) []byte {
	if len(frameHTML) == 0 {
		return frameHTML
	}

	// Check if already has serve styles
	if strings.Contains(string(frameHTML), "bn-serve-style") {
		return frameHTML
	}

	styleBlock := []byte(`
<link id="bn-serve-style" rel="stylesheet" href="/__bino/shared/tokens.css">
<link rel="stylesheet" href="/__bino/serve/serve.css">
`)

	// Find </head> and inject styles before it
	headClose := strings.Index(string(frameHTML), "</head>")
	if headClose == -1 {
		// No </head> found, prepend styles
		return append(styleBlock, frameHTML...)
	}

	result := make([]byte, 0, len(frameHTML)+len(styleBlock))
	result = append(result, frameHTML[:headClose]...)
	result = append(result, styleBlock...)
	result = append(result, frameHTML[headClose:]...)

	// Inject bino-serve-shell wrapper after <body>
	resultStr := string(result)
	bodyOpen := strings.Index(resultStr, "<body")
	if bodyOpen == -1 {
		return result
	}
	// Find the closing > of the body tag
	bodyClose := strings.Index(resultStr[bodyOpen:], ">")
	if bodyClose == -1 {
		return result
	}
	bodyEnd := bodyOpen + bodyClose + 1

	// Find </body> to wrap content
	bodyCloseTag := strings.Index(resultStr, "</body>")
	if bodyCloseTag == -1 {
		return result
	}

	// Extract original body content and wrap in shell
	originalBodyContent := resultStr[bodyEnd:bodyCloseTag]

	var sb strings.Builder
	sb.WriteString(resultStr[:bodyEnd])
	sb.WriteString(`<bino-serve-shell>`)
	sb.WriteString(originalBodyContent)
	sb.WriteString(`</bino-serve-shell>`)
	sb.WriteString(resultStr[bodyCloseTag:])

	return []byte(sb.String())
}
