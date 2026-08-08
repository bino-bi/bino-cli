// Package diagnostics is the single bundle-validation pipeline behind every
// editor-facing diagnostic surface: the daemon (HTTP/SSE), `bino lsp`, and
// `bino lsp-helper validate` all call Collect with their own Options instead
// of reimplementing the load → convert → lint → query-check sequence. Keeping
// one implementation makes the historic divergences (kind providers, plugin
// linters, engine compat, finding severity, nil-vs-[] JSON) impossible by
// construction.
package diagnostics

import (
	"context"
	"fmt"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/lint"
)

// Diagnostic represents a single diagnostic message for a file/document. The
// JSON tags are the daemon's stable HTTP/SSE contract (and the lsp-helper
// stdout contract) — do not change them.
type Diagnostic struct {
	File     string `json:"file"`
	Position int    `json:"position"` // 1-based document index within multi-doc YAML
	Line     int    `json:"line"`     // 1-based line number (0 if unknown)
	Column   int    `json:"column"`   // 1-based column number (0 if unknown)
	Severity string `json:"severity"` // "error", "warning", "info", "hint"
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`  // optional error code
	Field    string `json:"field,omitempty"` // optional field path
	Hint     string `json:"hint,omitempty"`  // actionable guidance (spec.Hint)
}

// Options configures Collect. The zero value runs the base pipeline: strict
// load, env-var check, and the default lint rules.
type Options struct {
	// KindProvider supplies plugin-registered kinds so plugin manifests are
	// not flagged as unknown. May be nil when no plugins are loaded.
	KindProvider config.KindProvider

	// SkipForeign skips strict validation of non-bino YAML (docker-compose,
	// CI files) instead of scolding it. Editor-facing callers set this.
	SkipForeign bool

	// PluginLinters, when non-nil, runs plugin-provided lint rules in
	// addition to the built-in ones.
	PluginLinters lint.PluginLinterRegistry

	// EngineCompat, when non-nil, is consulted once per Collect with the
	// bundle directory; ok=true appends the returned diagnostic (the
	// engine-version compatibility check lives in the CLI layer).
	EngineCompat func(dir string) (Diagnostic, bool)

	// ExecuteQueries additionally executes dataset queries and appends
	// data-validation diagnostics (slower but catches data issues).
	ExecuteQueries bool
}

// Collect validates the bundle at dir and returns its diagnostics. The result
// is never nil, so serializing it always yields [] rather than null.
func Collect(ctx context.Context, dir string, opts Options) []Diagnostic {
	// Load documents in strict mode to catch schema errors. Errors are
	// collected per document so every schema issue is reported, not just
	// the first one. ValidateDocuments runs inside the loader and its
	// failures land in loadErrs as well.
	var loadErrs []error
	docs, err := config.LoadDirWithOptions(ctx, dir, config.LoadOptions{
		Lenient:       false,
		KindProvider:  opts.KindProvider,
		CollectErrors: &loadErrs,
		SkipForeign:   opts.SkipForeign,
	})
	if err != nil {
		loadErrs = append(loadErrs, err)
	}
	diagnostics := make([]Diagnostic, 0, len(loadErrs))
	for _, loadErr := range loadErrs {
		diagnostics = append(diagnostics, FromLoadError(loadErr)...)
	}
	if len(loadErrs) > 0 {
		// Use lenient docs so downstream checks still see schema-invalid documents
		docs, _ = config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: true, KindProvider: opts.KindProvider}) //nolint:errcheck // lenient fallback pass; the strict errors were already collected
	}

	// Check for missing environment variables. Param names defined in
	// LayoutPages are excluded (they are resolved at render time).
	paramNames := config.CollectLayoutPageParamNames(docs)
	missingVars := config.CollectMissingEnvVarsExcluding(docs, paramNames)
	for _, mv := range missingVars {
		diagnostics = append(diagnostics, Diagnostic{
			File:     mv.File,
			Severity: "warning",
			Message:  fmt.Sprintf("Unresolved environment variable: %s", mv.VarName),
			Code:     "missing-env-var",
		})
	}

	if opts.EngineCompat != nil {
		if d, ok := opts.EngineCompat(dir); ok {
			diagnostics = append(diagnostics, d)
		}
	}

	// Run lint rules. DocumentsFromConfig carries metadata.params too — the
	// ref-params rule is inert without the declarations.
	lintDocs := lint.DocumentsFromConfig(docs)
	runner := lint.NewDefaultRunner()
	findings := runner.Run(ctx, lintDocs)
	if opts.PluginLinters != nil {
		pluginFindings := lint.RunPluginLinters(ctx, lintDocs, opts.PluginLinters)
		findings = append(findings, pluginFindings...)
	}
	for _, f := range findings {
		sev := f.Severity
		if sev == "" {
			sev = "warning"
		}
		diagnostics = append(diagnostics, Diagnostic{
			File:     f.File,
			Position: f.DocIdx,
			Line:     f.Line,
			Column:   f.Column,
			Severity: sev,
			Message:  f.Message,
			Code:     f.RuleID,
			Field:    f.Path,
		})
	}

	if opts.ExecuteQueries && len(docs) > 0 {
		diagnostics = append(diagnostics, RunQueryValidation(ctx, dir, docs)...)
	}

	return diagnostics
}

// RunQueryValidation executes dataset queries with data validation enabled and
// maps the warnings to diagnostics, locating each warning's DataSet document
// for file/position. It backs Collect's ExecuteQueries option and the daemon's
// two-phase ValidateWithQueries. The result is never nil.
func RunQueryValidation(ctx context.Context, dir string, docs []config.Document) []Diagnostic {
	diagnostics := []Diagnostic{}

	execOpts := &dataset.ExecuteOptions{
		DataValidation:           dataset.DataValidationWarn,
		DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
		ContinueOnQueryError:     true,
	}
	_, warnings, err := dataset.Execute(ctx, dir, docs, execOpts)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: "warning",
			Message:  fmt.Sprintf("Query execution failed: %v", err),
			Code:     "data-validation-error",
		})
	}
	for _, w := range warnings {
		var file string
		var position int
		for _, doc := range docs {
			if doc.Kind == "DataSet" && doc.Name == w.DataSet {
				file = doc.File
				position = doc.Position
				break
			}
		}
		diagnostics = append(diagnostics, Diagnostic{
			File:     file,
			Position: position,
			Severity: "warning",
			Message:  w.Message,
			Code:     "data-validation",
			Field:    w.DataSet,
		})
	}

	return diagnostics
}
