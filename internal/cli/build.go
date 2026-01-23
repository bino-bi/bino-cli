package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/playwright"
	previewhttp "bino.bi/bino/internal/preview/httpserver"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	reportgraph "bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/signing"
	"bino.bi/bino/internal/version"
	"bino.bi/bino/pkg/duckdb"
)

const componentReadyConsolePrefix = "componentRegisterIsRendered:"

// newBuildCommand creates the build subcommand.
// The build command respects context cancellation at multiple checkpoints:
//   - Before loading manifests
//   - Before building each artefact
//   - During datasource collection (queries)
//   - During PDF rendering via Playwright
//
// On cancellation, partial work is abandoned and resources are cleaned up.
func newBuildCommand() *cobra.Command {
	var (
		workdir   string
		outDir    string
		include   []string
		exclude   []string
		driverDir string
		browser   string
		noGraph   bool
		logSQL    bool
		noLint    bool

		// CSV embedding options
		embedDataCSV      bool
		embedDataMaxRows  int
		embedDataMaxBytes int
		embedDataBase64   bool
		embedDataRedact   bool

		// Build log format
		logFormat string

		// Detailed execution plan
		detailedExecutionPlan bool

		// Data validation
		dataValidation string
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Validate manifests and render report artefacts to PDF",
		Long: strings.TrimSpace(`Validate the manifest bundle, collect data, and render every ReportArtefact to PDF.
Tweak manifest scan limits via environment variables:
  - BNR_MAX_MANIFEST_FILES (default 500)
  - BNR_MAX_MANIFEST_DOCS (default 10 per file)
  - BNR_MAX_MANIFEST_BYTES (default 10 MB total)

Use --artefact/--exclude-artefact to control which metadata.name entries produce output.`),
		Example: strings.TrimSpace(`  bino build
  bino build --work-dir ./reports --artefact weekly --artefact monthly --out-dir dist`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("build")
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
			out.Header(fmt.Sprintf("BINO %s", version.Version))

			// Find project root (directory containing bino.toml)
			absDir, err := pipeline.ResolveProjectRoot(workdir)
			if err != nil {
				return ConfigError(err)
			}

			// Load project config for defaults
			projectCfg, cfgErr := pathutil.LoadProjectConfig(absDir)
			if cfgErr != nil {
				logger.Debugf("Could not load bino.toml defaults: %v", cfgErr)
				projectCfg = &pathutil.ProjectConfig{}
			}

			// Apply environment variables from TOML (actual env vars take precedence)
			projectCfg.Build.Env.Apply(func(key, tomlVal, envVal string) {
				out.Info(fmt.Sprintf("Environment variable %s overrides bino.toml (%q -> %q)", key, tomlVal, envVal))
			})

			// Resolve arguments with TOML defaults
			resolver := pathutil.NewArgResolver(cmd, projectCfg.Build.Args, func(format string, args ...any) {
				out.Info(fmt.Sprintf(format, args...))
			})

			outDir = resolver.ResolveString("out-dir", "out-dir", outDir)
			browser = resolver.ResolveString("browser", "browser", browser)
			driverDir = resolver.ResolveString("driver-dir", "driver-dir", driverDir)
			logFormat = resolver.ResolveString("log-format", "log-format", logFormat)
			noGraph = resolver.ResolveBool("no-graph", "no-graph", noGraph)
			noLint = resolver.ResolveBool("no-lint", "no-lint", noLint)
			logSQL = resolver.ResolveBool("log-sql", "log-sql", logSQL)
			embedDataCSV = resolver.ResolveBool("embed-data-csv", "embed-data-csv", embedDataCSV)
			embedDataMaxRows = resolver.ResolveInt("embed-data-max-rows", "embed-data-max-rows", embedDataMaxRows)
			embedDataMaxBytes = resolver.ResolveInt("embed-data-max-bytes", "embed-data-max-bytes", embedDataMaxBytes)
			embedDataBase64 = resolver.ResolveBool("embed-data-base64", "embed-data-base64", embedDataBase64)
			embedDataRedact = resolver.ResolveBool("embed-data-redact", "embed-data-redact", embedDataRedact)
			detailedExecutionPlan = resolver.ResolveBool("detailed-execution-plan", "detailed-execution-plan", detailedExecutionPlan)
			include = resolver.ResolveStringSlice("artefact", "artefact", include)
			exclude = resolver.ResolveStringSlice("exclude-artefact", "exclude-artefact", exclude)

			// Check for cancellation before starting expensive manifest loading
			if err := ctx.Err(); err != nil {
				return err
			}

			// Step 1: Load and validate manifests
			out.Step("Loading manifests...")
			loadStart := time.Now()
			documents, err := config.LoadDir(ctx, absDir)
			if err != nil {
				return ConfigError(err)
			}
			if len(documents) == 0 {
				return ConfigErrorf("no YAML documents found in %s", absDir)
			}

			// Fail build if any environment variables are unresolved
			if err := config.CheckMissingEnvVars(documents); err != nil {
				return ConfigError(err)
			}

			out.StepDone(fmt.Sprintf("Validated %d document(s)", len(documents)), time.Since(loadStart))

			// Show manifest summary
			out.Blank()
			out.Info("Manifest summary:")
			for _, doc := range documents {
				relPath := pathutil.RelPath(absDir, doc.File)
				out.ListColored(fmt.Sprintf("%s #%d", relPath, doc.Position), "kind", doc.Kind, "name", doc.Name)
			}
			out.Blank()

			// Run lint rules unless disabled
			var lintFindings []lint.Finding
			if !noLint {
				lintDocs := configDocsToLintDocs(documents)
				runner := lint.NewDefaultRunner()
				lintFindings = runner.Run(ctx, lintDocs)
				if len(lintFindings) > 0 {
					printLintFindings(out, lintFindings, absDir)
					out.Blank()
				}
			}

			artefacts, err := config.CollectArtefacts(documents)
			if err != nil {
				return ConfigError(err)
			}

			screenshotArtefacts, err := config.CollectScreenshotArtefacts(documents)
			if err != nil {
				return ConfigError(err)
			}

			documentArtefacts, err := config.CollectDocumentArtefacts(documents)
			if err != nil {
				return ConfigError(err)
			}

			if len(artefacts) == 0 && len(screenshotArtefacts) == 0 && len(documentArtefacts) == 0 {
				return ConfigErrorf("no ReportArtefact, ScreenshotArtefact, or DocumentArtefact manifests found in %s", absDir)
			}

			signingProfiles, err := config.CollectSigningProfiles(documents)
			if err != nil {
				return ConfigError(err)
			}

			// Validate that all include names exist in any artefact type
			if err := pipeline.ValidateAllArtefactNames(artefacts, screenshotArtefacts, documentArtefacts, include); err != nil {
				return ConfigError(err)
			}

			filterOpts := pipeline.FilterOptions{
				Include: include,
				Exclude: exclude,
			}
			selected := pipeline.FilterArtefacts(artefacts, filterOpts)
			selectedScreenshots := pipeline.FilterScreenshotArtefacts(screenshotArtefacts, filterOpts)
			selectedDocuments := pipeline.FilterDocumentArtefacts(documentArtefacts, filterOpts)

			if len(selected) == 0 && len(selectedScreenshots) == 0 && len(selectedDocuments) == 0 {
				return ConfigErrorf("no artefacts selected (check --artefact / --exclude-artefact)")
			}
			pipeline.LogArtefactWarnings(logger, selected)
			pipeline.LogDocumentArtefactWarnings(logger, selectedDocuments)

			if err := pipeline.EnsureSigningProfiles(selected, signingProfiles); err != nil {
				return ConfigError(err)
			}
			if err := pipeline.EnsureDocumentSigningProfiles(selectedDocuments, signingProfiles); err != nil {
				return ConfigError(err)
			}

			var graph *reportgraph.Graph
			if !noGraph {
				graph, err = reportgraph.Build(ctx, documents)
				if err != nil {
					return RuntimeError(err)
				}
			}

			outputDir := pipeline.ResolveOutputDir(absDir, outDir)
			if err := pathutil.EnsureDir(outputDir); err != nil {
				return RuntimeErrorf("create out dir %s: %w", outputDir, err)
			}

			cacheDir, err := previewCacheDir()
			if err != nil {
				return RuntimeError(err)
			}

			// Resolve template engine version
			engineVersion := projectCfg.EngineVersion
			engineVersionPinned := engineVersion != ""
			engineMgr, err := engine.NewManager()
			if err != nil {
				return RuntimeError(fmt.Errorf("initialize engine manager: %w", err))
			}
			engineInfo, err := engineMgr.EnsureVersion(ctx, engineVersion)
			if err != nil {
				return ConfigError(fmt.Errorf("template engine: %w", err))
			}
			engineVersion = engineInfo.Version
			out.Info(fmt.Sprintf("Using template engine %s", engineVersion))

			// Track build warnings for logs
			var buildWarnings []string
			if !engineVersionPinned {
				warnMsg := "No engine-version set in bino.toml - using latest local version. Pin a version for reproducible builds."
				out.Warning(warnMsg)
				buildWarnings = append(buildWarnings, warnMsg)
			}

			// Set up SQL query logger if --log-sql is enabled
			var executedQueries []string
			var queryLoggerMu sync.Mutex
			var queryLogger func(string)
			if logSQL {
				queryLogger = func(query string) {
					queryLoggerMu.Lock()
					executedQueries = append(executedQueries, query)
					queryLoggerMu.Unlock()

					// Always print to terminal when --log-sql is enabled
					if logx.DebugEnabled(ctx) {
						// Verbose mode: print with extra formatting
						out.Info(fmt.Sprintf("SQL query:\n%s", query))
					} else {
						// Normal mode: compact output
						out.Info(fmt.Sprintf("SQL: %s", strings.ReplaceAll(strings.TrimSpace(query), "\n", " ")))
					}
				}
			}

			// Set up CSV embedding options from CLI flags
			embedOpts := buildlog.EmbedOptions{
				Enable:   embedDataCSV,
				MaxRows:  embedDataMaxRows,
				MaxBytes: embedDataMaxBytes,
				Base64:   embedDataBase64,
				Redact:   embedDataRedact,
			}

			// Set up execution plan options
			planOpts := buildlog.ExecutionPlanOptions{
				Enabled: detailedExecutionPlan,
			}

			// Create execution plan if detailed tracking is enabled
			var execPlan *buildlog.ExecutionPlan
			if planOpts.Enabled {
				execPlan = buildlog.NewExecutionPlan()
			}

			// Set up query execution logger for detailed metadata collection
			var queryExecMetas []duckdb.QueryExecMeta
			var queryExecMu sync.Mutex
			var queryExecLogger duckdb.QueryExecLogger
			if embedDataCSV || detailedExecutionPlan {
				queryExecLogger = func(meta duckdb.QueryExecMeta) {
					queryExecMu.Lock()
					queryExecMetas = append(queryExecMetas, meta)
					queryExecMu.Unlock()
				}
			}

			// Mark variables as used (will be wired into build log in later steps)
			_ = planOpts
			_ = queryExecMetas
			_ = logFormat

			// Resolve data validation mode
			dataValidation = resolver.ResolveString("data-validation", "data-validation", dataValidation)
			dataValidationMode := dataset.DataValidationWarn // default
			switch dataValidation {
			case "fail":
				dataValidationMode = dataset.DataValidationFail
			case "warn":
				dataValidationMode = dataset.DataValidationWarn
			case "off":
				dataValidationMode = dataset.DataValidationOff
			default:
				return ConfigErrorf("invalid --data-validation value %q, expected 'fail', 'warn', or 'off'", dataValidation)
			}

			// Step 2: Build artefacts
			out.Step(fmt.Sprintf("Building %d artefact(s)...", len(selected)))

			results := make([]artefactResult, 0, len(selected))
			for _, artefact := range selected {
				// Check for cancellation before starting each artefact
				if err := ctx.Err(); err != nil {
					return err
				}

				var root *reportgraph.Node
				if graph != nil {
					node, ok := graph.ReportArtefactByName(artefact.Document.Name)
					if !ok {
						return RuntimeErrorf("graph: artefact node %s not found", artefact.Document.Name)
					}
					root = node
				}

				// Create spinner for this artefact
				spinner := NewSpinner(SpinnerConfig{
					Stdout:  cmd.OutOrStdout(),
					NoColor: logx.NoColorEnabled(ctx),
				})

				entry, err := buildArtefact(ctx, buildArtefactConfig{
					Logger:                   logger.Channel(artefact.Document.Name),
					Workdir:                  absDir,
					CacheDir:                 cacheDir,
					EngineVersion:            engineVersion,
					Docs:                     documents,
					Artefact:                 artefact,
					SigningProfiles:          signingProfiles,
					OutputDir:                outputDir,
					Browser:                  browser,
					DriverDir:                driverDir,
					Debug:                    logx.DebugEnabled(ctx),
					Graph:                    graph,
					GraphRoot:                root,
					GraphBase:                absDir,
					Spinner:                  spinner,
					QueryLogger:              queryLogger,
					QueryExecLogger:          queryExecLogger,
					EmbedOptions:             embedOpts,
					ExecutionPlan:            execPlan,
					DataValidation:           dataValidationMode,
					DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
				})
				if err != nil {
					policy := pipeline.ClassifyInvalidLayout(err, pipeline.RenderModeBuild)
					if policy.IsInvalidRoot {
						return ConfigError(err)
					}
					return RuntimeError(err)
				}
				results = append(results, entry)
			}

			// Step 3: Build screenshot artefacts (using pre-filtered selectedScreenshots)
			var screenshotResults []screenshotArtefactResult
			if len(selectedScreenshots) > 0 {
				out.Step(fmt.Sprintf("Capturing %d screenshot artefact(s)...", len(selectedScreenshots)))

				for _, ssArtefact := range selectedScreenshots {
					// Check for cancellation before starting each screenshot artefact
					if err := ctx.Err(); err != nil {
						return err
					}

					// Create spinner for this screenshot artefact
					spinner := NewSpinner(SpinnerConfig{
						Stdout:  cmd.OutOrStdout(),
						NoColor: logx.NoColorEnabled(ctx),
					})

					ssResults, err := buildScreenshotArtefact(ctx, buildScreenshotArtefactConfig{
						Logger:        logger.Channel(ssArtefact.Document.Name),
						Workdir:       absDir,
						CacheDir:      cacheDir,
						EngineVersion: engineVersion,
						Docs:          documents,
						Artefact:      ssArtefact,
						OutputDir:     outputDir,
						Browser:       browser,
						DriverDir:     driverDir,
						Debug:         logx.DebugEnabled(ctx),
						Spinner:       spinner,
						QueryLogger:   queryLogger,
					})
					if err != nil {
						return RuntimeError(err)
					}
					screenshotResults = append(screenshotResults, ssResults...)
				}
			}

			// Step 4: Build document artefacts (using pre-filtered selectedDocuments)
			var documentResults []documentArtefactResult
			if len(selectedDocuments) > 0 {
				out.Step(fmt.Sprintf("Building %d document artefact(s)...", len(selectedDocuments)))

				for _, docArtefact := range selectedDocuments {
					// Check for cancellation before starting each document artefact
					if err := ctx.Err(); err != nil {
						return err
					}

					// Create spinner for this document artefact
					spinner := NewSpinner(SpinnerConfig{
						Stdout:  cmd.OutOrStdout(),
						NoColor: logx.NoColorEnabled(ctx),
					})

					docResult, err := buildDocumentArtefact(ctx, buildDocumentArtefactConfig{
						Logger:          logger.Channel(docArtefact.Document.Name),
						Workdir:         absDir,
						CacheDir:        cacheDir,
						EngineVersion:   engineVersion,
						Artefact:        docArtefact,
						SigningProfiles: signingProfiles,
						OutputDir:       outputDir,
						Browser:         browser,
						DriverDir:       driverDir,
						Debug:           logx.DebugEnabled(ctx),
						Spinner:         spinner,
					})
					if err != nil {
						return RuntimeError(err)
					}
					documentResults = append(documentResults, docResult)
				}
			}

			// Write build log
			logPath := filepath.Join(outputDir, fmt.Sprintf("bino-build-%s.log", shortRunID))
			if err := writeBuildLog(logPath, runID, projectCfg.ReportID, engineVersion, startTime, absDir, documents, results, documentResults, executedQueries, buildWarnings); err != nil {
				logger.Warnf("failed to write build log: %v", err)
			}

			// Write JSON build log if requested or if CSV embedding is enabled
			var jsonLogPath string
			if logFormat == "json" || embedDataCSV {
				jsonLogPath = filepath.Join(outputDir, fmt.Sprintf("bino-build-%s.json", shortRunID))
				completedTime := time.Now()

				// Build document entries
				docEntries := make([]buildlog.DocumentEntry, 0, len(documents))
				for _, doc := range documents {
					docEntries = append(docEntries, buildlog.DocumentEntry{
						File:     doc.File,
						Position: doc.Position,
						Kind:     doc.Kind,
						Name:     doc.Name,
					})
				}

				// Build artefact entries
				artefactEntries := make([]buildlog.ArtefactEntry, 0, len(results))
				for _, res := range results {
					artefactEntries = append(artefactEntries, buildlog.ArtefactEntry{
						Name:  res.Name,
						PDF:   res.PDFPath,
						Graph: res.GraphPath,
					})
				}

				// Build query entries from collected metadata
				queryEntries := make([]buildlog.QueryEntry, 0, len(queryExecMetas))
				for _, meta := range queryExecMetas {
					queryEntries = append(queryEntries, buildlog.BuildQueryEntry(meta, embedOpts))
				}

				// Get execution plan steps if enabled
				var planSteps []buildlog.ExecutionStep
				if execPlan != nil {
					planSteps = execPlan.GetSteps()
				}

				jsonLog := &buildlog.JSONBuildLog{
					RunID:         runID,
					ReportID:      projectCfg.ReportID,
					EngineVersion: engineVersion,
					Started:       startTime,
					Completed:     completedTime,
					DurationMs:    completedTime.Sub(startTime).Milliseconds(),
					Workdir:       absDir,
					Documents:     docEntries,
					Artefacts:     artefactEntries,
					Queries:       queryEntries,
					ExecutionPlan: planSteps,
					Lint:          findingsToLintEntries(lintFindings),
					Warnings:      buildWarnings,
				}

				if err := buildlog.WriteJSONBuildLog(jsonLogPath, jsonLog); err != nil {
					logger.Warnf("failed to write JSON build log: %v", err)
				}
			}

			// Print results with relative paths
			out.Blank()
			resultItems := make([]string, 0, len(results))
			style := StyleFromContext(ctx)
			for _, res := range results {
				// Make PDF path relative to workdir for cleaner output
				relPDFPath := pathutil.RelPath(absDir, res.PDFPath)
				item := fmt.Sprintf("%s %s %s", FormatName(res.Name), style.Dim.Sprint(SymbolArrow), FormatPath(relPDFPath))
				if res.GraphPath != "" {
					item += style.Dim.Sprintf(" (+graph)")
				}
				resultItems = append(resultItems, item)
			}
			out.Summary(fmt.Sprintf("Generated %d artefact(s):", len(results)), resultItems)

			// Print screenshot results if any
			if len(screenshotResults) > 0 {
				out.Blank()
				ssItems := make([]string, 0, len(screenshotResults))
				var ssErrors []string
				for _, res := range screenshotResults {
					if res.Error != nil {
						ssErrors = append(ssErrors, fmt.Sprintf("%s/%s: %v", res.RefKind, res.RefName, res.Error))
						continue
					}
					relPath := pathutil.RelPath(absDir, res.FilePath)
					item := fmt.Sprintf("%s/%s %s %s", res.RefKind, res.RefName, style.Dim.Sprint(SymbolArrow), FormatPath(relPath))
					ssItems = append(ssItems, item)
				}
				out.Summary(fmt.Sprintf("Generated %d screenshot(s):", len(ssItems)), ssItems)
				if len(ssErrors) > 0 {
					for _, errMsg := range ssErrors {
						out.Warning(fmt.Sprintf("Screenshot error: %s", errMsg))
					}
				}
			}

			// Print document artefact results if any
			if len(documentResults) > 0 {
				out.Blank()
				docItems := make([]string, 0, len(documentResults))
				for _, res := range documentResults {
					relPath := pathutil.RelPath(absDir, res.PDFPath)
					item := fmt.Sprintf("%s %s %s", res.Name, style.Dim.Sprint(SymbolArrow), FormatPath(relPath))
					docItems = append(docItems, item)
				}
				out.Summary(fmt.Sprintf("Generated %d document(s):", len(documentResults)), docItems)
			}

			// Print final success
			out.Done("Build complete")

			// Show log file location in verbose mode
			if logx.DebugEnabled(ctx) {
				out.Info(fmt.Sprintf("Build log: %s", logPath))
				if jsonLogPath != "" {
					out.Info(fmt.Sprintf("JSON build log: %s", jsonLogPath))
				}
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory containing report manifests")
	cmd.Flags().StringVar(&outDir, "out-dir", "dist", "Directory (relative to --work-dir) for generated artefacts")
	cmd.Flags().StringSliceVar(&include, "artefact", nil, "metadata.name entries to build (default: all)")
	cmd.Flags().StringSliceVar(&exclude, "exclude-artefact", nil, "metadata.name entries to skip")
	cmd.Flags().StringVar(&driverDir, "driver-dir", "", "Override the Playwright driver cache directory")
	cmd.Flags().StringVar(&browser, "browser", "chromium", "Browser engine for PDF export (chromium, firefox, webkit)")
	cmd.Flags().BoolVar(&noGraph, "no-graph", false, "Skip writing .bngraph dependency summaries next to PDFs")
	cmd.Flags().BoolVar(&noLint, "no-lint", false, "Skip running lint rules")
	cmd.Flags().BoolVar(&logSQL, "log-sql", false, "Log all executed SQL queries to terminal and build log")

	// CSV embedding flags - WARNING: enabling may include sensitive data in build logs
	cmd.Flags().BoolVar(&embedDataCSV, "embed-data-csv", false,
		"Enable embedding CSV data samples in build log (SECURITY: may expose sensitive data)")
	cmd.Flags().IntVar(&embedDataMaxRows, "embed-data-max-rows", 10,
		"Maximum rows to embed per query when CSV embedding is enabled")
	cmd.Flags().IntVar(&embedDataMaxBytes, "embed-data-max-bytes", 65536,
		"Maximum bytes per embedded CSV (data is truncated if exceeded)")
	cmd.Flags().BoolVar(&embedDataBase64, "embed-data-base64", true,
		"Base64 encode embedded CSV data for safe transport")
	cmd.Flags().BoolVar(&embedDataRedact, "embed-data-redact", true,
		"Redact values in columns matching sensitive patterns (password, token, key, etc.)")

	// Build log format
	cmd.Flags().StringVar(&logFormat, "log-format", "text",
		"Build log format: 'text' for human-readable or 'json' for machine-parseable")

	// Detailed execution plan
	cmd.Flags().BoolVar(&detailedExecutionPlan, "detailed-execution-plan", false,
		"Enable detailed step-by-step timing in build log for performance analysis")

	// Data validation
	cmd.Flags().StringVar(&dataValidation, "data-validation", "warn",
		"Data validation mode: 'fail' treats errors as fatal, 'warn' logs and continues, 'off' skips validation")

	return cmd
}

type artefactResult struct {
	Name      string
	PDFPath   string
	GraphPath string
}

type buildArtefactConfig struct {
	Logger                   logx.Logger
	Workdir                  string
	CacheDir                 string
	EngineVersion            string
	Docs                     []config.Document
	Artefact                 config.Artefact
	SigningProfiles          map[string]config.SigningProfile
	OutputDir                string
	Browser                  string
	DriverDir                string
	Debug                    bool
	Graph                    *reportgraph.Graph
	GraphRoot                *reportgraph.Node
	GraphBase                string
	Spinner                  *Spinner
	QueryLogger              func(string)
	QueryExecLogger          duckdb.QueryExecLogger
	EmbedOptions             buildlog.EmbedOptions
	ExecutionPlan            *buildlog.ExecutionPlan
	DataValidation           dataset.DataValidationMode
	DataValidationSampleSize int
}

// buildArtefact renders a single report artefact to PDF.
// It respects context cancellation at the following checkpoints:
//   - Function entry
//   - Before starting the ephemeral server
//   - Before generating the PDF
//   - Before signing (if configured)
//
// The ephemeral server is automatically shut down on cancellation or completion.
func buildArtefact(ctx context.Context, cfg buildArtefactConfig) (artefactResult, error) {
	if err := ctx.Err(); err != nil {
		return artefactResult{}, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logx.Nop()
	}

	spinner := cfg.Spinner
	artefactName := cfg.Artefact.Document.Name

	// Start spinner for HTML rendering
	if spinner != nil {
		spinner.Start(fmt.Sprintf("Rendering %s", artefactName))
	}
	logger.Debugf("Rendering HTML for %s", artefactName)

	renderResult, err := pipeline.RenderArtefactHTML(ctx, cfg.Workdir, cfg.Docs, cfg.Artefact, pipeline.RenderArtefactOptions{
		EngineVersion:            cfg.EngineVersion,
		QueryLogger:              cfg.QueryLogger,
		QueryExecLogger:          cfg.QueryExecLogger,
		EmbedOptions:             cfg.EmbedOptions,
		ExecutionPlan:            cfg.ExecutionPlan,
		DataValidation:           cfg.DataValidation,
		DataValidationSampleSize: cfg.DataValidationSampleSize,
	})
	pipeline.LogDiagnostics(logger.Channel("datasource"), renderResult.Diagnostics)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to render %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artefact %s: %w", artefactName, err)
	}

	// Check for cancellation before starting the ephemeral server
	if err := ctx.Err(); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Cancelled %s", artefactName))
		}
		return artefactResult{}, err
	}

	server, err := startEphemeralServer(ctx, cfg.CacheDir, logger.Channel("server"), renderResult.HTML, pipeline.ConvertLocalAssets(renderResult.LocalAssets))
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to start server for %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artefact %s: %w", artefactName, err)
	}

	pdfFilename := cfg.Artefact.Spec.Filename
	if pdfFilename == "" {
		pdfFilename = artefactName + ".pdf"
	}
	pdfPath, err := pathutil.ResolveFilePath(cfg.OutputDir, pdfFilename)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to resolve PDF path for %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artefact %s: %w", artefactName, err)
	}

	// Update spinner for PDF generation
	if spinner != nil {
		spinner.Update(fmt.Sprintf("Generating PDF for %s", artefactName))
	}
	logger.Debugf("Generating PDF at %s", pdfPath)

	pdfOpts := playwright.PDFOptions{
		URL:                   server.URL(),
		PDFPath:               pdfPath,
		Browser:               cfg.Browser,
		DriverDirectory:       cfg.DriverDir,
		Format:                cfg.Artefact.Spec.Format,
		Orientation:           cfg.Artefact.Spec.Orientation,
		Timeout:               2 * time.Minute,
		Debug:                 cfg.Debug,
		WaitForComponentReady: true,
		ReadyConsolePrefix:    componentReadyConsolePrefix,
	}
	pdfErr := playwright.RenderPDF(ctx, pdfOpts)
	closeErr := server.Close()
	if pdfErr != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to generate PDF for %s", artefactName))
		}
		if closeErr != nil {
			logger.Warnf("server shutdown error: %v", closeErr)
		}
		return artefactResult{}, fmt.Errorf("artefact %s: %w", artefactName, pdfErr)
	}
	if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Server error for %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artefact %s: stop server: %w", artefactName, closeErr)
	}

	graphPath, err := writeGraphReport(cfg.Graph, cfg.GraphRoot, pdfPath, cfg.GraphBase)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to write graph for %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artefact %s: %w", artefactName, err)
	}

	// Check for cancellation before signing
	if err := ctx.Err(); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Cancelled %s", artefactName))
		}
		return artefactResult{}, err
	}

	if ref := strings.TrimSpace(cfg.Artefact.Spec.SigningProfile); ref != "" {
		if spinner != nil {
			spinner.Update(fmt.Sprintf("Signing %s", artefactName))
		}
		profile, ok := cfg.SigningProfiles[ref]
		if !ok {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Signing profile missing for %s", artefactName))
			}
			return artefactResult{}, fmt.Errorf("artefact %s: signing profile %s missing", artefactName, ref)
		}
		logger.Debugf("Signing PDF %s with profile %s", pdfPath, ref)
		if err := signing.Apply(ctx, profile, pdfPath); err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Failed to sign %s", artefactName))
			}
			return artefactResult{}, fmt.Errorf("artefact %s: %w", artefactName, err)
		}
	}

	// Success
	if spinner != nil {
		spinner.Stop()
	}
	return artefactResult{Name: artefactName, PDFPath: pdfPath, GraphPath: graphPath}, nil
}

