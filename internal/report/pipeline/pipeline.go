// Package pipeline provides shared build/preview/serve logic for manifest loading,
// artefact selection, and HTML rendering. Both CLI build and preview commands
// use these helpers to ensure consistent behavior and options.
package pipeline

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/markdown"
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
// Names in Include that don't match any artefact are skipped (they may be ScreenshotArtefact names).
// Use ValidateArtefactNames to check that all include names exist before calling this function.
func FilterArtefacts(all []config.Artefact, opts FilterOptions) []config.Artefact {
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
				// Skip - may be a ScreenshotArtefact name
				continue
			}
			if _, blocked := excludeSet[name]; blocked {
				continue
			}
			filtered = append(filtered, art)
			seen[name] = struct{}{}
		}
		return filtered
	}

	filtered := make([]config.Artefact, 0, len(all))
	for _, art := range all {
		if _, blocked := excludeSet[art.Document.Name]; blocked {
			continue
		}
		filtered = append(filtered, art)
	}
	return filtered
}

// FilterDocumentArtefacts selects document artefacts based on include/exclude rules.
// If Include is non-empty, only those names are selected (in order).
// Exclude names are always removed from the result.
func FilterDocumentArtefacts(all []config.DocumentArtefact, opts FilterOptions) []config.DocumentArtefact {
	excludeSet := make(map[string]struct{})
	for _, name := range opts.Exclude {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		excludeSet[name] = struct{}{}
	}

	if len(opts.Include) > 0 {
		index := make(map[string]config.DocumentArtefact, len(all))
		for _, art := range all {
			index[art.Document.Name] = art
		}
		seen := make(map[string]struct{})
		var filtered []config.DocumentArtefact
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
				// Skip - may be another artefact type
				continue
			}
			if _, blocked := excludeSet[name]; blocked {
				continue
			}
			filtered = append(filtered, art)
			seen[name] = struct{}{}
		}
		return filtered
	}

	filtered := make([]config.DocumentArtefact, 0, len(all))
	for _, art := range all {
		if _, blocked := excludeSet[art.Document.Name]; blocked {
			continue
		}
		filtered = append(filtered, art)
	}
	return filtered
}

// FilterScreenshotArtefacts selects screenshot artefacts based on include/exclude rules.
// If Include is non-empty, only those names are selected (in order).
// Exclude names are always removed from the result.
// Unlike FilterArtefacts, this function does not error when an include name is not found,
// as it may be a ReportArtefact name instead.
func FilterScreenshotArtefacts(all []config.ScreenshotArtefact, opts FilterOptions) []config.ScreenshotArtefact {
	excludeSet := make(map[string]struct{})
	for _, name := range opts.Exclude {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		excludeSet[name] = struct{}{}
	}

	if len(opts.Include) > 0 {
		index := make(map[string]config.ScreenshotArtefact, len(all))
		for _, art := range all {
			index[art.Document.Name] = art
		}
		seen := make(map[string]struct{})
		var filtered []config.ScreenshotArtefact
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
				// Skip - may be a ReportArtefact name
				continue
			}
			if _, blocked := excludeSet[name]; blocked {
				continue
			}
			filtered = append(filtered, art)
			seen[name] = struct{}{}
		}
		return filtered
	}

	filtered := make([]config.ScreenshotArtefact, 0, len(all))
	for _, art := range all {
		if _, blocked := excludeSet[art.Document.Name]; blocked {
			continue
		}
		filtered = append(filtered, art)
	}
	return filtered
}

// ValidateArtefactNames checks that all include names exist in either the ReportArtefact
// or ScreenshotArtefact lists.
func ValidateArtefactNames(artefacts []config.Artefact, screenshots []config.ScreenshotArtefact, include []string) error {
	return ValidateAllArtefactNames(artefacts, screenshots, nil, include)
}

