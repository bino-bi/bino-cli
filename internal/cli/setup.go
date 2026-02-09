package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/updater"
)

func newSetupCommand() *cobra.Command {
	var (
		dryRun         bool
		quiet          bool
		templateEngine bool
		engineVersion  string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Download or update Chrome headless shell and template engine",
		Long: strings.TrimSpace(`Ensure Chrome headless shell and template engine are available locally.
Use --verbose (-v) to surface verbose installer logs.`),
		Example: strings.TrimSpace(`  bino setup
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
			installChrome := !templateEngine

			// Calculate total steps for progress display
			totalSteps := 0
			if installTemplateEngine {
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
	cmd.Flags().StringVar(&engineVersion, "engine-version", "", "Specific template engine version to download (default: latest)")

	return cmd
}