func writeGraphReport(g *reportgraph.Graph, root *reportgraph.Node, pdfPath, base string) (string, error) {
	if g == nil || root == nil || pdfPath == "" {
		return "", nil
	}
	graphPath := pathutil.ResolveGraphPath(pdfPath)
	file, err := os.Create(graphPath)
	if err != nil {
		return "", fmt.Errorf("create graph file %s: %w", graphPath, err)
	}
	printGraphFlat(file, g, []*reportgraph.Node{root}, base)
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close graph file %s: %w", graphPath, err)
	}
	return graphPath, nil
}

type documentArtefactResult struct {
	Name    string
	PDFPath string
}

type buildDocumentArtefactConfig struct {
	Logger          logx.Logger
	Workdir         string
	CacheDir        string
	EngineVersion   string
	Artefact        config.DocumentArtefact
	SigningProfiles map[string]config.SigningProfile
	OutputDir       string
	Browser         string
	DriverDir       string
	Debug           bool
	Spinner         *Spinner
}

// buildDocumentArtefact renders a DocumentArtefact (markdown to PDF) using Playwright.
// It converts markdown files to HTML, serves them via an ephemeral server, and captures a PDF.
func buildDocumentArtefact(ctx context.Context, cfg buildDocumentArtefactConfig) (documentArtefactResult, error) {
	if err := ctx.Err(); err != nil {
		return documentArtefactResult{}, err
	}

	logger := cfg.Logger
	artefact := cfg.Artefact
	artefactName := artefact.Document.Name
	spec := artefact.Spec
	spinner := cfg.Spinner

	if spinner != nil {
		spinner.Start(fmt.Sprintf("Building document %s", artefactName))
	}

	// Render markdown to HTML
	renderResult, err := pipeline.RenderDocumentArtefactHTML(ctx, cfg.Workdir, artefact, pipeline.DocumentArtefactRenderOptions{
		EngineVersion: cfg.EngineVersion,
	})
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to render %s", artefactName))
		}
		return documentArtefactResult{}, err
	}

	// Determine output filename
	filename := spec.Filename
	if filename == "" {
		filename = artefactName + ".pdf"
	}
	pdfPath := filepath.Join(cfg.OutputDir, filename)

	// Start ephemeral server
	ephem, err := startEphemeralServer(ctx, cfg.CacheDir, logger.Channel("server"), renderResult.HTML, nil)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to start server for %s", artefactName))
		}
		return documentArtefactResult{}, fmt.Errorf("document artefact %s: %w", artefactName, err)
	}
	defer ephem.Close()

	// Check for cancellation before generating PDF
	if err := ctx.Err(); err != nil {
		return documentArtefactResult{}, err
	}

	if spinner != nil {
		spinner.Update(fmt.Sprintf("Generating PDF for %s", artefactName))
	}

	// Generate PDF using Playwright
	pdfOpts := playwright.PDFOptions{
		URL:             ephem.URL(),
		PDFPath:         pdfPath,
		Format:          spec.Format,
		Orientation:     spec.Orientation,
		DriverDirectory: cfg.DriverDir,
		Browser:         cfg.Browser,
		Debug:           cfg.Debug,
		Timeout:         2 * time.Minute,
	}
	// Header/footer support
	if spec.DisplayHeaderFooter {
		pdfOpts.DisplayHeaderFooter = true
		pdfOpts.HeaderTemplate = spec.HeaderTemplate
		pdfOpts.FooterTemplate = spec.FooterTemplate
		pdfOpts.MarginTop = spec.MarginTop
		pdfOpts.MarginBottom = spec.MarginBottom
		// Use default templates if not specified
		if pdfOpts.HeaderTemplate == "" {
			pdfOpts.HeaderTemplate = buildDefaultDocumentHeader(spec.Title)
		}
		if pdfOpts.FooterTemplate == "" {
			pdfOpts.FooterTemplate = buildDefaultDocumentFooter()
		}
	}
	if err := playwright.RenderPDF(ctx, pdfOpts); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to generate PDF for %s", artefactName))
		}
		return documentArtefactResult{}, fmt.Errorf("document artefact %s: %w", artefactName, err)
	}

	// Check for cancellation before signing
	if err := ctx.Err(); err != nil {
		return documentArtefactResult{}, err
	}

	// Apply digital signature if configured
	ref := strings.TrimSpace(spec.SigningProfile)
	if ref != "" {
		if spinner != nil {
			spinner.Update(fmt.Sprintf("Signing %s", artefactName))
		}
		profile, ok := cfg.SigningProfiles[ref]
		if !ok {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Signing profile not found for %s", artefactName))
			}
			return documentArtefactResult{}, fmt.Errorf("document artefact %s: signing profile %s missing", artefactName, ref)
		}
		logger.Debugf("Signing PDF %s with profile %s", pdfPath, ref)
		if err := signing.Apply(ctx, profile, pdfPath); err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Failed to sign %s", artefactName))
			}
			return documentArtefactResult{}, fmt.Errorf("document artefact %s: %w", artefactName, err)
		}
	}

	// Success
	if spinner != nil {
		spinner.Stop()
	}
	return documentArtefactResult{Name: artefactName, PDFPath: pdfPath}, nil
}

