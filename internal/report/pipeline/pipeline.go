// Package pipeline provides shared build/preview/serve logic for manifest loading,
// artefact selection, and HTML rendering. Both CLI build and preview commands
// use these helpers to ensure consistent behavior and options.
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/pkg/duckdb"
)

// LoadResult captures the outcome of loading and validating manifests from a workdir.
type LoadResult struct {
	Workdir         string
	Documents       []config.Document
	Artefacts       []config.Artefact
	SigningProfiles map[string]config.SigningProfile
}

// LoadManifests loads and validates all manifest documents from the given workdir.
// It returns collected artefacts and signing profiles ready for further processing.
func LoadManifests(ctx context.Context, workdir string) (LoadResult, error) {
	absDir, err := ResolveWorkdir(workdir)
	if err != nil {
		return LoadResult{}, err
	}

	docs, err := config.LoadDir(ctx, absDir)
	if err != nil {
		return LoadResult{}, fmt.Errorf("load manifests: %w", err)
	}
	if len(docs) == 0 {
		return LoadResult{}, fmt.Errorf("no YAML documents found in %s", absDir)
	}

	artefacts, err := config.CollectArtefacts(docs)
	if err != nil {
		return LoadResult{}, fmt.Errorf("collect artefacts: %w", err)
	}

	signingProfiles, err := config.CollectSigningProfiles(docs)
	if err != nil {
		return LoadResult{}, fmt.Errorf("collect signing profiles: %w", err)
	}

	return LoadResult{
		Workdir:         absDir,
		Documents:       docs,
		Artefacts:       artefacts,
		SigningProfiles: signingProfiles,
	}, nil
}

// FilterOptions specifies which artefacts to include or exclude from processing.
type FilterOptions struct {
	// Include lists specific metadata.name entries to process (empty means all).
	Include []string
	// Exclude lists metadata.name entries to skip.
	Exclude []string
}

// FilterArtefacts selects artefacts based on include/exclude rules.
// If Include is non-empty, only those names are selected (in order).
// Exclude names are always removed from the result.
func FilterArtefacts(all []config.Artefact, opts FilterOptions) ([]config.Artefact, error) {
	excludeSet := make(map[string]struct{})
	for _, name := range opts.Exclude {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		excludeSet[name] = struct{}{}
	}

	if len(opts.Include) > 0 {
		index := make(map[string]config.Artefact, len(all))
		for _, art := range all {
			index[art.Document.Name] = art
		}
		seen := make(map[string]struct{})
		var filtered []config.Artefact
		for _, raw := range opts.Include {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			art, ok := index[name]
			if !ok {
				return nil, fmt.Errorf("artefact %q not found", name)
			}
			if _, blocked := excludeSet[name]; blocked {
				continue
			}
			filtered = append(filtered, art)
			seen[name] = struct{}{}
		}
		return filtered, nil
	}

	filtered := make([]config.Artefact, 0, len(all))
	for _, art := range all {
		if _, blocked := excludeSet[art.Document.Name]; blocked {
			continue
		}
		filtered = append(filtered, art)
	}
	return filtered, nil
}

// EnsureSigningProfiles verifies that all artefacts referencing a signing profile
// have that profile available in the provided map.
func EnsureSigningProfiles(artefacts []config.Artefact, profiles map[string]config.SigningProfile) error {
	for _, art := range artefacts {
		ref := strings.TrimSpace(art.Spec.SigningProfile)
		if ref == "" {
			continue
		}
		if _, ok := profiles[ref]; !ok {
			return fmt.Errorf("artefact %s references unknown SigningProfile %q", art.Document.Name, ref)
		}
	}
	return nil
}

// LogArtefactWarnings logs any warnings collected during artefact validation.
func LogArtefactWarnings(logger logx.Logger, artefacts []config.Artefact) {
	if logger == nil {
		return
	}
	for _, art := range artefacts {
		for _, warn := range art.Warnings {
			logger.Warnf(warn)
		}
	}
}

