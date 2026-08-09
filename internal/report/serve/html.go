package serve

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// BuildHTML combines frame and context HTML with seamless navigation support.
// Instead of replacing the placeholder, it keeps the loading state and embeds context
// as data to be injected after the template engine is ready.
func BuildHTML(ctx context.Context, frameHTML, contextHTML []byte, liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, workdir string, docs []config.Document, session *duckdb.Session) []byte {
	frameStr := string(frameHTML)

	// Encode context HTML as base64 for safe embedding
	contextBase64 := base64.StdEncoding.EncodeToString(contextHTML)

	// Resolve dataset options for select parameters
	datasetOptions := ResolveDatasetOptions(ctx, workdir, docs, routeSpec, session)

	// Inject the navigation script and embedded context before </head>
	return injectScript([]byte(frameStr), liveArtefact, currentPath, routeSpec, rawQuery, contextBase64, datasetOptions, nil)
}

// BuildMissingParamsHTML generates a full HTML page with sidebar showing error indicators
// for missing required parameters and a message instead of the report content.
func BuildMissingParamsHTML(liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery string, missingParams []string, datasetOptions map[string][]QueryParamOptionItem) []byte {
	// Build a minimal HTML frame with the serve styles and the control panel
	// Context is empty since we don't render the report
	contextBase64 := ""

	// Create a set of missing params for quick lookup
	missingSet := make(map[string]struct{}, len(missingParams))
	for _, name := range missingParams {
		missingSet[name] = struct{}{}
	}

	// Apply serve styles to the frame HTML first, then inject script
	frameHTML := WithStyles([]byte(buildMissingParamsFrameHTML()))
	return injectScript(frameHTML, liveArtefact, currentPath, routeSpec, rawQuery, contextBase64, datasetOptions, missingSet)
}

// buildMissingParamsFrameHTML generates a minimal HTML frame for the missing params page.
// It includes the template engine so that navigation to other routes works properly.
func buildMissingParamsFrameHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Parameters Required</title>
<script type="module" src="/cdn/bn-template-engine/SNAPSHOT/bn-template-engine.esm.js"></script>
<script nomodule src="/cdn/bn-template-engine/SNAPSHOT/bn-template-engine.esm.js"></script>
</head>
<body>
</body>
</html>`
}

// injectScript adds the navigation script and embedded context before </head>.
// If missingParams is non-nil, it indicates which parameters are missing and should be highlighted with errors.
func injectScript(htmlBytes []byte, liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, contextBase64 string, datasetOptions map[string][]QueryParamOptionItem, missingParams map[string]struct{}) []byte {
	htmlStr := string(htmlBytes)
	script := buildScript(liveArtefact, currentPath, routeSpec, rawQuery, contextBase64, datasetOptions, missingParams)

	headClose := strings.Index(htmlStr, "</head>")
	if headClose == -1 {
		return htmlBytes
	}

	var b strings.Builder
	b.WriteString(htmlStr[:headClose])
	if liveArtefact.Spec.PWA != nil {
		b.WriteString(buildPWAHeadTags(liveArtefact.Spec.PWA))
	}
	b.WriteString(script)
	b.WriteString(htmlStr[headClose:])
	return []byte(b.String())
}

// buildRoutesJSON builds the routes map for navigation and returns JSON.
func buildRoutesJSON(liveArtefact config.LiveArtefact) []byte {
	routes := make(map[string]string)
	for path, route := range liveArtefact.Spec.Routes {
		title := route.Title
		if title == "" {
			title = route.Artifact
		}
		routes[path] = title
	}
	routesJSON, _ := json.Marshal(routes) //nolint:errcheck // map of strings cannot fail to marshal
	return routesJSON
}

// buildMissingParamsJSON builds a sorted list of missing parameter names and returns JSON.
func buildMissingParamsJSON(missingParams map[string]struct{}) []byte {
	missingList := make([]string, 0, len(missingParams))
	for name := range missingParams {
		missingList = append(missingList, name)
	}
	sort.Strings(missingList)                         // for consistent output
	missingParamsJSON, _ := json.Marshal(missingList) //nolint:errcheck // slice of strings cannot fail to marshal
	return missingParamsJSON
}

// buildQueryParamsJSON builds the query params array for the control panel and returns JSON.
func buildQueryParamsJSON(routeSpec config.LiveRouteSpec, datasetOptions map[string][]QueryParamOptionItem) []byte {
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
				opts.Items = make([]QueryParamOptionItem, 0, len(p.Options.Items))
				for _, item := range p.Options.Items {
					label := item.Label
					if label == "" {
						label = item.Value
					}
					opts.Items = append(opts.Items, QueryParamOptionItem{
						Value: item.Value,
						Label: label,
					})
				}
			}
			info.Options = opts
		}

		queryParams = append(queryParams, info)
	}
	queryParamsJSON, _ := json.Marshal(queryParams) //nolint:errcheck // slice of plain string structs cannot fail to marshal
	return queryParamsJSON
}

// buildScript generates an inline config script and a reference to the external serve runtime.
// If missingParams is non-nil, it indicates which parameters are missing and should be highlighted with errors.
func buildScript(liveArtefact config.LiveArtefact, currentPath string, routeSpec config.LiveRouteSpec, rawQuery, contextBase64 string, datasetOptions map[string][]QueryParamOptionItem, missingParams map[string]struct{}) string {
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
	cfgJSON, _ := json.Marshal(cfg) //nolint:errcheck // plain string struct cannot fail to marshal

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
	sb.WriteString(`<script type="module" src="/__bino/static/serve.js"></script>`)
	return sb.String()
}

// WithStyles applies production-appropriate styles to the frame HTML.
func WithStyles(frameHTML []byte) []byte {
	if len(frameHTML) == 0 {
		return frameHTML
	}

	// Check if already has serve styles
	if strings.Contains(string(frameHTML), "bn-serve-style") {
		return frameHTML
	}

	styleBlock := []byte(`
<link id="bn-serve-style" rel="stylesheet" href="/__bino/shared/tokens.css">
<link rel="stylesheet" href="/__bino/shared/fonts.css">
<link rel="stylesheet" href="/__bino/serve/serve.css">
<link rel="icon" type="image/png" href="/__bino/assets/favicon.png">
`)

	// Find </head> and inject styles before it
	headClose := bytes.Index(frameHTML, []byte("</head>"))
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
