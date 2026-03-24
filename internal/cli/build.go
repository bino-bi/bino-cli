package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/hooks"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	reportgraph "bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
	"bino.bi/bino/internal/version"
	"bino.bi/bino/pkg/duckdb"
)

const componentReadyConsolePrefix = "componentRegisterIsRendered:"

// newBuildCommand creates the build subcommand.
// The build command respects context cancellation at multiple checkpoints:
//   - Before loading manifests
//   - Before building each artifact
//   - During datasource collection (queries)
//   - During PDF rendering via Chrome headless shell
//
// On cancellation, partial work is abandoned and resources are cleaned up.
func newBuildCommand() *cobra.Command {
	var (
		workdir    string
		outDir     string
		include    []string
		exclude    []string
		chromePath string
		noGraph    bool
		logSQL     bool
		noLint     bool

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
		Short: "Validate manifests and render report artifacts to PDF",
		Long: strings.TrimSpace(`Validate the manifest bundle, collect data, and render every ReportArtefact to PDF.
Tweak manifest scan limits via environment variables:
  - BNR_MAX_MANIFEST_FILES (default 500)
  - BNR_MAX_MANIFEST_DOCS (default 10 per file)
  - BNR_MAX_MANIFEST_BYTES (default 10 MB total)

Use --artifact/--exclude-artifact to control which metadata.name entries produce output.`),
		Example: strings.TrimSpace(`  bino build
  bino build --work-dir ./reports --artifact weekly --artifact monthly --out-dir dist`),
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

			env, err := initCommandEnv(ctx, cmd, workdir, "build", logger)
			if err != nil {
				return err
			}
			if env.PluginManager != nil {
				defer env.PluginManager.ShutdownAll(ctx)
			}

			outDir = env.Resolver.ResolveString("out-dir", "out-dir", outDir)
			chromePath = env.Resolver.ResolveString("chrome-path", "chrome-path", chromePath)
			logFormat = env.Resolver.ResolveString("log-format", "log-format", logFormat)
			noGraph = env.Resolver.ResolveBool("no-graph", "no-graph", noGraph)
			noLint = env.Resolver.ResolveBool("no-lint", "no-lint", noLint)
			logSQL = env.Resolver.ResolveBool("log-sql", "log-sql", logSQL)
			embedDataCSV = env.Resolver.ResolveBool("embed-data-csv", "embed-data-csv", embedDataCSV)
			embedDataMaxRows = env.Resolver.ResolveInt("embed-data-max-rows", "embed-data-max-rows", embedDataMaxRows)
			embedDataMaxBytes = env.Resolver.ResolveInt("embed-data-max-bytes", "embed-data-max-bytes", embedDataMaxBytes)
			embedDataBase64 = env.Resolver.ResolveBool("embed-data-base64", "embed-data-base64", embedDataBase64)
			embedDataRedact = env.Resolver.ResolveBool("embed-data-redact", "embed-data-redact", embedDataRedact)
			detailedExecutionPlan = env.Resolver.ResolveBool("detailed-execution-plan", "detailed-execution-plan", detailedExecutionPlan)
			include = env.Resolver.ResolveStringSlice("artifact", "artifact", include)
			exclude = env.Resolver.ResolveStringSlice("exclude-artifact", "exclude-artifact", exclude)

			// Check for cancellation before starting expensive manifest loading
			if err := ctx.Err(); err != nil {
				return err
			}

			// Step 1: Load, validate, and filter manifests
			var pluginLinters lint.PluginLinterRegistry
			if env.PluginRegistry != nil {
				pluginLinters = plugin.NewLinterRegistry(env.PluginRegistry)
			}
			manifests, err := loadBuildManifests(ctx, out, logger, env.ProjectRoot, include, exclude, noLint, noGraph, env.PluginRegistry, pluginLinters)
			if err != nil {
				return err
			}
			documents := manifests.Documents
			lintFindings := manifests.LintFindings

			outputDir := pipeline.ResolveOutputDir(env.ProjectRoot, outDir)
			if err := pathutil.EnsureDir(outputDir); err != nil {
				return RuntimeErrorf("create out dir %s: %w", outputDir, err)
			}

			// Track build warnings for logs
			var buildWarnings []string
			if !env.EngineVersionPinned {
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
			dataValidation = env.Resolver.ResolveString("data-validation", "data-validation", dataValidation)
			dataValidationMode, err := resolveDataValidationMode(dataValidation)
			if err != nil {
				return ConfigError(err)
			}

			// Run pre-build hooks
			buildHookEnv := hooks.HookEnv{
				Mode:      "build",
				Workdir:   env.ProjectRoot,
				ReportID:  env.ProjectCfg.ReportID,
				Verbose:   logx.DebugEnabled(ctx),
				OutputDir: outputDir,
				Include:   strings.Join(include, ","),
				Exclude:   strings.Join(exclude, ","),
			}
			if err := env.HookRunner.Run(ctx, "pre-build", buildHookEnv); err != nil {
				return RuntimeError(err)
			}

			// Set up plugin integration for the build pipeline.
			var pluginOpts *render.PluginOptions
			var postRenderHTMLHook func(context.Context, []byte) ([]byte, error)
			var postDatasetHook func(context.Context, []pipeline.DatasetPayload) error
			var hostSvc *plugin.BinoHostServer
			if env.PluginManager != nil {
				hostSvc = env.PluginManager.HostService()
			}
			if env.PluginRegistry != nil {
				pluginOpts = plugin.BuildRenderOptions(ctx, env.PluginRegistry, env.ProjectRoot, "build")
				hookBus := plugin.NewHookBus(env.PluginRegistry, logger.Channel("plugin-hooks"))
				postRenderHTMLHook = func(hookCtx context.Context, html []byte) ([]byte, error) {
					modified, _, err := hookBus.DispatchPostRenderHTML(hookCtx, html)
					return modified, err
				}
				postDatasetHook = func(hookCtx context.Context, datasets []pipeline.DatasetPayload) error {
					pluginDatasets := make([]plugin.DatasetPayload, len(datasets))
					for i, ds := range datasets {
						pluginDatasets[i] = plugin.DatasetPayload{Name: ds.Name, JSONRows: ds.JSONRows, Columns: ds.Columns}
					}
					if hostSvc != nil {
						hostSvc.SetDatasets(pluginDatasets)
					}
					_, _, err := hookBus.DispatchPostDatasetExecute(hookCtx, pluginDatasets)
					return err
				}
				if hostSvc != nil {
					hostSvc.SetDocuments(plugin.DocumentsFromConfig(manifests.Documents))
					hostSvc.SetDefaultDuckDBOpener()
				}
			}

			// Step 2: Build all artifacts
			builder := &pipeline.Builder{
				Workdir:                  env.ProjectRoot,
				EngineVersion:            env.EngineVersion,
				CacheDir:                 env.CacheDir,
				Logger:                   logger,
				QueryLogger:              queryLogger,
				QueryExecLogger:          queryExecLogger,
				EmbedOptions:             embedOpts,
				ExecutionPlan:            execPlan,
				DataValidation:           dataValidationMode,
				DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
				PluginOptions:            pluginOpts,
				PostRenderHTMLHook:       postRenderHTMLHook,
				PostDatasetHook:          postDatasetHook,
				KindProvider:             env.PluginRegistry,
			}

			buildResults, err := buildAllArtefacts(ctx, out, manifests, buildExecutionConfig{
				Builder:    builder,
				Logger:     logger,
				OutputDir:  outputDir,
				ChromePath: chromePath,
				Debug:      logx.DebugEnabled(ctx),
				NoColor:    logx.NoColorEnabled(ctx),
				Stdout:     cmd.OutOrStdout(),
				HookRunner: env.HookRunner,
				HookEnv:    buildHookEnv,
			})
			if err != nil {
				return err
			}
			results := buildResults.Reports
			screenshotResults := buildResults.Screenshots
			documentResults := buildResults.Documents

			// Run post-build hooks
			if err := env.HookRunner.Run(ctx, "post-build", buildHookEnv); err != nil {
				return RuntimeError(err)
			}

			// Write build log
			logPath := filepath.Join(outputDir, fmt.Sprintf("bino-build-%s.log", shortRunID))
			if err := writeBuildLog(logPath, runID, env.ProjectCfg.ReportID, env.EngineVersion, startTime, env.ProjectRoot, documents, results, documentResults, executedQueries, buildWarnings); err != nil {
				logger.Warnf("failed to write build log: %v", err)
			}

			// Write JSON build log if requested or if CSV embedding is enabled
			var jsonLogPath string
			if logFormat == "json" || embedDataCSV {
				jsonLogPath = filepath.Join(outputDir, fmt.Sprintf("bino-build-%s.json", shortRunID))
				jsonLog := assembleJSONBuildLog(runID, env.ProjectCfg.ReportID, env.EngineVersion, startTime, env.ProjectRoot, documents, results, queryExecMetas, embedOpts, execPlan, lintFindings, buildWarnings)
				if err := buildlog.WriteJSONBuildLog(jsonLogPath, jsonLog); err != nil {
					logger.Warnf("failed to write JSON build log: %v", err)
				}
			}

			// Print results
			printBuildSummary(ctx, out, env.ProjectRoot, results, screenshotResults, documentResults)

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
	cmd.Flags().StringVar(&outDir, "out-dir", "dist", "Directory (relative to --work-dir) for generated artifacts")
	cmd.Flags().StringSliceVar(&include, "artifact", nil, "metadata.name entries to build (default: all)")
	cmd.Flags().StringSliceVar(&exclude, "exclude-artifact", nil, "metadata.name entries to skip")
	cmd.Flags().StringVar(&chromePath, "chrome-path", "", "Path to chrome-headless-shell binary (default: auto-detected or CHROME_PATH env)")
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
	Builder         *pipeline.Builder
	Logger          logx.Logger
	Docs            []config.Document
	Artifact        config.Artifact
	SigningProfiles map[string]config.SigningProfile
	OutputDir       string
	ChromePath      string
	Debug           bool
	Graph           *reportgraph.Graph
	GraphRoot       *reportgraph.Node
	GraphBase       string
	Spinner         *Spinner
	HookRunner      *hooks.Runner
	HookEnv         hooks.HookEnv
}

// buildArtefact renders a single report artifact to PDF.
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
	artefactName := cfg.Artifact.Document.Name

	// Run pre-datasource hook
	if cfg.HookRunner != nil {
		if err := cfg.HookRunner.Run(ctx, "pre-datasource", cfg.HookEnv); err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Hook failed for %s", artefactName))
			}
			return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
		}
	}

	// Start spinner for HTML rendering
	if spinner != nil {
		spinner.Start(fmt.Sprintf("Rendering %s", artefactName))
	}
	logger.Debugf("Rendering HTML for %s", artefactName)

	renderResult, err := cfg.Builder.RenderArtefactHTML(ctx, cfg.Docs, cfg.Artifact)
	pipeline.LogDiagnostics(logger.Channel("datasource"), renderResult.Diagnostics)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to render %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
	}

	// Run pre-render hook (after HTML rendered, before PDF generation)
	if cfg.HookRunner != nil {
		if err := cfg.HookRunner.Run(ctx, "pre-render", cfg.HookEnv); err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Hook failed for %s", artefactName))
			}
			return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
		}
	}

	// Check for cancellation before PDF generation
	if err := ctx.Err(); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Canceled %s", artefactName))
		}
		return artefactResult{}, err
	}

	pdfFilename := cfg.Artifact.Spec.Filename
	if pdfFilename == "" {
		pdfFilename = artefactName + ".pdf"
	}
	pdfPath, err := pathutil.ResolveFilePath(cfg.OutputDir, pdfFilename)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to resolve PDF path for %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
	}

	// Update spinner for PDF generation
	if spinner != nil {
		spinner.Update(fmt.Sprintf("Generating PDF for %s", artefactName))
	}
	logger.Debugf("Generating PDF at %s", pdfPath)

	// Generate PDF via Builder (ephemeral server + Chrome headless shell)
	if err := cfg.Builder.RenderPDF(ctx, renderResult.HTML, renderResult.LocalAssets, pipeline.PDFRenderOptions{
		PDFPath:               pdfPath,
		ChromePath:            cfg.ChromePath,
		Format:                cfg.Artifact.Spec.Format,
		Orientation:           cfg.Artifact.Spec.Orientation,
		Debug:                 cfg.Debug,
		WaitForComponentReady: true,
		ReadyConsolePrefix:    componentReadyConsolePrefix,
	}); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to generate PDF for %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
	}

	graphPath, err := writeGraphReport(cfg.Graph, cfg.GraphRoot, pdfPath, cfg.GraphBase)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to write graph for %s", artefactName))
		}
		return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
	}

	// Check for cancellation before signing
	if err := ctx.Err(); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Canceled %s", artefactName))
		}
		return artefactResult{}, err
	}

	if ref := strings.TrimSpace(cfg.Artifact.Spec.SigningProfile); ref != "" {
		if spinner != nil {
			spinner.Update(fmt.Sprintf("Signing %s", artefactName))
		}
		profile, ok := cfg.SigningProfiles[ref]
		if !ok {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Signing profile missing for %s", artefactName))
			}
			return artefactResult{}, fmt.Errorf("artifact %s: signing profile %s missing", artefactName, ref)
		}
		logger.Debugf("Signing PDF %s with profile %s", pdfPath, ref)
		if err := cfg.Builder.SignPDF(ctx, pdfPath, profile); err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Failed to sign %s", artefactName))
			}
			return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
		}
	}

	// Run post-render hook
	if cfg.HookRunner != nil {
		postRenderEnv := cfg.HookEnv
		postRenderEnv.PDFPath = pdfPath
		if err := cfg.HookRunner.Run(ctx, "post-render", postRenderEnv); err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Hook failed for %s", artefactName))
			}
			return artefactResult{}, fmt.Errorf("artifact %s: %w", artefactName, err)
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
	Builder         *pipeline.Builder
	Logger          logx.Logger
	Artifact        config.DocumentArtefact
	SigningProfiles map[string]config.SigningProfile
	OutputDir       string
	ChromePath      string
	Debug           bool
	Spinner         *Spinner
}

