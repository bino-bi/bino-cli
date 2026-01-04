package render

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/spec"
)

const (
	defaultLocale           = "de"
	defaultLayoutPageFormat = "xga"
)

// baseTemplate format args: locale, engineVersion, engineVersion, fontMarkup, orientationAttr, locale, body
var baseTemplate = strings.TrimSpace(`<!DOCTYPE html>
<html dir="ltr" lang="%s">

<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, minimum-scale=1.0, maximum-scale=1.0">
  <script type="module" src="/cdn/bn-template-engine/%s/bn-template-engine.esm.js"></script>
  <script nomodule src="/cdn/bn-template-engine/%s/bn-template-engine.esm.js"></script>
  <style>
    html,
    body {
      margin: 0;
      display: flex;
      justify-content: center;
      padding: 0;
    }
  </style>
%s
</head>
<body>
	<bn-context%s locale="%s">
%s
  </bn-context>
</body>

</html>
`)

// frameTemplate is a lightweight HTML shell that loads the template engine
// and contains an empty bn-context placeholder. Content is loaded asynchronously
// via SSE to reduce initial page load time.
// Format args: locale, engineVersion, engineVersion, fontMarkup, locale
var frameTemplate = strings.TrimSpace(`<!DOCTYPE html>
<html dir="ltr" lang="%s">

<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, minimum-scale=1.0, maximum-scale=1.0">
  <script type="module" src="/cdn/bn-template-engine/%s/bn-template-engine.esm.js"></script>
  <script nomodule src="/cdn/bn-template-engine/%s/bn-template-engine.esm.js"></script>
  <style>
    html,
    body {
      margin: 0;
      display: flex;
      justify-content: center;
      padding: 0;
    }
  </style>
%s
</head>
<body>
  <bn-context locale="%s">
    <div class="bn-loading" style="display:flex;align-items:center;justify-content:center;min-height:100vh;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#6b7280;">
      Loading report…
    </div>
  </bn-context>
</body>

</html>
`)

// contextTemplate wraps context-only content in a bn-context element for SSE delivery.
// This is used by FrameResult.ContextHTML so the client-side swapContext logic can
// parse and replace the bn-context element.
var contextTemplate = strings.TrimSpace(`<bn-context%s locale="%s">
%s
</bn-context>
`)

// Result captures the rendered HTML alongside metadata about local assets that must be proxied by the preview server.
type Result struct {
	HTML        []byte
	LocalAssets []LocalAsset
}

// FrameResult captures a two-phase render output: a lightweight frame HTML
// that loads quickly, and context HTML that contains the actual report content.
// This enables faster initial page loads by serving the frame first and then
// pushing context via SSE.
type FrameResult struct {
	// FrameHTML is a lightweight shell containing <head>, template engine scripts,
	// and an empty bn-context placeholder with a loading indicator.
	FrameHTML []byte
	// ContextHTML is a standalone <bn-context>...</bn-context> block containing
	// the full report content, suitable for SSE delivery and DOM replacement.
	ContextHTML []byte
	// LocalAssets lists files that need to be served by the preview HTTP server.
	LocalAssets []LocalAsset
}

// InvalidRootError indicates that a manifest attempted to render a root component other than LayoutPage.
type InvalidRootError struct {
	Kind string
	Name string
}

func (e *InvalidRootError) Error() string {
	if e == nil {
		return ""
	}
	target := "LayoutPage"
	if e.Name != "" {
		return fmt.Sprintf("document %s of kind %s cannot render as root; define a %s instead", e.Name, e.Kind, target)
	}
	return fmt.Sprintf("document kind %s cannot render as root; define a %s instead", e.Kind, target)
}

// IsInvalidRootError reports whether the provided error represents an unsupported root manifest kind.
func IsInvalidRootError(err error) bool {
	if err == nil {
		return false
	}
	var target *InvalidRootError
	return errors.As(err, &target)
}

// LocalAsset describes a file on disk that should be exposed through the preview HTTP server.
type LocalAsset struct {
	URLPath   string
	FilePath  string
	MediaType string
}

