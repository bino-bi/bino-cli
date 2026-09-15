package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/version"
)

// newLintCommand creates the lint subcommand.
// It loads and validates manifests, then runs lint rules and reports findings.
func newLintCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
	var (
		workdir        string
		outDir         string
		lintLogFormat  string
		executeQueries bool
		failOnWarnings bool
	)

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate manifests and run lint rules without building",
		Long: strings.TrimSpace(`Load manifests, validate against the schema, and run lint rules.
This command does not execute queries or generate PDFs.

Lint findings are warnings by default; the exit code is non-zero when manifests
fail to load, when a rule is raised to "error" in bino.toml's [lint] table, or
with --fail-on-warnings.`),
		Example: strings.TrimSpace(`  bino lint
  bino lint --work-dir ./reports
  bino lint --log-format json`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("lint")
			startTime := time.Now()

			// Create structured output
			out := NewOutput(OutputConfig{
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
				NoColor: logx.NoColorEnabled(ctx),
			})

			// Get run ID for log file
			runID := logx.RunIDFromContext(ctx)
			shortRunID := runID
			if len(runID) > 8 {
				shortRunID = runID[:8]
			}

			// Print header
			out.Header(fmt.Sprintf("BINO LINT %s", version.Version))

			// Find project root (directory containing bino.toml)
			projectRoot, err := pipeline.ResolveProjectRoot(workdir)
			if err != nil {
				return ConfigError(err)
			}

			// Load plugins if declared.
			var kindProvider config.KindProvider
			var pluginLinters lint.PluginLinterRegistry
			projectCfg, cfgErr := pathutil.LoadProjectConfig(projectRoot)
			if cfgErr == nil && len(projectCfg.Plugins) > 0 {
				mgr := plugin.NewManager(logger.Channel("plugin"))
				mgr.SetVerbose(logx.DebugEnabled(ctx))
				if err := mgr.LoadAll(ctx, projectCfg, projectRoot, version.Version); err != nil {
					logger.Warnf("Failed to load plugins: %v", err)
				} else {
					defer mgr.ShutdownAll(ctx)
					kindProvider = mgr.Registry()
					pluginLinters = plugin.NewLinterRegistry(mgr.Registry())
				}
			}

			runner := lint.NewProjectRunner(projectRoot)
			for _, w := range runner.ConfigWarnings() {
				out.Warning("bino.toml: " + w)
			}
			if cfgErr != nil {
				// The runner falls back to defaults when bino.toml cannot be
				// read, so a [lint] table in an unparseable file is silently
				// inert. Say so instead of letting the entries vanish.
				out.Warning(fmt.Sprintf("bino.toml could not be read, so any [lint] configuration was ignored: %v", cfgErr))
			}

			// Engine version compatibility check. Runs before manifest loading so
			// the finding is reported even when YAML manifests fail to parse —
			// the engine pin is the more actionable problem in that case. Forces
			// a non-zero exit independent of --fail-on-warnings.
			var compatFatal bool
			var compatFinding lint.Finding
			if cfgErr == nil && !runner.Skip("engine-version-incompatible") {
				if f, fatal := engineCompatFinding(projectRoot, projectCfg.EngineVersion); fatal {
					compatFinding = f
					compatFatal = true
				}
			}

			// Check for cancellation before starting expensive manifest loading
			if err := ctx.Err(); err != nil {
				return err
			}

			// Step 1: Load and validate manifests. Schema errors are collected
			// instead of aborting so every issue in the bundle is reported.
			out.Step("Loading manifests...")
			loadStart := time.Now()
			var loadErrors []error
			documents, err := config.LoadDirWithOptions(ctx, projectRoot, config.LoadOptions{KindProvider: kindProvider, CollectErrors: &loadErrors})
			if err != nil {
				if compatFatal {
					out.Blank()
					printCompatFinding(out, projectRoot, compatFinding)
				}
				return ConfigError(err)
			}
			if len(documents) == 0 && len(loadErrors) == 0 {
				if compatFatal {
					out.Blank()
					printCompatFinding(out, projectRoot, compatFinding)
					return RuntimeErrorf("engine-version-incompatible")
				}
				return ConfigErrorf("no YAML documents found in %s", projectRoot)
			}

			rawSchemaFindings := schemaFindingsFromErrors(loadErrors)
			schemaFindings := runner.Apply(rawSchemaFindings)
			if len(loadErrors) > 0 {
				out.StepDone(fmt.Sprintf("Validated %d document(s), %d failed validation", len(documents), len(loadErrors)), time.Since(loadStart))
			} else {
				out.StepDone(fmt.Sprintf("Validated %d document(s)", len(documents)), time.Since(loadStart))
			}

			// Convert config.Document to lint.Document
			lintDocs := lint.DocumentsFromConfig(documents)

			// Step 2: Run lint rules
			out.Step("Running lint rules...")
			lintStart := time.Now()
			findings := runner.Run(ctx, lintDocs)

			// Run plugin linters.
			if pluginLinters != nil {
				pluginFindings := lint.RunPluginLinters(ctx, lintDocs, pluginLinters)
				findings = append(findings, pluginFindings...)
			}
			findings = runner.Apply(findings)

			out.StepDone(fmt.Sprintf("Checked %d rule(s)", len(runner.Rules())), time.Since(lintStart))

			// Prepend the compat finding (resolved earlier) so it renders first.
			if compatFatal {
				findings = append([]lint.Finding{compatFinding}, findings...)
			}

			// Step 3: Execute queries and validate data (optional)
			var dataValidationWarnings []dataset.Warning
			if executeQueries {
				out.Step("Executing dataset queries...")
				queryStart := time.Now()

				execOpts := &dataset.ExecuteOptions{
					DataValidation:           dataset.DataValidationWarn,
					DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
					ContinueOnQueryError:     true,
				}
				results, warnings, err := dataset.Execute(ctx, projectRoot, documents, execOpts)
				if err != nil {
					out.StepDone("Query execution failed", time.Since(queryStart))
					out.Warning(fmt.Sprintf("Query execution error: %v", err))
				} else {
					out.StepDone(fmt.Sprintf("Executed %d dataset(s)", len(results)), time.Since(queryStart))
					dataValidationWarnings = warnings
				}
			}

			// Print schema validation issues
			if len(schemaFindings) > 0 {
				out.Blank()
				out.Warning(fmt.Sprintf("Found %d schema validation issue(s):", len(schemaFindings)))
				printFindingLines(out, projectRoot, schemaFindings, runner)
			}

			// Only severities the project set in bino.toml carry weight: "error"
			// fails on its own, "info" is exempt from --fail-on-warnings. Rules
			// that emit "error" natively stay advisory, as they always were. The
			// printed report and the exit code read the same counts, so the body
			// can never contradict the exit.
			errCount, warnCount, infoCount := countByLintSeverity(runner, findings)

			// Print lint findings
			if len(findings) > 0 {
				out.Blank()
				out.Warning(lintFindingSummary(errCount, warnCount, infoCount))
				printFindingLines(out, projectRoot, findings, runner)
			} else {
				out.Blank()
				out.Done("No lint warnings found")
			}

			// Print data validation warnings
			if len(dataValidationWarnings) > 0 {
				out.Blank()
				out.Warning(fmt.Sprintf("Found %d data validation warning(s):", len(dataValidationWarnings)))
				for _, w := range dataValidationWarnings {
					out.List(fmt.Sprintf("[data-validation] %s: %s", w.DataSet, w.Message))
				}
			}

			// Build output directory
			outputDir := pipeline.ResolveOutputDir(projectRoot, outDir)
			if err := pathutil.EnsureDir(outputDir); err != nil {
				logger.Warnf("failed to create output directory: %v", err)
			}

			// Write lint log (schema issues first, then lint findings)
			logFindings := append(append([]lint.Finding{}, schemaFindings...), findings...)
			logPath := filepath.Join(outputDir, fmt.Sprintf("bino-lint-%s.log", shortRunID))
			if err := writeLintLog(logPath, runID, startTime, projectRoot, documents, logFindings); err != nil {
				logger.Warnf("failed to write lint log: %v", err)
			}

			// Write JSON lint log if requested
			if lintLogFormat == "json" {
				jsonLogPath := filepath.Join(outputDir, fmt.Sprintf("bino-lint-%s.json", shortRunID))
				if err := writeLintJSONLog(jsonLogPath, runID, startTime, projectRoot, documents, logFindings); err != nil {
					logger.Warnf("failed to write JSON lint log: %v", err)
				}
			}

			out.Blank()
			out.Done("Lint complete")

			// Engine version incompatibility is a runtime-fatal condition; force
			// a non-zero exit regardless of --fail-on-warnings.
			if compatFatal {
				return RuntimeErrorf("engine-version-incompatible")
			}

			// Manifests that failed to load stay fatal even when [lint] hides
			// their findings: lint must never report success on a bundle it
			// could not read. The count is the pre-filter one, so it is honest.
			if len(loadErrors) > 0 {
				return ConfigErrorf("schema validation failed: %d issue(s) in %d document(s)", len(rawSchemaFindings), len(loadErrors))
			}

			if errCount > 0 {
				return RuntimeErrorf("lint found %d error(s)", errCount)
			}

			totalWarnings := warnCount + len(dataValidationWarnings)
			if failOnWarnings && totalWarnings > 0 {
				return RuntimeErrorf("lint found %d warning(s)", totalWarnings)
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory containing report manifests")
	cmd.Flags().StringVar(&outDir, "out-dir", "dist", "Directory (relative to --work-dir) for lint logs")
	cmd.Flags().StringVar(&lintLogFormat, "lint-log-format", "text", "Lint log file format: 'text' for human-readable or 'json' for machine-parseable")
	cmd.Flags().BoolVar(&executeQueries, "execute-queries", false,
		"Execute dataset queries and validate data (slower but catches data issues)")
	cmd.Flags().BoolVar(&failOnWarnings, "fail-on-warnings", false,
		"Exit with non-zero code if any warnings are found (useful for CI)")

	return cmd
}

// schemaFindingsFromErrors converts manifest load errors collected by
// config.LoadDirWithOptions into findings, one per schema issue, so that
// every issue is listed (and logged) alongside lint warnings.
func schemaFindingsFromErrors(loadErrors []error) []lint.Finding {
	var findings []lint.Finding
	for _, err := range loadErrors {
		var schemaErr *spec.SchemaValidationError
		if errors.As(err, &schemaErr) {
			for _, se := range schemaErr.Errors {
				findings = append(findings, lint.Finding{
					RuleID:  "schema-validation",
					File:    schemaErr.File,
					DocIdx:  schemaErr.DocPosition,
					Path:    se.Field,
					Line:    se.Line,
					Column:  se.Column,
					Message: se.Description,
				})
			}
			continue
		}
		findings = append(findings, lint.Finding{
			RuleID:  "manifest-load",
			Message: err.Error(),
		})
	}
	return findings
}

// countByLintSeverity splits findings by the severity the project set in
// bino.toml's [lint] table. A rule without an override counts as a warning
// whatever severity it emits natively — that is what the exit code has always
// keyed on.
func countByLintSeverity(runner *lint.Runner, findings []lint.Finding) (errCount, warnCount, infoCount int) {
	for _, f := range findings {
		switch runner.SeverityOverride(f.RuleID) {
		case "error":
			errCount++
		case "info":
			infoCount++
		default:
			warnCount++
		}
	}
	return errCount, warnCount, infoCount
}

// lintFindingSummary renders the count line above the finding list. Without a
// [lint] table every finding is a warning and the wording is the historic one.
func lintFindingSummary(errCount, warnCount, infoCount int) string {
	if errCount == 0 && infoCount == 0 {
		return fmt.Sprintf("Found %d lint warning(s):", warnCount)
	}
	parts := make([]string, 0, 3)
	if errCount > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s)", errCount))
	}
	if warnCount > 0 {
		parts = append(parts, fmt.Sprintf("%d warning(s)", warnCount))
	}
	if infoCount > 0 {
		parts = append(parts, fmt.Sprintf("%d info", infoCount))
	}
	return "Found " + strings.Join(parts, ", ") + " lint finding(s):"
}