// buildDefaultDocumentHeader creates the default header template for DocumentArtefact PDFs.
// The header displays the document title centered at the top.
// Playwright header/footer templates use special CSS classes for dynamic content.
func buildDefaultDocumentHeader(title string) string {
	escapedTitle := title
	// Basic HTML escaping for the title
	escapedTitle = strings.ReplaceAll(escapedTitle, "&", "&amp;")
	escapedTitle = strings.ReplaceAll(escapedTitle, "<", "&lt;")
	escapedTitle = strings.ReplaceAll(escapedTitle, ">", "&gt;")
	escapedTitle = strings.ReplaceAll(escapedTitle, "\"", "&quot;")
	return `<div style="width: 100%; font-size: 10px; font-family: Arial, sans-serif; text-align: center; color: #333;">` + escapedTitle + `</div>`
}

// buildDefaultDocumentFooter creates the default footer template for DocumentArtefact PDFs.
// The footer displays the date on the left and page number on the right.
// Playwright footer templates use special CSS classes:
// - "date" class shows the formatted print date
// - "pageNumber" class shows the current page number
// - "totalPages" class shows the total number of pages
func buildDefaultDocumentFooter() string {
	return `<div style="width: 100%; font-size: 9px; font-family: Arial, sans-serif; padding: 0 10mm; display: flex; justify-content: space-between; color: #666;">
  <span class="date"></span>
  <span>Page <span class="pageNumber"></span> of <span class="totalPages"></span></span>
</div>`
}

