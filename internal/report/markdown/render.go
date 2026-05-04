package markdown

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	htmltemplate "html/template"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"

	qjskatex "github.com/graemephi/goldmark-qjs-katex"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	htmlrender "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	toc "go.abhg.dev/goldmark/toc"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/render"
)

// RenderContext holds data needed for rendering :ref[Kind:name] references.
type RenderContext struct {
	// Documents is the complete list of manifest documents.
	Documents []config.Document
	// DatasetResults contains pre-executed dataset results.
	DatasetResults []dataset.Result
	// DatasourceResults contains collected datasource data.
	DatasourceResults []datasource.Result
	// Internationalizations contains i18n entries.
	Internationalizations []I18nEntry
	// ComponentStyles contains component style entries.
	ComponentStyles []ComponentStyleEntry
	// EngineVersion is the template engine version to use.
	EngineVersion string
	// AssetURLs maps asset names to resolved URLs for asset: image references.
	AssetURLs map[string]string
	// DataMode selects how datasource/dataset payloads are delivered. "" or
	// "inline" embeds gzip+base64; "url" emits URLs and populates the
	// EmittedData return value of WrapDocumentWithContext.
	DataMode string
	// DataBaseURL is the absolute base URL for url mode (e.g.
	// "http://127.0.0.1:45678").
	DataBaseURL string
	// docIndex maps kind:name to documents for ref resolution.
	docIndex map[string]config.Document
}

// I18nEntry represents an internationalization entry.
type I18nEntry struct {
	Code      string
	Namespace string
	Value     string
}

// ComponentStyleEntry represents a component style entry.
type ComponentStyleEntry struct {
	Name  string
	Value string
}