// printFindingLines prints findings one per line with file:line:col locations.
// A rule the project re-graded in [lint] severity leads with that severity;
// without a [lint] table the line is byte-for-byte what it always was.
func printFindingLines(out *Output, projectRoot string, findings []lint.Finding, runner *lint.Runner) {
	for _, f := range findings {
		relPath := pathutil.RelPath(projectRoot, f.File)
		loc := relPath
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d:%d", relPath, f.Line, f.Column)
		} else if f.DocIdx > 0 {
			loc = fmt.Sprintf("%s #%d", relPath, f.DocIdx)
		}
		if f.Path != "" {
			loc = fmt.Sprintf("%s (%s)", loc, f.Path)
		}
		line := fmt.Sprintf("[%s] %s: %s", f.RuleID, loc, f.Message)
		if sev := runner.SeverityOverride(f.RuleID); sev != "" {
			line = fmt.Sprintf("[%s] %s", sev, line)
		}
		out.List(line)
	}
}

// findingsToLintEntries converts lint findings to build log lint entries.
func findingsToLintEntries(findings []lint.Finding) []buildlog.LintEntry {
	entries := make([]buildlog.LintEntry, 0, len(findings))
	for _, f := range findings {
		entries = append(entries, buildlog.BuildLintEntry(
			f.RuleID, f.Severity, f.Message, f.File, f.DocIdx, f.Path, f.Line, f.Column,
		))
	}
	return entries
}

