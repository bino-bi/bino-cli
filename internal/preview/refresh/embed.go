package refresh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/report/config"
	embedkinds "bino.bi/bino/internal/report/embed"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
)

// lazyContent wraps renderFn in a ContentFunc that caches the first successful
// result. Errors are returned but not cached, so a failed first render (e.g.
// with an already-canceled request context) does not pin the error for all
// subsequent requests.
func lazyContent(renderFn httpserver.ContentFunc) httpserver.ContentFunc {
	var mu sync.Mutex
	var cached bool
	var cachedBody []byte
	var cachedCT string

	return func(ctx context.Context) ([]byte, string, error) {
		mu.Lock()
		defer mu.Unlock()
		if cached {
			return cachedBody, cachedCT, nil
		}
		body, ct, err := renderFn(ctx)
		if err != nil {
			return nil, "", err
		}
		cached = true
		cachedBody = body
		cachedCT = ct
		return cachedBody, cachedCT, nil
	}
}

// lazyPresentationContent returns a ContentFunc that renders the presentation view
// on first access, caching the result for subsequent requests.
func lazyPresentationContent(workdir string, docs []config.Document, art config.Artifact, cfg *Config, server *httpserver.Server, presPath string) httpserver.ContentFunc {
	return lazyContent(func(ctx context.Context) ([]byte, string, error) {
		renderResult, err := pipeline.RenderPresentationFrameAndContext(ctx, workdir, docs, art, pipeline.PresentationArtefactRenderOptions{
			EngineVersion:            cfg.EngineVersion,
			QueryLogger:              cfg.QueryLogger,
			DataValidation:           cfg.DataValidationMode,
			DataValidationSampleSize: cfg.DataValidationSampleSize,
			ContinueOnQueryError:     true,
			PluginOptions:            cfg.PluginOptions,
			PostDatasetHook:          cfg.PostDatasetHook,
			Session:                  cfg.Session,
		})
		if err != nil {
			return nil, "", err
		}
		pipeline.RegisterEmittedData(server, renderResult.EmittedData)
		frameHTML := withPreviewStyles(renderResult.FrameHTML)
		body := append([]byte(nil), frameHTML...)
		server.BroadcastContent(presPath, renderResult.ContextHTML)
		return body, "text/html; charset=utf-8", nil
	})
}

// embedTarget holds the single resolved document to render. Exactly one field
// is non-nil; all-nil means "not found".
type embedTarget struct {
	reportArt *config.Artifact
	docArt    *config.DocumentArtefact
	layoutDoc *config.Document
	compDoc   *config.Document
}

func (t embedTarget) found() bool {
	return t.reportArt != nil || t.docArt != nil || t.layoutDoc != nil || t.compDoc != nil
}

// resolveEmbedTarget picks the single document matching (name, kind) from the
// supplied artefact/document sets, applying the fixed kind priority
// (ReportArtefact → DocumentArtefact → LayoutPage → component) when kind is
// empty. The returned pointers alias into the input slices, so callers must
// keep those slices alive for the duration of the render.
func resolveEmbedTarget(name, kind string, artefacts []config.Artifact, docArts []config.DocumentArtefact, docs []config.Document) embedTarget {
	want := func(k string) bool { return kind == "" || kind == k }

	var t embedTarget
	if want("ReportArtefact") {
		for i := range artefacts {
			if artefacts[i].Document.Name == name {
				t.reportArt = &artefacts[i]
				return t
			}
		}
	}
	if want("DocumentArtefact") {
		for i := range docArts {
			if docArts[i].Document.Name == name {
				t.docArt = &docArts[i]
				return t
			}
		}
	}
	for i := range docs {
		d := &docs[i]
		if d.Name != name {
			continue
		}
		if d.Kind == "LayoutPage" && want("LayoutPage") {
			t.layoutDoc = d
			return t
		}
		if embedkinds.IsEmbeddable(d.Kind) && want(d.Kind) {
			t.compDoc = d
			return t
		}
	}
	return t
}

