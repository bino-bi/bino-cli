// Package cli implements the bino command-line interface.
//
// # Context and Cancellation Semantics
//
// The CLI uses Go's context.Context to propagate cancellation signals throughout
// the application. This enables graceful shutdown when users press Ctrl+C or when
// the process receives SIGTERM.
//
// ## Signal Handling
//
// The main package creates a root context using signal.NotifyContext that listens
// for os.Interrupt (Ctrl+C) and syscall.SIGTERM. This context is passed to
// app.Execute() and propagated to all subcommands via cobra's ExecuteContext.
//
// ## Cancellation Behavior by Command
//
// ### build
// - Checks ctx.Err() before starting each artefact build
// - Propagates context to datasource collection (DuckDB queries)
// - Propagates context to ephemeral HTTP server for PDF rendering
// - Propagates context to Playwright PDF rendering
// - On cancellation: stops current artefact, shuts down ephemeral server, returns
//
// ### preview
// - Propagates context to HTTP server's Start() method
// - Propagates context to file watcher's Run() method
// - Propagates context to content refresh operations
// - On cancellation: gracefully shuts down HTTP server with 5s timeout
//
// ## Package-Level Context Expectations
//
// ### internal/report/datasource
// - Collect() respects context for DuckDB session lifecycle
// - Individual queries use context with configurable timeout (BNR_MAX_QUERY_DURATION_MS)
// - Parent context cancellation stops in-flight queries
//
// ### internal/preview/httpserver
// - Server.Start() blocks until context is canceled
// - Graceful shutdown with 5-second timeout for in-flight requests
// - SSE connections respect request context for cleanup
//
// ### internal/playwright
// - RenderPDF() checks ctx.Err() at entry
// - Page navigation timeout is separate from context cancellation
// - waitForComponentReady() respects context for early termination
//
// ## Implementation Guidelines
//
// 1. Always check ctx.Err() at function entry points before expensive operations
// 2. Use context.WithTimeout for operations with bounded duration
// 3. Propagate context to all I/O operations (HTTP requests, database queries)
// 4. On cancellation, clean up resources (close connections, stop servers)
// 5. Return ctx.Err() or wrap it with additional context
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/updater"
	"bino.bi/bino/internal/version"
)

// newRootCommand creates the root cobra command for the bino CLI.
// The command's context is set via ExecuteContext from the main package
// and carries cancellation signals for graceful shutdown.
func newRootCommand() *cobra.Command {
	var (
		verbose bool
		noColor bool
	)
	cmd := &cobra.Command{
		Use:   "bino",
		Short: "Generate report bundles for BinoBI",
		Long:  "bino orchestrates data collection and rendering pipelines for report automation.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Run non-blocking update check in background
			go func() {
				if result, _ := updater.CheckForUpdate(ctx); result != nil {
					// Print to stderr to avoid polluting stdout (e.g. json output)
					fmt.Fprintf(os.Stderr, "\nUpdate available: %s -> %s\nRun 'bino update' to upgrade.\n\n",
						version.Version, result.LatestVersion)
				}
			}()

			ctx = logx.WithDebug(ctx, verbose)

			// Initialize centralized styling with NO_COLOR handling
			// This sets color.NoColor globally and initializes the style palette
			effectiveNoColor := noColor || os.Getenv("NO_COLOR") != ""
			InitStyle(effectiveNoColor)
			ctx = logx.WithNoColor(ctx, effectiveNoColor)

			// Create terminal logger with the same noColor setting
			var logger logx.Logger = logx.NewTerminalWithColor(cmd.OutOrStdout(), cmd.ErrOrStderr(), verbose, effectiveNoColor)

			// Only show run ID in logger prefix when verbose mode is enabled
			if verbose {
				if runID := logx.RunIDFromContext(ctx); runID != "" {
					// Use short run ID (first 8 characters) for display
					shortID := runID
					if len(runID) > 8 {
						shortID = runID[:8]
					}
					logger = logger.Channel(shortID)
				}
			}

			ctx = logx.WithLogger(ctx, logger)
			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging and show run ID")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")

	cmd.AddCommand(newVersionCommand())
	cmd.AddCommand(newAboutCommand())
	cmd.AddCommand(newPlaywrightCommand())
	cmd.AddCommand(newBuildCommand())
	cmd.AddCommand(newPreviewCommand())
	cmd.AddCommand(newLintCommand())
	cmd.AddCommand(newGraphCommand())
	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newLSPCommand())
	cmd.AddCommand(newCacheCommand())
	cmd.AddCommand(newUpdateCommand())

	return cmd
}