// writeLintLog writes a human-readable lint log file.
func writeLintLog(path, runID string, startTime time.Time, workdir string, docs []config.Document, findings []lint.Finding) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create lint log: %w", err)
	}

	fmt.Fprintf(file, "BINO LINT LOG\n")
	fmt.Fprintf(file, "=============\n\n")
	fmt.Fprintf(file, "Run ID:     %s\n", runID)
	fmt.Fprintf(file, "Started:    %s\n", startTime.Format(time.RFC3339))
	fmt.Fprintf(file, "Completed:  %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(file, "Duration:   %s\n", time.Since(startTime).Round(time.Millisecond))
	fmt.Fprintf(file, "Workdir:    %s\n\n", workdir)

	fmt.Fprintf(file, "DOCUMENTS (%d)\n", len(docs))
	fmt.Fprintf(file, "--------------\n")
	for _, doc := range docs {
		relPath := pathutil.RelPath(workdir, doc.File)
		fmt.Fprintf(file, "  - %s #%d: kind=%s name=%s\n", relPath, doc.Position, doc.Kind, doc.Name)
	}
	fmt.Fprintln(file)

	header := "LINT WARNINGS"
	for _, f := range findings {
		if f.Severity != "" && f.Severity != "warning" {
			header = "LINT FINDINGS"
			break
		}
	}
	fmt.Fprintf(file, "%s (%d)\n", header, len(findings))
	fmt.Fprintf(file, "------------------\n")
	if len(findings) == 0 {
		fmt.Fprintln(file, "  (none)")
	} else {
		for _, f := range findings {
			relPath := pathutil.RelPath(workdir, f.File)
			loc := relPath
			if f.DocIdx > 0 {
				loc = fmt.Sprintf("%s #%d", relPath, f.DocIdx)
			}
			if f.Path != "" {
				loc = fmt.Sprintf("%s (%s)", loc, f.Path)
			}
			fmt.Fprintf(file, "  - [%s] %s: %s\n", f.RuleID, loc, f.Message)
		}
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close lint log %s: %w", path, err)
	}
	return nil
}

