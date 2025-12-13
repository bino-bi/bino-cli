package cli

import (
	"fmt"

	"bino.bi/bino/internal/updater"
	"bino.bi/bino/internal/version"
	"github.com/spf13/cobra"
)

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update bino to the latest version",
		Long:  "Download and install the latest version of bino from GitHub releases.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "Checking for updates (current: %s)...\n", version.Version)

			result, err := updater.Update(cmd.Context())
			if err != nil {
				return fmt.Errorf("update failed: %w", err)
			}

			if result == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "You are already using the latest version.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Successfully updated from %s to %s\n", result.PreviousVersion, result.NewVersion)
			if result.ReleaseNotes != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "\nRelease notes:")
				fmt.Fprintln(cmd.OutOrStdout(), result.ReleaseNotes)
			}

			return nil
		},
	}
}
