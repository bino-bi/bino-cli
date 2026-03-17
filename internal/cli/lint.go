package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/version"
)

// newLintCommand creates the lint subcommand.
// It loads and validates manifests, then runs lint rules and reports findings.
func newLintCommand() *cobra.Command {
	var (
		workdir        string
		outDir         string
		logFormat      string
		executeQueries bool
		failOnWarnings bool
	)

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate manifests and run lint rules without building",
		Long: strings.TrimSpace(`Load manifests, validate against the schema, and run lint rules.
This command does not execute queries or generate PDFs.

All lint findings are treated as warnings. The exit code is always 0 unless
there is a fatal error loading manifests.`),
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

			// Check for cancellation before starting expensive manifest loading
			if err := ctx.Err(); err != nil {
				return err
			}

			// Step 1: Load and validate manifests
			out.Step("Loading manifests...")
			loadStart := time.Now()
			documents, err := config.LoadDir(ctx, projectRoot)
			if err != nil {
				return ConfigError(err)
			}
			if len(documents) == 0 {
				return ConfigErrorf("no YAML documents found in %s", projectRoot)
			}

			out.StepDone(fmt.Sprintf("Validated %d document(s)", len(documents)), time.Since(loadStart))

			// Convert config.Document to lint.Document
			lintDocs := configDocsToLintDocs(documents)

			// Step 2: Run lint rules
			out.Step("Running lint rules...")
			lintStart := time.Now()
			runner := lint.NewDefaultRunner()
			findings := runner.Run(ctx, lintDocs)
			out.StepDone(fmt.Sprintf("Checked %d rule(s)", len(runner.Rules())), time.Since(lintStart))

			// Step 3: Execute queries and validate data (optional)
			var dataValidationWarnings []dataset.Warning
			if executeQueries {
				out.Step("Executing dataset queries...")
				queryStart := time.Now()

				execOpts := &dataset.ExecuteOptions{
					DataValidation:           dataset.DataValidationWarn,
					DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
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

			// Print lint findings
			if len(findings) > 0 {
				out.Blank()
				out.Warning(fmt.Sprintf("Found %d lint warning(s):", len(findings)))
				for _, f := range findings {
					relPath := pathutil.RelPath(projectRoot, f.File)
					loc := relPath
					if f.DocIdx > 0 {
						loc = fmt.Sprintf("%s #%d", relPath, f.DocIdx)
					}
					if f.Path != "" {
						loc = fmt.Sprintf("%s (%s)", loc, f.Path)
					}
					out.List(fmt.Sprintf("[%s] %s: %s", f.RuleID, loc, f.Message))
				}
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

			// Write lint log
			logPath := filepath.Join(outputDir, fmt.Sprintf("bino-lint-%s.log", shortRunID))
			if err := writeLintLog(logPath, runID, startTime, projectRoot, documents, findings); err != nil {
				logger.Warnf("failed to write lint log: %v", err)
			}

			// Write JSON lint log if requested
			if logFormat == "json" {
				jsonLogPath := filepath.Join(outputDir, fmt.Sprintf("bino-lint-%s.json", shortRunID))
				if err := writeLintJSONLog(jsonLogPath, runID, startTime, projectRoot, documents, findings); err != nil {
					logger.Warnf("failed to write JSON lint log: %v", err)
				}
			}

			out.Blank()
			out.Done("Lint complete")

			// Exit with error if --fail-on-warnings and there are warnings
			totalWarnings := len(findings) + len(dataValidationWarnings)
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
	cmd.Flags().StringVar(&logFormat, "log-format", "text", "Lint log format: 'text' for human-readable or 'json' for machine-parseable")
	cmd.Flags().BoolVar(&executeQueries, "execute-queries", false,
		"Execute dataset queries and validate data (slower but catches data issues)")
	cmd.Flags().BoolVar(&failOnWarnings, "fail-on-warnings", false,
		"Exit with non-zero code if any warnings are found (useful for CI)")

	return cmd
}

// configDocsToLintDocs converts config.Document slice to lint.Document slice.
func configDocsToLintDocs(docs []config.Document) []lint.Document {
	lintDocs := make([]lint.Document, 0, len(docs))
	for _, d := range docs {
		lintDocs = append(lintDocs, lint.Document{
			File:        d.File,
			Position:    d.Position,
			Kind:        d.Kind,
			Name:        d.Name,
			Labels:      d.Labels,
			Constraints: d.Constraints,
			Raw:         d.Raw,
		})
	}
	return lintDocs
}

// findingsToLintEntries converts lint findings to build log lint entries.
func findingsToLintEntries(findings []lint.Finding) []buildlog.LintEntry {
	entries := make([]buildlog.LintEntry, 0, len(findings))
	for _, f := range findings {
		entries = append(entries, buildlog.BuildLintEntry(
			f.RuleID, f.Message, f.File, f.DocIdx, f.Path, f.Line, f.Column,
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
	defer file.Close()

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

	fmt.Fprintf(file, "LINT WARNINGS (%d)\n", len(findings))
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
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
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