// LintJSONLog represents the JSON structure for lint-only logs.
type LintJSONLog struct {
	RunID      string                   `json:"run_id"`
	Started    time.Time                `json:"started"`
	Completed  time.Time                `json:"completed"`
	DurationMs int64                    `json:"duration_ms"`
	Workdir    string                   `json:"workdir"`
	Documents  []buildlog.DocumentEntry `json:"documents"`
	Lint       []buildlog.LintEntry     `json:"lint"`
}

// writeLintJSONLog writes a JSON lint log file.
func writeLintJSONLog(path, runID string, startTime time.Time, workdir string, docs []config.Document, findings []lint.Finding) error {
	completedTime := time.Now()

	// Build document entries
	docEntries := make([]buildlog.DocumentEntry, 0, len(docs))
	for _, doc := range docs {
		docEntries = append(docEntries, buildlog.DocumentEntry{
			File:     doc.File,
			Position: doc.Position,
			Kind:     doc.Kind,
			Name:     doc.Name,
		})
	}

	log := &LintJSONLog{
		RunID:      runID,
		Started:    startTime,
		Completed:  completedTime,
		DurationMs: completedTime.Sub(startTime).Milliseconds(),
		Workdir:    workdir,
		Documents:  docEntries,
		Lint:       findingsToLintEntries(findings),
	}

	return writeJSON(path, log)
}

// writeJSON writes any value as indented JSON to a file.
func writeJSON(path string, v any) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create JSON file: %w", err)
	}

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	encErr := enc.Encode(v)
	closeErr := file.Close()
	if encErr != nil {
		return fmt.Errorf("encode JSON: %w", encErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close JSON file %s: %w", path, closeErr)
	}
	return nil
}

// printLintFindings prints lint findings to the output.
// This is a helper for use in build and preview commands.
func printLintFindings(out *Output, findings []lint.Finding, baseDir string) {
	if len(findings) == 0 {
		return
	}
	out.Blank()
	out.Warning(fmt.Sprintf("Lint warnings (%d):", len(findings)))
	for _, f := range findings {
		relPath := pathutil.RelPath(baseDir, f.File)
		loc := relPath
		if f.DocIdx > 0 {
			loc = fmt.Sprintf("%s #%d", relPath, f.DocIdx)
		}
		if f.Path != "" {
			loc = fmt.Sprintf("%s (%s)", loc, f.Path)
		}
		out.List(fmt.Sprintf("[%s] %s: %s", f.RuleID, loc, f.Message))
	}
}
