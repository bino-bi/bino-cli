package cli

import (
	"fmt"

	"golang.org/x/mod/semver"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/updater"
	"bino.bi/bino/internal/version"
	"github.com/spf13/cobra"
)

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update bino to the latest version",
		Long:  "Download and install the latest version of bino and template engine from GitHub releases.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			// Update CLI
			fmt.Fprintf(out, "Checking for CLI updates (current: %s)...\n", version.Version)

			result, err := updater.Update(ctx)
			if err != nil {
				return fmt.Errorf("CLI update failed: %w", err)
			}

			if result == nil {
				fmt.Fprintln(out, "CLI is already up to date.")
			} else {
				fmt.Fprintf(out, "Successfully updated CLI from %s to %s\n", result.PreviousVersion, result.NewVersion)
				if result.ReleaseNotes != "" {
					fmt.Fprintln(out, "\nRelease notes:")
					fmt.Fprintln(out, result.ReleaseNotes)
				}
			}

			// Update template engine
			fmt.Fprintln(out, "")
			if err := updateTemplateEngine(cmd); err != nil {
				return err
			}

			return nil
		},
	}
}

func updateTemplateEngine(cmd *cobra.Command) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	mgr, err := engine.NewManager()
	if err != nil {
		return fmt.Errorf("initialize engine manager: %w", err)
	}

	// Get current local version (if any)
	localVersion := ""
	localInfo, err := mgr.LatestLocalVersion()
	if err == nil {
		localVersion = localInfo.Version
	}

	if localVersion != "" {
		fmt.Fprintf(out, "Checking for template engine updates (current: %s)...\n", localVersion)
	} else {
		fmt.Fprintln(out, "Checking for template engine updates (not installed)...")
	}

	// Get latest remote version
	remoteVersion, err := mgr.FetchLatestRemoteVersion(ctx)
	if err != nil {
		return ExternalError(fmt.Errorf("fetch latest template engine version: %w", err))
	}

	// Compare versions
	if localVersion != "" && semver.Compare(remoteVersion, localVersion) <= 0 {
		fmt.Fprintln(out, "Template engine is already up to date.")
		return nil
	}

	// Download new version
	if localVersion != "" {
		fmt.Fprintf(out, "Downloading template engine %s...\n", remoteVersion)
	} else {
		fmt.Fprintf(out, "Installing template engine %s...\n", remoteVersion)
	}

	info, err := mgr.Download(ctx, remoteVersion)
	if err != nil {
		return ExternalError(fmt.Errorf("download template engine: %w", err))
	}

	if localVersion != "" {
		fmt.Fprintf(out, "Successfully updated template engine from %s to %s\n", localVersion, info.Version)
	} else {
		fmt.Fprintf(out, "Template engine %s installed at %s\n", info.Version, info.Path)
	}

	return nil
}