// buildDocumentArtefact renders a DocumentArtefact (markdown to PDF) using Chrome.
// It converts markdown files to HTML, serves them via an ephemeral server, and captures a PDF.
func buildDocumentArtefact(ctx context.Context, cfg buildDocumentArtefactConfig) (documentArtefactResult, error) {
	if err := ctx.Err(); err != nil {
		return documentArtefactResult{}, err
	}

	logger := cfg.Logger
	artifact := cfg.Artifact
	artefactName := artifact.Document.Name
	spec := artifact.Spec
	spinner := cfg.Spinner

	if spinner != nil {
		spinner.Start(fmt.Sprintf("Building document %s", artefactName))
	}

	// Determine output filename
	filename := spec.Filename
	if filename == "" {
		filename = artefactName + ".pdf"
	}
	pdfPath := filepath.Join(cfg.OutputDir, filename)

	// Build shared PDF render options (used for both passes)
	basePDFOpts := pipeline.PDFRenderOptions{
		ChromePath:  cfg.ChromePath,
		Format:      spec.Format,
		Orientation: spec.Orientation,
		Debug:       cfg.Debug,
	}
	// Header/footer support
	if spec.DisplayHeaderFooter {
		basePDFOpts.DisplayHeaderFooter = true
		basePDFOpts.HeaderTemplate = spec.HeaderTemplate
		basePDFOpts.FooterTemplate = spec.FooterTemplate
		basePDFOpts.MarginTop = spec.MarginTop
		basePDFOpts.MarginBottom = spec.MarginBottom
		if basePDFOpts.HeaderTemplate == "" {
			basePDFOpts.HeaderTemplate = buildDefaultDocumentHeader(spec.Title)
		}
		if basePDFOpts.FooterTemplate == "" {
			basePDFOpts.FooterTemplate = buildDefaultDocumentFooter()
		}
	}

	// Two-pass rendering for TOC with page numbers
	var tocPageNumbers map[string]int
	if spec.TableOfContents {
		if spinner != nil {
			spinner.Update(fmt.Sprintf("Collecting page numbers for %s", artefactName))
		}

		// First pass: render without page numbers to collect heading positions
		firstPassResult, err := cfg.Builder.RenderDocumentHTML(ctx, artifact, pipeline.DocumentArtefactRenderOptions{})
		if err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Failed to render %s", artefactName))
			}
			return documentArtefactResult{}, err
		}

		// Collect heading page numbers via Builder (ephemeral server + Chrome)
		tocPageNumbers, err = cfg.Builder.CollectHeadingPages(ctx, firstPassResult.HTML, firstPassResult.LocalAssets, basePDFOpts)
		if err != nil {
			logger.Warnf("Failed to collect heading pages for %s: %v (continuing without page numbers)", artefactName, err)
			tocPageNumbers = nil
		} else {
			logger.Debugf("Collected %d heading page numbers for %s", len(tocPageNumbers), artefactName)
		}
	}

	// Final pass: render with page numbers (if collected)
	renderResult, err := cfg.Builder.RenderDocumentHTML(ctx, artifact, pipeline.DocumentArtefactRenderOptions{
		TOCPageNumbers: tocPageNumbers,
	})
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to render %s", artefactName))
		}
		return documentArtefactResult{}, err
	}

	// Check for cancellation before generating PDF
	if err := ctx.Err(); err != nil {
		return documentArtefactResult{}, err
	}

	if spinner != nil {
		spinner.Update(fmt.Sprintf("Generating PDF for %s", artefactName))
	}

	// Generate final PDF via Builder (ephemeral server + Chrome)
	finalPDFOpts := basePDFOpts
	finalPDFOpts.PDFPath = pdfPath
	if err := cfg.Builder.RenderPDF(ctx, renderResult.HTML, renderResult.LocalAssets, finalPDFOpts); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to generate PDF for %s", artefactName))
		}
		return documentArtefactResult{}, fmt.Errorf("document artifact %s: %w", artefactName, err)
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
			return documentArtefactResult{}, fmt.Errorf("document artifact %s: signing profile %s missing", artefactName, ref)
		}
		logger.Debugf("Signing PDF %s with profile %s", pdfPath, ref)
		if err := cfg.Builder.SignPDF(ctx, pdfPath, profile); err != nil {
			if spinner != nil {
				spinner.StopWithError(fmt.Sprintf("Failed to sign %s", artefactName))
			}
			return documentArtefactResult{}, fmt.Errorf("document artifact %s: %w", artefactName, err)
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
// Chrome header/footer templates use special CSS classes for dynamic content.
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
// Chrome footer templates use special CSS classes:
// - "date" class shows the formatted print date
// - "pageNumber" class shows the current page number
// - "totalPages" class shows the total number of pages
func buildDefaultDocumentFooter() string {
	return `<div style="width: 100%; font-size: 9px; font-family: Arial, sans-serif; padding: 0 10mm; display: flex; justify-content: space-between; color: #666;">
  <span class="date"></span>
  <span>Page <span class="pageNumber"></span> of <span class="totalPages"></span></span>
</div>`
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

	fmt.Fprintf(file, "ARTIFACTS (%d)\n", len(results))
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
		fmt.Fprintf(file, "DOCUMENT ARTIFACTS (%d)\n", len(docResults))
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
	Builder    *pipeline.Builder
	Logger     logx.Logger
	Docs       []config.Document
	Artifact   config.ScreenshotArtefact
	OutputDir  string
	ChromePath string
	Debug      bool
	Spinner    *Spinner
}

// buildScreenshotArtefact captures screenshots of specific components.
// It renders the HTML containing the specified layout pages, starts an ephemeral
// server, and uses Chrome to capture screenshots of individual elements.
func buildScreenshotArtefact(ctx context.Context, cfg buildScreenshotArtefactConfig) ([]screenshotArtefactResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logx.Nop()
	}

	spinner := cfg.Spinner
	artefactName := cfg.Artifact.Document.Name

	// Start spinner for HTML rendering
	if spinner != nil {
		spinner.Start(fmt.Sprintf("Rendering %s", artefactName))
	}
	logger.Debugf("Rendering HTML for screenshot artifact %s", artefactName)

	renderResult, err := cfg.Builder.RenderScreenshotHTML(ctx, cfg.Docs, cfg.Artifact)
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to render %s", artefactName))
		}
		return nil, fmt.Errorf("screenshot artifact %s: %w", artefactName, err)
	}

	// Check for cancellation before screenshot capture
	if err := ctx.Err(); err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Canceled %s", artefactName))
		}
		return nil, err
	}

	// Update spinner for screenshot capture
	if spinner != nil {
		spinner.Update(fmt.Sprintf("Capturing screenshots for %s", artefactName))
	}
	logger.Debugf("Capturing screenshots for %s", artefactName)

	// Convert config refs to Builder refs
	refs := make([]pipeline.ScreenshotRef, len(cfg.Artifact.Spec.Refs))
	for i, ref := range cfg.Artifact.Spec.Refs {
		refs[i] = pipeline.ScreenshotRef{Kind: ref.Kind, Name: ref.Name}
	}

	// Map scale setting to device scale factor
	var scaleFactor float64
	if strings.EqualFold(cfg.Artifact.Spec.Scale, "device") {
		scaleFactor = 2.0
	}

	// Capture screenshots via Builder (ephemeral server + Chrome)
	captureResults, err := cfg.Builder.CaptureScreenshots(ctx, renderResult.HTML, renderResult.LocalAssets, pipeline.ScreenshotRenderOptions{
		OutputDir:             cfg.OutputDir,
		ChromePath:            cfg.ChromePath,
		Format:                cfg.Artifact.Spec.Format,
		Orientation:           cfg.Artifact.Spec.Orientation,
		Debug:                 cfg.Debug,
		WaitForComponentReady: true,
		ReadyConsolePrefix:    componentReadyConsolePrefix,
		Refs:                  refs,
		FilenamePrefix:        cfg.Artifact.Spec.FilenamePrefix,
		FilenamePattern:       cfg.Artifact.Spec.FilenamePattern,
		Scale:                 scaleFactor,
	})
	if err != nil {
		if spinner != nil {
			spinner.StopWithError(fmt.Sprintf("Failed to capture screenshots for %s", artefactName))
		}
		return nil, fmt.Errorf("screenshot artifact %s: %w", artefactName, err)
	}

	// Convert Builder results to CLI result type
	results := make([]screenshotArtefactResult, len(captureResults))
	for i, r := range captureResults {
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

// buildExecutionConfig holds configuration for building all artifact types.
type buildExecutionConfig struct {
	Builder    *pipeline.Builder
	Logger     logx.Logger
	OutputDir  string
	ChromePath string
	Debug      bool
	NoColor    bool
	Stdout     io.Writer
	HookRunner *hooks.Runner
	HookEnv    hooks.HookEnv
}

// buildAllResults holds the results from building all artifact types.
type buildAllResults struct {
	Reports     []artefactResult
	Screenshots []screenshotArtefactResult
	Documents   []documentArtefactResult
}

// buildAllArtefacts iterates over all selected artifact types and builds them.
func buildAllArtefacts(ctx context.Context, out *Output, manifests *buildManifestData, cfg buildExecutionConfig) (*buildAllResults, error) {
	spinnerCfg := SpinnerConfig{Stdout: cfg.Stdout, NoColor: cfg.NoColor}

	// Build report artifacts
	out.Step(fmt.Sprintf("Building %d artifact(s)...", len(manifests.Selected)))
	results := make([]artefactResult, 0, len(manifests.Selected))
	for _, artifact := range manifests.Selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var root *reportgraph.Node
		if manifests.Graph != nil {
			node, ok := manifests.Graph.ReportArtefactByName(artifact.Document.Name)
			if !ok {
				return nil, RuntimeErrorf("graph: artifact node %s not found", artifact.Document.Name)
			}
			root = node
		}

		artefactHookEnv := cfg.HookEnv
		artefactHookEnv.ArtefactName = artifact.Document.Name
		artefactHookEnv.ArtefactKind = "report"

		entry, err := buildArtefact(ctx, buildArtefactConfig{
			Builder:         cfg.Builder,
			Logger:          cfg.Logger.Channel(artifact.Document.Name),
			Docs:            manifests.Documents,
			Artifact:        artifact,
			SigningProfiles: manifests.SigningProfiles,
			OutputDir:       cfg.OutputDir,
			ChromePath:      cfg.ChromePath,
			Debug:           cfg.Debug,
			Graph:           manifests.Graph,
			GraphRoot:       root,
			GraphBase:       cfg.Builder.Workdir,
			Spinner:         NewSpinner(spinnerCfg),
			HookRunner:      cfg.HookRunner,
			HookEnv:         artefactHookEnv,
		})
		if err != nil {
			policy := pipeline.ClassifyInvalidLayout(err, pipeline.RenderModeBuild)
			if policy.IsInvalidRoot {
				return nil, ConfigError(err)
			}
			return nil, RuntimeError(err)
		}
		results = append(results, entry)
	}

	// Build screenshot artifacts
	var screenshotResults []screenshotArtefactResult
	if len(manifests.SelectedScreenshots) > 0 {
		out.Step(fmt.Sprintf("Capturing %d screenshot artifact(s)...", len(manifests.SelectedScreenshots)))
		for _, ssArtefact := range manifests.SelectedScreenshots {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			ssResults, err := buildScreenshotArtefact(ctx, buildScreenshotArtefactConfig{
				Builder:    cfg.Builder,
				Logger:     cfg.Logger.Channel(ssArtefact.Document.Name),
				Docs:       manifests.Documents,
				Artifact:   ssArtefact,
				OutputDir:  cfg.OutputDir,
				ChromePath: cfg.ChromePath,
				Debug:      cfg.Debug,
				Spinner:    NewSpinner(spinnerCfg),
			})
			if err != nil {
				return nil, RuntimeError(err)
			}
			screenshotResults = append(screenshotResults, ssResults...)
		}
	}

	// Build document artifacts
	var documentResults []documentArtefactResult
	if len(manifests.SelectedDocuments) > 0 {
		out.Step(fmt.Sprintf("Building %d document artifact(s)...", len(manifests.SelectedDocuments)))
		for _, docArtefact := range manifests.SelectedDocuments {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			docResult, err := buildDocumentArtefact(ctx, buildDocumentArtefactConfig{
				Builder:         cfg.Builder,
				Logger:          cfg.Logger.Channel(docArtefact.Document.Name),
				Artifact:        docArtefact,
				SigningProfiles: manifests.SigningProfiles,
				OutputDir:       cfg.OutputDir,
				ChromePath:      cfg.ChromePath,
				Debug:           cfg.Debug,
				Spinner:         NewSpinner(spinnerCfg),
			})
			if err != nil {
				return nil, RuntimeError(err)
			}
			documentResults = append(documentResults, docResult)
		}
	}

	return &buildAllResults{
		Reports:     results,
		Screenshots: screenshotResults,
		Documents:   documentResults,
	}, nil
}

