package markdown

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	htmltemplate "html/template"
	"os"
	"path/filepath"
	"strings"

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
		case "Text", "Table", "ChartStructure", "ChartTime", "ChartTree", "Image", "LayoutCard", "Grid":
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
}

// RenderFilesWithContext converts multiple markdown files to HTML with full bino context.
// This includes resolving :ref[Kind:name] to actual component HTML.
func RenderFilesWithContext(ctx context.Context, files []string, opts FullRenderOptions) ([]byte, error) {
	logger := logx.FromContext(ctx).Channel("markdown")
	rc := opts.RenderContext

	// Create goldmark with extensions including context-aware ref rendering
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Typographer,
			extension.Footnote,
			extension.DefinitionList,
			NewRefExtensionWithContext(rc), // Use context-aware ref extension
		),
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

	for i, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		absPath := file
		if !filepath.IsAbs(file) && opts.BaseDir != "" {
			absPath = filepath.Join(opts.BaseDir, file)
		}

		logger.Debugf("Processing markdown file: %s", absPath)

		content, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("read markdown file %s: %w", absPath, err)
		}

		// Add page break between files (except before the first one)
		if i > 0 && opts.PageBreakBetweenFiles {
			contentBuf.WriteString(`<div class="bn-page-break"></div>`)
			contentBuf.WriteString("\n")
		}

		// Parse for TOC extraction
		if opts.TableOfContents {
			reader := text.NewReader(content)
			doc := md.Parser().Parse(reader)
			fileTOC, err := toc.Inspect(doc, content)
			if err != nil {
				logger.Warnf("TOC extraction failed for %s: %v", absPath, err)
			} else if fileTOC != nil {
				tocTree.Items = append(tocTree.Items, fileTOC.Items...)
			}
		}

		// Render markdown to HTML
		if err := md.Convert(content, &contentBuf); err != nil {
			return nil, fmt.Errorf("convert markdown %s: %w", absPath, err)
		}
	}

	// Render TOC if enabled and has items
	if opts.TableOfContents && len(tocTree.Items) > 0 {
		tocBuf.WriteString(`<nav class="bn-toc"><h2>Table of Contents</h2>`)
		renderTOCWithPageNumbers(&tocBuf, tocTree.Items, opts.TOCPageNumbers)
		tocBuf.WriteString(`</nav>`)
	}

	// Combine TOC and content
	var finalBuf bytes.Buffer
	if tocBuf.Len() > 0 {
		finalBuf.Write(tocBuf.Bytes())
		finalBuf.WriteString("\n")
	}
	finalBuf.Write(contentBuf.Bytes())

	return finalBuf.Bytes(), nil
}

