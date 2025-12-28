package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/playwright"
)

func newSetupCommand() *cobra.Command {
	var (
		browsers  []string
		driverDir string
		dryRun    bool
		quiet     bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Download or update browser runtimes for PDF rendering",
		Long: strings.TrimSpace(`Ensure browser runtimes and the rendering driver are available locally.
Use --verbose (-v) to surface verbose installer logs.`),
		Example: strings.TrimSpace(`  bino setup --browser chromium --browser webkit
  bino setup --dry-run`),
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

			fmt.Fprintln(cmd.OutOrStdout(), "Browser runtimes are up to date.")
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&browsers, "browser", nil, "Browsers to install (default: chromium). Repeat to add firefox, webkit, etc.")
	cmd.Flags().StringVar(&driverDir, "driver-dir", "", "Override the browser driver cache directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the actions without downloading artifacts")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress verbose installer output")

	return cmd
}