// buildManifestData holds validated manifest data ready for building.
type buildManifestData struct {
	Documents           []config.Document
	Selected            []config.Artifact
	SelectedScreenshots []config.ScreenshotArtefact
	SelectedDocuments   []config.DocumentArtefact
	SigningProfiles     map[string]config.SigningProfile
	Graph               *reportgraph.Graph
	LintFindings        []lint.Finding
}

// loadBuildManifests loads YAML documents, validates them, collects and filters
// artifacts, and builds the dependency graph. It prints progress to out.
func loadBuildManifests(ctx context.Context, out *Output, logger logx.Logger, projectRoot string, include, exclude []string, noLint, noGraph bool, kindProvider config.KindProvider, pluginLinters lint.PluginLinterRegistry) (*buildManifestData, error) {
	out.Step("Loading manifests...")
	loadStart := time.Now()
	documents, err := config.LoadDirWithOptions(ctx, projectRoot, config.LoadOptions{KindProvider: kindProvider})
	if err != nil {
		return nil, ConfigError(err)
	}
	if len(documents) == 0 {
		return nil, ConfigErrorf("no YAML documents found in %s", projectRoot)
	}

	// Fail build if any environment variables are unresolved
	// Exclude param names defined in LayoutPages (they're resolved at render time)
	paramNames := config.CollectLayoutPageParamNames(documents)
	if err := config.CheckMissingEnvVarsExcluding(documents, paramNames); err != nil {
		return nil, ConfigError(err)
	}

	out.StepDone(fmt.Sprintf("Validated %d document(s)", len(documents)), time.Since(loadStart))

	// Show manifest summary
	out.Blank()
	out.Info("Manifest summary:")
	for _, doc := range documents {
		relPath := pathutil.RelPath(projectRoot, doc.File)
		out.ListColored(fmt.Sprintf("%s #%d", relPath, doc.Position), "kind", doc.Kind, "name", doc.Name)
	}
	out.Blank()

	// Run lint rules unless disabled
	var lintFindings []lint.Finding
	if !noLint {
		lintDocs := configDocsToLintDocs(documents)
		runner := lint.NewDefaultRunner()
		lintFindings = runner.Run(ctx, lintDocs)

		// Run plugin linters.
		if pluginLinters != nil {
			pluginFindings := lint.RunPluginLinters(ctx, lintDocs, pluginLinters)
			lintFindings = append(lintFindings, pluginFindings...)
		}

		if len(lintFindings) > 0 {
			printLintFindings(out, lintFindings, projectRoot)
			out.Blank()
		}
	}

	artifacts, err := config.CollectArtefacts(documents)
	if err != nil {
		return nil, ConfigError(err)
	}

	screenshotArtefacts, err := config.CollectScreenshotArtefacts(documents)
	if err != nil {
		return nil, ConfigError(err)
	}

	documentArtefacts, err := config.CollectDocumentArtefacts(documents)
	if err != nil {
		return nil, ConfigError(err)
	}

	if len(artifacts) == 0 && len(screenshotArtefacts) == 0 && len(documentArtefacts) == 0 {
		return nil, ConfigErrorf("no ReportArtefact, ScreenshotArtefact, or DocumentArtefact manifests found in %s", projectRoot)
	}

	signingProfiles, err := config.CollectSigningProfiles(documents)
	if err != nil {
		return nil, ConfigError(err)
	}

	// Validate that all include names exist in any artifact type
	if err := pipeline.ValidateAllArtefactNames(artifacts, screenshotArtefacts, documentArtefacts, include); err != nil {
		return nil, ConfigError(err)
	}

	filterOpts := pipeline.FilterOptions{
		Include: include,
		Exclude: exclude,
	}
	selected := pipeline.FilterArtefacts(artifacts, filterOpts)
	selectedScreenshots := pipeline.FilterScreenshotArtefacts(screenshotArtefacts, filterOpts)
	selectedDocuments := pipeline.FilterDocumentArtefacts(documentArtefacts, filterOpts)

	if len(selected) == 0 && len(selectedScreenshots) == 0 && len(selectedDocuments) == 0 {
		return nil, ConfigErrorf("no artifacts selected (check --artifact / --exclude-artifact)")
	}
	pipeline.LogArtefactWarnings(logger, selected)
	pipeline.LogDocumentArtefactWarnings(logger, selectedDocuments)

	if err := pipeline.EnsureSigningProfiles(selected, signingProfiles); err != nil {
		return nil, ConfigError(err)
	}
	if err := pipeline.EnsureDocumentSigningProfiles(selectedDocuments, signingProfiles); err != nil {
		return nil, ConfigError(err)
	}

	var graph *reportgraph.Graph
	if !noGraph {
		graph, err = reportgraph.Build(ctx, documents)
		if err != nil {
			return nil, RuntimeError(err)
		}
	}

	return &buildManifestData{
		Documents:           documents,
		Selected:            selected,
		SelectedScreenshots: selectedScreenshots,
		SelectedDocuments:   selectedDocuments,
		SigningProfiles:     signingProfiles,
		Graph:               graph,
		LintFindings:        lintFindings,
	}, nil
}

