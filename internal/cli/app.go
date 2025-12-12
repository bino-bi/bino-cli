package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// App wires root command and exposes a uniform execution entry point.
type App struct {
	root *cobra.Command
}

// New constructs the CLI with every available command registered.
func New() *App {
	return &App{root: newRootCommand()}
}

// Execute runs the CLI within the provided context.
func (a *App) Execute(ctx context.Context) error {
	return a.root.ExecuteContext(ctx)
}

// Context returns the most recent command context, falling back to nil when unavailable.
func (a *App) Context() context.Context {
	if a == nil || a.root == nil {
		return nil
	}
	return a.root.Context()
}