// NewRenderContext creates a render context from documents.
func NewRenderContext(docs []config.Document, datasetResults []dataset.Result, datasourceResults []datasource.Result, engineVersion string) *RenderContext {
	rc := &RenderContext{
		Documents:         docs,
		DatasetResults:    datasetResults,
		DatasourceResults: datasourceResults,
		EngineVersion:     engineVersion,
		docIndex:          make(map[string]config.Document, len(docs)),
	}

	// Build document index for ref resolution
	for _, doc := range docs {
		switch doc.Kind {
		case "Text", "Table", "ChartStructure", "ChartTime", "Tree", "Image", "LayoutCard", "Grid":
			key := doc.Kind + ":" + doc.Name
			rc.docIndex[key] = doc
		}
	}

	// Collect i18n entries
	for _, doc := range docs {
		if doc.Kind == "Internationalization" {
			var payload struct {
				Spec struct {
					Code      string `json:"code"`
					Namespace string `json:"namespace"`
					Value     string `json:"value"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err == nil && payload.Spec.Value != "" {
				rc.Internationalizations = append(rc.Internationalizations, I18nEntry{
					Code:      payload.Spec.Code,
					Namespace: payload.Spec.Namespace,
					Value:     payload.Spec.Value,
				})
			}
		}
	}

	// Collect component styles
	for _, doc := range docs {
		if doc.Kind == "ComponentStyle" {
			var payload struct {
				Spec struct {
					Style string `json:"style"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err == nil && payload.Spec.Style != "" {
				rc.ComponentStyles = append(rc.ComponentStyles, ComponentStyleEntry{
					Name:  doc.Name,
					Value: payload.Spec.Style,
				})
			}
		}
	}

	return rc
}

// ResolveRef looks up a document by kind and name.
func (rc *RenderContext) ResolveRef(kind, name string) (config.Document, bool) {
	if rc == nil {
		return config.Document{}, false
	}
	key := kind + ":" + name
	doc, ok := rc.docIndex[key]
	return doc, ok
}

// FullRenderOptions extends RenderOptions with additional context for full rendering.
type FullRenderOptions struct {
	RenderOptions
	// RenderContext provides document and dataset context for :ref resolution.
	RenderContext *RenderContext
	// Locale for the document (e.g., "en", "de").
	Locale string
	// TOCPageNumbers maps heading IDs to page numbers (from two-pass rendering).
	TOCPageNumbers map[string]int
	// Math enables LaTeX math rendering via KaTeX ($...$, $$...$$).
	Math bool
	// ExcludeTOC suppresses TOC generation even if TableOfContents is true.
	// Used to render content-only HTML for the split-PDF pipeline.
	ExcludeTOC bool
	// TOCOnly renders only the TOC section without the content body.
	// Used to render the TOC as a separate PDF with Roman numeral page numbers.
	TOCOnly bool
	// TOCNumbering enables hierarchical chapter numbers in the TOC
	// (e.g. 1, 1.1, 1.1.1, 1.1.1 a).
	TOCNumbering bool
}

// RenderResult holds the output of RenderFilesWithContext.
type RenderResult struct {
	// HTML is the rendered HTML content.
	HTML []byte
	// HeadingIDs contains heading anchor IDs extracted from the TOC tree.
	// These are used for page number extraction from the content PDF.
	HeadingIDs []string
}

// RenderFilesWithContext converts multiple markdown files to HTML with full bino context.
// This includes resolving :ref[Kind:name] to actual component HTML.
func RenderFilesWithContext(ctx context.Context, files []string, opts FullRenderOptions) (RenderResult, error) {
	logger := logx.FromContext(ctx).Channel("markdown")
	rc := opts.RenderContext

	// Build list of extensions
	extensions := []goldmark.Extender{
		extension.GFM,
		extension.Typographer,
		extension.Footnote,
		extension.DefinitionList,
		NewRefExtensionWithContext(rc), // Use context-aware ref extension
	}

	// Add asset URL resolution if asset URLs are available
	if rc != nil && len(rc.AssetURLs) > 0 {
		extensions = append(extensions, render.NewAssetExtension(rc.AssetURLs))
	}

	// Add KaTeX extension for math rendering if enabled
	if opts.Math {
		extensions = append(extensions, &qjskatex.Extension{})
	}

	// Create goldmark with extensions including context-aware ref rendering
	md := goldmark.New(
		goldmark.WithExtensions(extensions...),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			htmlrender.WithUnsafe(),
		),
	)

	var contentBuf bytes.Buffer
	var tocBuf bytes.Buffer
	tocTree := &toc.TOC{}

	// Always extract the TOC tree when TableOfContents is enabled (needed for heading IDs).
	extractTOC := opts.TableOfContents

	for i, file := range files {
		if err := ctx.Err(); err != nil {
			return RenderResult{}, err
		}

		absPath := file
		if !filepath.IsAbs(file) && opts.BaseDir != "" {
			absPath = filepath.Join(opts.BaseDir, file)
		}

		logger.Debugf("Processing markdown file: %s", absPath)

		content, err := os.ReadFile(absPath)
		if err != nil {
			return RenderResult{}, fmt.Errorf("read markdown file %s: %w", absPath, err)
		}

		// Add page break between files (except before the first one)
		if !opts.TOCOnly && i > 0 && opts.PageBreakBetweenFiles {
			contentBuf.WriteString(`<div class='bn-page-break'></div>`)
			contentBuf.WriteString("\n")
		}

		// Parse for TOC extraction
		if extractTOC {
			reader := text.NewReader(content)
			doc := md.Parser().Parse(reader)
			fileTOC, err := toc.Inspect(doc, content)
			if err != nil {
				logger.Warnf("TOC extraction failed for %s: %v", absPath, err)
			} else if fileTOC != nil {
				tocTree.Items = append(tocTree.Items, fileTOC.Items...)
			}
		}

		// Render markdown to HTML (skip if TOCOnly)
		if !opts.TOCOnly {
			if err := md.Convert(content, &contentBuf); err != nil {
				return RenderResult{}, fmt.Errorf("convert markdown %s: %w", absPath, err)
			}
		}
	}

	// Collect heading IDs from the TOC tree.
	var headingIDs []string
	if extractTOC {
		headingIDs = collectHeadingIDs(tocTree.Items)
	}

	// Render TOC HTML if enabled and not excluded.
	renderTOC := opts.TableOfContents && !opts.ExcludeTOC && len(tocTree.Items) > 0
	if renderTOC {
		tocBuf.WriteString(`<nav class='bn-toc'><h2>Table of Contents</h2>`)
		counters := make([]int, 0, 6) // tracks numbering state per depth level
		renderTOCItems(&tocBuf, tocTree.Items, opts.TOCPageNumbers, opts.TOCNumbering, counters, 0)
		tocBuf.WriteString(`</nav>`)
	}

	// Combine TOC and content
	var finalBuf bytes.Buffer
	if tocBuf.Len() > 0 {
		finalBuf.Write(tocBuf.Bytes())
		if !opts.TOCOnly {
			finalBuf.WriteString("\n")
		}
	}
	if !opts.TOCOnly {
		finalBuf.Write(contentBuf.Bytes())
	}

	return RenderResult{
		HTML:       finalBuf.Bytes(),
		HeadingIDs: headingIDs,
	}, nil
}

// collectHeadingIDs recursively collects heading IDs from TOC items.
func collectHeadingIDs(items toc.Items) []string {
	var ids []string
	for _, item := range items {
		if len(item.ID) > 0 {
			ids = append(ids, string(item.ID))
		}
		if len(item.Items) > 0 {
			ids = append(ids, collectHeadingIDs(item.Items)...)
		}
	}
	return ids
}

// renderTOCItems renders TOC items as HTML with optional page numbers and
// hierarchical chapter numbering.
//
// Numbering scheme by depth:
//
//	depth 0 (h1): 1, 2, 3
//	depth 1 (h2): 1.1, 1.2, 2.1
//	depth 2 (h3): 1.1.1, 1.1.2
//	depth 3 (h4): 1.1.1 a), 1.1.1 b)
//	depth 4+ (h5+): 1.1.1 a) i), 1.1.1 a) ii)
func renderTOCItems(buf *bytes.Buffer, items toc.Items, pageNumbers map[string]int, numbering bool, counters []int, depth int) {
	if len(items) == 0 {
		return
	}

	// Ensure counters slice has enough depth.
	for len(counters) <= depth {
		counters = append(counters, 0)
	}
	// Reset all deeper counters when entering a new level.
	counters[depth] = 0
	for i := depth + 1; i < len(counters); i++ {
		counters[i] = 0
	}

	buf.WriteString("<ul>")
	for _, item := range items {
		counters[depth]++

		buf.WriteString("<li>")
		itemID := string(item.ID)

		// Add page number first (floated right) if available
		if pageNumbers != nil && len(item.ID) > 0 {
			if pageNum, ok := pageNumbers[itemID]; ok && pageNum > 0 {
				buf.WriteString(`<span class='bn-toc-page'>`)
				fmt.Fprintf(buf, "%d", pageNum)
				buf.WriteString(`</span>`)
			}
		}

		// Write the link with optional chapter number prefix
		if len(item.ID) > 0 {
			buf.WriteString(`<a href='#`)
			buf.WriteString(html.EscapeString(itemID))
			buf.WriteString(`'>`)
		}
		if numbering {
			buf.WriteString(`<span class='bn-toc-num'>`)
			buf.WriteString(formatChapterNumber(counters, depth))
			buf.WriteString(`</span> `)
		}
		buf.WriteString(html.EscapeString(string(item.Title)))
		if len(item.ID) > 0 {
			buf.WriteString("</a>")
		}

		// Recurse for children
		if len(item.Items) > 0 {
			renderTOCItems(buf, item.Items, pageNumbers, numbering, counters, depth+1)
		}
		buf.WriteString("</li>")
	}
	buf.WriteString("</ul>")
}

// formatChapterNumber builds the hierarchical number string for a TOC entry.
func formatChapterNumber(counters []int, depth int) string {
	if depth <= 2 {
		// Levels 0-2: dotted numbers (1, 1.1, 1.1.1)
		var b strings.Builder
		for i := 0; i <= depth; i++ {
			if i > 0 {
				b.WriteByte('.')
			}
			fmt.Fprintf(&b, "%d", counters[i])
		}
		return b.String()
	}

	// Build the numeric prefix from levels 0-2
	var b strings.Builder
	limit := 2
	if depth < limit {
		limit = depth
	}
	for i := 0; i <= limit; i++ {
		if i > 0 {
			b.WriteByte('.')
		}
		fmt.Fprintf(&b, "%d", counters[i])
	}

	// Level 3: append letter suffix  a), b), c)
	if depth >= 3 {
		idx := counters[3] - 1
		if idx < 0 {
			idx = 0
		}
		if idx > 25 {
			idx = 25
		}
		fmt.Fprintf(&b, " %c)", 'a'+rune(idx)) //nolint:gosec // idx is clamped to [0,25]
	}

	// Level 4+: append Roman numeral suffix  i), ii), iii)
	if depth >= 4 {
		b.WriteByte(' ')
		b.WriteString(toLowerRoman(counters[4]))
		b.WriteByte(')')
	}

	return b.String()
}

// toLowerRoman converts a small integer to a lowercase Roman numeral.
func toLowerRoman(n int) string {
	if n <= 0 {
		return ""
	}
	vals := []int{10, 9, 5, 4, 1}
	syms := []string{"x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// FullDocumentOptions extends DocumentOptions for full rendering with bino context.
type FullDocumentOptions struct {
	DocumentOptions
	// Locale for the document (e.g., "en", "de").
	Locale string
	// RenderContext provides document and dataset context.
	RenderContext *RenderContext
}

// WrapDocumentWithContext wraps rendered HTML content in a full bino HTML document.
// This includes the template engine, bn-context, datasources, datasets, i18n, etc.
//
// When RenderContext.DataMode == "url", the returned []render.EmittedData
// describes the bodies that must be registered on previewhttp.Server before
// the HTML becomes reachable to the browser. In inline mode it is nil.
func WrapDocumentWithContext(content []byte, opts FullDocumentOptions) ([]byte, []render.EmittedData) {
	rc := opts.RenderContext
	locale := opts.Locale
	if locale == "" {
		locale = "de"
	}

	dataMode := ""
	dataBaseURL := ""
	if rc != nil {
		dataMode = rc.DataMode
		dataBaseURL = rc.DataBaseURL
	}
	useURL := dataMode == render.DataModeURL
	var emitted []render.EmittedData

	// Build context segments (datasources, datasets, i18n, styles)
	var segments []string

	// Render i18n entries
	if rc != nil {
		for _, entry := range rc.Internationalizations {
			if entry.Value == "" {
				continue
			}
			var b strings.Builder
			b.WriteString("<bn-internationalization")
			writeAttr(&b, "code", entry.Code)
			writeAttr(&b, "namespace", entry.Namespace)
			b.WriteString(">")
			b.WriteString(html.EscapeString(entry.Value))
			b.WriteString("</bn-internationalization>")
			segments = append(segments, b.String())
		}
	}

	// Render component styles
	if rc != nil {
		for _, style := range rc.ComponentStyles {
			if style.Value == "" {
				continue
			}
			var b strings.Builder
			b.WriteString("<bn-component-style")
			writeAttr(&b, "name", style.Name)
			b.WriteString(">")
			b.WriteString(html.EscapeString(style.Value))
			b.WriteString("</bn-component-style>")
			segments = append(segments, b.String())
		}
	}

	// Render datasources (deduped by name; see render.dedupeDatasourceResultsByName).
	if rc != nil {
		for _, res := range dedupeDatasourceResultsByName(rc.DatasourceResults) {
			var b strings.Builder
			b.WriteString("<bn-datasource")
			writeAttr(&b, "name", res.Name)
			if useURL {
				b.WriteString(">")
				hash := render.ContentHash(res.Data)
				b.WriteString(html.EscapeString(buildMarkdownDataURL(dataBaseURL, render.EmittedKindDatasource, res.Name, hash)))
				b.WriteString("</bn-datasource>")
				emitted = append(emitted, render.EmittedData{
					Kind: render.EmittedKindDatasource,
					Name: res.Name,
					Hash: hash,
					Body: res.Data,
				})
			} else {
				writeAttr(&b, "raw", "false")
				b.WriteString(">")
				compressed, err := render.CompressContent(res.Data)
				if err != nil {
					b.WriteString(html.EscapeString(string(res.Data)))
				} else {
					b.WriteString(compressed)
				}
				b.WriteString("</bn-datasource>")
			}
			segments = append(segments, b.String())
		}
	}

	// Render datasets (deduped by name; see render.dedupeDatasetResultsByName).
	if rc != nil {
		for _, res := range dedupeDatasetResultsByName(rc.DatasetResults) {
			var b strings.Builder
			b.WriteString("<bn-dataset")
			writeAttr(&b, "name", res.Name)
			writeAttr(&b, "static", "true")
			if useURL {
				b.WriteString(">")
				hash := render.ContentHash(res.Data)
				b.WriteString(html.EscapeString(buildMarkdownDataURL(dataBaseURL, render.EmittedKindDataset, res.Name, hash)))
				b.WriteString("</bn-dataset>")
				emitted = append(emitted, render.EmittedData{
					Kind: render.EmittedKindDataset,
					Name: res.Name,
					Hash: hash,
					Body: res.Data,
				})
			} else {
				writeAttr(&b, "raw", "false")
				b.WriteString(">")
				compressed, err := render.CompressContent(res.Data)
				if err != nil {
					b.WriteString(html.EscapeString(string(res.Data)))
				} else {
					b.WriteString(compressed)
				}
				b.WriteString("</bn-dataset>")
			}
			segments = append(segments, b.String())
		}
	}

	// Build context body
	var contextBody strings.Builder
	for _, seg := range segments {
		contextBody.WriteString(seg)
		contextBody.WriteByte('\n')
	}
	// Add the markdown content wrapped in a document section
	contextBody.WriteString(`<section class='bn-document-content'>`)
	contextBody.WriteString("\n")
	contextBody.Write(content)
	contextBody.WriteString("\n</section>")

	// Get engine version
	engineVersion := "latest"
	if rc != nil && rc.EngineVersion != "" {
		engineVersion = rc.EngineVersion
	}

	// Generate page CSS
	pageCSS := generatePageCSS(opts.Format, opts.Orientation)

	// Build custom CSS
	customCSS := ""
	if opts.Stylesheet != "" {
		customCSS = opts.Stylesheet
	}

	// Use the full template
	data := fullTemplateData{
		Locale:        locale,
		EngineVersion: engineVersion,
		Title:         opts.Title,
		PageCSS:       htmltemplate.CSS(pageCSS),               //nolint:gosec // G203: generated from trusted format/orientation values
		CustomCSS:     htmltemplate.CSS(customCSS),             //nolint:gosec // G203: stylesheet is from trusted manifest config
		ContextBody:   htmltemplate.HTML(contextBody.String()), //nolint:gosec // G203: content is rendered from trusted template engine output
	}

	var buf bytes.Buffer
	if err := fullDocumentTemplate.Execute(&buf, data); err != nil {
		// Fallback to basic wrapping if template fails
		buf.Reset()
		buf.WriteString("<!DOCTYPE html><html lang='")
		buf.WriteString(html.EscapeString(locale))
		buf.WriteString("'><head><meta charset='utf-8'><title>")
		buf.WriteString(html.EscapeString(opts.Title))
		buf.WriteString("</title></head><body><bn-context locale='")
		buf.WriteString(html.EscapeString(locale))
		buf.WriteString("'>")
		buf.Write(content)
		buf.WriteString("</bn-context></body></html>")
	}

	return buf.Bytes(), emitted
}

// buildMarkdownDataURL composes the URL the template engine fetches in url
// mode. Path segments are escaped to allow names with special characters.
func buildMarkdownDataURL(baseURL, kind, name, hash string) string {
	base := strings.TrimRight(baseURL, "/")
	return fmt.Sprintf("%s/__bino/data/%s/%s?hash=%s",
		base, kind, neturl.PathEscape(name), neturl.QueryEscape(hash))
}

// dedupeDatasetResultsByName collapses duplicate-name entries to the last
// occurrence (mirrors render.dedupeDatasetResultsByName — see comment there).
func dedupeDatasetResultsByName(results []dataset.Result) []dataset.Result {
	if len(results) <= 1 {
		return results
	}
	seen := make(map[string]int, len(results))
	out := make([]dataset.Result, 0, len(results))
	for _, r := range results {
		if idx, ok := seen[r.Name]; ok {
			out[idx] = r
			continue
		}
		seen[r.Name] = len(out)
		out = append(out, r)
	}
	return out
}

// dedupeDatasourceResultsByName mirrors dedupeDatasetResultsByName for
// datasource.Result.
func dedupeDatasourceResultsByName(results []datasource.Result) []datasource.Result {
	if len(results) <= 1 {
		return results
	}
	seen := make(map[string]int, len(results))
	out := make([]datasource.Result, 0, len(results))
	for _, r := range results {
		if idx, ok := seen[r.Name]; ok {
			out[idx] = r
			continue
		}
		seen[r.Name] = len(out)
		out = append(out, r)
	}
	return out
}

// writeAttr writes an HTML attribute if the value is non-empty.
func writeAttr(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString("='")
	b.WriteString(html.EscapeString(value))
	b.WriteString("'")
}

// fullTemplateData holds data for the full document template.
type fullTemplateData struct {
	Locale        string
	EngineVersion string
	Title         string
	PageCSS       htmltemplate.CSS
	CustomCSS     htmltemplate.CSS
	ContextBody   htmltemplate.HTML
}

// fullDocumentTemplate is the HTML template for full bino document rendering.
var fullDocumentTemplate = mustParseTemplate("fullDocument", `<!DOCTYPE html>
<html dir='ltr' lang='{{.Locale}}'>

<head>
  <meta charset='utf-8'>
  <meta name='viewport' content='width=device-width, initial-scale=1.0, minimum-scale=1.0, maximum-scale=1.0'>
  <title>{{.Title}}</title>
  <script type='module' src='/cdn/bn-template-engine/{{.EngineVersion}}/bn-template-engine.esm.js'></script>
  <script nomodule src='/cdn/bn-template-engine/{{.EngineVersion}}/bn-template-engine.esm.js'></script>
  <style>
    /* Page format */
    {{.PageCSS}}

    /* Document typography */
    :root {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
      font-size: 11pt;
      line-height: 1.6;
      color: #1a1a1a;
    }

    html, body {
      margin: 0;
      padding: 0;
    }

    bn-context {
      display: block;
    }

    .bn-document-content {
      padding: 0;
    }

    /* Headings */
    h1, h2, h3, h4, h5, h6 {
      margin-top: 1.5em;
      margin-bottom: 0.5em;
      font-weight: 600;
      line-height: 1.3;
    }

    h1 { font-size: 2em; border-bottom: 1px solid #e0e0e0; padding-bottom: 0.3em; }
    h2 { font-size: 1.5em; }
    h3 { font-size: 1.25em; }
    h4 { font-size: 1.1em; }
    h5, h6 { font-size: 1em; }

    /* Paragraphs */
    p {
      margin: 0 0 1em 0;
    }

    /* Lists */
    ul, ol {
      margin: 0 0 1em 0;
      padding-left: 2em;
    }

    li {
      margin-bottom: 0.25em;
    }

    /* Code */
    code {
      font-family: 'SF Mono', SFMono-Regular, Consolas, 'Liberation Mono', Menlo, monospace;
      font-size: 0.9em;
      background: #f4f4f4;
      padding: 0.15em 0.4em;
      border-radius: 3px;
    }

    pre {
      background: #f4f4f4;
      padding: 1em;
      overflow-x: auto;
      border-radius: 4px;
      margin: 0 0 1em 0;
    }

    pre code {
      background: none;
      padding: 0;
    }

    /* Tables */
    table {
      width: 100%;
      border-collapse: collapse;
      margin: 0 0 1em 0;
      font-size: 0.95em;
    }

    th, td {
      border: 1px solid #d0d0d0;
      padding: 0.5em 0.75em;
      text-align: left;
    }

    th {
      background: #f0f0f0;
      font-weight: 600;
    }

    tr:nth-child(even) {
      background: #fafafa;
    }

    /* Blockquotes */
    blockquote {
      margin: 0 0 1em 0;
      padding: 0.5em 1em;
      border-left: 4px solid #d0d0d0;
      background: #f8f8f8;
      color: #555;
    }

    blockquote p:last-child {
      margin-bottom: 0;
    }

    /* Links */
    a {
      color: #0066cc;
      text-decoration: none;
    }

    a:hover {
      text-decoration: underline;
    }

    @media print {
      a {
        color: inherit;
        text-decoration: none;
      }

      a[href^="http"]::after {
        content: " (" attr(href) ")";
        font-size: 0.8em;
        color: #666;
      }
    }

    /* Images */
    img {
      max-width: 100%;
      height: auto;
    }

    /* Horizontal rules */
    hr {
      border: none;
      border-top: 1px solid #d0d0d0;
      margin: 2em 0;
    }

    /* Table of Contents */
    .bn-toc {
      margin-bottom: 2em;
    }

    .bn-toc h2 {
      font-size: 1.25em;
      margin-top: 0;
    }

    .bn-toc ul {
      list-style: none;
      padding-left: 0;
    }

    .bn-toc li {
      margin-bottom: 0.35em;
    }

    .bn-toc ul ul {
      padding-left: 1.5em;
      margin-top: 0.35em;
    }

    .bn-toc a {
      color: inherit;
      text-decoration: none;
    }

    .bn-toc-page {
      color: #666;
      float: right;
    }

    .bn-toc-num {
      color: #444;
      font-weight: 500;
      min-width: 2.5em;
      display: inline-block;
    }

    /* Page breaks */
    .bn-page-break {
      height: 0;
      page-break-after: always;
      break-after: page;
    }

    @media print {
      .bn-page-break {
        page-break-after: always;
        break-after: page;
      }

      .bn-toc {
        page-break-after: always;
        break-after: page;
      }
    }

    /* Embedded components */
    .bn-ref-container {
      margin: 1em 0;
      display: flex;
      justify-content: center;
      page-break-inside: avoid;
      break-inside: avoid;
    }

    /* Figure and caption styles */
    .bn-figure {
      margin: 1.5em 0;
      page-break-inside: avoid;
      break-inside: avoid;
    }

    .bn-figure figcaption {
      font-size: 0.9em;
      color: #555;
      text-align: center;
      margin-top: 0.5em;
      font-style: italic;
    }

    /* KaTeX math styles */
    .katex {
      font-size: 1.1em;
    }

    .katex-display {
      margin: 1em 0;
      text-align: center;
    }

    /* Custom styles */
    {{.CustomCSS}}
  </style>
</head>
<body>
  <bn-context locale='{{.Locale}}'>
{{.ContextBody}}
  </bn-context>
</body>

</html>
`)

func mustParseTemplate(name, textVal string) *htmltemplate.Template {
	t, err := htmltemplate.New(name).Parse(textVal)
	if err != nil {
		panic(fmt.Sprintf("failed to parse template %s: %v", name, err))
	}
	return t
}

// RenderComponentHTML renders a component from its document spec.
// This is used by :ref[Kind:name] to inline component HTML.
// It delegates to render.ComponentFromSpec to ensure consistent HTML output
// between DocumentArtefact and ReportArtefact rendering.
func RenderComponentHTML(doc config.Document, assetURLs map[string]string) (string, error) {
	var payload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		return "", fmt.Errorf("parse %s %s: %w", doc.Kind, doc.Name, err)
	}
	return render.ComponentFromSpec(doc.Kind, payload.Spec, assetURLs)
}

// NewRefExtensionWithContext creates a goldmark extension that renders :ref[Kind:name]
// as actual component HTML using the provided render context.
func NewRefExtensionWithContext(rc *RenderContext) goldmark.Extender {
	return &refExtensionWithContext{rc: rc}
}

// refExtensionWithContext is a context-aware version of RefExtension.
type refExtensionWithContext struct {
	rc *RenderContext
}

// Extend implements goldmark.Extender.
func (e *refExtensionWithContext) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(NewRefParser(), 100),
		),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(newRefRendererWithContext(e.rc), 100),
		),
	)
}

