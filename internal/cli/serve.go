package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/config"
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

			// Check for missing env vars - in serve mode this is an error
			if err := config.CheckMissingEnvVars(docs); err != nil {
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

			// Collect report artefacts for validation
			artefacts, err := config.CollectArtefacts(docs)
			if err != nil {
				return ConfigError(err)
			}

			// Validate the live artefact
			if err := config.ValidateLiveArtefact(*liveArtefact, artefacts); err != nil {
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
				art, ok := artefactMap[route.Artefact]
				if !ok {
					// This should have been caught by validation, but be safe
					return ConfigErrorf("route %q references unknown artefact %q", path, route.Artefact)
				}

				// Capture variables for closure
				routePath := path
				routeArt := art
				routeSpec := route // Capture route spec for query param validation

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
			}

			// Set up the server routes
			server.SetContentRoutes(routeMap)

			// Set default content function for root if "/" is in routes
			if rootRoute, ok := liveArtefact.Spec.Routes["/"]; ok {
				rootArt := artefactMap[rootRoute.Artefact]
				rootSpec := rootRoute // Capture route spec
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
			}

			// Collect all assets from all referenced artefacts
			allAssets := make([]previewhttp.LocalAsset, 0)
			for _, route := range liveArtefact.Spec.Routes {
				art := artefactMap[route.Artefact]
				renderResult, err := pipeline.RenderArtefactFrameAndContextWithMode(ctx, watchDir, docs, art, nil, spec.ModeServe)
				if err != nil {
					logger.Warnf("Could not pre-render artefact %s for asset collection: %v", art.Document.Name, err)
					continue
				}
				allAssets = append(allAssets, pipeline.ConvertLocalAssets(renderResult.LocalAssets)...)
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
	queryParams, err := validateAndMergeQueryParams(routeSpec, reqInfo.Query)
	if err != nil {
		// Return 400 Bad Request for missing required params
		return nil, "", previewhttp.NewHTTPError(400, err.Error())
	}

	// Build cache key from artefact name + sorted query params
	cacheKey := buildCacheKey(artefact.Document.Name, queryParams)

	// Try cache first
	if entry, ok := cache.Get(cacheKey); ok {
		return buildServeHTML(entry.frameHTML, entry.contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery), "text/html; charset=utf-8", nil
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

	return buildServeHTML(frameHTML, contextHTML, liveArtefact, routePath, routeSpec, reqInfo.RawQuery), "text/html; charset=utf-8", nil
}

// validateAndMergeQueryParams validates query parameters against route spec.
// Returns merged params (request values + defaults) or error if required param is missing.
func validateAndMergeQueryParams(routeSpec config.LiveRouteSpec, requestQuery map[string][]string) (map[string]string, error) {
	result := make(map[string]string)

	// Apply defaults first
	defaults := routeSpec.GetQueryParamDefaults()
	for name, defaultVal := range defaults {
		result[name] = defaultVal
	}

	// Override with request values (only for declared params)
	declaredParams := make(map[string]struct{})
	for _, p := range routeSpec.QueryParams {
		declaredParams[p.Name] = struct{}{}
	}

	for name := range declaredParams {
		if values, ok := requestQuery[name]; ok && len(values) > 0 {
			result[name] = values[0]
		}
	}

	// Check for missing required params (params with no default)
	for _, requiredName := range routeSpec.GetRequiredQueryParams() {
		if _, ok := result[requiredName]; !ok {
			return nil, fmt.Errorf("missing required query parameter: %s", requiredName)
		}
	}

	return result, nil
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
func buildServeHTML(frameHTML, contextHTML []byte, liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery string) []byte {
	frameStr := string(frameHTML)

	// Encode context HTML as base64 for safe embedding
	contextBase64 := base64.StdEncoding.EncodeToString(contextHTML)

	// Inject the navigation script and embedded context before </head>
	return injectServeScript([]byte(frameStr), liveArtefact, currentPath, routeSpec, rawQuery, contextBase64)
}

// injectServeScript adds the navigation script and embedded context before </head>.
func injectServeScript(htmlBytes []byte, liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, contextBase64 string) []byte {
	htmlStr := string(htmlBytes)
	script := buildServeScript(liveArtefact, currentPath, routeSpec, rawQuery, contextBase64)

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
	Name        string  `json:"name"`
	Default     *string `json:"default,omitempty"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required"`
}

// buildServeScript generates the JavaScript for seamless navigation, content injection, and control panel.
func buildServeScript(liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, contextBase64 string) string {
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

	// Build queryParams JSON for the control panel
	queryParams := make([]queryParamInfo, 0, len(routeSpec.QueryParams))
	for _, p := range routeSpec.QueryParams {
		queryParams = append(queryParams, queryParamInfo{
			Name:        p.Name,
			Default:     p.Default,
			Description: p.Description,
			Required:    p.Default == nil,
		})
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

  // Build control panel for query parameters
  function buildControlPanel() {
    if (queryParams.length === 0) return;

    var panel = document.getElementById('bino-control-panel');
    if (!panel) return;

    // Parse current URL params
    var urlParams = new URLSearchParams(window.location.search);

    var html = '<h3>Parameters</h3>';
    queryParams.forEach(function(param) {
      var value = urlParams.get(param.name);
      if (value === null && param.default !== undefined && param.default !== null) {
        value = param.default;
      }
      value = value || '';

      html += '<div class="bino-param-group">';
      html += '<label class="bino-param-label" for="bino-param-' + param.name + '">' + 
              escapeHtml(param.name) + 
              (param.required ? '<span class="required">*</span>' : '') + 
              '</label>';
      if (param.description) {
        html += '<p class="bino-param-desc">' + escapeHtml(param.description) + '</p>';
      }
      html += '<input type="text" class="bino-param-input" id="bino-param-' + param.name + '" ' +
              'name="' + param.name + '" value="' + escapeHtml(value) + '" ' +
              'data-required="' + param.required + '" ' +
              (param.default !== undefined && param.default !== null ? 'placeholder="' + escapeHtml(param.default) + '"' : '') +
              '>';
      html += '</div>';
    });

    html += '<button type="button" id="bino-apply-btn">Apply</button>';
    panel.innerHTML = html;

    // Add event listeners
    var applyBtn = document.getElementById('bino-apply-btn');
    applyBtn.addEventListener('click', applyParams);

    // Allow Enter key to apply
    panel.querySelectorAll('.bino-param-input').forEach(function(input) {
      input.addEventListener('keypress', function(e) {
        if (e.key === 'Enter') applyParams();
      });
      input.addEventListener('input', function() {
        input.classList.remove('invalid');
      });
    });
  }

  function escapeHtml(text) {
    var div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
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

    if (routes[path]) {
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
      var newContext = doc.querySelector('bn-context');

      if (newContext && context) {
        context.replaceWith(newContext);
      } else {
        window.location.href = url;
        return;
      }

      var newTitle = doc.querySelector('title');
      if (newTitle) {
        document.title = newTitle.textContent;
      }

      // Update control panel with new URL params
      updateControlPanelFromURL(url);

      if (applyBtn) {
        applyBtn.disabled = false;
        applyBtn.textContent = 'Apply';
      }
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
`, string(routesJSON), string(queryParamsJSON), html.EscapeString(currentPath), html.EscapeString(currentURL), contextBase64)
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