// EmbedByName resolves a name (optionally disambiguated by kind) and renders the
// matching document as standalone HTML — equivalent to what `bino build` feeds
// to Chrome. Supported kinds are ReportArtefact, DocumentArtefact, LayoutPage
// and the standalone component kinds; LayoutPages and components are rendered by
// synthesizing a one-page artefact. When kind is empty the lookup falls back to
// a fixed priority (ReportArtefact → DocumentArtefact → LayoutPage → component).
// Names are unique per kind, not globally, so callers that know the kind should
// pass it.
//
// language overrides the artefact language ("de" or "en") for this render; empty
// means "use whatever the manifest says". It is what lets a caller preview the
// same component under a different Internationalization bundle.
//
// Two render sources exist:
//   - Disk path (no live overrides): resolves from the last refresh snapshot
//     (state.artefacts/documentArtefacts/lastDocs) and memoizes the rendered
//     HTML in state.embeddingCache keyed by "kind:name:language"; the cache is
//     reset on every refresh.
//   - Override path (one or more live overrides set): performs a FRESH lenient
//     load with the buffer overlay, resolves from THOSE docs, renders only the
//     requested target, and bypasses embeddingCache entirely — so the previewed
//     component reflects unsaved editor edits without a disk write or a full
//     report refresh.
func EmbedByName(ctx context.Context, name, kind, language string, mu *sync.Mutex, state *State, cfg *Config, server *httpserver.Server) ([]byte, error) {
	if err := validateEmbedLanguage(language); err != nil {
		return nil, err
	}

	mu.Lock()
	if overlay := state.liveOverridesSnapshot(); overlay != nil {
		mu.Unlock()
		return embedFromOverlay(ctx, name, kind, language, overlay, cfg, server)
	}

	// The language is part of the key: the same component renders differently
	// per locale, so omitting it would pin whichever language rendered first.
	cacheKey := kind + ":" + name + ":" + language
	if cached, ok := state.embeddingCache[cacheKey]; ok {
		mu.Unlock()
		return cached, nil
	}

	target := resolveEmbedTarget(name, kind, state.artefacts, state.documentArtefacts, state.lastDocs)
	if !target.found() {
		err := embedNotFoundError(name, kind, state)
		mu.Unlock()
		return nil, err
	}

	// Release the lock for the slow render call; the captured values are copies
	// and docs are replaced (never mutated) by refreshes.
	docs := state.lastDocs
	mu.Unlock()

	body, err := renderEmbedTarget(ctx, name, target, docs, cfg, language, server)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	if state.embeddingCache == nil {
		state.embeddingCache = make(map[string][]byte)
	}
	state.embeddingCache[cacheKey] = body
	mu.Unlock()
	return body, nil
}

// embedFromOverlay renders a single named document from a fresh lenient load
// that applies the buffer overlay, re-rendering ONLY that component. It never
// reads or writes embeddingCache and never triggers a full report refresh, so
// the preview reflects unsaved edits cheaply. Lenient loading means mid-edit
// invalid YAML degrades to "not found" rather than aborting.
func embedFromOverlay(ctx context.Context, name, kind, language string, overlay map[string]string, cfg *Config, server *httpserver.Server) ([]byte, error) {
	docs, err := config.LoadDirWithOptions(ctx, cfg.Workdir, config.LoadOptions{
		Lenient:      true,
		KindProvider: cfg.KindProvider,
		Overlay:      overlay,
	})
	if err != nil {
		return nil, fmt.Errorf("embed %q: load overlay: %w", name, err)
	}

	artefacts, err := config.CollectArtefacts(docs)
	if err != nil {
		return nil, fmt.Errorf("embed %q: collect artefacts: %w", name, err)
	}
	docArts, err := config.CollectDocumentArtefacts(docs)
	if err != nil {
		return nil, fmt.Errorf("embed %q: collect document artefacts: %w", name, err)
	}

	target := resolveEmbedTarget(name, kind, artefacts, docArts, docs)
	if !target.found() {
		if kind != "" {
			return nil, httpserver.NewHTTPError(http.StatusNotFound, fmt.Sprintf("no embeddable %s named %q", kind, name))
		}
		return nil, httpserver.NewHTTPError(http.StatusNotFound, fmt.Sprintf("no embeddable document named %q", name))
	}

	return renderEmbedTarget(ctx, name, target, docs, cfg, language, server)
}

// validateEmbedLanguage rejects anything outside the ReportArtefact.spec.language
// enum. Internationalization lookup is an exact string match with no BCP 47
// normalization, so passing e.g. "de-DE" through would silently render an
// untranslated report; a 400 is more useful than a confusing render.
func validateEmbedLanguage(language string) error {
	switch language {
	case "", "de", "en":
		return nil
	}
	return httpserver.NewHTTPError(http.StatusBadRequest,
		fmt.Sprintf("unsupported language %q; want de or en", language))
}

