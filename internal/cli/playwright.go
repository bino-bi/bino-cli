package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/playwright"
)

func newPlaywrightCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playwright",
		Short: "Manage Playwright runtime dependencies",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newPlaywrightInstallCommand())

	return cmd
}

func newPlaywrightInstallCommand() *cobra.Command {
	var (
		browsers  []string
		driverDir string
		dryRun    bool
		quiet     bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Download or update the Playwright driver and browsers",
		Long: strings.TrimSpace(`Ensure Playwright browsers and the driver binary are available locally.
Use --verbose (-v) to surface verbose installer logs.`),
		Example: strings.TrimSpace(`  bino playwright install --browser chromium --browser webkit
	  bino playwright install --dry-run`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := playwright.InstallOptions{
				Browsers:        browsers,
				DriverDirectory: driverDir,
				DryRun:          dryRun,
				Quiet:           quiet,
				Stdout:          cmd.OutOrStdout(),
				Stderr:          cmd.ErrOrStderr(),
			}

			if err := playwright.Install(cmd.Context(), opts); err != nil {
				return ExternalError(err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Playwright assets are up to date.")
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&browsers, "browser", nil, "Browsers to install (default: chromium). Repeat to add firefox, webkit, etc.")
	cmd.Flags().StringVar(&driverDir, "driver-dir", "", "Override the Playwright driver cache directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the actions without downloading artifacts")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress verbose installer output")

	return cmd
}