// RenderOptions configures HTML rendering for a single artefact or preview.
type RenderOptions struct {
	// Workdir is the working directory for dataset execution. Required for datasets.
	Workdir string
	// Language for internationalization (defaults to "de" if empty).
	Language string
	// Orientation for rendering (e.g., "landscape", "portrait").
	Orientation string
	// Format for page sizing (e.g., "xga", "a4").
	Format string
	// Mode indicates whether this is a build or preview render.
	Mode RenderMode
	// EngineVersion specifies the template engine version to use (e.g., "v1.2.3").
	EngineVersion string
	// QueryLogger is called for each SQL query executed. May be nil.
	QueryLogger func(query string)
	// QueryExecLogger is called for each query execution with detailed metadata. May be nil.
	QueryExecLogger duckdb.QueryExecLogger
	// EmbedOptions configures CSV embedding for build logs.
	EmbedOptions buildlog.EmbedOptions
	// ExecutionPlan tracks build execution steps. May be nil.
	ExecutionPlan *buildlog.ExecutionPlan
	// ConstraintContext provides the context for evaluating inline child constraints.
	// May be nil if no constraint filtering is needed for inline children.
	ConstraintContext *spec.ConstraintContext
	// AllDocs is the complete unfiltered document set. Used to distinguish between
	// refs that don't exist vs refs that were filtered by constraints. When provided,
	// refs that exist in AllDocs but not in the filtered docs are silently skipped.
	// Refs that don't exist in AllDocs at all will error unless marked optional.
	AllDocs []config.Document
	// DataValidation controls how data validation errors are handled.
	DataValidation dataset.DataValidationMode
	// DataValidationSampleSize limits how many rows are validated.
	DataValidationSampleSize int
}

// RenderResult captures the outcome of rendering HTML from documents.
type RenderResult struct {
	HTML        []byte
	LocalAssets []render.LocalAsset
	Diagnostics []datasource.Diagnostic
}

// FrameRenderResult captures the outcome of a two-phase (frame + context) render.
// This is used for preview mode to enable faster initial page loads.
type FrameRenderResult struct {
	// FrameHTML is the lightweight shell with template engine and placeholder.
	FrameHTML []byte
	// ContextHTML is the full bn-context block for SSE delivery.
	ContextHTML []byte
	// LocalAssets lists files that need to be served by the preview HTTP server.
	LocalAssets []render.LocalAsset
	// Diagnostics contains any warnings or errors from datasource/dataset processing.
	Diagnostics []datasource.Diagnostic
}

// RenderHTML generates HTML from the provided documents using the given options.
// This is the shared entry point for both build and preview rendering.
// If Workdir is provided, datasets will be executed and their results included.
func RenderHTML(ctx context.Context, docs []config.Document, opts RenderOptions) (RenderResult, error) {
	var datasetResults []dataset.Result
	var diags []datasource.Diagnostic

	// Execute datasets if workdir is provided
	if opts.Workdir != "" {
		// Track dataset execution step if execution plan is enabled
		var stepID string
		if opts.ExecutionPlan != nil {
			stepID = opts.ExecutionPlan.StartStep(buildlog.StepExecuteDatasets, "pipeline")
		}

		execOpts := &dataset.ExecuteOptions{
			QueryLogger:              opts.QueryLogger,
			QueryExecLogger:          opts.QueryExecLogger,
			EmbedOptions:             opts.EmbedOptions,
			DataValidation:           opts.DataValidation,
			DataValidationSampleSize: opts.DataValidationSampleSize,
		}
		results, warnings, err := dataset.Execute(ctx, opts.Workdir, docs, execOpts)

		// End dataset execution step
		if opts.ExecutionPlan != nil {
			opts.ExecutionPlan.EndStep(stepID, err)
		}

		if err != nil {
			return RenderResult{}, fmt.Errorf("pipeline: execute datasets: %w", err)
		}
		datasetResults = results
		// Convert dataset warnings to diagnostics
		for _, w := range warnings {
			diags = append(diags, datasource.Diagnostic{
				Datasource: w.DataSet,
				Stage:      "dataset",
				Err:        fmt.Errorf("%s", w.Message),
			})
		}
	}

	// Track render step if execution plan is enabled
	var renderStepID string
	if opts.ExecutionPlan != nil {
		renderStepID = opts.ExecutionPlan.StartStep(buildlog.StepRenderHTML, "pipeline")
	}

	result, renderDiags, err := render.GenerateHTMLFromDocumentsWithDatasets(ctx, docs, datasetResults, opts.Language, opts.Orientation, opts.Format, opts.Mode, diags, opts.ConstraintContext, opts.EngineVersion, opts.AllDocs)

	// End render step
	if opts.ExecutionPlan != nil {
		opts.ExecutionPlan.EndStep(renderStepID, err)
	}

	if err != nil {
		return RenderResult{Diagnostics: append(diags, renderDiags...)}, err
	}
	return RenderResult{
		HTML:        result.HTML,
		LocalAssets: result.LocalAssets,
		Diagnostics: renderDiags,
	}, nil
}

