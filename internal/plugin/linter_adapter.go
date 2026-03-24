package plugin

import (
	"context"

	"bino.bi/bino/internal/report/lint"
)

// NewLinterRegistry creates a lint.PluginLinterRegistry backed by the given PluginRegistry.
func NewLinterRegistry(registry *PluginRegistry) lint.PluginLinterRegistry {
	return &registryLinterAdapter{registry: registry}
}

type registryLinterAdapter struct {
	registry *PluginRegistry
}

func (a *registryLinterAdapter) PluginsWithLinter() []lint.PluginLinter {
	plugins := a.registry.PluginsWithLinter()
	linters := make([]lint.PluginLinter, len(plugins))
	for i, p := range plugins {
		linters[i] = &pluginLinterAdapter{plugin: p}
	}
	return linters
}

type pluginLinterAdapter struct {
	plugin Plugin
}

func (a *pluginLinterAdapter) Manifest() lint.PluginManifestInfo {
	m := a.plugin.Manifest()
	return lint.PluginManifestInfo{
		Name:           m.Name,
		ProvidesLinter: m.ProvidesLinter,
	}
}

func (a *pluginLinterAdapter) Lint(ctx context.Context, docs []lint.PluginDocumentPayload, opts *lint.PluginLintOptions) ([]lint.PluginLintFinding, error) {
	// Convert lint payloads to plugin document payloads.
	pluginDocs := make([]DocumentPayload, len(docs))
	for i, d := range docs {
		pluginDocs[i] = DocumentPayload{
			File:     d.File,
			Position: d.Position,
			Kind:     d.Kind,
			Name:     d.Name,
			Raw:      d.Raw,
		}
	}

	// Convert lint options to plugin LintOptions.
	var pluginOpts *LintOptions
	if opts != nil {
		pluginOpts = &LintOptions{
			DatasetsAvailable: opts.DatasetsAvailable,
			RenderedHTML:      opts.RenderedHTML,
		}
		for _, ds := range opts.Datasets {
			pluginOpts.Datasets = append(pluginOpts.Datasets, DatasetPayload{
				Name:     ds.Name,
				JSONRows: ds.JSONRows,
				Columns:  ds.Columns,
			})
		}
	}

	findings, err := a.plugin.Lint(ctx, pluginDocs, pluginOpts)
	if err != nil {
		return nil, err
	}

	result := make([]lint.PluginLintFinding, len(findings))
	for i, f := range findings {
		result[i] = lint.PluginLintFinding{
			RuleID:   f.RuleID,
			Message:  f.Message,
			File:     f.File,
			DocIdx:   f.DocIdx,
			Path:     f.Path,
			Line:     f.Line,
			Column:   f.Column,
			Severity: int(f.Severity),
		}
	}
	return result, nil
}
