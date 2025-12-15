package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/version"
)

func newAboutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "about",
		Short: "Display project information and dependencies",
		Long:  "Show detailed information about bino including version, author, license, and a list of direct dependencies with their licenses.",
		Run: func(cmd *cobra.Command, _ []string) {
			printAbout(cmd.OutOrStdout())
		},
	}
}

func printAbout(w io.Writer) {
	s := GetStyle()

	// Header
	fmt.Fprintln(w)
	s.CyanBold.Fprintf(w, "  %s\n", AppMetadata.Name)
	s.Dim.Fprintf(w, "  %s\n", AppMetadata.Description)
	fmt.Fprintln(w)

	// Version info
	s.Bold.Fprintf(w, "  Version\n")
	fmt.Fprintf(w, "    %s %s\n", SymbolBullet, version.BuildSummary())
	fmt.Fprintln(w)

	// // Project details
	// s.Bold.Fprintf(w, "  Project\n")
	// fmt.Fprintf(w, "    %s URL:     %s\n", SymbolBullet, AppMetadata.URL)
	// fmt.Fprintf(w, "    %s Author:  %s <%s>\n", SymbolBullet, AppMetadata.Author, AppMetadata.Email)
	// fmt.Fprintf(w, "    %s Years:   %s\n", SymbolBullet, AppMetadata.Years)
	// fmt.Fprintf(w, "    %s License: %s\n", SymbolBullet, AppMetadata.License)
	// fmt.Fprintln(w)

	// Dependencies
	s.Bold.Fprintf(w, "  Dependencies (%d direct)\n", len(DirectDependencies))
	for _, dep := range DirectDependencies {
		fmt.Fprintf(w, "    %s %-44s %s\n", SymbolBullet, dep.Module, s.Dim.Sprint(dep.Version))
		fmt.Fprintf(w, "      %s %s  %s %s\n",
			SymbolArrow, dep.URL,
			s.Yellow.Sprint("License:"), dep.License)
	}
	fmt.Fprintln(w)
}