// RenderArtefactOptions configures HTML rendering for a specific artefact.
type RenderArtefactOptions struct {
	// EngineVersion specifies the template engine version to use (e.g., "v1.2.3").
	EngineVersion string
	// QueryLogger is called for each SQL query executed. May be nil.
	QueryLogger func(query string)
	// QueryExecLogger is called for each query execution with detailed metadata. May be nil.
	QueryExecLogger duckdb.QueryExecLogger
	// EmbedOptions configures CSV embedding for build logs.
	EmbedOptions buildlog.EmbedOptions
	// ExecutionPlan tracks build execution steps. May be nil.
	ExecutionPlan *buildlog.ExecutionPlan
	// DataValidation controls how data validation errors are handled.
	DataValidation dataset.DataValidationMode
	// DataValidationSampleSize limits how many rows are validated.
	DataValidationSampleSize int
}

// RenderArtefactHTML generates HTML for a specific artefact using its spec settings.
// It uses RenderModeBuild by default since artefacts are typically rendered for PDF generation.
// For preview rendering, use RenderArtefactHTMLForPreview instead.
// The workdir parameter is required for dataset execution.
func RenderArtefactHTML(ctx context.Context, workdir string, docs []config.Document, artefact config.Artefact, opts RenderArtefactOptions) (RenderResult, error) {
	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, spec.ModeBuild)
	if err != nil {
		return RenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err := filterDocsByConstraintsWithContext(docs, constraintCtx)
	if err != nil {
		return RenderResult{}, err
	}

	// Validate name uniqueness after filtering
	if err := config.ValidateArtefactNames(artefact.Document.Name, filtered); err != nil {
		return RenderResult{}, err
	}

	return RenderHTML(ctx, filtered, RenderOptions{
		Workdir:                  workdir,
		Language:                 artefact.Spec.Language,
		Orientation:              artefact.Spec.Orientation,
		Format:                   artefact.Spec.Format,
		Mode:                     RenderModeBuild,
		EngineVersion:            opts.EngineVersion,
		QueryLogger:              opts.QueryLogger,
		QueryExecLogger:          opts.QueryExecLogger,
		EmbedOptions:             opts.EmbedOptions,
		ExecutionPlan:            opts.ExecutionPlan,
		ConstraintContext:        constraintCtx,
		AllDocs:                  docs,
		DataValidation:           opts.DataValidation,
		DataValidationSampleSize: opts.DataValidationSampleSize,
	})
}

// RenderArtefactHTMLForPreview generates HTML for a specific artefact in preview mode.
// Unlike RenderArtefactHTML, this does not include build-specific attributes like render-orientation.
// The workdir parameter is required for dataset execution.
// The queryLogger parameter is optional and can be used to log SQL queries.
// The engineVersion parameter specifies which template engine version to use.
func RenderArtefactHTMLForPreview(ctx context.Context, workdir string, docs []config.Document, artefact config.Artefact, queryLogger func(string), engineVersion string) (RenderResult, error) {
	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, spec.ModePreview)
	if err != nil {
		return RenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err := filterDocsByConstraintsWithContext(docs, constraintCtx)
	if err != nil {
		return RenderResult{}, err
	}

	// Validate name uniqueness after filtering
	if err := config.ValidateArtefactNames(artefact.Document.Name, filtered); err != nil {
		return RenderResult{}, err
	}

	return RenderHTML(ctx, filtered, RenderOptions{
		Workdir:           workdir,
		Language:          artefact.Spec.Language,
		Orientation:       artefact.Spec.Orientation,
		Format:            artefact.Spec.Format,
		Mode:              RenderModePreview,
		EngineVersion:     engineVersion,
		QueryLogger:       queryLogger,
		ConstraintContext: constraintCtx,
		AllDocs:           docs,
	})
}

// RenderScreenshotArtefactOptions configures screenshot artefact HTML rendering.
type RenderScreenshotArtefactOptions struct {
	EngineVersion            string
	QueryLogger              func(string)
	QueryExecLogger          duckdb.QueryExecLogger
	EmbedOptions             buildlog.EmbedOptions
	ExecutionPlan            *buildlog.ExecutionPlan
	DataValidation           dataset.DataValidationMode
	DataValidationSampleSize int
}

