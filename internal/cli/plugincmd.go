package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/version"
)

// newPluginExecCommand creates a catch-all command for running plugin commands.
// Usage: bino exec <plugin>:<command> [args...] [flags]
// This avoids eager plugin loading at CLI startup.
func newPluginExecCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "exec <plugin:command> [args...]",
		Short:              "Run a plugin command",
		Long:               "Loads a plugin and executes one of its CLI commands.\nExample: bino exec example:hello --name World",
		DisableFlagParsing: true, // Pass all flags through to the plugin
		Args:               cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Parse "plugin:command" from first arg.
			parts := strings.SplitN(args[0], ":", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("expected <plugin>:<command>, got %q", args[0])
			}
			pluginName, cmdName := parts[0], parts[1]
			cmdArgs := args[1:]

			// Load the specific plugin.
			workdir, _ := os.Getwd()
			projectRoot, err := pathutil.FindProjectRoot(workdir)
			if err != nil {
				return fmt.Errorf("no bino project found (missing bino.toml)")
			}
			projectCfg, err := pathutil.LoadProjectConfig(projectRoot)
			if err != nil {
				return fmt.Errorf("loading bino.toml: %w", err)
			}

			if _, ok := projectCfg.Plugins[pluginName]; !ok {
				return fmt.Errorf("plugin %q is not declared in bino.toml", pluginName)
			}

			mgr := plugin.NewManager(logx.Nop())
			mgr.SetVerbose(logx.DebugEnabled(ctx))
			if err := mgr.LoadAll(ctx, projectCfg, projectRoot, version.Version); err != nil {
				return fmt.Errorf("loading plugins: %w", err)
			}
			defer mgr.KillAll()

			p, ok := mgr.Registry().Get(pluginName)
			if !ok {
				return fmt.Errorf("plugin %q failed to load", pluginName)
			}

			// Parse flags from raw args: extract --key value and --key=value pairs.
			parsedFlags := make(map[string]string)
			var positionalArgs []string
			for i := 0; i < len(cmdArgs); i++ {
				arg := cmdArgs[i]
				if strings.HasPrefix(arg, "--") || (strings.HasPrefix(arg, "-") && len(arg) == 2) {
					key := strings.TrimLeft(arg, "-")
					if idx := strings.Index(key, "="); idx >= 0 {
						parsedFlags[key[:idx]] = key[idx+1:]
					} else if i+1 < len(cmdArgs) && !strings.HasPrefix(cmdArgs[i+1], "-") {
						parsedFlags[key] = cmdArgs[i+1]
						i++
					} else {
						parsedFlags[key] = "true"
					}
				} else {
					positionalArgs = append(positionalArgs, arg)
				}
			}

			// Execute the command, streaming output.
			exitCode, err := p.ExecCommand(ctx, cmdName, positionalArgs, parsedFlags, workdir,
				func(stdout, stderr []byte) {
					if len(stdout) > 0 {
						os.Stdout.Write(stdout)
					}
					if len(stderr) > 0 {
						os.Stderr.Write(stderr)
					}
				},
			)
			if err != nil {
				return fmt.Errorf("plugin command %s:%s: %w", pluginName, cmdName, err)
			}
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		},
	}
}
