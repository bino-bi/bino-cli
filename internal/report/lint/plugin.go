package lint

import (
	"context"
	"strings"
)

// PluginLinter can run lint rules against documents.
type PluginLinter interface {
	Manifest() PluginManifestInfo
	Lint(ctx context.Context, docs []PluginDocumentPayload, opts *PluginLintOptions) ([]PluginLintFinding, error)
}

// PluginLintOptions provides optional enriched context for plugin linting.
type PluginLintOptions struct {
	// Datasets contains pre-computed dataset results as serialized DatasetPayload.
	Datasets []PluginDatasetPayload
	// DatasetsAvailable is true when Datasets is populated.
	DatasetsAvailable bool
	// RenderedHTML contains the rendered HTML output. Nil if not available.
	RenderedHTML []byte
}

// PluginDatasetPayload is a serialized dataset result for plugin linters.
type PluginDatasetPayload struct {
	Name     string
	JSONRows []byte
	Columns  []string
}

// PluginManifestInfo is the subset of plugin manifest needed for linting.
type PluginManifestInfo struct {
	Name           string
	ProvidesLinter bool
}

// PluginDocumentPayload is the document format sent to plugin linters.
type PluginDocumentPayload struct {
	File     string
	Position int
	Kind     string
	Name     string
	Raw      []byte
}

// PluginLintFinding is a finding returned by a plugin linter.
type PluginLintFinding struct {
	RuleID   string
	Message  string
	File     string
	DocIdx   int
	Path     string
	Line     int
	Column   int
	Severity int // 0=Warning, 1=Error, 2=Info
}

// PluginLinterRegistry provides access to plugin linters.
type PluginLinterRegistry interface {
	PluginsWithLinter() []PluginLinter
}

// RunPluginLinters invokes all plugin linters and converts their findings.
// The opts parameter is optional and provides enriched context (datasets, rendered HTML).
func RunPluginLinters(ctx context.Context, docs []Document, registry PluginLinterRegistry, opts ...*PluginLintOptions) []Finding {
	if registry == nil {
		return nil
	}

	linters := registry.PluginsWithLinter()
	if len(linters) == 0 {
		return nil
	}

	// Convert lint.Document to plugin payload format.
	payloads := make([]PluginDocumentPayload, len(docs))
	for i, d := range docs {
		payloads[i] = PluginDocumentPayload{
			File:     d.File,
			Position: d.Position,
			Kind:     d.Kind,
			Name:     d.Name,
			Raw:      d.Raw,
		}
	}

	// Extract optional lint options.
	var lintOpts *PluginLintOptions
	if len(opts) > 0 {
		lintOpts = opts[0]
	}

	var allFindings []Finding

	for _, p := range linters {
		pluginFindings, err := p.Lint(ctx, payloads, lintOpts)
		if err != nil {
			// Non-fatal: skip this plugin's findings.
			continue
		}

		pluginName := p.Manifest().Name
		for _, f := range pluginFindings {
			ruleID := f.RuleID
			if !strings.Contains(ruleID, "/") {
				ruleID = pluginName + "/" + ruleID
			}
			allFindings = append(allFindings, Finding{
				RuleID:  ruleID,
				Message: f.Message,
				File:    f.File,
				DocIdx:  f.DocIdx,
				Path:    f.Path,
				Line:    f.Line,
				Column:  f.Column,
			})
		}
	}

	return allFindings
}

// LintConfig holds lint filtering configuration from bino.toml.
//
//nolint:revive // name used widely across codebase
type LintConfig struct {
	Disable  []string
	Severity map[string]string
}

// FilterFindings removes findings for disabled rules and applies severity overrides.
func FilterFindings(findings []Finding, cfg LintConfig) []Finding {
	if len(cfg.Disable) == 0 && len(cfg.Severity) == 0 {
		return findings
	}

	disabled := make(map[string]struct{}, len(cfg.Disable))
	for _, id := range cfg.Disable {
		disabled[id] = struct{}{}
	}

	filtered := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if _, ok := disabled[f.RuleID]; ok {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered
}