// RenderScreenshotArtefactHTML generates HTML for capturing screenshots.
// It renders the specified layout pages and their dependencies.
// The workdir parameter is required for dataset execution.
func RenderScreenshotArtefactHTML(ctx context.Context, workdir string, docs []config.Document, artefact config.ScreenshotArtefact, opts RenderScreenshotArtefactOptions) (RenderResult, error) {
	// Build constraint context from screenshot artefact
	constraintCtx, err := buildScreenshotConstraintContext(artefact, spec.ModeBuild)
	if err != nil {
		return RenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err := filterDocsByConstraintsWithContext(docs, constraintCtx)
	if err != nil {
		return RenderResult{}, err
	}

	// Further filter to only include specified layout pages and their dependencies
	filtered, err = filterDocsForLayoutPages(filtered, artefact.Spec.LayoutPages)
	if err != nil {
		return RenderResult{}, err
	}

	return RenderHTML(ctx, filtered, RenderOptions{
		Workdir:                  workdir,
		Language:                 artefact.Spec.Language,
		Orientation:              artefact.Spec.Orientation,
		Format:                   artefact.Spec.Format,
		Mode:                     RenderModeBuild,
		EngineVersion:            opts.EngineVersion,
		QueryLogger:              opts.QueryLogger,
		QueryExecLogger:          opts.QueryExecLogger,
		EmbedOptions:             opts.EmbedOptions,
		ExecutionPlan:            opts.ExecutionPlan,
		ConstraintContext:        constraintCtx,
		AllDocs:                  docs,
		DataValidation:           opts.DataValidation,
		DataValidationSampleSize: opts.DataValidationSampleSize,
	})
}

// buildScreenshotConstraintContext creates a constraint context from a screenshot artefact.
func buildScreenshotConstraintContext(artefact config.ScreenshotArtefact, mode spec.Mode) (*spec.ConstraintContext, error) {
	specMap, err := spec.SpecToMap(artefact.Document.Raw)
	if err != nil {
		return nil, fmt.Errorf("screenshot artefact %s: parse spec for constraints: %w", artefact.Document.Name, err)
	}

	return &spec.ConstraintContext{
		Labels: artefact.Labels,
		Spec:   specMap,
		Mode:   mode,
	}, nil
}

// filterDocsForLayoutPages filters documents to include only the specified layout pages
// and all documents they depend on (datasources, datasets, components, etc.).
func filterDocsForLayoutPages(docs []config.Document, layoutPageNames []string) ([]config.Document, error) {
	if len(layoutPageNames) == 0 {
		return nil, fmt.Errorf("no layout pages specified")
	}

	// Build a set of required layout page names
	requiredPages := make(map[string]bool)
	for _, name := range layoutPageNames {
		requiredPages[name] = true
	}

	// Filter documents: keep all non-LayoutPage docs (they might be dependencies)
	// and only the specified LayoutPage docs
	result := make([]config.Document, 0, len(docs))
	foundPages := make(map[string]bool)
	for _, doc := range docs {
		if doc.Kind == "LayoutPage" {
			if requiredPages[doc.Name] {
				result = append(result, doc)
				foundPages[doc.Name] = true
			}
		} else {
			// Keep all other documents (dependencies will be resolved by the renderer)
			result = append(result, doc)
		}
	}

	// Verify all requested pages were found
	for name := range requiredPages {
		if !foundPages[name] {
			return nil, fmt.Errorf("layout page %q not found", name)
		}
	}

	return result, nil
}

// RenderHTMLFrameAndContext generates a two-phase render output for preview mode.
// It returns a lightweight frame HTML that loads quickly, and context HTML that
// contains the full report content for SSE delivery.
// The workdir parameter is required for dataset execution.
func RenderHTMLFrameAndContext(ctx context.Context, docs []config.Document, opts RenderOptions) (FrameRenderResult, error) {
	var datasetResults []dataset.Result
	var diags []datasource.Diagnostic

	// Execute datasets if workdir is provided
	if opts.Workdir != "" {
		execOpts := &dataset.ExecuteOptions{
			QueryLogger:              opts.QueryLogger,
			DataValidation:           opts.DataValidation,
			DataValidationSampleSize: opts.DataValidationSampleSize,
		}
		results, warnings, err := dataset.Execute(ctx, opts.Workdir, docs, execOpts)
		if err != nil {
			return FrameRenderResult{}, fmt.Errorf("pipeline: execute datasets: %w", err)
		}
		datasetResults = results
		for _, w := range warnings {
			diags = append(diags, datasource.Diagnostic{
				Datasource: w.DataSet,
				Stage:      "dataset",
				Err:        fmt.Errorf("%s", w.Message),
			})
		}
	}

	result, renderDiags, err := render.GenerateFrameAndContext(ctx, docs, datasetResults, opts.Language, opts.Format, diags, opts.ConstraintContext, opts.EngineVersion, opts.AllDocs)
	if err != nil {
		return FrameRenderResult{Diagnostics: append(diags, renderDiags...)}, err
	}
	return FrameRenderResult{
		FrameHTML:   result.FrameHTML,
		ContextHTML: result.ContextHTML,
		LocalAssets: result.LocalAssets,
		Diagnostics: renderDiags,
	}, nil
}

// FrameRenderOptions configures frame rendering for preview mode.
type FrameRenderOptions struct {
	// QueryLogger is called for each SQL query executed. May be nil.
	QueryLogger func(string)
	// EngineVersion specifies the template engine version to use.
	EngineVersion string
	// DataValidation controls how data validation errors are handled.
	DataValidation dataset.DataValidationMode
	// DataValidationSampleSize limits how many rows are validated.
	DataValidationSampleSize int
}

// RenderArtefactFrameAndContext generates a two-phase render for a specific artefact in preview mode.
// It returns a lightweight frame HTML and context HTML for SSE delivery.
// The workdir parameter is required for dataset execution.
// The queryLogger parameter is optional and can be used to log SQL queries.
// The engineVersion parameter specifies which template engine version to use.
func RenderArtefactFrameAndContext(ctx context.Context, workdir string, docs []config.Document, artefact config.Artefact, queryLogger func(string), engineVersion string) (FrameRenderResult, error) {
	return RenderArtefactFrameAndContextWithMode(ctx, workdir, docs, artefact, queryLogger, spec.ModePreview, engineVersion)
}

// RenderArtefactFrameAndContextWithOptions generates a two-phase render for a specific artefact in preview mode with options.
// It returns a lightweight frame HTML and context HTML for SSE delivery.
// The workdir parameter is required for dataset execution.
func RenderArtefactFrameAndContextWithOptions(ctx context.Context, workdir string, docs []config.Document, artefact config.Artefact, opts FrameRenderOptions) (FrameRenderResult, error) {
	return RenderArtefactFrameAndContextWithModeAndOptions(ctx, workdir, docs, artefact, spec.ModePreview, opts)
}

// RenderArtefactFrameAndContextWithMode generates a two-phase render for a specific artefact with a specified mode.
// It returns a lightweight frame HTML and context HTML for SSE delivery.
// The workdir parameter is required for dataset execution.
// The queryLogger parameter is optional and can be used to log SQL queries.
// The mode parameter controls constraint evaluation (preview, serve, or build).
// The engineVersion parameter specifies which template engine version to use.
func RenderArtefactFrameAndContextWithMode(ctx context.Context, workdir string, docs []config.Document, artefact config.Artefact, queryLogger func(string), mode spec.Mode, engineVersion string) (FrameRenderResult, error) {
	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, mode)
	if err != nil {
		return FrameRenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err := filterDocsByConstraintsWithContext(docs, constraintCtx)
	if err != nil {
		return FrameRenderResult{}, err
	}

	// Validate name uniqueness after filtering
	if err := config.ValidateArtefactNames(artefact.Document.Name, filtered); err != nil {
		return FrameRenderResult{}, err
	}

	// Map spec.Mode to RenderMode
	var renderMode RenderMode
	switch mode {
	case spec.ModeBuild:
		renderMode = RenderModeBuild
	case spec.ModeServe:
		renderMode = RenderModeServe
	default:
		renderMode = RenderModePreview
	}

	return RenderHTMLFrameAndContext(ctx, filtered, RenderOptions{
		Workdir:           workdir,
		Language:          artefact.Spec.Language,
		Format:            artefact.Spec.Format,
		Mode:              renderMode,
		EngineVersion:     engineVersion,
		QueryLogger:       queryLogger,
		ConstraintContext: constraintCtx,
		AllDocs:           docs,
	})
}

// RenderArtefactFrameAndContextWithModeAndOptions generates a two-phase render for a specific artefact with a specified mode and options.
// It returns a lightweight frame HTML and context HTML for SSE delivery.
// The workdir parameter is required for dataset execution.
// The mode parameter controls constraint evaluation (preview, serve, or build).
func RenderArtefactFrameAndContextWithModeAndOptions(ctx context.Context, workdir string, docs []config.Document, artefact config.Artefact, mode spec.Mode, opts FrameRenderOptions) (FrameRenderResult, error) {
	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, mode)
	if err != nil {
		return FrameRenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err := filterDocsByConstraintsWithContext(docs, constraintCtx)
	if err != nil {
		return FrameRenderResult{}, err
	}

	// Validate name uniqueness after filtering
	if err := config.ValidateArtefactNames(artefact.Document.Name, filtered); err != nil {
		return FrameRenderResult{}, err
	}

	// Map spec.Mode to RenderMode
	var renderMode RenderMode
	switch mode {
	case spec.ModeBuild:
		renderMode = RenderModeBuild
	case spec.ModeServe:
		renderMode = RenderModeServe
	default:
		renderMode = RenderModePreview
	}

	return RenderHTMLFrameAndContext(ctx, filtered, RenderOptions{
		Workdir:                  workdir,
		Language:                 artefact.Spec.Language,
		Format:                   artefact.Spec.Format,
		Mode:                     renderMode,
		EngineVersion:            opts.EngineVersion,
		QueryLogger:              opts.QueryLogger,
		ConstraintContext:        constraintCtx,
		AllDocs:                  docs,
		DataValidation:           opts.DataValidation,
		DataValidationSampleSize: opts.DataValidationSampleSize,
	})
}

// buildConstraintContext creates a constraint context from an artefact and mode.
func buildConstraintContext(artefact config.Artefact, mode spec.Mode) (*spec.ConstraintContext, error) {
	specMap, err := spec.SpecToMap(artefact.Document.Raw)
	if err != nil {
		return nil, fmt.Errorf("artefact %s: parse spec for constraints: %w", artefact.Document.Name, err)
	}

	return &spec.ConstraintContext{
		Labels: artefact.Labels,
		Spec:   specMap,
		Mode:   mode,
	}, nil
}

// filterDocsByConstraints filters documents based on the artefact's labels, spec, and the mode.
func filterDocsByConstraints(docs []config.Document, artefact config.Artefact, mode spec.Mode) ([]config.Document, error) {
	constraintCtx, err := buildConstraintContext(artefact, mode)
	if err != nil {
		return nil, err
	}
	return filterDocsByConstraintsWithContext(docs, constraintCtx)
}

// filterDocsByConstraintsWithContext filters documents using a pre-built constraint context.
func filterDocsByConstraintsWithContext(docs []config.Document, constraintCtx *spec.ConstraintContext) ([]config.Document, error) {
	if constraintCtx == nil {
		return docs, nil
	}

	// Filter documents by constraints
	result := make([]config.Document, 0, len(docs))
	for _, doc := range docs {
		// ReportArtefacts are never filtered by constraints
		if doc.Kind == "ReportArtefact" {
			result = append(result, doc)
			continue
		}

		// No constraints means always included
		if len(doc.Constraints) == 0 {
			result = append(result, doc)
			continue
		}

		// Evaluate constraints
		match, err := spec.EvaluateParsedConstraintsWithContext(doc.Constraints, constraintCtx, doc.Kind, doc.Name)
		if err != nil {
			return nil, err
		}

		if match {
			result = append(result, doc)
		}
	}

	return result, nil
}

// LogDiagnostics logs datasource diagnostics as errors.
func LogDiagnostics(logger logx.Logger, diags []datasource.Diagnostic) {
	if logger == nil || len(diags) == 0 {
		return
	}
	for _, diag := range diags {
		logger.Errorf(diag.Error())
	}
}

// IsInvalidRootError delegates to render.IsInvalidRootError for error classification.
func IsInvalidRootError(err error) bool {
	return render.IsInvalidRootError(err)
}

// RenderMode describes the caller context for rendering.
type RenderMode = render.RenderMode

const (
	// RenderModeBuild indicates a build (PDF generation) context.
	RenderModeBuild RenderMode = render.RenderModeBuild
	// RenderModePreview indicates a live preview (HTTP server) context.
	RenderModePreview RenderMode = render.RenderModePreview
	// RenderModeServe indicates a production serve (bino serve) context.
	RenderModeServe RenderMode = render.RenderModeServe
)

// InvalidLayoutPolicy describes how callers should react to an invalid layout error.
type InvalidLayoutPolicy = render.InvalidLayoutPolicy

// ClassifyInvalidLayout inspects err and returns policy info for handling invalid layouts.
func ClassifyInvalidLayout(err error, mode RenderMode) InvalidLayoutPolicy {
	return render.ClassifyInvalidLayout(err, mode)
}