// ephemeralServer wraps an HTTP server used for PDF rendering.
// It manages a child context that can be canceled independently.
type ephemeralServer struct {
	server *previewhttp.Server
	cancel context.CancelFunc
	errCh  chan error
}

// startEphemeralServer creates and starts a temporary HTTP server for PDF rendering.
// The server runs in a goroutine and is stopped when Close() is called or when
// the parent context is canceled. The child context isolates the server's lifecycle
// from the parent context, allowing controlled shutdown.
func startEphemeralServer(ctx context.Context, cacheDir string, logger logx.Logger, html []byte, assets []previewhttp.LocalAsset) (*ephemeralServer, error) {
	// Check for cancellation before allocating resources
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	srv, err := previewhttp.New(previewhttp.Config{
		ListenAddr: "127.0.0.1:0",
		CacheDir:   cacheDir,
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}
	srv.SetLocalAssets(assets)
	srv.SetContentFunc(previewhttp.StaticContent(append([]byte(nil), html...), "text/html; charset=utf-8"))
	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(runCtx)
	}()
	return &ephemeralServer{server: srv, cancel: cancel, errCh: errCh}, nil
}

func (s *ephemeralServer) URL() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.URL()
}

func (s *ephemeralServer) Close() error {
	if s == nil {
		return nil
	}
	s.cancel()
	select {
	case err := <-s.errCh:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("server shutdown timed out")
	}
}

