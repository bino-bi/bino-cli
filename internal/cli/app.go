package cli

import (
	"context"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
)

// App wires root command and exposes a uniform execution entry point.
type App struct {
	root *cobra.Command
	ran  *cobra.Command
}

// New constructs the CLI with every available command registered.
func New() *App {
	return &App{root: newRootCommand()}
}

// Execute runs the CLI within the provided context.
func (a *App) Execute(ctx context.Context) error {
	ran, err := a.root.ExecuteContextC(ctx)
	a.ran = ran
	return err
}

// JSONLogger returns the logger of the command that ran when it logs JSON
// (--log-format json).
func (a *App) JSONLogger() (*logx.JSONLogger, bool) {
	if a == nil || a.ran == nil {
		return nil, false
	}
	jl, ok := logx.FromContext(a.ran.Context()).(*logx.JSONLogger)
	return jl, ok
}

// Context returns the most recent command context, falling back to nil when unavailable.
func (a *App) Context() context.Context {
	if a == nil || a.root == nil {
		return nil
	}
	return a.root.Context()
}
