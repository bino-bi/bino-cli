package render

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/spec"
)

const (
	// revealJSCDN is the absolute CDN URL for Reveal.js (always loaded from jsdelivr).
	revealJSCDN = "https://cdn.jsdelivr.net/npm/reveal.js@5.2.1"
	// cdnBaseURLServer is the server-relative CDN prefix used in preview/present mode.
	cdnBaseURLServer = "/cdn"
	// cdnBaseURLBinoRemote is the absolute CDN URL for the bino template engine (R2 bucket).
	cdnBaseURLBinoRemote = "https://pub-5000c2eb6ba64ece971b69ce37fed581.r2.dev"
)

// presentationTemplate format args:
//
//	locale, engineCDN, engineVersion, engineCDN, engineVersion,
//	revealCDN, revealCDN, theme,
//	extraHead, locale, contextBody, slides,
//	revealCDN, revealConfig
var presentationTemplate = strings.TrimSpace(`<!DOCTYPE html>
<html dir='ltr' lang='%s'>

<head>
  <meta charset='utf-8'>
  <meta name='viewport' content='width=device-width, initial-scale=1.0'>
  <script type='module' src='%s/bn-template-engine/%s/bn-template-engine.esm.js'></script>
  <script nomodule src='%s/bn-template-engine/%s/bn-template-engine.esm.js'></script>
  <link rel='stylesheet' href='%s/dist/reveal.css'>
  <link rel='stylesheet' href='%s/dist/theme/%s.css'>
  <style>
    html, body { margin: 0; padding: 0 !important; width: 100%%; height: 100%%; background: #fff; }
    bn-context { display: block; width: 100%%; height: 100%%; }
    .reveal { background: #fff !important; }
    .reveal .slide-background { background: transparent !important; }
    .reveal .slides section {
      padding: 0 !important;
      margin: 0 !important;
      display: flex !important;
      align-items: center;
      justify-content: center;
      overflow: hidden;
    }
    .reveal .slides section bn-layout-page {
      transform-origin: center center;
      margin: 0 !important;
      flex-shrink: 0;
      text-align: initial;
    }
    /* Hide Reveal.js controls and progress in presentation mode */
    .reveal .controls { opacity: 0.3; }
    .reveal .controls:hover { opacity: 1; }
    .reveal .progress { height: 3px !important; }
  </style>
%s
</head>
<body>
  <bn-context locale='%s'>
%s
  <div class='reveal'>
    <div class='slides'>
%s
    </div>
  </div>
  </bn-context>
  <script src='%s/dist/reveal.js'></script>
  <script>
    // Wait for the template engine to register bn-layout-page before
    // initializing Reveal.js so it can measure rendered slide dimensions.
    var cfg = %s;
    function scalePages() {
      document.querySelectorAll('.reveal .slides section bn-layout-page').forEach(function(page) {
        var section = page.closest('section');
        if (!section) return;
        var sw = section.offsetWidth, sh = section.offsetHeight;
        var pw = page.offsetWidth, ph = page.offsetHeight;
        if (pw > 0 && ph > 0) {
          var scale = Math.min(sw / pw, sh / ph);
          page.style.transform = 'scale(' + scale + ')';
        }
      });
    }
    function initReveal() {
      Reveal.initialize(cfg);
      Reveal.on('ready', function() { setTimeout(function(){ Reveal.layout(); scalePages(); }, 300); });
      Reveal.on('resize', scalePages);
    }
    if (customElements.get('bn-layout-page')) {
      initReveal();
    } else {
      customElements.whenDefined('bn-layout-page').then(initReveal);
    }
  </script>
</body>

</html>
`)

