package refresh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"path"
	"strings"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/report/config"
	reportgraph "bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/spec"
)

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
	reachable := g.CollectReachable([]*reportgraph.Node{root})

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
			Name:      node.DisplayName(),
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
	b.WriteString(`<bino-inspector></bino-inspector>`)

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

func setErrorPage(server *httpserver.Server, message, hint string) {
	if server == nil {
		return
	}
	content := buildErrorPage(message, hint)
	server.SetLocalAssets(nil)
	server.SetContentRoutes(nil)
	server.SetContentFunc(httpserver.StaticContent(append([]byte(nil), content...), "text/html; charset=utf-8"))
	server.BroadcastContent("/", content)
}

func buildErrorPage(message, hint string) []byte {
	if message == "" {
		message = "An invalid layout configuration prevented preview rendering."
	}
	if hint == "" {
		hint = "Ensure at least one LayoutPage is defined and referenced by your report artefact."
	}
	var b strings.Builder
	// Standalone page — BinoBI DS values inlined (gray-50/700/900, gray-200 border, bad red, DS lg shadow).
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n  <meta charset=\"utf-8\">\n  <title>bino preview</title>\n  <link rel=\"icon\" type=\"image/png\" href=\"/__bino/assets/favicon.png\">\n  <style>body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', 'Noto Sans', Arial, sans-serif; background:#f6f8f9; color:#333c41; display:flex; align-items:center; justify-content:center; min-height:100vh; margin:0; } bn-context { display:flex; align-items:center; justify-content:center; width:100%; } .card { background:#fff; border:1px solid #e0e6e9; border-top:3px solid #c0392b; border-radius:16px; padding:2rem; max-width:520px; box-shadow:0 12px 28px rgba(17, 22, 26, 0.10), 0 4px 10px rgba(17, 22, 26, 0.05);} h1 { margin-top:0; font-size:1.5rem; color:#11161a;} p { line-height:1.5; } </style>\n</head>\n<body>\n  <bn-context>\n    <div class=\"card\">\n      <h1>Cannot render preview</h1>\n      <p>")
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
			"\t<link rel=\"stylesheet\" href=\"/__bino/shared/fonts.css\">\n" +
			"\t<link rel=\"stylesheet\" href=\"/__bino/preview/preview.css\">\n" +
			"\t<link rel=\"icon\" type=\"image/png\" href=\"/__bino/assets/favicon.png\">\n" +
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
	updated := append([]byte(nil), ctx[:insertAt]...)
	updated = append(updated, attr...)
	updated = append(updated, ctx[insertAt:]...)
	return updated
}
