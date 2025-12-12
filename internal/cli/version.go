package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/version"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the bino version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.BuildSummary())
		},
	}
}
