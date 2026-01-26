// Package markdown provides markdown-to-HTML conversion for DocumentArtefact rendering.
// It uses goldmark with GFM extensions and generates print-optimized HTML.
package markdown

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	toc "go.abhg.dev/goldmark/toc"

	"bino.bi/bino/internal/logx"
)

// RenderOptions configures markdown-to-HTML conversion.
type RenderOptions struct {
	// BaseDir is the directory to resolve relative paths from.
	BaseDir string
	// Stylesheet is an optional path to a custom CSS file.
	Stylesheet string
	// TableOfContents enables TOC generation from headings.
	TableOfContents bool
	// PageBreakBetweenFiles inserts page breaks between source files.
	PageBreakBetweenFiles bool
	// Math enables LaTeX math rendering via KaTeX ($...$, $$...$$).
	Math bool
}

// DocumentOptions configures the HTML document wrapper.
type DocumentOptions struct {
	Title       string
	Author      string
	Subject     string
	Keywords    []string
	Format      string
	Orientation string
	Stylesheet  string // Custom CSS content (already loaded)
}

// RenderFiles converts multiple markdown files to a single HTML document.
// Files are processed in order and concatenated.
func RenderFiles(ctx context.Context, files []string, opts RenderOptions) ([]byte, error) {
	logger := logx.FromContext(ctx).Channel("markdown")

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Typographer,
			extension.Footnote,
			extension.DefinitionList,
			Ref(), // Custom :ref[Kind:name] syntax
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
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
		tocList := toc.RenderList(tocTree)
		if tocList != nil {
			tocBuf.WriteString(`<nav class="bn-toc"><h2>Table of Contents</h2>`)
			if err := goldmark.DefaultRenderer().Render(&tocBuf, nil, tocList); err != nil {
				logger.Warnf("TOC rendering failed: %v", err)
			} else {
				tocBuf.WriteString(`</nav>`)
			}
		}
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

// WrapDocument wraps rendered HTML content in a print-optimized HTML document.
func WrapDocument(content []byte, opts DocumentOptions) []byte {
	var customCSS template.CSS
	if opts.Stylesheet != "" {
		customCSS = template.CSS(opts.Stylesheet)
	}

	data := struct {
		Title       string
		Content     template.HTML
		PageCSS     template.CSS
		CustomCSS   template.CSS
		Format      string
		Orientation string
	}{
		Title:       opts.Title,
		Content:     template.HTML(content),
		PageCSS:     template.CSS(generatePageCSS(opts.Format, opts.Orientation)),
		CustomCSS:   customCSS,
		Format:      opts.Format,
		Orientation: opts.Orientation,
	}

	var buf bytes.Buffer
	if err := documentTemplate.Execute(&buf, data); err != nil {
		// Fallback to basic wrapping if template fails
		buf.Reset()
		buf.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>")
		buf.WriteString(template.HTMLEscapeString(opts.Title))
		buf.WriteString("</title></head><body>")
		buf.Write(content)
		buf.WriteString("</body></html>")
	}

	return buf.Bytes()
}

// LoadStylesheet reads a CSS file from disk.
func LoadStylesheet(baseDir, path string) (string, error) {
	if path == "" {
		return "", nil
	}

	absPath := path
	if !filepath.IsAbs(path) && baseDir != "" {
		absPath = filepath.Join(baseDir, path)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read stylesheet %s: %w", absPath, err)
	}

	return string(content), nil
}

// generatePageCSS returns CSS for the specified page format and orientation.
func generatePageCSS(format, orientation string) string {
	pageSize := getPageSize(format)
	if orientation == "landscape" {
		pageSize = pageSize + " landscape"
	}

	return fmt.Sprintf(`
@page {
	size: %s;
	margin: 2cm;
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
`, pageSize)
}

// getPageSize returns the CSS page size for a format.
func getPageSize(format string) string {
	switch format {
	case "a4":
		return "A4"
	case "a5":
		return "A5"
	case "letter":
		return "letter"
	case "legal":
		return "legal"
	default:
		return "A4"
	}
}

var documentTemplate = template.Must(template.New("document").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>{{.Title}}</title>
	<style>
		/* Base print styles */
		{{.PageCSS}}

		/* Document typography */
		:root {
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
			font-size: 11pt;
			line-height: 1.6;
			color: #1a1a1a;
		}

		body {
			margin: 0;
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
		}

		/* Page breaks */
		.bn-page-break {
			height: 0;
			page-break-after: always;
			break-after: page;
		}

		/* Custom styles */
		{{.CustomCSS}}
	</style>
</head>
<body>
<bn-context>
{{.Content}}
</bn-context>
</body>
</html>
`))