// presentationFrameTemplate is the lightweight shell for two-phase rendering.
// It loads the template engine and Reveal.js, with a loading placeholder.
// Format args: locale, engineCDN, engineVersion, engineCDN, engineVersion,
//
//	revealCDN, revealCDN, theme, extraHead, revealCDN, revealConfig, locale
var presentationFrameTemplate = strings.TrimSpace(`<!DOCTYPE html>
<html dir='ltr' lang='%s'>

<head>
  <meta charset='utf-8'>
  <meta name='viewport' content='width=device-width, initial-scale=1.0'>
  <script type='module' src='%s/bn-template-engine/%s/bn-template-engine.esm.js'></script>
  <script nomodule src='%s/bn-template-engine/%s/bn-template-engine.esm.js'></script>
  <link rel='stylesheet' href='%s/dist/reveal.css'>
  <link rel='stylesheet' href='%s/dist/theme/%s.css'>
  <style>
    html, body { margin: 0; padding: 0 !important; width: 100%%; height: 100%%; background: #fff; }
    bn-context { display: block; width: 100%%; height: 100%%; }
    .reveal { background: #fff !important; }
    .reveal .slide-background { background: transparent !important; }
    .reveal .slides section {
      padding: 0 !important;
      margin: 0 !important;
      display: flex !important;
      align-items: center;
      justify-content: center;
      overflow: hidden;
    }
    .reveal .slides section bn-layout-page {
      transform-origin: center center;
      margin: 0 !important;
      flex-shrink: 0;
      text-align: initial;
    }
    /* Hide Reveal.js controls and progress in presentation mode */
    .reveal .controls { opacity: 0.3; }
    .reveal .controls:hover { opacity: 1; }
    .reveal .progress { height: 3px !important; }
  </style>
%s
</head>
<body>
  <bn-context locale='%s'>
    <div class='bn-loading' style='display:flex;align-items:center;justify-content:center;min-height:100vh;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;color:#6b7280;'>
      Loading presentation…
    </div>
  </bn-context>
  <script src='%s/dist/reveal.js'></script>
  <script>
    var revealCfg = %s;
    function scalePages() {
      document.querySelectorAll('.reveal .slides section bn-layout-page').forEach(function(page) {
        var section = page.closest('section');
        if (!section) return;
        var sw = section.offsetWidth, sh = section.offsetHeight;
        var pw = page.offsetWidth, ph = page.offsetHeight;
        if (pw > 0 && ph > 0) {
          var scale = Math.min(sw / pw, sh / ph);
          page.style.transform = 'scale(' + scale + ')';
        }
      });
    }
    function initReveal() {
      if (!document.querySelector('.reveal')) return;
      if (typeof Reveal !== 'undefined' && Reveal.isReady && Reveal.isReady()) {
        try { Reveal.destroy(); } catch(e) {}
      }
      Reveal.initialize(revealCfg);
      Reveal.on('ready', function() { setTimeout(function(){ Reveal.layout(); scalePages(); }, 300); });
      Reveal.on('resize', scalePages);
    }
    function tryInit() {
      if (!document.querySelector('.reveal')) return;
      if (customElements.get('bn-layout-page')) {
        requestAnimationFrame(initReveal);
      } else {
        customElements.whenDefined('bn-layout-page').then(function() {
          requestAnimationFrame(initReveal);
        });
      }
    }
    // Listen for preview SSE context swap (dispatched by preview-app.js)
    document.addEventListener('bn-preview:content-updated', tryInit);
    // Also try on initial load in case context is already present
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', tryInit);
    } else {
      tryInit();
    }
  </script>
</body>

</html>
`)

// presentationContextTemplate wraps slides in a <bn-context> block for SSE delivery.
// Format args: locale, contextBody, slides
var presentationContextTemplate = strings.TrimSpace(`<bn-context locale='%s'>
%s
<div class='reveal'>
  <div class='slides'>
%s
  </div>
</div>
</bn-context>
`)

// PresentationResult captures the rendered presentation HTML alongside local assets.
type PresentationResult struct {
	HTML        []byte
	LocalAssets []LocalAsset
	// EmittedData is non-nil only when PluginOptions.DataMode == "url".
	EmittedData []EmittedData
}