// renderTOCWithPageNumbers renders TOC items as HTML with optional page numbers.
// If pageNumbers is nil or empty, page numbers are omitted.
func renderTOCWithPageNumbers(buf *bytes.Buffer, items toc.Items, pageNumbers map[string]int) {
	if len(items) == 0 {
		return
	}
	buf.WriteString("<ul>")
	for _, item := range items {
		buf.WriteString("<li>")
		itemID := string(item.ID)

		// Add page number first (floated right) if available
		if pageNumbers != nil && len(item.ID) > 0 {
			if pageNum, ok := pageNumbers[itemID]; ok && pageNum > 0 {
				buf.WriteString(`<span class="bn-toc-page">`)
				fmt.Fprintf(buf, "%d", pageNum)
				buf.WriteString(`</span>`)
			}
		}

		// Write the link with title
		if len(item.ID) > 0 {
			buf.WriteString(`<a href="#`)
			buf.WriteString(html.EscapeString(itemID))
			buf.WriteString(`">`)
		}
		buf.WriteString(html.EscapeString(string(item.Title)))
		if len(item.ID) > 0 {
			buf.WriteString("</a>")
		}

		// Recurse for children
		if len(item.Items) > 0 {
			renderTOCWithPageNumbers(buf, item.Items, pageNumbers)
		}
		buf.WriteString("</li>")
	}
	buf.WriteString("</ul>")
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
func WrapDocumentWithContext(content []byte, opts FullDocumentOptions) []byte {
	rc := opts.RenderContext
	locale := opts.Locale
	if locale == "" {
		locale = "de"
	}

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

	// Render datasources
	if rc != nil {
		for _, res := range rc.DatasourceResults {
			var b strings.Builder
			b.WriteString("<bn-datasource")
			writeAttr(&b, "name", res.Name)
			b.WriteString(">")
			b.WriteString(html.EscapeString(string(res.Data)))
			b.WriteString("</bn-datasource>")
			segments = append(segments, b.String())
		}
	}

	// Render datasets
	if rc != nil {
		for _, res := range rc.DatasetResults {
			var b strings.Builder
			b.WriteString("<bn-dataset")
			writeAttr(&b, "name", res.Name)
			writeAttr(&b, "static", "true")
			b.WriteString(">")
			b.WriteString(html.EscapeString(string(res.Data)))
			b.WriteString("</bn-dataset>")
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
	contextBody.WriteString(`<section class="bn-document-content">`)
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
		PageCSS:       htmltemplate.CSS(pageCSS),
		CustomCSS:     htmltemplate.CSS(customCSS),
		ContextBody:   htmltemplate.HTML(contextBody.String()),
	}

	var buf bytes.Buffer
	if err := fullDocumentTemplate.Execute(&buf, data); err != nil {
		// Fallback to basic wrapping if template fails
		buf.Reset()
		buf.WriteString("<!DOCTYPE html><html lang=\"")
		buf.WriteString(html.EscapeString(locale))
		buf.WriteString("\"><head><meta charset=\"utf-8\"><title>")
		buf.WriteString(html.EscapeString(opts.Title))
		buf.WriteString("</title></head><body><bn-context locale=\"")
		buf.WriteString(html.EscapeString(locale))
		buf.WriteString("\">")
		buf.Write(content)
		buf.WriteString("</bn-context></body></html>")
	}

	return buf.Bytes()
}

// writeAttr writes an HTML attribute if the value is non-empty.
func writeAttr(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString("=\"")
	b.WriteString(html.EscapeString(value))
	b.WriteString("\"")
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
<html dir="ltr" lang="{{.Locale}}">

<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, minimum-scale=1.0, maximum-scale=1.0">
  <title>{{.Title}}</title>
  <script type="module" src="/cdn/bn-template-engine/{{.EngineVersion}}/bn-template-engine.esm.js"></script>
  <script nomodule src="/cdn/bn-template-engine/{{.EngineVersion}}/bn-template-engine.esm.js"></script>
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

    /* Custom styles */
    {{.CustomCSS}}
  </style>
</head>
<body>
  <bn-context locale="{{.Locale}}">
{{.ContextBody}}
  </bn-context>
</body>

</html>
`)

func mustParseTemplate(name, text string) *htmltemplate.Template {
	t, err := htmltemplate.New(name).Parse(text)
	if err != nil {
		panic(fmt.Sprintf("failed to parse template %s: %v", name, err))
	}
	return t
}

// RenderComponentHTML renders a component from its document spec.
// This is used by :ref[Kind:name] to inline component HTML.
// It delegates to render.RenderComponentFromSpec to ensure consistent HTML output
// between DocumentArtefact and ReportArtefact rendering.
func RenderComponentHTML(doc config.Document) (string, error) {
	var payload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		return "", fmt.Errorf("parse %s %s: %w", doc.Kind, doc.Name, err)
	}
	return render.RenderComponentFromSpec(doc.Kind, payload.Spec)
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

	n := node.(*RefNode)
	kind := n.RefKind
	name := n.RefName

	// Try to resolve the reference
	if r.rc != nil {
		doc, found := r.rc.ResolveRef(kind, name)
		if found {
			// Render the actual component
			componentHTML, err := RenderComponentHTML(doc)
			if err == nil {
				_, _ = w.WriteString(`<div class="bn-ref-container" data-ref-kind="`)
				_, _ = w.WriteString(html.EscapeString(kind))
				_, _ = w.WriteString(`" data-ref-name="`)
				_, _ = w.WriteString(html.EscapeString(name))
				_, _ = w.WriteString(`">`)
				_, _ = w.WriteString(componentHTML)
				_, _ = w.WriteString(`</div>`)
				return ast.WalkContinue, nil
			}
			// Fall through to placeholder if rendering fails
		}
	}

	// Fallback: render as placeholder reference
	_, _ = w.WriteString(`<bn-ref kind="`)
	_, _ = w.WriteString(html.EscapeString(kind))
	_, _ = w.WriteString(`" name="`)
	_, _ = w.WriteString(html.EscapeString(name))
	_, _ = w.WriteString(`"></bn-ref>`)

	return ast.WalkContinue, nil
}
