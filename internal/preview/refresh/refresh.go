// Package refresh implements the preview server's incremental content
// rebuilder: it loads manifests, renders affected artefacts (full or
// selective, driven by the dependency graph), and pushes updated content to
// the preview HTTP server. The CLI wires it to the file watcher and the
// debounce loop; this package owns the render/broadcast logic.
package refresh

import (
	"context"
	"fmt"
	"path/filepath"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/preview/bootstatus"
	"bino.bi/bino/internal/preview/explorer"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	reportgraph "bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/pkg/duckdb"
)

// Request carries one debounce input from the file watcher.
// Files holds the absolute paths that changed; nil signals a full rebuild
// (e.g. initial load, or --incremental=false). The debounce loop merges
// multiple requests into a single call to Run.
type Request struct {
	Reason string
	Files  []string
}

// State carries cached output from the previous refresh so selective rebuilds
// can re-broadcast unchanged routes without re-rendering. The state is
// mutated under the caller's refresh mutex; no extra synchronization is
// needed.
type State struct {
	allPagesFrameHTML   []byte
	allPagesContextHTML []byte
	allPagesAssets      []httpserver.LocalAsset

	// perArtefactAssets keys are route paths ("/name", "/doc/name") so the
	// asset union can be rebuilt across full and selective refreshes.
	perArtefactAssets map[string][]httpserver.LocalAsset

	// lastDocs is the most recent successfully-loaded manifest set. Replaced
	// (never mutated) on every refresh, so a goroutine that grabbed the
	// slice header earlier can keep using it without holding the refresh
	// mutex. The boot's explorer-init goroutine reads this so a
	// freshly-created explorer session can be populated even when it
	// finishes initializing after the first refresh.
	lastDocs []config.Document

	// artefacts and documentArtefacts are snapshots used by the
	// /__embedding/{name} handler to find an artefact by name and render
	// it as standalone build-equivalent HTML.
	artefacts         []config.Artifact
	documentArtefacts []config.DocumentArtefact

	// embeddingCache memoizes rendered embedding HTML keyed by artefact
	// name. Reset to nil at the start of every refresh.
	embeddingCache map[string][]byte

	// liveOverrides maps absolute, filepath.Clean'd file paths to unsaved
	// editor-buffer content. While non-empty, EmbedByName renders the
	// requested component from a fresh overlaid load (reflecting the buffer)
	// and bypasses embeddingCache entirely, so the previewed component tracks
	// the editor without a disk write or a full report refresh. Guarded by the
	// refresh mutex like the rest of State.
	liveOverrides map[string]string
}

// NewState creates an empty refresh state ready for the first refresh.
func NewState() *State {
	return &State{perArtefactAssets: make(map[string][]httpserver.LocalAsset)}
}

// LastDocs returns the most recent successfully-loaded manifest set. Callers
// must hold the same mutex that guards Run, but may keep using the returned
// slice after releasing it — refreshes replace the slice, never mutate it.
func (s *State) LastDocs() []config.Document {
	return s.lastDocs
}

// SetLiveOverride records unsaved editor-buffer content for a file so the next
// EmbedByName renders from the buffer instead of disk. The file key is
// normalized to an absolute clean path. Callers must hold the refresh mutex.
func (s *State) SetLiveOverride(file, content string) {
	if s.liveOverrides == nil {
		s.liveOverrides = make(map[string]string)
	}
	s.liveOverrides[liveOverrideKey(file)] = content
}

// ClearLiveOverride drops the buffer override for a single file (e.g. on save,
// when disk becomes authoritative again). Callers must hold the refresh mutex.
func (s *State) ClearLiveOverride(file string) {
	if s.liveOverrides == nil {
		return
	}
	delete(s.liveOverrides, liveOverrideKey(file))
}

// ClearLiveOverrides drops every buffer override (e.g. when the embedded
// preview closes). Callers must hold the refresh mutex.
func (s *State) ClearLiveOverrides() {
	s.liveOverrides = nil
}

// liveOverridesSnapshot returns a copy of the current override map for use by a
// render that runs without holding the refresh mutex. Returns nil when there
// are no overrides. Callers must hold the refresh mutex while calling.
func (s *State) liveOverridesSnapshot() map[string]string {
	if len(s.liveOverrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.liveOverrides))
	for k, v := range s.liveOverrides {
		out[k] = v
	}
	return out
}

