package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

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
			queryParamNames := make(map[string]struct{})
			for _, route := range liveArtefact.Spec.Routes {
				for _, p := range route.QueryParams {
					queryParamNames[p.Name] = struct{}{}
				}
			}

			// Check for missing env vars - exclude query params which will be provided at runtime
			if err := config.CheckMissingEnvVarsExcluding(docs, queryParamNames); err != nil {
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
						)
					}
				} else {
					// Route references layoutPages directly
					routeLayoutPages := route.LayoutPages

					routeMap[routePath] = func(reqCtx context.Context) ([]byte, string, error) {
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
						)
					})
				} else {
					rootLayoutPages := rootRoute.LayoutPages
					server.SetContentFunc(func(reqCtx context.Context) ([]byte, string, error) {
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
						)
					})
				}
			}

			// Collect all assets from all referenced routes
			allAssets := make([]previewhttp.LocalAsset, 0)
			for _, route := range liveArtefact.Spec.Routes {
				if route.Artefact != "" {
					art := artefactMap[route.Artefact]
					renderResult, err := pipeline.RenderArtefactFrameAndContextWithMode(ctx, watchDir, docs, art, nil, spec.ModeServe)
					if err != nil {
						logger.Warnf("Could not pre-render artefact %s for asset collection: %v", art.Document.Name, err)
						continue
					}
					allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
				} else {
					// For layoutPages routes, render with default settings to collect assets
					renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{
						Workdir:     watchDir,
						Mode:        pipeline.RenderModeServe,
						QueryLogger: nil,
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
	renderResult, err := pipeline.RenderArtefactFrameAndContextWithMode(ctx, workdir, docs, currentArtefact, queryLogger, spec.ModeServe)
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
	layoutPages config.StringOrSlice,
	liveArtefact config.LiveArtefact,
	routePath string,
	routeSpec config.LiveRouteSpec,
	queryLogger func(string),
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

	// Build cache key from layout pages + sorted query params
	cacheKey := buildLayoutPagesCacheKey(layoutPages, queryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		return buildServeHTML(ctx, entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery, workdir, baseDocs), "text/html; charset=utf-8", nil
	}

	// If we have query params, reload documents with query params as variables
	docs := baseDocs
	if len(queryParams) > 0 {
		// Create a lookup that checks query params first, then falls back to env vars
		lookup := config.ChainLookup(config.MapLookup(queryParams), config.EnvLookup())

		// Reload documents with the custom lookup
		reloadedDocs, err := config.LoadDirWithOptions(ctx, workdir, config.LoadOptions{
			Lookup: lookup,
		})
		if err != nil {
			logger.Errorf("Reload failed for layoutPages with query params: %v", err)
			return nil, "", err
		}
		docs = reloadedDocs
	}

	// Filter documents to include only the specified LayoutPages (plus dependencies)
	filteredDocs := filterDocsForLayoutPages(docs, layoutPages)

	// Render the layout pages directly
	renderResult, err := pipeline.RenderHTMLFrameAndContext(ctx, filteredDocs, pipeline.RenderOptions{
		Workdir:     workdir,
		Mode:        pipeline.RenderModeServe,
		QueryLogger: queryLogger,
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

	return buildServeHTML(ctx, frameHTML, contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery, workdir, docs), "text/html; charset=utf-8", nil
}

// filterDocsForLayoutPages filters documents to include only LayoutPages with matching names
// and all other document types (DataSets, DataSources, etc.) needed for rendering.
func filterDocsForLayoutPages(docs []config.Document, layoutPages config.StringOrSlice) []config.Document {
	// Build a set of requested layout page names
	requestedPages := make(map[string]struct{})
	for _, name := range layoutPages {
		requestedPages[name] = struct{}{}
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

// buildLayoutPagesCacheKey creates a cache key from layout page names and sorted query params.
func buildLayoutPagesCacheKey(layoutPages config.StringOrSlice, params map[string]string) string {
	// Sort layout pages for consistent key
	pages := make([]string, len(layoutPages))
	copy(pages, layoutPages)
	sort.Strings(pages)
	key := "layoutPages:" + strings.Join(pages, ",")

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
func validateAndMergeQueryParams(routeSpec config.LiveRouteSpec, requestQuery map[string][]string) queryParamValidationResult {
	result := queryParamValidationResult{
		Params:       make(map[string]string),
		MissingNames: nil,
	}

	// Apply defaults first
	defaults := routeSpec.GetQueryParamDefaults()
	for name, defaultVal := range defaults {
		result.Params[name] = defaultVal
	}

	// Override with request values (only for declared params)
	declaredParams := make(map[string]struct{})
	for _, p := range routeSpec.QueryParams {
		declaredParams[p.Name] = struct{}{}
	}

	for name := range declaredParams {
		if values, ok := requestQuery[name]; ok && len(values) > 0 {
			result.Params[name] = values[0]
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
	// Simple sort
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

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

// buildServeScript generates the JavaScript for seamless navigation, content injection, and control panel.
// If missingParams is non-nil, it indicates which parameters are missing and should be highlighted with errors.
func buildServeScript(liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, contextBase64 string, datasetOptions map[string][]queryParamOptionItem, missingParams map[string]struct{}) string {
	// Build routes JSON for the navigation script
	routes := make(map[string]string)
	for path, route := range liveArtefact.Spec.Routes {
		title := route.Title
		if title == "" {
			title = route.Artefact
		}
		routes[path] = title
	}
	routesJSON, _ := json.Marshal(routes)

	// Build missing params JSON
	missingList := make([]string, 0, len(missingParams))
	for name := range missingParams {
		missingList = append(missingList, name)
	}
	sort.Strings(missingList) // for consistent output
	missingParamsJSON, _ := json.Marshal(missingList)

	// Build queryParams JSON for the control panel
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

	// Build full URL with query string for initial state
	currentURL := currentPath
	if rawQuery != "" {
		currentURL = currentPath + "?" + rawQuery
	}

	return fmt.Sprintf(`
<script id="bino-serve-runtime">
(function() {
  var routes = %s;
  var queryParams = %s;
  var missingParams = %s;
  var currentPath = %q;
  var currentURL = %q;
  var initialContextBase64 = %q;

  // Decode base64 helper
  function decodeBase64(input) {
    if (!input) return '';
    try {
      return atob(input);
    } catch (err) {
      console.error('bino: decode failed', err);
      return '';
    }
  }

  // Parser for HTML content
  var parser = new DOMParser();

  // Wait for template engine to be ready before showing content
  var engineReady = false;

  function waitForEngine() {
    // If we have missing params, build control panel once DOM is ready (no report to render)
    if (missingParams && missingParams.length > 0) {
      if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', buildControlPanel);
      } else {
        buildControlPanel();
      }
      return;
    }
    
    if (customElements.get('bn-context')) {
      engineReady = true;
      onEngineReady();
      return;
    }
    customElements.whenDefined('bn-context').then(function() {
      engineReady = true;
      onEngineReady();
    });
  }

  function onEngineReady() {
    injectInitialContent();
    buildControlPanel();
  }

  function injectInitialContent() {
    if (!initialContextBase64) return;
    var html = decodeBase64(initialContextBase64);
    swapContext(html);
    initialContextBase64 = null;
  }

  function swapContext(html) {
    if (!html) return;
    var doc = parser.parseFromString(html, 'text/html');
    var newContext = doc.querySelector('bn-context');
    var currentContext = document.querySelector('bn-context');
    if (newContext && currentContext) {
      currentContext.replaceWith(newContext);
    }
  }

  // Check if a param is missing
  function isParamMissing(paramName) {
    return missingParams && missingParams.indexOf(paramName) !== -1;
  }

  // Build sitemap for navigation
  function buildSitemap() {
    var panel = document.getElementById('bino-control-panel');
    if (!panel) return '';
    
    var routeKeys = Object.keys(routes).sort();
    if (routeKeys.length <= 1) return '';
    
    var html = '<div class="bino-sitemap">';
    html += '<h3>Navigation</h3>';
    html += '<ul class="bino-route-list">';
    routeKeys.forEach(function(path) {
      var title = routes[path] || path;
      var isActive = path === currentPath;
      var activeClass = isActive ? ' class="active"' : '';
      html += '<li' + activeClass + '><a href="' + escapeHtml(path) + '">' + escapeHtml(title) + '</a></li>';
    });
    html += '</ul>';
    html += '</div>';
    return html;
  }

  // Build missing params message
  function buildMissingParamsMessage() {
    if (!missingParams || missingParams.length === 0) return '';
    
    var html = '<div class="bino-missing-params-banner">';
    html += '<div class="bino-missing-icon">⚠</div>';
    html += '<div class="bino-missing-text">';
    html += '<strong>Required parameters missing</strong>';
    html += '<p>Please fill in the required fields marked with <span class="required">*</span> to view the report.</p>';
    html += '</div>';
    html += '</div>';
    return html;
  }

  // Build control panel for query parameters
  function buildControlPanel() {
    var panel = document.getElementById('bino-control-panel');
    if (!panel) return;

    var html = '';
    
    // Add sitemap first
    html += buildSitemap();
    
    // Add parameters section if there are any
    if (queryParams.length > 0) {
      // Parse current URL params
      var urlParams = new URLSearchParams(window.location.search);

      html += '<h3>Parameters</h3>';
      queryParams.forEach(function(param) {
        var value = urlParams.get(param.name);
        var value2 = null; // For number_range (max value)
        
        // Handle number_range which uses param_name and param_name_max
        if (param.type === 'number_range') {
          value2 = urlParams.get(param.name + '_max');
        }
        
        if (value === null && param.default !== undefined && param.default !== null) {
          value = param.default;
        }
        value = value || '';
        value2 = value2 || '';

        // Check if this param is in the missing list
        var isMissing = isParamMissing(param.name);
        var groupClass = isMissing ? ' bino-param-missing' : '';

        html += '<div class="bino-param-group' + groupClass + '">';
        html += '<label class="bino-param-label" for="bino-param-' + param.name + '">' + 
                escapeHtml(param.name) + 
                (param.required ? '<span class="required">*</span>' : '') + 
                '</label>';
        if (param.description) {
          html += '<p class="bino-param-desc">' + escapeHtml(param.description) + '</p>';
        }
      
        // Render input based on type
        html += buildInputForType(param, value, value2, isMissing);
        
        html += '</div>';
      });

      html += '<button type="button" id="bino-apply-btn">Apply</button>';
    }
    
    panel.innerHTML = html;

    // Show missing params message in content area
    if (missingParams && missingParams.length > 0) {
      var contentArea = document.getElementById('bino-content-area');
      if (contentArea) {
        contentArea.innerHTML = buildMissingParamsMessage();
      }
    }

    // Add event listeners
    var applyBtn = document.getElementById('bino-apply-btn');
    if (applyBtn) {
      applyBtn.addEventListener('click', applyParams);
    }

    // Allow Enter key to apply
    panel.querySelectorAll('.bino-param-input').forEach(function(input) {
      input.addEventListener('keypress', function(e) {
        if (e.key === 'Enter') applyParams();
      });
      input.addEventListener('input', function() {
        input.classList.remove('invalid');
        input.closest('.bino-param-group').classList.remove('bino-param-missing');
      });
    });

    // Setup range slider interactions
    setupRangeSliders(panel);
  }

  function setupRangeSliders(panel) {
    var dualRanges = panel.querySelectorAll('.bino-dual-range');
    dualRanges.forEach(function(container) {
      var minSlider = container.querySelector('.bino-range-min-slider');
      var maxSlider = container.querySelector('.bino-range-max-slider');
      if (!minSlider || !maxSlider) return;
      
      var minDisplay = document.getElementById('bino-range-display-' + minSlider.name);
      var maxDisplay = document.getElementById('bino-range-display-' + maxSlider.name);
      
      function updateDisplay() {
        var minVal = parseFloat(minSlider.value);
        var maxVal = parseFloat(maxSlider.value);
        
        // Ensure min doesn't exceed max
        if (minVal > maxVal) {
          if (this === minSlider) {
            minSlider.value = maxVal;
            minVal = maxVal;
          } else {
            maxSlider.value = minVal;
            maxVal = minVal;
          }
        }
        
        if (minDisplay) minDisplay.textContent = minSlider.value;
        if (maxDisplay) maxDisplay.textContent = maxSlider.value;
      }
      
      minSlider.addEventListener('input', updateDisplay);
      maxSlider.addEventListener('input', updateDisplay);
    });
  }

  function escapeHtml(text) {
    var div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  function buildInputForType(param, value, value2, isMissing) {
    var type = param.type || 'string';
    var opts = param.options || {};
    var placeholder = param.default !== undefined && param.default !== null ? 'placeholder="' + escapeHtml(param.default) + '"' : '';
    var required = 'data-required="' + param.required + '"';
    var minAttr = opts.min !== undefined ? ' min="' + opts.min + '"' : '';
    var maxAttr = opts.max !== undefined ? ' max="' + opts.max + '"' : '';
    var stepAttr = opts.step !== undefined ? ' step="' + opts.step + '"' : '';
    var invalidClass = isMissing ? ' invalid' : '';
    
    switch (type) {
      case 'number':
        return '<input type="number" class="bino-param-input' + invalidClass + '" id="bino-param-' + param.name + '" ' +
               'name="' + param.name + '" value="' + escapeHtml(value) + '" ' +
               required + ' ' + placeholder + minAttr + maxAttr + stepAttr + '>';
      
      case 'number_range':
        var minVal = opts.min !== undefined ? opts.min : 0;
        var maxVal = opts.max !== undefined ? opts.max : 100;
        var stepVal = opts.step !== undefined ? opts.step : 1;
        var currentMin = value !== '' ? parseFloat(value) : minVal;
        var currentMax = value2 !== '' ? parseFloat(value2) : maxVal;
        return '<div class="bino-range-slider-container">' +
               '<div class="bino-range-values">' +
               '<span class="bino-range-value-min" id="bino-range-display-' + param.name + '">' + currentMin + '</span>' +
               '<span class="bino-range-sep">–</span>' +
               '<span class="bino-range-value-max" id="bino-range-display-' + param.name + '_max">' + currentMax + '</span>' +
               '</div>' +
               '<div class="bino-dual-range">' +
               '<input type="range" class="bino-param-input bino-range-slider bino-range-min-slider' + invalidClass + '" ' +
               'id="bino-param-' + param.name + '" name="' + param.name + '" ' +
               'value="' + currentMin + '" min="' + minVal + '" max="' + maxVal + '" step="' + stepVal + '" ' +
               required + '>' +
               '<input type="range" class="bino-param-input bino-range-slider bino-range-max-slider" ' +
               'id="bino-param-' + param.name + '_max" name="' + param.name + '_max" ' +
               'value="' + currentMax + '" min="' + minVal + '" max="' + maxVal + '" step="' + stepVal + '" ' +
               'data-required="false">' +
               '</div>' +
               '</div>';
      
      case 'select':
        var html = '<select class="bino-param-input bino-param-select' + invalidClass + '" id="bino-param-' + param.name + '" ' +
                   'name="' + param.name + '" ' + required + '>';
        if (!param.required) {
          html += '<option value="">-- Select --</option>';
        }
        if (opts.items && opts.items.length > 0) {
          opts.items.forEach(function(item) {
            var selected = value === item.value ? ' selected' : '';
            html += '<option value="' + escapeHtml(item.value) + '"' + selected + '>' + 
                    escapeHtml(item.label || item.value) + '</option>';
          });
        }
        html += '</select>';
        return html;
      
      case 'date':
        return '<input type="date" class="bino-param-input' + invalidClass + '" id="bino-param-' + param.name + '" ' +
               'name="' + param.name + '" value="' + escapeHtml(value) + '" ' +
               required + ' ' + placeholder + '>';
      
      case 'date_time':
        return '<input type="datetime-local" class="bino-param-input' + invalidClass + '" id="bino-param-' + param.name + '" ' +
               'name="' + param.name + '" value="' + escapeHtml(value) + '" ' +
               required + ' ' + placeholder + '>';
      
      case 'string':
      default:
        return '<input type="text" class="bino-param-input' + invalidClass + '" id="bino-param-' + param.name + '" ' +
               'name="' + param.name + '" value="' + escapeHtml(value) + '" ' +
               required + ' ' + placeholder + '>';
    }
  }

  function applyParams() {
    var panel = document.getElementById('bino-control-panel');
    var inputs = panel.querySelectorAll('.bino-param-input');
    var params = new URLSearchParams();
    var valid = true;

    inputs.forEach(function(input) {
      var name = input.name;
      var value = input.value.trim();
      var required = input.dataset.required === 'true';

      if (required && !value) {
        input.classList.add('invalid');
        valid = false;
      } else {
        input.classList.remove('invalid');
        if (value) {
          params.set(name, value);
        }
      }
    });

    if (!valid) return;

    var newURL = currentPath;
    var queryString = params.toString();
    if (queryString) {
      newURL += '?' + queryString;
    }

    navigateTo(newURL);
  }

  // Start waiting for engine immediately
  waitForEngine();

  // Intercept link clicks for seamless navigation
  document.addEventListener('click', function(e) {
    var link = e.target.closest('a[href]');
    if (!link) return;

    var href = link.getAttribute('href');
    if (!href || href.startsWith('http') || href.startsWith('//') || href.startsWith('#')) return;

    var url = new URL(href, window.location.origin);
    var path = url.pathname;

    // Check if this path is in our routes (use hasOwnProperty to handle empty string values)
    if (routes.hasOwnProperty(path)) {
      e.preventDefault();
      navigateTo(path + url.search);
    }
  });

  // Handle browser back/forward
  window.addEventListener('popstate', function(e) {
    if (e.state && e.state.url) {
      loadContent(e.state.url);
    }
  });

  function navigateTo(url) {
    history.pushState({ url: url }, '', url);
    loadContent(url);
  }

  function loadContent(url) {
    var context = document.querySelector('bn-context');
    if (context) {
      context.style.opacity = '0.5';
    }

    var applyBtn = document.getElementById('bino-apply-btn');
    if (applyBtn) {
      applyBtn.disabled = true;
      applyBtn.textContent = 'Loading...';
    }

    fetch(url, {
      headers: { 'X-Requested-With': 'bino-serve' }
    })
    .then(function(response) {
      if (!response.ok) {
        throw new Error('HTTP ' + response.status);
      }
      return response.text();
    })
    .then(function(html) {
      var doc = parser.parseFromString(html, 'text/html');
      
      // Extract the script data from the new page
      var scriptEl = doc.getElementById('bino-serve-runtime');
      if (!scriptEl) {
        console.error('bino: no runtime script found in response');
        var applyBtn = document.getElementById('bino-apply-btn');
        if (applyBtn) {
          applyBtn.disabled = false;
          applyBtn.textContent = 'Apply';
        }
        return;
      }
      
      var scriptText = scriptEl.textContent;
      
      // Extract missingParams from new page
      var newMissingParams = [];
      var missingMatch = scriptText.match(/var missingParams = (\[[^\]]*\])/);
      if (missingMatch && missingMatch[1]) {
        try {
          newMissingParams = JSON.parse(missingMatch[1]);
        } catch (e) {}
      }
      
      // Extract queryParams from new page
      var newQueryParams = [];
      var queryParamsMatch = scriptText.match(/var queryParams = (\[[\s\S]*?\]);[\s\n]*var missingParams/);
      if (queryParamsMatch && queryParamsMatch[1]) {
        try {
          newQueryParams = JSON.parse(queryParamsMatch[1]);
        } catch (e) {}
      }
      
      // Extract currentPath from new page
      var pathMatch = scriptText.match(/var currentPath = "([^"]*)"/);
      if (pathMatch && pathMatch[1]) {
        currentPath = pathMatch[1];
      }
      
      // Update missingParams and queryParams
      missingParams = newMissingParams;
      queryParams = newQueryParams;
      
      // Extract context HTML
      var newContextHtml = null;
      var contextMatch = scriptText.match(/var initialContextBase64 = "([^"]*)"/);
      if (contextMatch && contextMatch[1]) {
        newContextHtml = decodeBase64(contextMatch[1]);
      }
      
      // Update content area
      var contentArea = document.getElementById('bino-content-area');
      // Re-query context as it may have changed since function start
      var currentContext = document.querySelector('bn-context');
      
      if (newContextHtml) {
        var contextDoc = parser.parseFromString(newContextHtml, 'text/html');
        var newContext = contextDoc.querySelector('bn-context');
        if (newContext) {
          if (currentContext) {
            currentContext.replaceWith(newContext);
          } else if (contentArea) {
            contentArea.innerHTML = '';
            contentArea.appendChild(newContext);
          }
        }
      } else if (missingParams.length > 0 && contentArea) {
        // Show missing params message - also remove any existing bn-context
        if (currentContext) {
          currentContext.remove();
        }
        contentArea.innerHTML = buildMissingParamsMessage();
      }

      var newTitle = doc.querySelector('title');
      if (newTitle) {
        document.title = newTitle.textContent;
      }

      // Rebuild control panel with new route's params
      buildControlPanel();
    })
    .catch(function(err) {
      console.error('bino: navigation failed', err);
      if (applyBtn) {
        applyBtn.disabled = false;
        applyBtn.textContent = 'Apply';
      }
      // Show error to user
      if (context) {
        context.style.opacity = '1';
      }
      alert('Failed to load: ' + err.message);
    });
  }

  function updateControlPanelFromURL(url) {
    var urlObj = new URL(url, window.location.origin);
    var urlParams = urlObj.searchParams;
    var panel = document.getElementById('bino-control-panel');
    if (!panel) return;

    panel.querySelectorAll('.bino-param-input').forEach(function(input) {
      var value = urlParams.get(input.name);
      if (value !== null) {
        input.value = value;
      }
    });
  }

  // Set initial state with full URL
  if (!history.state) {
    history.replaceState({ url: currentURL }, '', currentURL);
  }
})();
</script>
`, string(routesJSON), string(queryParamsJSON), string(missingParamsJSON), html.EscapeString(currentPath), html.EscapeString(currentURL), contextBase64)
}

// withServeStyles applies production-appropriate styles to the frame HTML.
func withServeStyles(frameHTML []byte) []byte {
	// Similar to withPreviewStyles but optimized for production
	if len(frameHTML) == 0 {
		return frameHTML
	}

	// Check if already has serve styles
	if strings.Contains(string(frameHTML), "bn-serve-style") {
		return frameHTML
	}

	styleBlock := []byte(`
<style id="bn-serve-style">
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
		display: flex !important;
		margin: 0;
		min-height: 100vh;
		background: #f5f6fb;
		font-family: inherit;
		color: #111827;
	}
	#bino-control-panel {
		width: 280px;
		min-width: 280px;
		background: #ffffff;
		border-right: 1px solid #e5e7eb;
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 1rem;
		overflow-y: auto;
		max-height: 100vh;
		position: sticky;
		top: 0;
	}
	#bino-control-panel:empty {
		display: none;
	}
	#bino-control-panel h3 {
		margin: 0;
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: #6b7280;
	}
	.bino-param-group {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
	}
	.bino-param-label {
		font-size: 0.8125rem;
		font-weight: 500;
		color: #374151;
	}
	.bino-param-label .required {
		color: #dc2626;
		margin-left: 2px;
	}
	.bino-param-desc {
		font-size: 0.75rem;
		color: #6b7280;
		margin: 0;
	}
	.bino-param-input {
		padding: 0.5rem 0.75rem;
		border: 1px solid #d1d5db;
		border-radius: 6px;
		font-size: 0.875rem;
		font-family: inherit;
		transition: border-color 0.15s, box-shadow 0.15s;
	}
	.bino-param-input:focus {
		outline: none;
		border-color: #3b82f6;
		box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
	}
	.bino-param-input.invalid {
		border-color: #dc2626;
	}
	.bino-param-group.bino-param-missing {
		background: #fef2f2;
		border: 1px solid #fecaca;
		border-radius: 8px;
		padding: 0.75rem;
		margin: -0.25rem;
	}
	.bino-param-group.bino-param-missing .bino-param-label {
		color: #dc2626;
	}
	.bino-sitemap {
		border-bottom: 1px solid #e5e7eb;
		padding-bottom: 1rem;
		margin-bottom: 0.5rem;
	}
	.bino-route-list {
		list-style: none;
		margin: 0.5rem 0 0 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}
	.bino-route-list li a {
		display: block;
		padding: 0.5rem 0.75rem;
		border-radius: 6px;
		text-decoration: none;
		color: #374151;
		font-size: 0.875rem;
		transition: background 0.15s;
	}
	.bino-route-list li a:hover {
		background: #f3f4f6;
	}
	.bino-route-list li.active a {
		background: #eff6ff;
		color: #1d4ed8;
		font-weight: 500;
	}
	.bino-missing-params-banner {
		display: flex;
		align-items: flex-start;
		gap: 1rem;
		background: #fef3c7;
		border: 1px solid #fcd34d;
		border-radius: 8px;
		padding: 1.5rem;
		max-width: 480px;
		margin: 2rem auto;
	}
	.bino-missing-icon {
		font-size: 1.5rem;
		flex-shrink: 0;
	}
	.bino-missing-text strong {
		display: block;
		color: #92400e;
		margin-bottom: 0.5rem;
	}
	.bino-missing-text p {
		margin: 0;
		color: #78350f;
		font-size: 0.875rem;
		line-height: 1.5;
	}
	.bino-missing-text .required {
		color: #dc2626;
		font-weight: bold;
	}
	.bino-param-select {
		appearance: none;
		background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%236b7280' d='M3 5l3 3 3-3'/%3E%3C/svg%3E");
		background-repeat: no-repeat;
		background-position: right 0.75rem center;
		padding-right: 2rem;
		cursor: pointer;
	}
	.bino-range-inputs {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}
	.bino-range-inputs .bino-param-input {
		flex: 1;
		min-width: 0;
	}
	.bino-range-sep {
		color: #6b7280;
		font-size: 0.875rem;
	}
	.bino-range-slider-container {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}
	.bino-range-values {
		display: flex;
		justify-content: space-between;
		align-items: center;
		font-size: 0.8125rem;
		color: #374151;
		font-weight: 500;
	}
	.bino-range-value-min,
	.bino-range-value-max {
		background: #f3f4f6;
		padding: 0.25rem 0.5rem;
		border-radius: 4px;
		min-width: 3rem;
		text-align: center;
	}
	.bino-dual-range {
		position: relative;
		height: 1.5rem;
	}
	.bino-range-slider {
		position: absolute;
		width: 100%;
		height: 6px;
		top: 50%;
		transform: translateY(-50%);
		-webkit-appearance: none;
		appearance: none;
		background: transparent;
		pointer-events: none;
		padding: 0;
		border: none;
		margin: 0;
	}
	.bino-range-min-slider {
		z-index: 1;
	}
	.bino-range-max-slider {
		z-index: 2;
	}
	.bino-range-slider::-webkit-slider-runnable-track {
		width: 100%;
		height: 6px;
		background: #e5e7eb;
		border-radius: 3px;
	}
	.bino-range-min-slider::-webkit-slider-runnable-track {
		background: linear-gradient(to right, #e5e7eb 0%, #3b82f6 0%, #3b82f6 100%, #e5e7eb 100%);
	}
	.bino-range-slider::-webkit-slider-thumb {
		-webkit-appearance: none;
		appearance: none;
		width: 18px;
		height: 18px;
		background: #ffffff;
		border: 2px solid #3b82f6;
		border-radius: 50%;
		cursor: pointer;
		pointer-events: auto;
		margin-top: -6px;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
	}
	.bino-range-slider::-webkit-slider-thumb:hover {
		background: #eff6ff;
	}
	.bino-range-slider::-moz-range-track {
		width: 100%;
		height: 6px;
		background: #e5e7eb;
		border-radius: 3px;
	}
	.bino-range-slider::-moz-range-thumb {
		width: 14px;
		height: 14px;
		background: #ffffff;
		border: 2px solid #3b82f6;
		border-radius: 50%;
		cursor: pointer;
		pointer-events: auto;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
	}
	.bino-range-slider::-moz-range-thumb:hover {
		background: #eff6ff;
	}
	input[type="date"].bino-param-input,
	input[type="datetime-local"].bino-param-input {
		cursor: pointer;
	}
	input[type="number"].bino-param-input {
		-moz-appearance: textfield;
	}
	input[type="number"].bino-param-input::-webkit-outer-spin-button,
	input[type="number"].bino-param-input::-webkit-inner-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}
	#bino-apply-btn {
		padding: 0.625rem 1rem;
		background: #3b82f6;
		color: #ffffff;
		border: none;
		border-radius: 6px;
		font-size: 0.875rem;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.15s;
	}
	#bino-apply-btn:hover {
		background: #2563eb;
	}
	#bino-apply-btn:disabled {
		background: #9ca3af;
		cursor: not-allowed;
	}
	#bino-content-area {
		flex: 1;
		padding: 1.5rem;
		overflow-x: auto;
		min-width: 0;
	}
	bn-context {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 1.75rem;
		width: fit-content;
		min-width: 100%;
		margin: 0 auto;
		transition: opacity 0.15s ease;
	}
	bn-layout-page {
		box-sizing: border-box;
		background: #ffffff;
		box-shadow: 0 14px 40px rgba(15, 23, 42, 0.12);
		flex-shrink: 0;
	}
</style>
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

	// Inject control panel and content wrapper after <body>
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

	// Inject the layout wrapper: control panel + content area
	// Find </body> to wrap content
	bodyCloseTag := strings.Index(resultStr, "</body>")
	if bodyCloseTag == -1 {
		return result
	}

	// Extract original body content
	originalBodyContent := resultStr[bodyEnd:bodyCloseTag]

	// Build new body structure
	var sb strings.Builder
	sb.WriteString(resultStr[:bodyEnd])
	sb.WriteString(`<div id="bino-control-panel"></div><div id="bino-content-area">`)
	sb.WriteString(originalBodyContent)
	sb.WriteString(`</div>`)
	sb.WriteString(resultStr[bodyCloseTag:])

	return []byte(sb.String())
}