// PresentationFrameResult captures a two-phase render for preview mode.
type PresentationFrameResult struct {
	FrameHTML   []byte
	ContextHTML []byte
	LocalAssets []LocalAsset
	// EmittedData is non-nil only when PluginOptions.DataMode == "url".
	EmittedData []EmittedData
}

// PresentationOptions configures presentation rendering.
type PresentationOptions struct {
	// Standalone uses absolute CDN URLs for a self-contained HTML file.
	// When false (default), uses server-relative /cdn/ paths for preview/present mode.
	Standalone bool
}

// GeneratePresentationHTML renders a Reveal.js presentation from LayoutPage documents.
// Each LayoutPage is embedded as-is inside a Reveal.js <section>, preserving the exact
// layout and rendering from the report pipeline. Reveal.js handles slide transitions
// and keyboard navigation.
func GeneratePresentationHTML(ctx context.Context, docs []config.Document, datasetResults []dataset.Result, artifact config.Artifact, presCfg config.PresentationConfig, existingDiags []datasource.Diagnostic, constraintCtx *spec.ConstraintContext, engineVersion string, allDocs []config.Document, pluginOpts *PluginOptions, opts *PresentationOptions) (PresentationResult, []datasource.Diagnostic, error) {
	locale := artifact.Spec.Language
	if locale == "" {
		locale = defaultLocale
	}

	var collectOpts *datasource.CollectOptions
	var extraHeadMarkup string
	var pluginRenderer PluginComponentRenderer
	var dataMode, dataBaseURL string
	if pluginOpts != nil {
		collectOpts = pluginOpts.CollectOptions
		extraHeadMarkup = pluginOpts.ExtraHeadMarkup
		pluginRenderer = pluginOpts.ComponentRenderer
		dataMode = pluginOpts.DataMode
		dataBaseURL = pluginOpts.DataBaseURL
	}

	sources, diags, err := datasource.Collect(ctx, docs, collectOpts)
	if err != nil {
		return PresentationResult{}, append(existingDiags, diags...), fmt.Errorf("render presentation: collect datasources: %w", err)
	}
	diags = append(existingDiags, diags...)

	internationalizations, err := collectInternationalizations(docs)
	if err != nil {
		return PresentationResult{}, diags, err
	}

	componentStyles, err := collectComponentStyles(docs)
	if err != nil {
		return PresentationResult{}, diags, err
	}

	fontAssets, assetComponents, localAssets, err := collectAssets(docs)
	if err != nil {
		return PresentationResult{}, diags, err
	}

	scalingGroups, err := collectScalingGroups(docs)
	if err != nil {
		return PresentationResult{}, diags, err
	}

	// Build context body segments (datasources, datasets, i18n, styles, assets)
	segments := renderInternationalizations(internationalizations)
	if renderedStyles := renderComponentStyles(componentStyles); len(renderedStyles) > 0 {
		segments = append(segments, renderedStyles...)
	}
	if renderedAssets := renderAssetComponents(assetComponents); len(renderedAssets) > 0 {
		segments = append(segments, renderedAssets...)
	}
	if sg := renderScalingGroups(scalingGroups); len(sg) > 0 {
		segments = append(segments, sg...)
	}
	referencedSources := collectReferencedDatasources(docs, allDocs)
	sources = filterDatasourcesByRefs(sources, referencedSources)
	var emitted []EmittedData
	if ds, em := renderDatasources(sources, dataMode, dataBaseURL); len(ds) > 0 {
		segments = append(segments, ds...)
		emitted = append(emitted, em...)
	}
	if ds, em := renderDatasets(datasetResults, dataMode, dataBaseURL); len(ds) > 0 {
		segments = append(segments, ds...)
		emitted = append(emitted, em...)
	}

	assetURLMap := make(map[string]string, len(assetComponents))
	for _, ac := range assetComponents {
		assetURLMap[ac.name] = ac.value
	}

	renderModeStr := "build"
	if opts == nil || !opts.Standalone {
		renderModeStr = "preview"
	}
	rc := newRenderCtx(ctx, docs, constraintCtx, allDocs, assetURLMap, pluginRenderer, renderModeStr)

	// Render each LayoutPage as a slide — the page is embedded as-is inside a <section>.
	var slides strings.Builder
	for _, doc := range docs {
		if doc.Kind != "LayoutPage" {
			continue
		}
		slideHTML, err := renderPresentationSlide(doc, presCfg.Format, rc)
		if err != nil {
			return PresentationResult{}, diags, fmt.Errorf("render presentation slide %s: %w", doc.Name, err)
		}
		slides.WriteString(slideHTML)
		slides.WriteByte('\n')
	}

	var contextBody strings.Builder
	for _, seg := range segments {
		contextBody.WriteString(seg)
		contextBody.WriteByte('\n')
	}

	fontMarkup := renderFontLinks(fontAssets)
	headMarkup := fontMarkup + extraHeadMarkup

	if presCfg.CustomCSS != "" {
		headMarkup += fmt.Sprintf("\n  <link rel='stylesheet' href='/assets/%s'>", html.EscapeString(presCfg.CustomCSS))
		localAssets = append(localAssets, LocalAsset{
			URLPath:   "/assets/" + presCfg.CustomCSS,
			FilePath:  presCfg.CustomCSS,
			MediaType: "text/css",
		})
	}

	revealConfig := buildRevealConfig(presCfg)

	// Reveal.js always from jsdelivr; template engine CDN depends on mode.
	standalone := opts != nil && opts.Standalone
	engineCDN := cdnBaseURLServer
	if standalone {
		engineCDN = cdnBaseURLBinoRemote
	}

	markup := fmt.Sprintf(presentationTemplate,
		html.EscapeString(locale),
		engineCDN, engineVersion, engineCDN, engineVersion,
		revealJSCDN, revealJSCDN,
		html.EscapeString(presCfg.Theme),
		headMarkup,
		html.EscapeString(locale),
		contextBody.String(),
		slides.String(),
		revealJSCDN,
		revealConfig,
	)

	return PresentationResult{HTML: []byte(markup), LocalAssets: localAssets, EmittedData: emitted}, diags, nil
}