// renderEmbedTarget renders the resolved target against docs and registers any
// emitted data with the server. It is the shared render path for both the disk
// and overlay sources.
func renderEmbedTarget(ctx context.Context, name string, target embedTarget, docs []config.Document, cfg *Config, language string, server *httpserver.Server) ([]byte, error) {
	var body []byte
	var emitted []render.EmittedData
	var err error
	switch {
	case target.reportArt != nil:
		// resolveEmbedTarget's pointers alias into state.artefacts, so the
		// language override goes on a copy — writing through would poison the
		// shared refresh snapshot for every later render.
		art := *target.reportArt
		if language != "" {
			art.Spec.Language = language
		}
		result, e := pipeline.RenderArtefactHTML(ctx, cfg.Workdir, docs, art, embedRenderOpts(cfg))
		body, emitted, err = result.HTML, result.EmittedData, e
	case target.docArt != nil:
		// Same aliasing rule. DocumentArtefact spells the axis spec.locale.
		docArt := *target.docArt
		if language != "" {
			docArt.Spec.Locale = language
		}
		result, e := pipeline.RenderDocumentArtefactHTML(ctx, cfg.Workdir, docs, docArt, pipeline.DocumentArtefactRenderOptions{
			EngineVersion:        cfg.EngineVersion,
			Session:              cfg.Session,
			ContinueOnQueryError: true,
			PluginOptions:        cfg.PluginOptions,
			PostRenderHTMLHook:   cfg.PostRenderHTMLHook,
			PostDatasetHook:      cfg.PostDatasetHook,
		})
		body, emitted, err = result.HTML, result.EmittedData, e
	case target.layoutDoc != nil:
		result, e := pipeline.RenderArtefactHTML(ctx, cfg.Workdir, docs, syntheticPageArtefact(*target.layoutDoc, language), embedRenderOpts(cfg))
		body, emitted, err = result.HTML, result.EmittedData, e
	default: // target.compDoc != nil
		if _, direct := directEmbeddableComponentKinds[target.compDoc.Kind]; direct {
			// Leaf components render directly inside <bn-context>, no wrapping page.
			result, e := pipeline.RenderHTML(ctx, docsWithoutLayoutPages(docs), embedComponentOpts(cfg, name, language))
			body, emitted, err = result.HTML, result.EmittedData, e
			break
		}
		// Container components (Tree, Grid) need child resolution: wrap in a page.
		page, e := syntheticComponentPage(target.compDoc.Kind, name)
		if e != nil {
			err = e
			break
		}
		synthDocs := append(append([]config.Document(nil), docs...), page)
		result, e2 := pipeline.RenderArtefactHTML(ctx, cfg.Workdir, synthDocs, syntheticPageArtefact(page, language), embedRenderOpts(cfg))
		body, emitted, err = result.HTML, result.EmittedData, e2
	}
	if err != nil {
		return nil, fmt.Errorf("embed %q: %w", name, err)
	}

	pipeline.RegisterEmittedData(server, emitted)
	return body, nil
}

// directEmbeddableComponentKinds are component kinds rendered directly inside
// <bn-context> via RenderHTML's RootComponent option (no wrapping LayoutPage).
// Container kinds (Tree, Grid) are absent: they need child ref resolution and
// must be wrapped in a synthetic LayoutPage.
var directEmbeddableComponentKinds = map[string]struct{}{
	"Text":           {},
	"Table":          {},
	"ChartStructure": {},
	"ChartTime":      {},
	"ChartScatter":   {},
	"ChartBubble":    {},
	"ChartBullet":    {},
	"Image":          {},
}

// docsWithoutLayoutPages returns docs with LayoutPage documents removed so that
// RenderHTML renders only the requested RootComponent and not every page.
func docsWithoutLayoutPages(docs []config.Document) []config.Document {
	out := make([]config.Document, 0, len(docs))
	for _, d := range docs {
		if d.Kind != "LayoutPage" {
			out = append(out, d)
		}
	}
	return out
}

// embedComponentOpts builds RenderHTML options that render a single named
// component directly inside <bn-context>.
func embedComponentOpts(cfg *Config, name, language string) pipeline.RenderOptions {
	if language == "" {
		language = config.DefaultArtefactLanguage
	}
	return pipeline.RenderOptions{
		Workdir:                  cfg.Workdir,
		Mode:                     pipeline.RenderModeBuild,
		Language:                 language,
		EngineVersion:            cfg.EngineVersion,
		QueryLogger:              cfg.QueryLogger,
		DataValidation:           cfg.DataValidationMode,
		DataValidationSampleSize: cfg.DataValidationSampleSize,
		ContinueOnQueryError:     true,
		PluginOptions:            cfg.PluginOptions,
		PostRenderHTMLHook:       cfg.PostRenderHTMLHook,
		PostDatasetHook:          cfg.PostDatasetHook,
		RootComponent:            name,
	}
}

