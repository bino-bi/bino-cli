package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/updater"
	"bino.bi/bino/pkg/duckdb"
)

// setupTasks reports which setup tasks the given flags select. Chrome is the
// default: it runs only when no explicit task flag is passed.
func setupTasks(templateEngine, duckdbExtensions bool) (installEngine, installExtensions, installChrome bool) {
	return templateEngine, duckdbExtensions, !templateEngine && !duckdbExtensions
}

// prefetchDuckDBExtensions installs and loads every extension the CLI can use
// into the shared extension cache, so later runs need no network. LOAD follows
// INSTALL so a broken extension fails here rather than at the first render.
//
// Loading webdavfs prints one "[WebDAV Extension] ..." line from C++ directly to
// the process's stderr, bypassing the command's writer, so --quiet cannot
// suppress it. It is informational, not an error.
func prefetchDuckDBExtensions(ctx context.Context, opts duckdb.Options) error {
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	if err := session.InstallAndLoadExtensions(ctx, duckdb.DefaultExtensions()); err != nil {
		return err
	}

	return session.InstallAndLoadCommunityExtensions(ctx, duckdb.CommunityExtensions())
}

func newSetupCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
	var (
		dryRun           bool
		quiet            bool
		templateEngine   bool
		duckdbExtensions bool
		engineVersion    string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Download or update Chrome headless shell, template engine and DuckDB extensions",
		Long: strings.TrimSpace(`Ensure Chrome headless shell, template engine and DuckDB extensions are available locally.
Use --verbose (-v) to surface verbose installer logs.`),
		Example: strings.TrimSpace(`  bino setup
  bino setup --template-engine
  bino setup --template-engine --engine-version v1.2.3
  bino setup --duckdb-extensions
  bino setup --dry-run`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			if !quiet {
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "Setting up bino dependencies...")
				fmt.Fprintln(out, strings.Repeat("─", 50))
			}

			// Determine which tasks to run
			installTemplateEngine, installDuckDBExtensions, installChrome := setupTasks(templateEngine, duckdbExtensions)

			// Calculate total steps for progress display
			totalSteps := 0
			if installTemplateEngine {
				totalSteps++
			}
			if installDuckDBExtensions {
				totalSteps++
			}
			if installChrome {
				totalSteps++
			}
			currentStep := 0

			// Handle template engine download
			if installTemplateEngine {
				currentStep++
				if !quiet {
					fmt.Fprintln(out, "")
					fmt.Fprintf(out, "[%d/%d] Template Engine\n", currentStep, totalSteps)
				}

				mgr, err := engine.NewManager()
				if err != nil {
					return RuntimeError(fmt.Errorf("initialize engine manager: %w", err))
				}

				version := engineVersion
				if version == "" {
					if !quiet {
						fmt.Fprintln(out, "      Resolving latest version from GitHub...")
					}
					version, err = mgr.FetchLatestRemoteVersion(ctx)
					if err != nil {
						return ExternalError(fmt.Errorf("fetch latest version: %w", err))
					}
					if !quiet {
						fmt.Fprintf(out, "      Latest version: %s\n", version)
					}
				}

				if dryRun {
					fmt.Fprintf(out, "      [dry-run] Would download template engine %s\n", version)
				} else {
					if !quiet {
						fmt.Fprintf(out, "      Downloading %s...\n", version)
					}
					info, err := mgr.Download(ctx, version)
					if err != nil {
						return ExternalError(fmt.Errorf("download template engine: %w", err))
					}
					if !quiet {
						fmt.Fprintf(out, "      Installed to: %s\n", info.Path)
						fmt.Fprintln(out, "      ✓ Template engine ready")
					}
				}
			}

			// Handle DuckDB extension prefetch
			if installDuckDBExtensions {
				currentStep++

				if !quiet {
					fmt.Fprintln(out, "")
					fmt.Fprintf(out, "[%d/%d] DuckDB Extensions\n", currentStep, totalSteps)
				}

				opts, err := duckdb.DefaultOptions()
				if err != nil {
					return RuntimeError(fmt.Errorf("resolve duckdb cache dir: %w", err))
				}

				names := append(duckdb.DefaultExtensions(), duckdb.CommunityExtensions()...)

				if !quiet {
					fmt.Fprintf(out, "      Cache: %s\n", opts.CacheDir)
				}

				if dryRun {
					fmt.Fprintf(out, "      [dry-run] Would install %s\n", strings.Join(names, ", "))
				} else {
					if !quiet {
						fmt.Fprintf(out, "      Installing %s...\n", strings.Join(names, ", "))
					}
					if err := prefetchDuckDBExtensions(ctx, opts); err != nil {
						return ExternalError(fmt.Errorf("prefetch duckdb extensions: %w", err))
					}
					if !quiet {
						fmt.Fprintln(out, "      ✓ DuckDB extensions ready")
					}
				}
			}

			// Handle Chrome headless shell installation (default behavior when no flags specified)
			if installChrome {
				currentStep++

				if !quiet {
					fmt.Fprintln(out, "")
					fmt.Fprintf(out, "[%d/%d] Chrome Headless Shell\n", currentStep, totalSteps)
				}

				mgr, err := chrome.NewManager()
				if err != nil {
					return RuntimeError(fmt.Errorf("initialize chrome manager: %w", err))
				}

				if !quiet {
					fmt.Fprintf(out, "      Cache: %s\n", mgr.CacheDir())
				}

				info, err := mgr.Install(ctx, chrome.InstallOptions{
					DryRun: dryRun,
					Quiet:  quiet,
					Stdout: out,
					Stderr: cmd.ErrOrStderr(),
				})
				if err != nil {
					return ExternalError(err)
				}

				if !quiet {
					fmt.Fprintln(out, "      ✓ Chrome headless shell ready")
				}

				// Save version to state
				if !dryRun && info.Version != "" {
					state, err := updater.LoadState()
					if err != nil {
						state = &updater.State{}
					}
					state.ChromeVersion = info.Version
				}
			}

			// Mark setup as completed in state (skip for dry-run)
			if !dryRun {
				state, err := updater.LoadState()
				if err != nil {
					state = &updater.State{}
				}
				state.SetupCompleted = true
				if err := updater.SaveState(state); err != nil {
					// Non-fatal: log warning but don't fail the setup
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not save setup state: %v\n", err)
				}
			}

			if !quiet {
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, strings.Repeat("─", 50))
				fmt.Fprintln(out, "Setup complete! Run 'bino version' to verify.")
				fmt.Fprintln(out, "")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the actions without downloading artifacts")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress verbose installer output")
	cmd.Flags().BoolVar(&templateEngine, "template-engine", false, "Download or update the bn-template-engine")
	cmd.Flags().BoolVar(&duckdbExtensions, "duckdb-extensions", false, "Download DuckDB extensions into the local cache for offline use")
	cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Specific template engine version to download (default: latest)")

	return cmd
}