// GeneratePresentationFrameAndContext produces a two-phase render for preview mode.
// The frame loads the template engine and Reveal.js with a placeholder.
// The context contains the full slides content and is delivered via SSE.
func GeneratePresentationFrameAndContext(ctx context.Context, docs []config.Document, datasetResults []dataset.Result, artifact config.Artifact, presCfg config.PresentationConfig, existingDiags []datasource.Diagnostic, constraintCtx *spec.ConstraintContext, engineVersion string, allDocs []config.Document, pluginOpts *PluginOptions) (PresentationFrameResult, []datasource.Diagnostic, error) {
	locale := artifact.Spec.Language
	if locale == "" {
		locale = defaultLocale
	}

	var collectOpts *datasource.CollectOptions
	var extraHeadMarkup string
	var pluginRenderer PluginComponentRenderer
	var dataMode, dataBaseURL string
	if pluginOpts != nil {
		collectOpts = pluginOpts.CollectOptions
		extraHeadMarkup = pluginOpts.ExtraHeadMarkup
		pluginRenderer = pluginOpts.ComponentRenderer
		dataMode = pluginOpts.DataMode
		dataBaseURL = pluginOpts.DataBaseURL
	}

	sources, diags, err := datasource.Collect(ctx, docs, collectOpts)
	if err != nil {
		return PresentationFrameResult{}, append(existingDiags, diags...), fmt.Errorf("render presentation: collect datasources: %w", err)
	}
	diags = append(existingDiags, diags...)

	internationalizations, err := collectInternationalizations(docs)
	if err != nil {
		return PresentationFrameResult{}, diags, err
	}
	componentStyles, err := collectComponentStyles(docs)
	if err != nil {
		return PresentationFrameResult{}, diags, err
	}
	fontAssets, assetComponents, localAssets, err := collectAssets(docs)
	if err != nil {
		return PresentationFrameResult{}, diags, err
	}

	scalingGroups, err := collectScalingGroups(docs)
	if err != nil {
		return PresentationFrameResult{}, diags, err
	}

	segments := renderInternationalizations(internationalizations)
	if renderedStyles := renderComponentStyles(componentStyles); len(renderedStyles) > 0 {
		segments = append(segments, renderedStyles...)
	}
	if renderedAssets := renderAssetComponents(assetComponents); len(renderedAssets) > 0 {
		segments = append(segments, renderedAssets...)
	}
	if sg := renderScalingGroups(scalingGroups); len(sg) > 0 {
		segments = append(segments, sg...)
	}
	referencedSources := collectReferencedDatasources(docs, allDocs)
	sources = filterDatasourcesByRefs(sources, referencedSources)
	var emitted []EmittedData
	if ds, em := renderDatasources(sources, dataMode, dataBaseURL); len(ds) > 0 {
		segments = append(segments, ds...)
		emitted = append(emitted, em...)
	}
	if ds, em := renderDatasets(datasetResults, dataMode, dataBaseURL); len(ds) > 0 {
		segments = append(segments, ds...)
		emitted = append(emitted, em...)
	}

	assetURLMap := make(map[string]string, len(assetComponents))
	for _, ac := range assetComponents {
		assetURLMap[ac.name] = ac.value
	}
	rc := newRenderCtx(ctx, docs, constraintCtx, allDocs, assetURLMap, pluginRenderer, "preview")

	var slides strings.Builder
	for _, doc := range docs {
		if doc.Kind != "LayoutPage" {
			continue
		}
		slideHTML, err := renderPresentationSlide(doc, presCfg.Format, rc)
		if err != nil {
			return PresentationFrameResult{}, diags, fmt.Errorf("render presentation slide %s: %w", doc.Name, err)
		}
		slides.WriteString(slideHTML)
		slides.WriteByte('\n')
	}

	var contextBody strings.Builder
	for _, seg := range segments {
		contextBody.WriteString(seg)
		contextBody.WriteByte('\n')
	}

	fontMarkup := renderFontLinks(fontAssets)
	headMarkup := fontMarkup + extraHeadMarkup
	revealConfig := buildRevealConfig(presCfg)
	engineCDN := cdnBaseURLServer

	// Frame: lightweight shell with Reveal.js + template engine
	frameMarkup := fmt.Sprintf(presentationFrameTemplate,
		html.EscapeString(locale),
		engineCDN, engineVersion, engineCDN, engineVersion,
		revealJSCDN, revealJSCDN,
		html.EscapeString(presCfg.Theme),
		headMarkup,
		html.EscapeString(locale),
		revealJSCDN,
		revealConfig,
	)

	// Context: bn-context block with slides for SSE delivery
	contextMarkup := fmt.Sprintf(presentationContextTemplate,
		html.EscapeString(locale),
		contextBody.String(),
		slides.String(),
	)

	return PresentationFrameResult{
		FrameHTML:   []byte(frameMarkup),
		ContextHTML: []byte(contextMarkup),
		LocalAssets: localAssets,
		EmittedData: emitted,
	}, diags, nil
}

