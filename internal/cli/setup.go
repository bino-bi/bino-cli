package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/playwright"
	"bino.bi/bino/internal/updater"
)

func newSetupCommand() *cobra.Command {
	var (
		browsers       []string
		driverDir      string
		dryRun         bool
		quiet          bool
		templateEngine bool
		engineVersion  string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Download or update browser runtimes and template engine",
		Long: strings.TrimSpace(`Ensure browser runtimes, rendering driver, and template engine are available locally.
Use --verbose (-v) to surface verbose installer logs.`),
		Example: strings.TrimSpace(`  bino setup --browser chromium --browser webkit
  bino setup --template-engine
  bino setup --template-engine --engine-version v1.2.3
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
			installTemplateEngine := templateEngine
			installBrowsers := !templateEngine || len(browsers) > 0 || driverDir != ""

			// Calculate total steps for progress display
			totalSteps := 0
			if installTemplateEngine {
				totalSteps++
			}
			if installBrowsers {
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

			// Handle browser runtime installation (default behavior when no flags specified)
			if installBrowsers {
				currentStep++
				browserList := browsers
				if len(browserList) == 0 {
					browserList = []string{"chromium"}
				}

				if !quiet {
					fmt.Fprintln(out, "")
					fmt.Fprintf(out, "[%d/%d] Browser Runtimes\n", currentStep, totalSteps)
					fmt.Fprintf(out, "      Browsers: %s\n", strings.Join(browserList, ", "))

					// Show cache directory
					cacheDir, err := pathutil.CacheDir("playwright")
					if err == nil {
						fmt.Fprintf(out, "      Cache: %s\n", cacheDir)
					}
					fmt.Fprintln(out, "      Installing Playwright driver and browsers...")
					fmt.Fprintln(out, "")
				}

				opts := playwright.InstallOptions{
					Browsers:        browsers,
					DriverDirectory: driverDir,
					DryRun:          dryRun,
					Quiet:           quiet,
					Stdout:          out,
					Stderr:          cmd.ErrOrStderr(),
				}

				if err := playwright.Install(ctx, opts); err != nil {
					return ExternalError(err)
				}

				if !quiet {
					fmt.Fprintln(out, "      ✓ Browser runtimes ready")
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

	cmd.Flags().StringSliceVar(&browsers, "browser", nil, "Browsers to install (default: chromium). Repeat to add firefox, webkit, etc.")
	cmd.Flags().StringVar(&driverDir, "driver-dir", "", "Override the browser driver cache directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the actions without downloading artifacts")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress verbose installer output")
	cmd.Flags().BoolVar(&templateEngine, "template-engine", false, "Download or update the bn-template-engine")
	cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Specific template engine version to download (default: latest)")

	return cmd
}


