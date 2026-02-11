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

			sp := NewSpinner(SpinnerConfig{Stdout: out})

			// Update CLI
			sp.Start(fmt.Sprintf("Checking for CLI updates (current: %s)", version.Version))

			result, err := updater.Update(ctx, func(msg string) {
				sp.Update(msg)
			})
			if err != nil {
				sp.StopWithError("CLI update failed")
				return fmt.Errorf("CLI update failed: %w", err)
			}

			if result == nil {
				sp.Update("CLI is already up to date")
				sp.Stop()
			} else {
				sp.Update(fmt.Sprintf("Updated CLI from %s to %s", result.PreviousVersion, result.NewVersion))
				sp.Stop()
				if result.ReleaseNotes != "" {
					fmt.Fprintln(out, "\nRelease notes:")
					fmt.Fprintln(out, result.ReleaseNotes)
				}
			}

			// Update template engine
			if err := updateTemplateEngine(cmd, sp); err != nil {
				return err
			}

			return nil
		},
	}
}

func updateTemplateEngine(cmd *cobra.Command, sp *Spinner) error {
	ctx := cmd.Context()

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
		sp.Start(fmt.Sprintf("Checking for template engine updates (current: %s)", localVersion))
	} else {
		sp.Start("Checking for template engine updates (not installed)")
	}

	// Get latest remote version
	remoteVersion, err := mgr.FetchLatestRemoteVersion(ctx)
	if err != nil {
		sp.StopWithError("Failed to check template engine updates")
		return ExternalError(fmt.Errorf("fetch latest template engine version: %w", err))
	}

	// Compare versions
	if localVersion != "" && semver.Compare(remoteVersion, localVersion) <= 0 {
		sp.Update("Template engine is already up to date")
		sp.Stop()
		return nil
	}

	// Download new version
	if localVersion != "" {
		sp.Update(fmt.Sprintf("Downloading template engine %s", remoteVersion))
	} else {
		sp.Update(fmt.Sprintf("Installing template engine %s", remoteVersion))
	}

	info, err := mgr.Download(ctx, remoteVersion)
	if err != nil {
		sp.StopWithError("Template engine update failed")
		return ExternalError(fmt.Errorf("download template engine: %w", err))
	}

	if localVersion != "" {
		sp.Update(fmt.Sprintf("Updated template engine from %s to %s", localVersion, info.Version))
	} else {
		sp.Update(fmt.Sprintf("Installed template engine %s", info.Version))
	}
	sp.Stop()

	return nil
}