// writeBuildLog writes a detailed build log file with run information.
func writeBuildLog(path, runID, reportID, engineVersion string, startTime time.Time, workdir string, docs []config.Document, results []artefactResult, docResults []documentArtefactResult, sqlQueries []string, warnings []string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create build log: %w", err)
	}
	defer file.Close()

	fmt.Fprintf(file, "BINO BUILD LOG\n")
	fmt.Fprintf(file, "==============\n\n")
	fmt.Fprintf(file, "Run ID:         %s\n", runID)
	fmt.Fprintf(file, "Report ID:      %s\n", reportID)
	fmt.Fprintf(file, "Engine Version: %s\n", engineVersion)
	fmt.Fprintf(file, "Started:        %s\n", startTime.Format(time.RFC3339))
	fmt.Fprintf(file, "Completed:      %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(file, "Duration:       %s\n", time.Since(startTime).Round(time.Millisecond))
	fmt.Fprintf(file, "Workdir:        %s\n", workdir)

	if len(warnings) > 0 {
		fmt.Fprintf(file, "\nWARNINGS (%d)\n", len(warnings))
		fmt.Fprintf(file, "-------------\n")
		for _, w := range warnings {
			fmt.Fprintf(file, "  - %s\n", w)
		}
	}
	fmt.Fprintln(file)

	fmt.Fprintf(file, "DOCUMENTS (%d)\n", len(docs))
	fmt.Fprintf(file, "--------------\n")
	for _, doc := range docs {
		fmt.Fprintf(file, "  - %s #%d: kind=%s name=%s\n", doc.File, doc.Position, doc.Kind, doc.Name)
	}
	fmt.Fprintln(file)

	fmt.Fprintf(file, "ARTEFACTS (%d)\n", len(results))
	fmt.Fprintf(file, "--------------\n")
	for _, res := range results {
		fmt.Fprintf(file, "  - %s\n", res.Name)
		fmt.Fprintf(file, "    PDF:   %s\n", res.PDFPath)
		if res.GraphPath != "" {
			fmt.Fprintf(file, "    Graph: %s\n", res.GraphPath)
		}
	}

	if len(docResults) > 0 {
		fmt.Fprintln(file)
		fmt.Fprintf(file, "DOCUMENT ARTEFACTS (%d)\n", len(docResults))
		fmt.Fprintf(file, "-----------------------\n")
		for _, res := range docResults {
			fmt.Fprintf(file, "  - %s\n", res.Name)
			fmt.Fprintf(file, "    PDF: %s\n", res.PDFPath)
		}
	}

	if len(sqlQueries) > 0 {
		fmt.Fprintln(file)
		fmt.Fprintf(file, "SQL QUERIES (%d)\n", len(sqlQueries))
		fmt.Fprintf(file, "----------------\n")
		for i, query := range sqlQueries {
			fmt.Fprintf(file, "\n-- Query %d --\n", i+1)
			fmt.Fprintf(file, "%s\n", query)
		}
	}

	return nil
}