// resolveDataValidationMode parses a data validation mode string.
func resolveDataValidationMode(value string) (dataset.DataValidationMode, error) {
	switch value {
	case "fail":
		return dataset.DataValidationFail, nil
	case "warn":
		return dataset.DataValidationWarn, nil
	case "off":
		return dataset.DataValidationOff, nil
	default:
		return "", fmt.Errorf("invalid data-validation value %q, expected 'fail', 'warn', or 'off'", value)
	}
}

// assembleJSONBuildLog creates a JSON build log structure from collected build data.
func assembleJSONBuildLog(runID, reportID, engineVersion string, startTime time.Time, projectRoot string, documents []config.Document, results []artefactResult, queryExecMetas []duckdb.QueryExecMeta, embedOpts buildlog.EmbedOptions, execPlan *buildlog.ExecutionPlan, lintFindings []lint.Finding, buildWarnings []string) *buildlog.JSONBuildLog {
	completedTime := time.Now()

	docEntries := make([]buildlog.DocumentEntry, 0, len(documents))
	for _, doc := range documents {
		docEntries = append(docEntries, buildlog.DocumentEntry{
			File:     doc.File,
			Position: doc.Position,
			Kind:     doc.Kind,
			Name:     doc.Name,
		})
	}

	artefactEntries := make([]buildlog.ArtefactEntry, 0, len(results))
	for _, res := range results {
		artefactEntries = append(artefactEntries, buildlog.ArtefactEntry{
			Name:  res.Name,
			PDF:   res.PDFPath,
			Graph: res.GraphPath,
		})
	}

	queryEntries := make([]buildlog.QueryEntry, 0, len(queryExecMetas))
	for _, meta := range queryExecMetas {
		queryEntries = append(queryEntries, buildlog.BuildQueryEntry(meta, embedOpts))
	}

	var planSteps []buildlog.ExecutionStep
	if execPlan != nil {
		planSteps = execPlan.GetSteps()
	}

	return &buildlog.JSONBuildLog{
		RunID:         runID,
		ReportID:      reportID,
		EngineVersion: engineVersion,
		Started:       startTime,
		Completed:     completedTime,
		DurationMs:    completedTime.Sub(startTime).Milliseconds(),
		Workdir:       projectRoot,
		Documents:     docEntries,
		Artifacts:     artefactEntries,
		Queries:       queryEntries,
		ExecutionPlan: planSteps,
		Lint:          findingsToLintEntries(lintFindings),
		Warnings:      buildWarnings,
	}
}

