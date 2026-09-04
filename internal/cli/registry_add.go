package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
)

func newRegistryAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <package[@version|@tag]>...",
		Short: "Add registry packages as dependencies",
		Long: `Resolves each package (and its transitive dependencies), downloads the
verified documents into .bino/registry/, pins the result in bino.lock, and
records the dependency in bino.toml. A bare package name follows the "latest"
tag; "@1.2.3" pins that exact version.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			res, err := registryAdd(cmd.Context(), p, args)
			if err != nil {
				return err
			}

			requested := make(map[string]bool, len(args))
			for _, arg := range args {
				spec, _ := registry.ParseSpec(arg) // already validated by registryAdd
				requested[spec.Name] = true
			}
			for _, c := range res.Changes {
				if c.After != "" && requested[c.Name] {
					p.Out.Success(fmt.Sprintf("Added %s %s%s", c.Name, c.After, tagSuffix(c.Tag)))
				}
			}
			for _, c := range res.Changes {
				if c.After != "" && !requested[c.Name] {
					p.Out.List(fmt.Sprintf("%s %s (dependency)", c.Name, c.After))
				}
			}
			for _, w := range res.Warnings {
				p.Out.Warning(w)
			}
			registryGitignoreHint(p)
			return nil
		},
	}
}

// registryAdd resolves args on top of the declared dependencies, materializes
// the closure and records the new declarations in bino.toml. It never prints:
// the MCP server calls it too, and its stdout may be the JSON-RPC channel.
func registryAdd(ctx context.Context, p registryProject, args []string) (mcp.RegistryMutationResult, error) {
	specs := make([]registry.Spec, 0, len(args))
	for _, arg := range args {
		spec, err := registry.ParseSpec(arg)
		if err != nil {
			return mcp.RegistryMutationResult{}, ConfigError(err)
		}
		if spec.Ref == "" {
			spec.Ref = "latest"
		}
		specs = append(specs, spec)
	}

	roots, err := dependencyRoots(p.Cfg)
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}
	// New specs override an existing declaration of the same package.
	byName := make(map[string]int, len(roots))
	for i, r := range roots {
		byName[r.Name] = i
	}
	for _, spec := range specs {
		if i, ok := byName[spec.Name]; ok {
			roots[i].Ref = spec.Ref
		} else {
			roots = append(roots, registry.Root{Name: spec.Name, Ref: spec.Ref})
		}
	}

	client, err := p.client()
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}
	plan, err := registry.ResolveClosure(ctx, client, roots)
	if err != nil {
		return mcp.RegistryMutationResult{}, registryCommandError(err)
	}
	oldLock, err := registry.LoadLockfile(p.Root)
	if err != nil {
		return mcp.RegistryMutationResult{}, ConfigError(err)
	}
	plans, err := registrySync(ctx, p, client, plan)
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}

	for _, spec := range specs {
		if err := registry.SetDependency(p.Root, spec.Name, spec.Ref); err != nil {
			return mcp.RegistryMutationResult{}, ConfigError(err)
		}
	}
	return mcp.RegistryMutationResult{
		Changes:  lockChanges(oldLock, plan),
		Warnings: registryCompatWarnings(p, planEntries(plans)),
	}, nil
}

func tagSuffix(tag string) string {
	if tag == "" {
		return " (pinned)"
	}
	return fmt.Sprintf(" (%s)", tag)
}