type screenshotArtefactResult struct {
	ArtefactName string
	RefKind      string
	RefName      string
	FilePath     string
	Error        error
}

type buildScreenshotArtefactConfig struct {
	Logger        logx.Logger
	Workdir       string
	CacheDir      string
	EngineVersion string
	Docs          []config.Document
	Artefact      config.ScreenshotArtefact
	OutputDir     string
	Browser       string
	DriverDir     string
	Debug         bool
	Spinner       *Spinner
	QueryLogger   func(string)
}

// buildScreenshotArtefact captures screenshots of specific components.
// It renders the HTML containing the specified layout pages, starts an ephemeral
// server, and uses Playwright to capture screenshots of individual elements.
func buildScreenshotArtefact(ctx context.Context, cfg buildScreenshotArtefactConfig) ([]screenshotArtefactResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logx.Nop()
	}

	spinner := cfg.Spinner
	artefactName := cfg.Artefact.Document.Name

	// Start spinner for HTML rendering
	if spinner != nil {
		spinner.Start(fmt.Sprintf("Rendering %s", artefactName))
	}
	logger.Debugf("Rendering HTML for screenshot artefact %s", artefactName)

	renderResult, err := pipeline.RenderScreenshotArtefactHTML(ctx, cfg.Workdir, cfg.Docs, cfg.Artefact, pipeline.RenderScreenshotArtefactOptions{
		EngineVersion: cfg.EngineVersion,
		QueryLogger:   cfg.QueryLogger,
	})
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to render %s", artefactName))
		}
		return nil, fmt.Errorf("screenshot artefact %s: %w", artefactName, err)
	}

	// DEBUG: Write rendered HTML to file for inspection
	if err := os.WriteFile("/tmp/screenshot-debug.html", []byte(renderResult.HTML), 0644); err != nil {
		logger.Warnf("Failed to write debug HTML: %v", err)
	}

	// Check for cancellation before starting the ephemeral server
	if err := ctx.Err(); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Cancelled %s", artefactName))
		}
		return nil, err
	}

	server, err := startEphemeralServer(ctx, cfg.CacheDir, logger.Channel("server"), renderResult.HTML, pipeline.ConvertLocalAssets(renderResult.LocalAssets))
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to start server for %s", artefactName))
		}
		return nil, fmt.Errorf("screenshot artefact %s: %w", artefactName, err)
	}

	// Update spinner for screenshot capture
	if spinner != nil {
		spinner.Update(fmt.Sprintf("Capturing screenshots for %s", artefactName))
	}
	logger.Debugf("Capturing screenshots for %s", artefactName)

	// Convert config refs to playwright refs
	pwRefs := make([]playwright.ScreenshotRef, len(cfg.Artefact.Spec.Refs))
	for i, ref := range cfg.Artefact.Spec.Refs {
		pwRefs[i] = playwright.ScreenshotRef{
			Kind: ref.Kind,
			Name: ref.Name,
		}
	}

	screenshotOpts := playwright.ScreenshotOptions{
		URL:                   server.URL(),
		OutputDir:             cfg.OutputDir,
		Browser:               cfg.Browser,
		DriverDirectory:       cfg.DriverDir,
		Format:                cfg.Artefact.Spec.Format,
		Orientation:           cfg.Artefact.Spec.Orientation,
		Timeout:               2 * time.Minute,
		Debug:                 cfg.Debug,
		WaitForComponentReady: true,
		ReadyConsolePrefix:    componentReadyConsolePrefix,
		Refs:                  pwRefs,
		FilenamePrefix:        cfg.Artefact.Spec.FilenamePrefix,
		FilenamePattern:       cfg.Artefact.Spec.FilenamePattern,
		ImageFormat:           cfg.Artefact.Spec.ImageFormat,
		Quality:               cfg.Artefact.Spec.Quality,
		OmitBackground:        cfg.Artefact.Spec.OmitBackground,
		Scale:                 cfg.Artefact.Spec.Scale,
	}

	pwResults, screenshotErr := playwright.RenderScreenshots(ctx, screenshotOpts)
	closeErr := server.Close()

	if screenshotErr != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to capture screenshots for %s", artefactName))
		}
		if closeErr != nil {
			logger.Warnf("server shutdown error: %v", closeErr)
		}
		return nil, fmt.Errorf("screenshot artefact %s: %w", artefactName, screenshotErr)
	}
	if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Server error for %s", artefactName))
		}
		return nil, fmt.Errorf("screenshot artefact %s: stop server: %w", artefactName, closeErr)
	}

	// Convert playwright results to our result type
	results := make([]screenshotArtefactResult, len(pwResults))
	for i, r := range pwResults {
		results[i] = screenshotArtefactResult{
			ArtefactName: artefactName,
			RefKind:      r.Ref.Kind,
			RefName:      r.Ref.Name,
			FilePath:     r.FilePath,
			Error:        r.Error,
		}
	}

	// Success
	if spinner != nil {
		spinner.Stop()
	}
	return results, nil
}
