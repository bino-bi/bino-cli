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

// EmbedByName resolves a name (optionally disambiguated by kind) from the latest
// refresh snapshot and renders the matching document as standalone HTML —
// equivalent to what `bino build` feeds to Chrome. Supported kinds are
// ReportArtefact, DocumentArtefact, LayoutPage and the standalone component
// kinds; LayoutPages and components are rendered by synthesizing a one-page
// artefact. When kind is empty the lookup falls back to a fixed priority
// (ReportArtefact → DocumentArtefact → LayoutPage → component). Names are unique
// per kind, not globally, so callers that know the kind should pass it. Renders
// are memoized in state.embeddingCache keyed by "kind:name"; the cache is reset
// on every refresh.
func EmbedByName(ctx context.Context, name, kind string, mu *sync.Mutex, state *State, cfg *Config, server *httpserver.Server) ([]byte, error) {
	cacheKey := kind + ":" + name

	mu.Lock()
	if cached, ok := state.embeddingCache[cacheKey]; ok {
		mu.Unlock()
		return cached, nil
	}

	want := func(k string) bool { return kind == "" || kind == k }

	var reportArt *config.Artifact
	if want("ReportArtefact") {
		for i := range state.artefacts {
			if state.artefacts[i].Document.Name == name {
				reportArt = &state.artefacts[i]
				break
			}
		}
	}
	var docArt *config.DocumentArtefact
	if reportArt == nil && want("DocumentArtefact") {
		for i := range state.documentArtefacts {
			if state.documentArtefacts[i].Document.Name == name {
				docArt = &state.documentArtefacts[i]
				break
			}
		}
	}
	var layoutDoc, compDoc *config.Document
	if reportArt == nil && docArt == nil {
		for i := range state.lastDocs {
			d := &state.lastDocs[i]
			if d.Name != name {
				continue
			}
			if d.Kind == "LayoutPage" && want("LayoutPage") {
				layoutDoc = d
				break
			}
			if embedkinds.IsEmbeddable(d.Kind) && want(d.Kind) {
				compDoc = d
				break
			}
		}
	}

	if reportArt == nil && docArt == nil && layoutDoc == nil && compDoc == nil {
		err := embedNotFoundError(name, kind, state)
		mu.Unlock()
		return nil, err
	}

	// Release the lock for the slow render call; the captured values are copies
	// and docs are replaced (never mutated) by refreshes.
	docs := state.lastDocs
	mu.Unlock()

	var body []byte
	var emitted []render.EmittedData
	var err error
	switch {
	case reportArt != nil:
		result, e := pipeline.RenderArtefactHTML(ctx, cfg.Workdir, docs, *reportArt, embedRenderOpts(cfg))
		body, emitted, err = result.HTML, result.EmittedData, e
	case docArt != nil:
		result, e := pipeline.RenderDocumentArtefactHTML(ctx, cfg.Workdir, *docArt, pipeline.DocumentArtefactRenderOptions{
			EngineVersion:        cfg.EngineVersion,
			Session:              cfg.Session,
			ContinueOnQueryError: true,
			PluginOptions:        cfg.PluginOptions,
			KindProvider:         cfg.KindProvider,
			PostRenderHTMLHook:   cfg.PostRenderHTMLHook,
			PostDatasetHook:      cfg.PostDatasetHook,
		})
		body, emitted, err = result.HTML, result.EmittedData, e
	case layoutDoc != nil:
		result, e := pipeline.RenderArtefactHTML(ctx, cfg.Workdir, docs, syntheticPageArtefact(*layoutDoc), embedRenderOpts(cfg))
		body, emitted, err = result.HTML, result.EmittedData, e
	default: // compDoc != nil
		if _, direct := directEmbeddableComponentKinds[compDoc.Kind]; direct {
			// Leaf components render directly inside <bn-context>, no wrapping page.
			result, e := pipeline.RenderHTML(ctx, docsWithoutLayoutPages(docs), embedComponentOpts(cfg, name))
			body, emitted, err = result.HTML, result.EmittedData, e
			break
		}
		// Container components (Tree, Grid) need child resolution: wrap in a page.
		page, e := syntheticComponentPage(compDoc.Kind, name)
		if e != nil {
			err = e
			break
		}
		synthDocs := append(append([]config.Document(nil), docs...), page)
		result, e2 := pipeline.RenderArtefactHTML(ctx, cfg.Workdir, synthDocs, syntheticPageArtefact(page), embedRenderOpts(cfg))
		body, emitted, err = result.HTML, result.EmittedData, e2
	}
	if err != nil {
		return nil, fmt.Errorf("embed %q: %w", name, err)
	}

	pipeline.RegisterEmittedData(server, emitted)

	mu.Lock()
	if state.embeddingCache == nil {
		state.embeddingCache = make(map[string][]byte)
	}
	state.embeddingCache[cacheKey] = body
	mu.Unlock()
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
func embedComponentOpts(cfg *Config, name string) pipeline.RenderOptions {
	return pipeline.RenderOptions{
		Workdir:                  cfg.Workdir,
		Mode:                     pipeline.RenderModeBuild,
		Language:                 config.DefaultArtefactLanguage,
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
func syntheticPageArtefact(page config.Document) config.Artifact {
	format, orientation := pageFormatAndOrientation(page)
	return config.Artifact{
		Document: config.Document{Kind: "ReportArtefact", Name: "__embed_" + page.Name},
		Spec: config.ReportArtefactSpec{
			Format:      format,
			Orientation: orientation,
			Language:    config.DefaultArtefactLanguage,
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
