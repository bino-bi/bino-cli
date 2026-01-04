package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/playwright"
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

			// Handle template engine download
			if templateEngine {
				mgr, err := engine.NewManager()
				if err != nil {
					return RuntimeError(fmt.Errorf("initialize engine manager: %w", err))
				}

				version := engineVersion
				if version == "" {
					if !quiet {
						fmt.Fprintln(cmd.OutOrStdout(), "Resolving latest template engine version...")
					}
					version, err = mgr.FetchLatestRemoteVersion(ctx)
					if err != nil {
						return ExternalError(fmt.Errorf("fetch latest version: %w", err))
					}
				}

				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "Would download template engine %s\n", version)
				} else {
					if !quiet {
						fmt.Fprintf(cmd.OutOrStdout(), "Downloading template engine %s...\n", version)
					}
					info, err := mgr.Download(ctx, version)
					if err != nil {
						return ExternalError(fmt.Errorf("download template engine: %w", err))
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Template engine %s installed at %s\n", info.Version, info.Path)
				}
			}

			// Handle browser runtime installation (default behavior when no flags specified)
			if !templateEngine || len(browsers) > 0 || driverDir != "" {
				opts := playwright.InstallOptions{
					Browsers:        browsers,
					DriverDirectory: driverDir,
					DryRun:          dryRun,
					Quiet:           quiet,
					Stdout:          cmd.OutOrStdout(),
					Stderr:          cmd.ErrOrStderr(),
				}

				if err := playwright.Install(ctx, opts); err != nil {
					return ExternalError(err)
				}

				fmt.Fprintln(cmd.OutOrStdout(), "Browser runtimes are up to date.")
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