// ValidateAllArtefactNames checks that all include names exist in any of the artefact type lists.
func ValidateAllArtefactNames(artefacts []config.Artefact, screenshots []config.ScreenshotArtefact, documents []config.DocumentArtefact, include []string) error {
	if len(include) == 0 {
		return nil
	}

	// Build a set of all known artefact names
	known := make(map[string]struct{})
	for _, art := range artefacts {
		known[art.Document.Name] = struct{}{}
	}
	for _, ss := range screenshots {
		known[ss.Document.Name] = struct{}{}
	}
	for _, doc := range documents {
		known[doc.Document.Name] = struct{}{}
	}

	// Check that all include names exist
	for _, raw := range include {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := known[name]; !ok {
			return fmt.Errorf("artefact %q not found", name)
		}
	}
	return nil
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
	// Select LayoutPages by refs (before constraint filtering)
	filtered, _, err := selectLayoutPagesByRefs(docs, artefact.Spec.LayoutPages)
	if err != nil {
		return RenderResult{}, fmt.Errorf("artefact %s: %w", artefact.Document.Name, err)
	}

	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, spec.ModeBuild)
	if err != nil {
		return RenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err = filterDocsByConstraintsWithContext(filtered, constraintCtx)
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
	// Select LayoutPages by refs (before constraint filtering)
	filtered, _, err := selectLayoutPagesByRefs(docs, artefact.Spec.LayoutPages)
	if err != nil {
		return RenderResult{}, fmt.Errorf("artefact %s: %w", artefact.Document.Name, err)
	}

	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, spec.ModePreview)
	if err != nil {
		return RenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err = filterDocsByConstraintsWithContext(filtered, constraintCtx)
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
		Labels:       artefact.Labels,
		Spec:         specMap,
		Mode:         mode,
		ArtefactKind: "screenshot",
	}, nil
}

// buildDocumentConstraintContext creates a constraint context from a document artefact.
func buildDocumentConstraintContext(artefact config.DocumentArtefact, mode spec.Mode) (*spec.ConstraintContext, error) {
	specMap, err := spec.SpecToMap(artefact.Document.Raw)
	if err != nil {
		return nil, fmt.Errorf("document artefact %s: parse spec for constraints: %w", artefact.Document.Name, err)
	}

	return &spec.ConstraintContext{
		Labels:       artefact.Labels,
		Spec:         specMap,
		Mode:         mode,
		ArtefactKind: "document",
	}, nil
}