// liveOverrideKey normalizes a file path to the absolute clean form used as the
// override map key, matching config.LoadOptions.Overlay's key convention.
func liveOverrideKey(file string) string {
	if abs, err := filepath.Abs(file); err == nil {
		return abs
	}
	return filepath.Clean(file)
}

// MergeRequests collapses a debounce window into a single (reason, files)
// pair. If any input had nil files, the result is nil (full rebuild) —
// mixing partial signals with a full-rebuild signal must not lose
// information.
func MergeRequests(reqs []Request) (reason string, files []string) {
	if len(reqs) == 0 {
		return "unknown", nil
	}
	reasons := make([]string, 0, len(reqs))
	fullRebuild := false
	for _, r := range reqs {
		reasons = append(reasons, r.Reason)
		if r.Files == nil {
			fullRebuild = true
			continue
		}
		files = append(files, r.Files...)
	}
	if fullRebuild {
		return coalesceReasons(reasons), nil
	}
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, f := range files {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return coalesceReasons(reasons), out
}

// Config holds configuration for a preview content refresh.
type Config struct {
	Logger                   logx.Logger
	Workdir                  string
	EnableLint               bool
	EngineVersion            string
	QueryLogger              func(string)
	DataValidationMode       dataset.DataValidationMode
	DataValidationSampleSize int
	HookRunner               *hooks.Runner
	HookEnv                  hooks.HookEnv

	// CollectedDirs, when non-nil, receives directories visited during LoadDir.
	// This is set for the initial refresh and cleared afterwards so subsequent
	// refreshes skip the overhead of collecting directories.
	CollectedDirs *[]string

	// Session is a shared DuckDB session reused across refreshes.
	// Extensions are loaded once; views are re-registered on each refresh.
	Session *duckdb.Session

	// KindProvider supplies plugin-registered kinds for document validation.
	KindProvider config.KindProvider

	// PluginOptions carries plugin integration state. May be nil.
	PluginOptions *render.PluginOptions
	// PostRenderHTMLHook is called after HTML generation. May be nil.
	PostRenderHTMLHook func(ctx context.Context, html []byte) ([]byte, error)
	// PostDatasetHook is called after dataset execution. May be nil.
	PostDatasetHook func(ctx context.Context, datasets []pipeline.DatasetPayload) error
	// PluginLinters runs plugin lint rules alongside built-in rules. May be nil.
	PluginLinters lint.PluginLinterRegistry
	// HostService is the shared BinoHost server for updating documents. May be nil.
	HostService *plugin.BinoHostServer
	// Reporter receives phase events for the CLI spinner and the loading-page
	// SSE stream. May be nil for callers that do not surface progress (tests).
	Reporter bootstatus.Reporter

	// Include/Exclude restrict the artefact set rendered each refresh.
	// Empty Include means "render all". Applied on every refresh so the
	// filter remains stable across manifest changes.
	Include []string
	Exclude []string
}

// logLintFindings runs the lint rules over the loaded manifests and logs every
// finding the project's [lint] table keeps, at the severity that table gives
// it. A rule the project raised to "error" must not scroll by as a warning,
// and one lowered to "info" must not shout; without a [lint] table every
// finding logs as a warning exactly as before.
func logLintFindings(ctx context.Context, logger logx.Logger, watchDir string, lintDocs []lint.Document, pluginLinters lint.PluginLinterRegistry) {
	runner := lint.NewProjectRunner(watchDir)
	findings := runner.Run(ctx, lintDocs)
	if pluginLinters != nil {
		findings = append(findings, lint.RunPluginLinters(ctx, lintDocs, pluginLinters)...)
	}
	findings = runner.Apply(findings)
	for _, f := range findings {
		relPath := pathutil.RelPath(watchDir, f.File)
		loc := relPath
		if f.DocIdx > 0 {
			loc = fmt.Sprintf("%s #%d", relPath, f.DocIdx)
		}
		line := fmt.Sprintf("[%s] %s: %s", f.RuleID, loc, f.Message)
		switch runner.SeverityOverride(f.RuleID) {
		case "error":
			logger.Errorf("%s", line)
		case "info":
			logger.Infof("%s", line)
		default:
			logger.Warnf("%s", line)
		}
	}
}

// Run loads manifests, renders affected artifacts, and updates the preview
// server. When changed is nil, every artefact is re-rendered (full rebuild).
// When changed lists file paths and every path maps to a node in the
// dependency graph, only the artefacts that transitively depend on those
// files are re-rendered; unaffected routes keep their cached content. The
// returned slice lists every route that received a fresh content broadcast
// so the caller can forward it to BroadcastRefreshDone — clients viewing a
// path not in the slice know their view was not part of this refresh
// (failure or simply unaffected).
func Run(ctx context.Context, reason string, changed []string, server *httpserver.Server, explorerSession *explorer.Session, cfg *Config, state *State) ([]string, error) { //nolint:gocognit,funlen // grandfathered complexity — refactor before extending
	logger := cfg.Logger
	watchDir := cfg.Workdir
	report := cfg.Reporter
	if report == nil {
		report = bootstatus.Nop()
	}

	refreshHookEnv := cfg.HookEnv
	refreshHookEnv.RefreshReason = reason
	if err := cfg.HookRunner.Run(ctx, "pre-refresh", refreshHookEnv); err != nil {
		logger.Errorf("pre-refresh hook failed: %v", err)
		return nil, nil
	}

	report.Begin(bootstatus.PhaseManifests, fmt.Sprintf("Loading manifests (%s)", reason))
	loadOpts := config.LoadOptions{CollectedDirs: cfg.CollectedDirs, KindProvider: cfg.KindProvider}
	docs, err := config.LoadDirWithOptions(ctx, watchDir, loadOpts)
	if err != nil {
		report.Fail(bootstatus.PhaseManifests, err)
		logger.Errorf("Render failed (%s): %v", reason, err)
		return nil, err
	}
	report.End(bootstatus.PhaseManifests)
	// Snapshot for late-arriving consumers (e.g. the explorer init goroutine
	// when it finishes after the first refresh).
	state.lastDocs = docs

	if cfg.HostService != nil {
		cfg.HostService.SetDocuments(plugin.DocumentsFromConfig(docs))
	}

	for _, m := range config.CollectMissingEnvVars(docs) {
		logger.Warnf("unresolved environment variable %s in %s", m.VarName, m.File)
	}

	if explorerSession != nil {
		if err := explorerSession.Refresh(ctx, docs); err != nil {
			logger.Warnf("Explorer refresh: %v", err)
		}
	}

	if cfg.EnableLint {
		logLintFindings(ctx, logger, watchDir, lint.DocumentsFromConfig(docs), cfg.PluginLinters)
	}

	artifacts, err := config.CollectArtefacts(docs)
	if err != nil {
		logger.Errorf("Artifact scan failed (%s): %v", reason, err)
		return nil, err
	}
	pipeline.LogArtefactWarnings(logger, artifacts)

	documentArtefacts, err := config.CollectDocumentArtefacts(docs)
	if err != nil {
		logger.Errorf("DocumentArtefact scan failed (%s): %v", reason, err)
		return nil, err
	}
	pipeline.LogDocumentArtefactWarnings(logger, documentArtefacts)

	// Apply --artefact / --exclude-artefact. Validation is best-effort here:
	// if a name disappears mid-session (manifest renamed/removed) we warn
	// rather than kill the live-reload loop.
	if len(cfg.Include) > 0 || len(cfg.Exclude) > 0 {
		if err := pipeline.ValidateAllArtefactNames(artifacts, nil, documentArtefacts, cfg.Include); err != nil {
			logger.Warnf("artefact filter: %v", err)
		}
		filterOpts := pipeline.FilterOptions{Include: cfg.Include, Exclude: cfg.Exclude}
		artifacts = pipeline.FilterArtefacts(artifacts, filterOpts)
		documentArtefacts = pipeline.FilterDocumentArtefacts(documentArtefacts, filterOpts)
	}

	// Snapshot for the /__embedding/{name} handler and invalidate any
	// previously cached embedding renders. Refreshes hold the refresh mutex,
	// so the handler observes either the pre-refresh or post-refresh snapshot
	// atomically.
	state.artefacts = artifacts
	state.documentArtefacts = documentArtefacts
	state.embeddingCache = nil

	artefactInfos := make([]previewArtefactInfo, 0, len(artifacts)+len(documentArtefacts))
	for _, art := range artifacts {
		artefactInfos = append(artefactInfos, previewArtefactInfo{
			Name:   art.Document.Name,
			Title:  art.Spec.Title,
			Format: art.Spec.Format,
		})
	}
	for _, docArt := range documentArtefacts {
		artefactInfos = append(artefactInfos, previewArtefactInfo{
			Name:   docArt.Document.Name,
			Title:  docArt.Spec.Title,
			Format: docArt.Spec.Format,
			IsDoc:  true,
		})
	}

	documentInfos := make([]previewDocumentInfo, 0, len(docs))
	for _, doc := range docs {
		var cs []string
		for _, c := range doc.Constraints {
			cs = append(cs, formatConstraint(c))
		}
		documentInfos = append(documentInfos, previewDocumentInfo{
			Kind:        doc.Kind,
			Name:        doc.Name,
			File:        pathutil.RelPath(watchDir, doc.File),
			Labels:      doc.Labels,
			Constraints: cs,
		})
	}

	report.Begin(bootstatus.PhaseGraph, "Building dependency graph")
	g, graphErr := reportgraph.Build(ctx, docs)
	if graphErr != nil {
		logger.Warnf("Graph build skipped: %v", graphErr)
	}
	report.End(bootstatus.PhaseGraph)

	// Decide selective vs full. Selective requires: changed != nil, the graph
	// built successfully, and every changed file maps to at least one graph
	// node. Anything else falls back to full rebuild — safe behavior for
	// new files, deletions, .bnignore changes, plugin assets, etc. Note: on
	// the first refresh state.allPagesFrameHTML is empty, so even if changed
	// is non-nil we must do a full rebuild.
	selective := false
	var affectedReports map[string]struct{}
	var affectedDocArts map[string]struct{}
	renderAllPages := true
	if changed != nil && g != nil && len(state.allPagesFrameHTML) > 0 {
		fileSet := make(map[string]struct{}, len(changed))
		for _, f := range changed {
			fileSet[f] = struct{}{}
		}
		seeds := g.NodesByFile(fileSet)
		seenFiles := make(map[string]struct{}, len(seeds))
		for _, n := range seeds {
			seenFiles[n.File] = struct{}{}
		}
		allMapped := true
		for f := range fileSet {
			if _, ok := seenFiles[f]; !ok {
				allMapped = false
				break
			}
		}
		if allMapped && len(seeds) > 0 {
			reports, dArts := g.AffectedArtefacts(seeds)
			affectedReports = make(map[string]struct{}, len(reports))
			for _, n := range reports {
				affectedReports[n] = struct{}{}
			}
			affectedDocArts = make(map[string]struct{}, len(dArts))
			for _, n := range dArts {
				affectedDocArts[n] = struct{}{}
			}
			renderAllPages = needsAllPagesRerender(seeds)
			selective = true
			logger.Infof("Selective refresh: %d affected artefact(s), %d affected document(s), all-pages=%t", len(affectedReports), len(affectedDocArts), renderAllPages)
		}
	}

	totalRender := 0
	if renderAllPages {
		totalRender++
	}
	totalRender += len(artifacts) + len(documentArtefacts)
	if totalRender > 0 {
		report.Begin(bootstatus.PhaseRendering, fmt.Sprintf("Rendering report (%s)", reason))
	}
	rendered := 0

	// Render "All Pages" if needed. Cache the frame and context HTML so we
	// can reuse them on selective refreshes that don't touch the layout.
	if renderAllPages {
		report.Progress(rendered, totalRender, "All Pages")
		allPagesResult, rerr := pipeline.RenderHTMLFrameAndContext(ctx, docs, pipeline.RenderOptions{
			Workdir:                  watchDir,
			Language:                 "de",
			Mode:                     pipeline.RenderModePreview,
			EngineVersion:            cfg.EngineVersion,
			QueryLogger:              cfg.QueryLogger,
			DataValidation:           cfg.DataValidationMode,
			DataValidationSampleSize: cfg.DataValidationSampleSize,
			ContinueOnQueryError:     true,
			Session:                  cfg.Session,
			PluginOptions:            cfg.PluginOptions,
			PostRenderHTMLHook:       cfg.PostRenderHTMLHook,
			PostDatasetHook:          cfg.PostDatasetHook,
		})
		if rerr != nil {
			policy := pipeline.ClassifyInvalidLayout(rerr, pipeline.RenderModePreview)
			if policy.IsInvalidRoot {
				logger.Errorf("Render blocked (%s): %s", reason, policy.Message)
				setErrorPage(server, policy.Message, policy.Hint)
				return nil, nil
			}
			logger.Errorf("Render failed (%s): %v", reason, rerr)
			return nil, rerr
		}
		pipeline.LogDiagnostics(logger.Channel("datasource"), allPagesResult.Diagnostics)
		pipeline.RegisterEmittedData(server, allPagesResult.EmittedData)
		allPagesFrameHTML := withPreviewHeader(withPreviewStyles(allPagesResult.FrameHTML), artefactInfos, documentInfos, "/", nil)
		pageMeta := buildPageMetadata(docs, artifacts)
		allPagesContextHTML := withPreviewPageMetadata(withPreviewContextStyles(allPagesResult.ContextHTML), pageMeta)
		state.allPagesFrameHTML = allPagesFrameHTML
		state.allPagesContextHTML = allPagesContextHTML
		state.allPagesAssets = pipeline.ConvertLocalAssets(allPagesResult.LocalAssets)
	}

	routeMap := make(map[string]httpserver.ContentFunc, len(artifacts)+len(documentArtefacts)+1)
	broadcastPaths := make([]string, 0, len(artifacts)+len(documentArtefacts)+1)

	// Broadcast "/" early so any client viewing All Pages sees the new
	// content while we render individual artefacts.
	if renderAllPages {
		server.BroadcastContent("/", state.allPagesContextHTML)
		broadcastPaths = append(broadcastPaths, "/")
		rendered++
		report.Progress(rendered, totalRender, "All Pages")
	}

	// On a full rebuild, drop stale per-artefact assets so deleted artefacts
	// stop leaking into the asset union. Selective rebuilds preserve them
	// because the artefact set is unchanged (deletions fall back to full).
	if !selective {
		for k := range state.perArtefactAssets {
			delete(state.perArtefactAssets, k)
		}
	}

	// Render ReportArtefacts. In selective mode skip those not in the
	// affected set; their cached route entry stays via UpdateContentRoutes.
	for _, art := range artifacts {
		artPath := "/" + art.Document.Name
		if selective {
			if _, ok := affectedReports[art.Document.Name]; !ok {
				continue
			}
		}
		report.Progress(rendered, totalRender, art.Document.Name)
		renderResult, rerr := pipeline.RenderArtefactFrameAndContextWithOptions(ctx, watchDir, docs, art, pipeline.FrameRenderOptions{
			QueryLogger:              cfg.QueryLogger,
			EngineVersion:            cfg.EngineVersion,
			DataValidation:           cfg.DataValidationMode,
			DataValidationSampleSize: cfg.DataValidationSampleSize,
			ContinueOnQueryError:     true,
			Session:                  cfg.Session,
			PluginOptions:            cfg.PluginOptions,
			PostRenderHTMLHook:       cfg.PostRenderHTMLHook,
			PostDatasetHook:          cfg.PostDatasetHook,
		})
		if rerr != nil {
			if pipeline.IsInvalidRootError(rerr) {
				logger.Errorf("Render blocked for artefact %s (%s): %v", art.Document.Name, reason, rerr)
				server.BroadcastRefreshError(artPath, rerr.Error())
				continue
			}
			logger.Errorf("Render failed for %s (%s): %v", art.Document.Name, reason, rerr)
			server.BroadcastRefreshError(artPath, rerr.Error())
			return nil, rerr
		}
		pipeline.LogDiagnostics(logger.Channel("datasource").Channel(art.Document.Name), renderResult.Diagnostics)
		pipeline.RegisterEmittedData(server, renderResult.EmittedData)
		var artGraph *previewGraphData
		if g != nil {
			if rootNode, ok := g.ReportArtefactByName(art.Document.Name); ok {
				artGraph = buildPreviewGraphData(g, rootNode)
			}
		}
		frameHTML := withPreviewHeader(withPreviewStyles(renderResult.FrameHTML), artefactInfos, documentInfos, artPath, artGraph)
		contextHTML := withPreviewContextStyles(renderResult.ContextHTML)
		state.perArtefactAssets[artPath] = pipeline.ConvertLocalAssets(renderResult.LocalAssets)
		routeMap[artPath] = httpserver.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
		// Broadcast immediately so the browser can fetch new context HTML
		// while later artefacts are still rendering.
		server.BroadcastContent(artPath, contextHTML)
		broadcastPaths = append(broadcastPaths, artPath)
		rendered++
		report.Progress(rendered, totalRender, art.Document.Name)
	}

	for _, docArt := range documentArtefacts {
		docPath := "/doc/" + docArt.Document.Name
		if selective {
			if _, ok := affectedDocArts[docArt.Document.Name]; !ok {
				continue
			}
		}
		report.Progress(rendered, totalRender, docArt.Document.Name)
		renderResult, rerr := pipeline.RenderDocumentArtefactHTML(ctx, watchDir, docArt, pipeline.DocumentArtefactRenderOptions{
			EngineVersion:        cfg.EngineVersion,
			Session:              cfg.Session,
			ContinueOnQueryError: true,
			PluginOptions:        cfg.PluginOptions,
			KindProvider:         cfg.KindProvider,
			PostRenderHTMLHook:   cfg.PostRenderHTMLHook,
			PostDatasetHook:      cfg.PostDatasetHook,
		})
		if rerr != nil {
			logger.Errorf("Render failed for DocumentArtefact %s (%s): %v", docArt.Document.Name, reason, rerr)
			server.BroadcastRefreshError(docPath, rerr.Error())
			continue
		}
		pipeline.RegisterEmittedData(server, renderResult.EmittedData)
		state.perArtefactAssets[docPath] = pipeline.ConvertLocalAssets(renderResult.LocalAssets)
		var docGraph *previewGraphData
		if g != nil {
			if rootNode, ok := g.DocumentArtefactByName(docArt.Document.Name); ok {
				docGraph = buildPreviewGraphData(g, rootNode)
			}
		}
		styledHTML := withPreviewStyles(withDocumentPageWidth(renderResult.HTML, docArt.Spec.Format, docArt.Spec.Orientation))
		frameHTML := withPreviewHeader(styledHTML, artefactInfos, documentInfos, docPath, docGraph)
		routeMap[docPath] = httpserver.StaticContent(append([]byte(nil), frameHTML...), "text/html; charset=utf-8")
		// Broadcast like the report loop does: swapContext extracts the
		// <bn-context> from the full page, so no frame/context split needed.
		server.BroadcastContent(docPath, styledHTML)
		broadcastPaths = append(broadcastPaths, docPath)
		rendered++
		report.Progress(rendered, totalRender, docArt.Document.Name)
	}

	// Register lazy presentation routes. On a full rebuild we register all
	// of them (the previous closures captured stale docs). On a selective
	// rebuild we only re-register the affected ones; unaffected /pres/X
	// routes still serve cached output keyed on docs that were correct at
	// last full rebuild — fine because nothing in their dependency tree
	// changed.
	for _, art := range artifacts {
		if selective {
			if _, ok := affectedReports[art.Document.Name]; !ok {
				continue
			}
		}
		presPath := "/pres/" + art.Document.Name
		routeMap[presPath] = lazyPresentationContent(watchDir, docs, art, cfg, server, presPath)
	}

	// Rebuild the asset union and push it to the server. SetLocalAssets
	// replaces the table, so we always send the full union — selective
	// refreshes preserve unchanged entries via the per-artefact cache.
	allAssets := make([]httpserver.LocalAsset, 0, len(state.allPagesAssets)+len(state.perArtefactAssets)*4)
	allAssets = append(allAssets, state.allPagesAssets...)
	for _, assets := range state.perArtefactAssets {
		allAssets = append(allAssets, assets...)
	}
	server.SetLocalAssets(allAssets)
	if selective {
		server.UpdateContentRoutes(routeMap)
	} else {
		server.SetContentRoutes(routeMap)
	}
	server.SetContentFunc(httpserver.StaticContent(append([]byte(nil), state.allPagesFrameHTML...), "text/html; charset=utf-8"))
	if totalRender > 0 {
		report.End(bootstatus.PhaseRendering)
	}
	logger.Successf("Content refreshed (%s)", reason)
	return broadcastPaths, nil
}

// needsAllPagesRerender returns true when any seed node is a LayoutPage or a
// ReportArtefact — both affect what the "All Pages" view shows. Other kinds
// (DataSource, DataSet, Component, MarkdownFile, LayoutCard, DocumentArtefact)
// only change what renders inside an artefact, so the All-Pages frame stays
// valid.
func needsAllPagesRerender(seeds []*reportgraph.Node) bool {
	for _, n := range seeds {
		if n == nil {
			continue
		}
		switch n.Kind {
		case reportgraph.NodeLayoutPage, reportgraph.NodeReportArtefact:
			return true
		case reportgraph.NodeDocumentArtefact, reportgraph.NodeLayoutCard, reportgraph.NodeComponent,
			reportgraph.NodeDataSet, reportgraph.NodeDataSource, reportgraph.NodeMarkdownFile:
			// These kinds only change content within artefacts, not the
			// All-Pages frame itself.
		}
	}
	return false
}

// coalesceReasons merges multiple file-change reasons into a single human-readable string.
func coalesceReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "unknown"
	}
	if len(reasons) == 1 {
		return reasons[0]
	}
	return fmt.Sprintf("%s (+%d more)", reasons[0], len(reasons)-1)
}
