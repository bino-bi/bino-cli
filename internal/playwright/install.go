package playwright

import (
	"context"
	"fmt"
	"io"

	pw "github.com/playwright-community/playwright-go"
)

// InstallOptions configures the dependency bootstrapper for Playwright.
type InstallOptions struct {
	Browsers        []string
	DriverDirectory string
	DryRun          bool
	Quiet           bool
	Stdout          io.Writer
	Stderr          io.Writer
}

// Install downloads or updates the Playwright driver and browsers.
// It checks ctx.Err() at entry to allow early cancellation before the
// potentially long-running download operation begins.
//
// Note: The underlying pw.Install() does not support context cancellation.
// Once the download starts, it will run to completion or fail on its own.
// The context check at entry prevents unnecessary work when already canceled.
func Install(ctx context.Context, opts InstallOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	browsers := opts.Browsers
	if len(browsers) == 0 {
		browsers = []string{"chromium"}
	}

	runOpts := &pw.RunOptions{
		Browsers:        browsers,
		DriverDirectory: opts.DriverDirectory,
		DryRun:          opts.DryRun,
	}

	if opts.Quiet {
		runOpts.Verbose = false
	}

	if opts.Stdout != nil {
		runOpts.Stdout = opts.Stdout
	}

	if opts.Stderr != nil {
		runOpts.Stderr = opts.Stderr
	}

	if err := pw.Install(runOpts); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("install playwright artifacts: %w", err)
	}

	return nil
}