// buildLiveConstraintContext creates a constraint context from a live artefact.
func buildLiveConstraintContext(artefact config.LiveArtefact, mode spec.Mode) (*spec.ConstraintContext, error) {
	specMap, err := spec.SpecToMap(artefact.Document.Raw)
	if err != nil {
		return nil, fmt.Errorf("live artefact %s: parse spec for constraints: %w", artefact.Document.Name, err)
	}

	return &spec.ConstraintContext{
		Labels:       nil, // LiveArtefact doesn't have labels field
		Spec:         specMap,
		Mode:         mode,
		ArtefactKind: "livereport",
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

// SelectedLayoutPage represents a LayoutPage selected for rendering,
// along with any params that should be applied to it.
type SelectedLayoutPage struct {
	Doc    config.Document
	Params map[string]string // Params to apply when rendering this page
}

// expandLayoutPageWithParams expands params into a LayoutPage document.
// Returns a new document with:
// - Params expanded into the Raw content using ${PARAM} substitution
// - For select params, ${PARAM_LABEL} is also available with the label from the option item
// - A unique name suffix based on the param values (e.g., "page#REGION=EU,YEAR=2024")
// If both params is empty and doc has no params defined, returns the original document unchanged.
func expandLayoutPageWithParams(doc config.Document, params map[string]string) (config.Document, error) {
	// If no explicit params and no defined params, return unchanged
	if len(params) == 0 && len(doc.Params) == 0 {
		return doc, nil
	}

	envLookup := config.EnvLookup()

	// Step 1: Expand param values themselves (they may contain ${VAR} from ENV)
	expandedParams := make(map[string]string)
	for k, v := range params {
		expanded, _ := config.ExpandVars(v, envLookup)
		expandedParams[k] = expanded
	}

	// Step 2: Build effective params: explicit params > defaults > ENV fallback
	// Also add _LABEL variants for select type params
	effectiveParams := make(map[string]string)
	for _, def := range doc.Params {
		// Priority: explicit params > defaults > env
		if v, ok := expandedParams[def.Name]; ok {
			effectiveParams[def.Name] = v
			// For select params, also set the _LABEL variant
			if def.Type == "select" && def.Options != nil {
				effectiveParams[def.Name+"_LABEL"] = lookupSelectLabel(def.Options.Items, v)
			}
		} else if def.Default != nil {
			effectiveParams[def.Name] = *def.Default
			// For select params with default, also set the label
			if def.Type == "select" && def.Options != nil {
				effectiveParams[def.Name+"_LABEL"] = lookupSelectLabel(def.Options.Items, *def.Default)
			}
		} else if envVal, found := envLookup(def.Name); found {
			// Param comes from environment variable - still look up its label
			effectiveParams[def.Name] = envVal
			if def.Type == "select" && def.Options != nil {
				effectiveParams[def.Name+"_LABEL"] = lookupSelectLabel(def.Options.Items, envVal)
			}
		}
	}

	// Step 3: Create lookup chain: params > ENV (fallback for non-param vars)
	lookup := config.ChainLookup(
		config.MapLookup(effectiveParams),
		envLookup,
	)

	// Step 4: Expand document content
	expandedContent, _ := config.ExpandVars(string(doc.Raw), lookup)

	// Step 5: Generate unique name suffix based on effective param values
	keys := make([]string, 0, len(doc.Params))
	for _, def := range doc.Params {
		if v, ok := effectiveParams[def.Name]; ok {
			keys = append(keys, def.Name+"="+v)
		}
	}
	var nameSuffix string
	if len(keys) > 0 {
		nameSuffix = "#" + strings.Join(keys, ",")
	}

	// Create new document with expanded content and unique name
	return config.Document{
		File:           doc.File,
		Position:       doc.Position,
		Kind:           doc.Kind,
		Name:           doc.Name + nameSuffix,
		Labels:         doc.Labels,
		Constraints:    doc.Constraints,
		Params:         doc.Params,
		Raw:            []byte(expandedContent),
		MissingEnvVars: nil, // Params should have resolved any missing vars
	}, nil
}

// lookupSelectLabel finds the label for a given value in a list of select option items.
// If the value is not found or has no label, the value itself is returned.
func lookupSelectLabel(items []config.LayoutPageParamOptionItem, value string) string {
	for _, item := range items {
		if item.Value == value {
			if item.Label != "" {
				return item.Label
			}
			return value // No label defined, use value
		}
	}
	return value // Value not found in items, use value as-is
}

// selectLayoutPagesByRefs filters and orders LayoutPage documents by LayoutPageRef entries.
// Refs can be glob patterns (no params) or explicit page names with params.
// Returns pages in ref order; within glob patterns, pages are sorted alphabetically by name.
// Non-LayoutPage documents are preserved at the beginning of the result in their original order.
// If refs is empty or contains only "*", the function returns docs unchanged (default behavior).
// LayoutPages with defined params are expanded with default values even without explicit params.
func selectLayoutPagesByRefs(docs []config.Document, refs config.LayoutPagesOrRefs) ([]config.Document, []SelectedLayoutPage, error) {
	// Check if using default pattern (select all)
	if len(refs) == 0 || (len(refs) == 1 && refs[0].Page == "*" && len(refs[0].Params) == 0) {
		// Return all LayoutPages, expanding those with params using defaults
		var expandedDocs []config.Document
		var layoutPages []SelectedLayoutPage
		for _, doc := range docs {
			if doc.Kind == "LayoutPage" {
				// Expand with defaults if the page has params defined
				if len(doc.Params) > 0 {
					expandedDoc, err := expandLayoutPageWithParams(doc, nil)
					if err != nil {
						return nil, nil, fmt.Errorf("expand defaults for %q: %w", doc.Name, err)
					}
					expandedDocs = append(expandedDocs, expandedDoc)
					layoutPages = append(layoutPages, SelectedLayoutPage{Doc: expandedDoc})
				} else {
					expandedDocs = append(expandedDocs, doc)
					layoutPages = append(layoutPages, SelectedLayoutPage{Doc: doc})
				}
			} else {
				expandedDocs = append(expandedDocs, doc)
			}
		}
		return expandedDocs, layoutPages, nil
	}

	// Separate LayoutPage documents from others
	var layoutPages []config.Document
	var others []config.Document
	for _, doc := range docs {
		if doc.Kind == "LayoutPage" {
			layoutPages = append(layoutPages, doc)
		} else {
			others = append(others, doc)
		}
	}

	// Build name-to-document map for LayoutPages
	pagesByName := make(map[string]config.Document, len(layoutPages))
	for _, doc := range layoutPages {
		pagesByName[doc.Name] = doc
	}

	// Select pages in ref order
	// For globs: track seen to avoid duplicates
	// For explicit refs with params: allow same page multiple times with different params
	seenGlob := make(map[string]bool)
	var selectedDocs []config.Document
	var selectedPages []SelectedLayoutPage

	for _, ref := range refs {
		pageName := strings.TrimSpace(ref.Page)
		if pageName == "" {
			continue
		}

		// Check if this is a glob pattern
		if ref.IsGlob() {
			// Validate pattern syntax
			if _, err := path.Match(pageName, ""); err != nil {
				return nil, nil, fmt.Errorf("invalid layoutPages pattern %q: %w", pageName, err)
			}

			// Find all matching pages
			var matches []config.Document
			for name, doc := range pagesByName {
				if seenGlob[name] {
					continue
				}
				matched, _ := path.Match(pageName, name)
				if matched {
					matches = append(matches, doc)
				}
			}

			// Sort matches alphabetically by name for deterministic order
			sort.Slice(matches, func(i, j int) bool {
				return matches[i].Name < matches[j].Name
			})

			// Add to selected (mark as seen to avoid duplicates from globs)
			// Expand pages with params using their defaults
			for _, doc := range matches {
				if len(doc.Params) > 0 {
					expandedDoc, err := expandLayoutPageWithParams(doc, nil)
					if err != nil {
						return nil, nil, fmt.Errorf("expand defaults for %q: %w", doc.Name, err)
					}
					selectedDocs = append(selectedDocs, expandedDoc)
					selectedPages = append(selectedPages, SelectedLayoutPage{Doc: expandedDoc})
				} else {
					selectedDocs = append(selectedDocs, doc)
					selectedPages = append(selectedPages, SelectedLayoutPage{Doc: doc})
				}
				seenGlob[doc.Name] = true
			}
		} else {
			// Explicit page name with optional params
			doc, found := pagesByName[pageName]
			if !found {
				// Page not found - will be caught during rendering
				continue
			}

			// For explicit refs with params, allow duplicates (same page, different params)
			// For explicit refs without params, treat like globs (skip if already seen)
			// Expand pages with params using their defaults or provided params
			if len(ref.Params) == 0 {
				if seenGlob[pageName] {
					continue
				}
				seenGlob[pageName] = true
				// Expand with defaults if the page has params defined
				if len(doc.Params) > 0 {
					expandedDoc, err := expandLayoutPageWithParams(doc, nil)
					if err != nil {
						return nil, nil, fmt.Errorf("expand defaults for %q: %w", pageName, err)
					}
					selectedDocs = append(selectedDocs, expandedDoc)
					selectedPages = append(selectedPages, SelectedLayoutPage{Doc: expandedDoc})
				} else {
					selectedDocs = append(selectedDocs, doc)
					selectedPages = append(selectedPages, SelectedLayoutPage{Doc: doc})
				}
			} else {
				// Expand params into document to create unique instance
				expandedDoc, err := expandLayoutPageWithParams(doc, ref.Params)
				if err != nil {
					return nil, nil, fmt.Errorf("expand params for %q: %w", pageName, err)
				}
				selectedDocs = append(selectedDocs, expandedDoc)
				selectedPages = append(selectedPages, SelectedLayoutPage{Doc: expandedDoc, Params: ref.Params})
			}
		}
	}

	// Combine: non-LayoutPage docs first, then selected LayoutPages
	result := make([]config.Document, 0, len(others)+len(selectedDocs))
	result = append(result, others...)
	result = append(result, selectedDocs...)

	return result, selectedPages, nil
}

// selectLayoutPagesByPatterns filters and orders LayoutPage documents by name patterns.
// Patterns are matched against metadata.name using path.Match (glob syntax).
// Returns pages in pattern order; within each pattern, pages are sorted alphabetically by name.
// Non-LayoutPage documents are preserved at the beginning of the result in their original order.
// If patterns is empty or contains only "*", the function returns docs unchanged (default behavior).
// Deprecated: Use selectLayoutPagesByRefs for full LayoutPagesOrRefs support.
func selectLayoutPagesByPatterns(docs []config.Document, patterns []string) ([]config.Document, error) {
	// Convert string patterns to LayoutPagesOrRefs
	refs := make(config.LayoutPagesOrRefs, len(patterns))
	for i, p := range patterns {
		refs[i] = config.LayoutPageRef{Page: p}
	}
	result, _, err := selectLayoutPagesByRefs(docs, refs)
	return result, err
}

// RenderHTMLFrameAndContext generates a two-phase render output for preview mode.
// It returns a lightweight frame HTML that loads quickly, and context HTML that
// contains the full report content for SSE delivery.
// The workdir parameter is required for dataset execution.
func RenderHTMLFrameAndContext(ctx context.Context, docs []config.Document, opts RenderOptions) (FrameRenderResult, error) {
	// Expand LayoutPages with defined params using their defaults/env values
	expandedDocs := make([]config.Document, 0, len(docs))
	for _, doc := range docs {
		if doc.Kind == "LayoutPage" && len(doc.Params) > 0 {
			expandedDoc, err := expandLayoutPageWithParams(doc, nil)
			if err != nil {
				return FrameRenderResult{}, fmt.Errorf("expand params for %q: %w", doc.Name, err)
			}
			expandedDocs = append(expandedDocs, expandedDoc)
		} else {
			expandedDocs = append(expandedDocs, doc)
		}
	}
	docs = expandedDocs

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
	// Select LayoutPages by refs (before constraint filtering)
	filtered, _, err := selectLayoutPagesByRefs(docs, artefact.Spec.LayoutPages)
	if err != nil {
		return FrameRenderResult{}, fmt.Errorf("artefact %s: %w", artefact.Document.Name, err)
	}

	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, mode)
	if err != nil {
		return FrameRenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err = filterDocsByConstraintsWithContext(filtered, constraintCtx)
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
	// Select LayoutPages by refs (before constraint filtering)
	filtered, _, err := selectLayoutPagesByRefs(docs, artefact.Spec.LayoutPages)
	if err != nil {
		return FrameRenderResult{}, fmt.Errorf("artefact %s: %w", artefact.Document.Name, err)
	}

	// Build constraint context from artefact
	constraintCtx, err := buildConstraintContext(artefact, mode)
	if err != nil {
		return FrameRenderResult{}, err
	}

	// Filter documents by constraints for this artefact
	filtered, err = filterDocsByConstraintsWithContext(filtered, constraintCtx)
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
		Labels:       artefact.Labels,
		Spec:         specMap,
		Mode:         mode,
		ArtefactKind: "report",
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

// DocumentArtefactResult captures the outcome of rendering a document artefact.
type DocumentArtefactResult struct {
	HTML        []byte
	LocalAssets []render.LocalAsset
}

// DocumentArtefactRenderOptions configures document artefact rendering.
type DocumentArtefactRenderOptions struct {
	// EngineVersion is the template engine version to use (e.g., "v1.2.3").
	// If empty, a default version is used.
	EngineVersion string
	// TOCPageNumbers maps heading IDs to page numbers (from two-pass rendering).
	// If provided and TableOfContents is enabled, page numbers are included in the TOC.
	TOCPageNumbers map[string]int
}

// RenderDocumentArtefactHTML generates HTML from markdown files for a DocumentArtefact.
// It reads the specified source markdown files, converts them to HTML using goldmark,
// and wraps them in a full bino HTML document with template engine, bn-context, datasources, etc.
func RenderDocumentArtefactHTML(ctx context.Context, workdir string, artefact config.DocumentArtefact, opts DocumentArtefactRenderOptions) (DocumentArtefactResult, error) {
	logger := logx.FromContext(ctx).Channel("document")
	spec := artefact.Spec

	// Load all documents from the workdir
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		return DocumentArtefactResult{}, fmt.Errorf("document artefact %s: load manifests: %w", artefact.Document.Name, err)
	}

	// Execute datasets and collect datasources
	datasetResults, _, err := dataset.Execute(ctx, workdir, docs, nil)
	if err != nil {
		return DocumentArtefactResult{}, fmt.Errorf("document artefact %s: execute datasets: %w", artefact.Document.Name, err)
	}

	datasourceResults, _, err := datasource.Collect(ctx, docs)
	if err != nil {
		return DocumentArtefactResult{}, fmt.Errorf("document artefact %s: collect datasources: %w", artefact.Document.Name, err)
	}

	// Get the manifest file's directory to resolve relative paths
	manifestDir := filepath.Dir(artefact.Document.File)
	if manifestDir == "" {
		manifestDir = workdir
	}

	logger.Debugf("Rendering DocumentArtefact %s with %d source pattern(s)", artefact.Document.Name, len(spec.Sources))

	// Resolve source files (expand globs, filter .md files, sort)
	files, err := markdown.ResolveSourceFiles(manifestDir, spec.Sources)
	if err != nil {
		return DocumentArtefactResult{}, fmt.Errorf("document artefact %s: %w", artefact.Document.Name, err)
	}

	logger.Debugf("Resolved %d markdown file(s) from sources", len(files))

	// Load custom stylesheet if specified
	var customCSS string
	if spec.Stylesheet != "" {
		var err error
		customCSS, err = markdown.LoadStylesheet(manifestDir, spec.Stylesheet)
		if err != nil {
			return DocumentArtefactResult{}, fmt.Errorf("document artefact %s: %w", artefact.Document.Name, err)
		}
	}

	// Get template engine version (use provided or default)
	engineVersion := opts.EngineVersion
	if engineVersion == "" {
		engineVersion = "latest"
	}

	// Resolve asset URLs for asset: image references in markdown
	assetURLs, assetLocals, err := render.ResolveAssetURLs(docs)
	if err != nil {
		return DocumentArtefactResult{}, fmt.Errorf("document artefact %s: %w", artefact.Document.Name, err)
	}

	// Create render context with documents, datasets, and datasources
	renderCtx := markdown.NewRenderContext(docs, datasetResults, datasourceResults, engineVersion)
	renderCtx.AssetURLs = assetURLs

	// Render markdown files to HTML content with full context
	mathEnabled := spec.MathEnabled()
	content, err := markdown.RenderFilesWithContext(ctx, files, markdown.FullRenderOptions{
		RenderOptions: markdown.RenderOptions{
			BaseDir:               manifestDir,
			Stylesheet:            spec.Stylesheet,
			TableOfContents:       spec.TableOfContents,
			PageBreakBetweenFiles: spec.PageBreakBetweenSources,
			Math:                  mathEnabled,
		},
		RenderContext:  renderCtx,
		Locale:         spec.Locale,
		TOCPageNumbers: opts.TOCPageNumbers,
		Math:           mathEnabled,
	})
	if err != nil {
		return DocumentArtefactResult{}, fmt.Errorf("document artefact %s: %w", artefact.Document.Name, err)
	}

	// Wrap in full bino HTML document with template engine, bn-context, etc.
	html := markdown.WrapDocumentWithContext(content, markdown.FullDocumentOptions{
		DocumentOptions: markdown.DocumentOptions{
			Title:       spec.Title,
			Author:      spec.Author,
			Subject:     spec.Subject,
			Keywords:    spec.Keywords,
			Format:      spec.Format,
			Orientation: spec.Orientation,
			Stylesheet:  customCSS,
		},
		Locale:        spec.Locale,
		RenderContext: renderCtx,
	})

	return DocumentArtefactResult{HTML: html, LocalAssets: assetLocals}, nil
}

// LogDocumentArtefactWarnings logs any warnings collected during document artefact validation.
func LogDocumentArtefactWarnings(logger logx.Logger, artefacts []config.DocumentArtefact) {
	if logger == nil {
		return
	}
	for _, art := range artefacts {
		for _, warn := range art.Warnings {
			logger.Warnf(warn)
		}
	}
}

// EnsureDocumentSigningProfiles verifies that all document artefacts referencing a signing profile
// have that profile available in the provided map.
func EnsureDocumentSigningProfiles(artefacts []config.DocumentArtefact, profiles map[string]config.SigningProfile) error {
	for _, art := range artefacts {
		ref := strings.TrimSpace(art.Spec.SigningProfile)
		if ref == "" {
			continue
		}
		if _, ok := profiles[ref]; !ok {
			return fmt.Errorf("document artefact %s references unknown SigningProfile %q", art.Document.Name, ref)
		}
	}
	return nil
}