// GenerateHTML walks the workdir manifests and renders HTML markup that can be served to the preview browser.
// Datasource diagnostics and local assets that need HTTP proxying are returned alongside the rendered markup.
// The engineVersion parameter specifies which template engine version to use (e.g., "v1.2.3").
func GenerateHTML(ctx context.Context, workdir string, locale string, renderOrientation string, renderFormat string, mode RenderMode, engineVersion string) (Result, []datasource.Diagnostic, error) {
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		return Result{}, nil, fmt.Errorf("render: load manifests: %w", err)
	}

	// Execute datasets and collect warnings
	datasetResults, datasetWarnings, err := dataset.Execute(ctx, workdir, docs, nil)
	if err != nil {
		return Result{}, nil, fmt.Errorf("render: execute datasets: %w", err)
	}

	// Convert dataset warnings to diagnostics
	var diags []datasource.Diagnostic
	for _, w := range datasetWarnings {
		diags = append(diags, datasource.Diagnostic{
			Datasource: w.DataSet,
			Stage:      "dataset",
			Err:        fmt.Errorf("%s", w.Message),
		})
	}

	return GenerateHTMLFromDocumentsWithDatasets(ctx, docs, datasetResults, locale, renderOrientation, renderFormat, mode, diags, nil, engineVersion)
}

// GenerateHTMLFromDocuments renders HTML using an already loaded set of manifests.
// The mode parameter determines whether build-specific attributes like render-orientation are included.
// The engineVersion parameter specifies which template engine version to use (e.g., "v1.2.3").
func GenerateHTMLFromDocuments(ctx context.Context, docs []config.Document, locale string, renderOrientation string, renderFormat string, mode RenderMode, engineVersion string) (Result, []datasource.Diagnostic, error) {
	return GenerateHTMLFromDocumentsWithDatasets(ctx, docs, nil, locale, renderOrientation, renderFormat, mode, nil, nil, engineVersion)
}

// GenerateHTMLFromDocumentsWithDatasets renders HTML using loaded manifests and pre-executed dataset results.
// The constraintCtx parameter is optional and used for filtering inline layout children by constraints.
// The engineVersion parameter specifies which template engine version to use (e.g., "v1.2.3").
func GenerateHTMLFromDocumentsWithDatasets(ctx context.Context, docs []config.Document, datasetResults []dataset.Result, locale string, renderOrientation string, renderFormat string, mode RenderMode, existingDiags []datasource.Diagnostic, constraintCtx *spec.ConstraintContext, engineVersion string) (Result, []datasource.Diagnostic, error) {
	if locale == "" {
		locale = defaultLocale
	}
	targetFormat := strings.TrimSpace(renderFormat)

	sources, diags, err := datasource.Collect(ctx, docs)
	if err != nil {
		return Result{}, append(existingDiags, diags...), fmt.Errorf("render: collect datasources: %w", err)
	}
	// Merge existing diagnostics (from dataset warnings) with datasource diagnostics
	diags = append(existingDiags, diags...)

	internationalizations, err := collectInternationalizations(docs)
	if err != nil {
		return Result{}, diags, err
	}

	componentStyles, err := collectComponentStyles(docs)
	if err != nil {
		return Result{}, diags, err
	}

	fontAssets, assetComponents, localAssets, err := collectAssets(docs)
	if err != nil {
		return Result{}, diags, err
	}

	segments := renderInternationalizations(internationalizations)
	if renderedStyles := renderComponentStyles(componentStyles); len(renderedStyles) > 0 {
		segments = append(segments, renderedStyles...)
	}
	if renderedAssets := renderAssetComponents(assetComponents); len(renderedAssets) > 0 {
		segments = append(segments, renderedAssets...)
	}
	if ds := renderDatasources(sources); len(ds) > 0 {
		segments = append(segments, ds...)
	}
	// Render dataset results as <bn-dataset> elements
	if ds := renderDatasets(datasetResults); len(ds) > 0 {
		segments = append(segments, ds...)
	}

	// Create render context for layout children ref resolution.
	rc := newRenderCtx(ctx, docs, constraintCtx)

	for _, doc := range docs {
		switch doc.Kind {
		case "LayoutPage":
			htmlContent, include, err := renderLayoutPage(doc.Raw, targetFormat, rc)
			if err != nil {
				return Result{}, diags, fmt.Errorf("render: layout page %s: %w", doc.Name, err)
			}
			if !include {
				continue
			}
			segments = append(segments, htmlContent)
		case "LayoutCard", "Text", "ChartStructure", "ChartTime", "Table", "Image":
			// These kinds can be referenced as layout children but cannot be rendered as root.
			// Skip them silently - they will be rendered when referenced via ref in a LayoutPage.
			continue
		}
	}

	var body strings.Builder
	if len(segments) == 0 {
		body.WriteString("<section class=\"empty-state\">Define a LayoutPage or Text manifest to see the preview.</section>")
	} else {
		for _, segment := range segments {
			body.WriteString(segment)
			body.WriteByte('\n')
		}
	}

	fontMarkup := renderFontLinks(fontAssets)
	orientationAttr := ""
	// render-orientation is only added in build mode for PDF generation
	if mode == RenderModeBuild {
		if trimmed := strings.TrimSpace(renderOrientation); trimmed != "" {
			orientationAttr = fmt.Sprintf(" render-orientation=\"%s\"", html.EscapeString(trimmed))
		}
	}
	markup := fmt.Sprintf(baseTemplate, html.EscapeString(locale), engineVersion, engineVersion, fontMarkup, orientationAttr, html.EscapeString(locale), body.String())
	return Result{HTML: []byte(markup), LocalAssets: localAssets}, diags, nil
}