// printBuildSummary prints the build results to the structured output.
func printBuildSummary(ctx context.Context, out *Output, projectRoot string, results []artefactResult, screenshotResults []screenshotArtefactResult, documentResults []documentArtefactResult) {
	out.Blank()
	style := StyleFromContext(ctx)
	resultItems := make([]string, 0, len(results))
	for _, res := range results {
		relPDFPath := pathutil.RelPath(projectRoot, res.PDFPath)
		item := fmt.Sprintf("%s %s %s", FormatName(res.Name), style.Dim.Sprint(SymbolArrow), FormatPath(relPDFPath))
		if res.GraphPath != "" {
			item += style.Dim.Sprintf(" (+graph)")
		}
		resultItems = append(resultItems, item)
	}
	out.Summary(fmt.Sprintf("Generated %d artifact(s):", len(results)), resultItems)

	if len(screenshotResults) > 0 {
		out.Blank()
		ssItems := make([]string, 0, len(screenshotResults))
		var ssErrors []string
		for _, res := range screenshotResults {
			if res.Error != nil {
				ssErrors = append(ssErrors, fmt.Sprintf("%s/%s: %v", res.RefKind, res.RefName, res.Error))
				continue
			}
			relPath := pathutil.RelPath(projectRoot, res.FilePath)
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

	if len(documentResults) > 0 {
		out.Blank()
		docItems := make([]string, 0, len(documentResults))
		for _, res := range documentResults {
			relPath := pathutil.RelPath(projectRoot, res.PDFPath)
			item := fmt.Sprintf("%s %s %s", res.Name, style.Dim.Sprint(SymbolArrow), FormatPath(relPath))
			docItems = append(docItems, item)
		}
		out.Summary(fmt.Sprintf("Generated %d document(s):", len(documentResults)), docItems)
	}
}
