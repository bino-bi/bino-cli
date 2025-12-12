package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

// newCacheCommand creates the cache command group.
func newCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage bino cache",
		Long:  "Commands for managing the bino cache directories.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newCacheCleanCommand())
	return cmd
}

// newCacheCleanCommand creates the cache clean subcommand.
func newCacheCleanCommand() *cobra.Command {
	var (
		workdir string
		global  bool
	)
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove cached data",
		Long: `Remove cached dataset results and other cached data.

By default, removes the .bncache directory in the current working directory
(or the directory specified by --work-dir).

Use --global to also remove the global cache directory (~/.bn/).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := logx.FromContext(cmd.Context())

			// Resolve workdir
			resolvedWorkdir, err := pathutil.ResolveWorkdir(workdir)
			if err != nil {
				return fmt.Errorf("resolve workdir: %w", err)
			}

			// Clean local cache
			localCacheDir := filepath.Join(resolvedWorkdir, ".bncache")
			if err := cleanCacheDir(logger, localCacheDir, "local"); err != nil {
				return err
			}

			// Clean global cache if requested
			if global {
				globalCacheDir, err := globalCachePath()
				if err != nil {
					return fmt.Errorf("resolve global cache: %w", err)
				}
				if err := cleanCacheDir(logger, globalCacheDir, "global"); err != nil {
					return err
				}
			}

			logger.Infof("Cache cleaned successfully")
			return nil
		},
	}

	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory containing .bncache")
	cmd.Flags().BoolVar(&global, "global", false, "Also remove global cache (~/.bn/)")

	return cmd
}

// cleanCacheDir removes a cache directory if it exists.
func cleanCacheDir(logger logx.Logger, dir, label string) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		logger.Debugf("No %s cache directory found at %s", label, dir)
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s cache: %w", label, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s cache path is not a directory: %s", label, dir)
	}

	logger.Infof("Removing %s cache: %s", label, dir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s cache: %w", label, err)
	}
	return nil
}

// globalCachePath returns the path to the global bino cache directory.
func globalCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, ".bn"), nil
}