// renderPresentationSlide renders a LayoutPage inside a Reveal.js <section>.
// The page is rendered exactly as in the report pipeline via renderLayoutContainer,
// preserving all layout, styling, and component behavior.
func renderPresentationSlide(doc config.Document, defaultFormat string, rc *renderCtx) (string, error) {
	var payload struct {
		Spec layoutPageSpec `json:"spec"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		return "", err
	}

	// Apply format defaults
	if payload.Spec.PageFormat == "" {
		if defaultFormat != "" {
			payload.Spec.PageFormat = defaultFormat
		} else {
			payload.Spec.PageFormat = "hd"
		}
	}
	if payload.Spec.PageOrientation == "" {
		payload.Spec.PageOrientation = "landscape"
	}

	slideAttrs := extractSlideAttrs(doc.Labels)
	notes := doc.Labels["presentation.slide.notes"]

	var b strings.Builder
	b.WriteString("      <section")
	writeSlideAttrs(&b, slideAttrs)
	b.WriteString(">\n")

	// Embed the full bn-layout-page as-is
	containerHTML, err := renderLayoutContainer("bn-layout-page", payload.Spec, doc.Name, rc)
	if err != nil {
		return "", err
	}
	b.WriteString("        ")
	b.WriteString(containerHTML)
	b.WriteByte('\n')

	if notes != "" {
		b.WriteString("        <aside class='notes'>")
		b.WriteString(html.EscapeString(notes))
		b.WriteString("</aside>\n")
	}

	b.WriteString("      </section>")
	return b.String(), nil
}

// writeSlideAttrs writes Reveal.js data-* attributes to a string builder.
func writeSlideAttrs(b *strings.Builder, attrs map[string]string) {
	for attr, val := range attrs {
		b.WriteByte(' ')
		b.WriteString(attr)
		if val != "" {
			b.WriteString("='")
			b.WriteString(html.EscapeString(val))
			b.WriteString("'")
		}
	}
}

// extractSlideAttrs extracts Reveal.js data-* attributes from presentation.slide.* labels on a document.
func extractSlideAttrs(labels map[string]string) map[string]string {
	attrs := make(map[string]string)
	for key, val := range labels {
		if !strings.HasPrefix(key, "presentation.slide.") {
			continue
		}
		slideKey := strings.TrimPrefix(key, "presentation.slide.")
		switch slideKey {
		case "transition":
			attrs["data-transition"] = val
		case "transitionSpeed":
			attrs["data-transition-speed"] = val
		case "backgroundColor":
			attrs["data-background-color"] = val
		case "backgroundImage":
			attrs["data-background-image"] = val
		case "backgroundVideo":
			attrs["data-background-video"] = val
		case "backgroundSize":
			attrs["data-background-size"] = val
		case "backgroundOpacity":
			attrs["data-background-opacity"] = val
		case "autoAnimate":
			if val == "true" {
				attrs["data-auto-animate"] = ""
			}
		case "autoAnimateId":
			attrs["data-auto-animate-id"] = val
		case "visibility":
			attrs["data-visibility"] = val
			// notes handled separately
		}
	}
	return attrs
}

// buildRevealConfig generates the JSON configuration object for Reveal.initialize().
func buildRevealConfig(cfg config.PresentationConfig) string {
	revealCfg := map[string]any{
		"transition":      cfg.Transition,
		"transitionSpeed": cfg.TransitionSpeed,
		"autoSlide":       cfg.AutoSlide,
		"loop":            cfg.Loop,
		"controls":        cfg.Controls,
		"progress":        cfg.Progress,
		"slideNumber":     false,
		"hash":            cfg.Hash,
		"margin":          0,
		"center":          false,
	}

	// Map format to pixel dimensions for Reveal.js
	w, h := formatToDimensions(cfg.Format)
	revealCfg["width"] = w
	revealCfg["height"] = h

	data, err := json.Marshal(revealCfg)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// formatToDimensions maps a bino pageFormat string to pixel width and height.
func formatToDimensions(format string) (width int, height int) {
	switch strings.ToLower(format) {
	case "xga":
		return 1024, 768
	case "hd":
		return 1280, 720
	case "full-hd":
		return 1920, 1080
	case "4k":
		return 3840, 2160
	case "a4":
		return 1123, 794
	case "a3":
		return 1587, 1123
	case "letter":
		return 1100, 850
	default:
		return 1280, 720 // default to HD
	}
}