// embedRenderOpts builds the shared render options used by every embedding render.
func embedRenderOpts(cfg *Config) pipeline.RenderArtefactOptions {
	return pipeline.RenderArtefactOptions{
		EngineVersion:            cfg.EngineVersion,
		QueryLogger:              cfg.QueryLogger,
		DataValidation:           cfg.DataValidationMode,
		DataValidationSampleSize: cfg.DataValidationSampleSize,
		ContinueOnQueryError:     true,
		PluginOptions:            cfg.PluginOptions,
		PostRenderHTMLHook:       cfg.PostRenderHTMLHook,
		PostDatasetHook:          cfg.PostDatasetHook,
	}
}

// syntheticPageArtefact builds an in-memory ReportArtefact that renders exactly
// the given LayoutPage. The artefact format/orientation are taken from the page's
// own pageFormat/pageOrientation (falling back to defaults) so the page is not
// dropped by the artefact-vs-page format filter in renderLayoutPage.
// An empty language falls back to the artefact default.
func syntheticPageArtefact(page config.Document, language string) config.Artifact {
	format, orientation := pageFormatAndOrientation(page)
	if language == "" {
		language = config.DefaultArtefactLanguage
	}
	return config.Artifact{
		Document: config.Document{Kind: "ReportArtefact", Name: "__embed_" + page.Name},
		Spec: config.ReportArtefactSpec{
			Format:      format,
			Orientation: orientation,
			Language:    language,
			LayoutPages: config.LayoutPagesOrRefs{{Page: page.Name}},
		},
	}
}

// pageFormatAndOrientation extracts spec.pageFormat/spec.pageOrientation from a
// LayoutPage document, falling back to the artefact defaults when unset.
func pageFormatAndOrientation(page config.Document) (format, orientation string) {
	format, orientation = config.DefaultArtefactFormat, config.DefaultArtefactOrientation
	var payload struct {
		Spec struct {
			PageFormat      string `json:"pageFormat"`
			PageOrientation string `json:"pageOrientation"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(page.Raw, &payload); err == nil {
		if payload.Spec.PageFormat != "" {
			format = payload.Spec.PageFormat
		}
		if payload.Spec.PageOrientation != "" {
			orientation = payload.Spec.PageOrientation
		}
	}
	return format, orientation
}

// syntheticComponentPage wraps a standalone component (referenced by kind+name)
// in a single-child LayoutPage so it can be rendered on its own.
func syntheticComponentPage(compKind, compName string) (config.Document, error) {
	pageName := "__embed_page_" + compName
	raw, err := json.Marshal(map[string]any{
		"apiVersion": "bino.bi/v1alpha1",
		"kind":       "LayoutPage",
		"metadata":   map[string]any{"name": pageName},
		"spec": map[string]any{
			"children": []any{
				map[string]any{
					"kind":     compKind,
					"metadata": map[string]any{"name": compName},
					"ref":      compName,
				},
			},
		},
	})
	if err != nil {
		return config.Document{}, err
	}
	return config.Document{Kind: "LayoutPage", Name: pageName, Raw: raw, File: "synthetic"}, nil
}

// embedNotFoundError builds the error returned when no embeddable document
// matches. It must be called while holding the refresh mutex.
func embedNotFoundError(name, kind string, state *State) error {
	if kind != "" {
		return httpserver.NewHTTPError(http.StatusNotFound, fmt.Sprintf("no embeddable %s named %q", kind, name))
	}
	// The name exists, but under a kind that cannot be rendered standalone.
	for i := range state.lastDocs {
		if state.lastDocs[i].Name == name {
			return httpserver.NewHTTPError(http.StatusUnprocessableEntity,
				fmt.Sprintf("%q (kind %s) cannot be embedded; only artefacts, LayoutPages and standalone components are embeddable", name, state.lastDocs[i].Kind))
		}
	}
	names := embeddableNames(state)
	if len(names) == 0 {
		return httpserver.NewHTTPError(http.StatusNotFound, fmt.Sprintf("no embeddable document named %q. No embeddable documents are registered yet.", name))
	}
	return httpserver.NewHTTPError(http.StatusNotFound, fmt.Sprintf("no embeddable document named %q. Available: %s", name, strings.Join(names, ", ")))
}

// embeddableNames lists the names of every embeddable document in the snapshot.
// It must be called while holding the refresh mutex.
func embeddableNames(state *State) []string {
	names := make([]string, 0, len(state.artefacts)+len(state.documentArtefacts))
	for _, a := range state.artefacts {
		names = append(names, a.Document.Name)
	}
	for _, a := range state.documentArtefacts {
		names = append(names, a.Document.Name)
	}
	for i := range state.lastDocs {
		d := &state.lastDocs[i]
		if d.Kind == "LayoutPage" {
			names = append(names, d.Name)
			continue
		}
		if embedkinds.IsEmbeddable(d.Kind) {
			names = append(names, d.Name)
		}
	}
	return names
}
