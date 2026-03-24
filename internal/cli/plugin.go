package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/version"
)

func newPluginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage and inspect plugins",
		Long:  "Commands for listing and inspecting bino plugins declared in bino.toml.",
	}

	cmd.AddCommand(newPluginListCommand())
	cmd.AddCommand(newPluginExecCommand())

	return cmd
}

func newPluginListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List loaded plugins",
		Long:  "Discovers and loads all plugins declared in bino.toml, then displays their name, version, and description.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := logx.Nop()

			workdir, _ := os.Getwd()
			projectRoot, err := pathutil.FindProjectRoot(workdir)
			if err != nil {
				return fmt.Errorf("no bino project found (missing bino.toml)")
			}

			projectCfg, err := pathutil.LoadProjectConfig(projectRoot)
			if err != nil {
				return fmt.Errorf("loading bino.toml: %w", err)
			}

			if len(projectCfg.Plugins) == 0 {
				fmt.Println("No plugins declared in bino.toml.")
				return nil
			}

			mgr := plugin.NewManager(logger)
			mgr.SetVerbose(logx.DebugEnabled(ctx))
			if err := mgr.LoadAll(ctx, projectCfg, projectRoot, version.Version); err != nil {
				return fmt.Errorf("loading plugins: %w", err)
			}
			defer mgr.KillAll()

			registry := mgr.Registry()
			plugins := registry.AllPlugins()

			if len(plugins) == 0 {
				fmt.Println("No plugins loaded.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION")
			for _, p := range plugins {
				m := p.Manifest()
				fmt.Fprintf(w, "%s\t%s\t%s\n", m.Name, m.Version, m.Description)
			}
			w.Flush()

			return nil
		},
	}
}
