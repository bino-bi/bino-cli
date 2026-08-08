package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/version"
)

func newSchemaCommand() *cobra.Command {
	var kindFilter string

	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Output the merged JSON Schema (built-in + plugin kinds)",
		Long:  "Outputs the complete JSON Schema for all document kinds, including any kinds provided by plugins declared in bino.toml.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			logger := logx.Nop()

			// Try to load project config for plugins. Outside a project the
			// builtin schema is served; a real filesystem failure must not be
			// misread as "no project".
			workdir, _ := os.Getwd() //nolint:errcheck // outside a project the builtin schema is still served
			projectRoot, err := pathutil.FindProjectRoot(workdir)
			if err != nil && !errors.Is(err, pathutil.ErrProjectRootNotFound) {
				return ConfigErrorf("resolve project root: %w", err)
			}

			var registry *plugin.PluginRegistry

			if projectRoot != "" {
				projectCfg, err := pathutil.LoadProjectConfig(projectRoot)
				if err == nil && len(projectCfg.Plugins) > 0 {
					mgr := plugin.NewManager(logger)
					mgr.SetVerbose(logx.DebugEnabled(ctx))
					if err := mgr.LoadAll(ctx, projectCfg, projectRoot, version.Version); err == nil {
						defer mgr.KillAll()
						registry = mgr.Registry()
					}
				}
			}

			aggregator := plugin.NewSchemaAggregator(plugin.NewRegistry())
			if registry != nil {
				aggregator = plugin.NewSchemaAggregator(registry)
			}
			if err := aggregator.Build(ctx); err != nil {
				return fmt.Errorf("building schema: %w", err)
			}

			var schemaBytes json.RawMessage
			if kindFilter != "" {
				s, ok := aggregator.SchemaForKind(kindFilter)
				if !ok {
					return fmt.Errorf("unknown kind: %s", kindFilter)
				}
				schemaBytes = s
			} else {
				schemaBytes = aggregator.MergedSchema()
			}

			var buf bytes.Buffer
			if err := json.Indent(&buf, schemaBytes, "", "  "); err != nil {
				return fmt.Errorf("formatting schema: %w", err)
			}
			fmt.Fprintln(os.Stdout, buf.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&kindFilter, "kind", "", "Output schema for a specific plugin kind only")

	return cmd
}