// GenerateFrameAndContext produces a two-phase render output for preview mode.
// The frame HTML loads quickly (template engine + placeholder), while the context
// HTML contains the full report content and is delivered asynchronously via SSE.
// This reduces perceived latency because the browser can start loading the template
// engine JS while the server is still rendering the report content.
// The engineVersion parameter specifies which template engine version to use (e.g., "v1.2.3").
func GenerateFrameAndContext(ctx context.Context, docs []config.Document, datasetResults []dataset.Result, locale string, renderFormat string, existingDiags []datasource.Diagnostic, constraintCtx *spec.ConstraintContext, engineVersion string) (FrameResult, []datasource.Diagnostic, error) {
	if locale == "" {
		locale = defaultLocale
	}
	targetFormat := strings.TrimSpace(renderFormat)

	sources, diags, err := datasource.Collect(ctx, docs)
	if err != nil {
		return FrameResult{}, append(existingDiags, diags...), fmt.Errorf("render: collect datasources: %w", err)
	}
	diags = append(existingDiags, diags...)

	internationalizations, err := collectInternationalizations(docs)
	if err != nil {
		return FrameResult{}, diags, err
	}

	componentStyles, err := collectComponentStyles(docs)
	if err != nil {
		return FrameResult{}, diags, err
	}

	fontAssets, assetComponents, localAssets, err := collectAssets(docs)
	if err != nil {
		return FrameResult{}, diags, err
	}

	// Build context body segments (everything inside <bn-context>)
	segments := renderInternationalizations(internationalizations)
	if renderedStyles := renderComponentStyles(componentStyles); len(renderedStyles) > 0 {
		segments = append(segments, renderedStyles...)
	}
	if renderedAssets := renderAssetComponents(assetComponents); len(renderedAssets) > 0 {
		segments = append(segments, renderedAssets...)
	}
	if ds := renderDatasources(sources); len(ds) > 0 {
		segments = append(segments, ds...)
	}
	if ds := renderDatasets(datasetResults); len(ds) > 0 {
		segments = append(segments, ds...)
	}

	// Create render context for layout children ref resolution.
	rc := newRenderCtx(ctx, docs, constraintCtx)

	for _, doc := range docs {
		switch doc.Kind {
		case "LayoutPage":
			htmlContent, include, err := renderLayoutPage(doc.Raw, targetFormat, rc)
			if err != nil {
				return FrameResult{}, diags, fmt.Errorf("render: layout page %s: %w", doc.Name, err)
			}
			if !include {
				continue
			}
			segments = append(segments, htmlContent)
		case "LayoutCard", "Text", "ChartStructure", "ChartTime", "Table", "Image":
			// These kinds can be referenced as layout children but cannot be rendered as root.
			// Skip them silently - they will be rendered when referenced via ref in a LayoutPage.
			continue
		}
	}

	var body strings.Builder
	if len(segments) == 0 {
		body.WriteString("<section class=\"empty-state\">Define a LayoutPage or Text manifest to see the preview.</section>")
	} else {
		for _, segment := range segments {
			body.WriteString(segment)
			body.WriteByte('\n')
		}
	}

	fontMarkup := renderFontLinks(fontAssets)

	// Frame: lightweight shell with template engine and loading placeholder
	frameMarkup := fmt.Sprintf(frameTemplate, html.EscapeString(locale), engineVersion, engineVersion, fontMarkup, html.EscapeString(locale))

	// Context: standalone <bn-context> block for SSE delivery
	// No render-orientation in preview mode
	contextMarkup := fmt.Sprintf(contextTemplate, "", html.EscapeString(locale), body.String())

	return FrameResult{
		FrameHTML:   []byte(frameMarkup),
		ContextHTML: []byte(contextMarkup),
		LocalAssets: localAssets,
	}, diags, nil
}