// refRendererWithContext renders RefNode using the render context.
type refRendererWithContext struct {
	htmlrender.Config
	rc *RenderContext
}

// newRefRendererWithContext creates a context-aware ref renderer.
func newRefRendererWithContext(rc *RenderContext) renderer.NodeRenderer {
	return &refRendererWithContext{
		Config: htmlrender.NewConfig(),
		rc:     rc,
	}
}

// RegisterFuncs implements renderer.NodeRenderer.
func (r *refRendererWithContext) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindRefNode, r.renderRef)
}

func (r *refRendererWithContext) renderRef(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n, _ := node.(*RefNode)
	kind := n.RefKind
	name := n.RefName
	caption := n.Caption

	// Wrap in figure if caption is present
	if caption != "" {
		_, _ = w.WriteString(`<figure class='bn-figure'>`)
	}

	// Try to resolve the reference
	if r.rc != nil {
		doc, found := r.rc.ResolveRef(kind, name)
		if found {
			// Render the actual component
			componentHTML, err := RenderComponentHTML(doc, r.rc.AssetURLs)
			if err == nil {
				_, _ = w.WriteString(`<div class='bn-ref-container' data-ref-kind='`)
				_, _ = w.WriteString(html.EscapeString(kind))
				_, _ = w.WriteString(`' data-ref-name='`)
				_, _ = w.WriteString(html.EscapeString(name))
				_, _ = w.WriteString(`'>`)
				_, _ = w.WriteString(componentHTML)
				_, _ = w.WriteString(`</div>`)

				// Close figure and add caption
				if caption != "" {
					_, _ = w.WriteString(`<figcaption>`)
					_, _ = w.WriteString(html.EscapeString(caption))
					_, _ = w.WriteString(`</figcaption></figure>`)
				}
				return ast.WalkContinue, nil
			}
			// Fall through to placeholder if rendering fails
		}
	}

	// Fallback: render as placeholder reference
	_, _ = w.WriteString(`<bn-ref kind='`)
	_, _ = w.WriteString(html.EscapeString(kind))
	_, _ = w.WriteString(`' name='`)
	_, _ = w.WriteString(html.EscapeString(name))
	_, _ = w.WriteString(`'></bn-ref>`)

	// Close figure and add caption for fallback case
	if caption != "" {
		_, _ = w.WriteString(`<figcaption>`)
		_, _ = w.WriteString(html.EscapeString(caption))
		_, _ = w.WriteString(`</figcaption></figure>`)
	}

	return ast.WalkContinue, nil
}
