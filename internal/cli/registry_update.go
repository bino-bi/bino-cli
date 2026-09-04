package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
)

func newRegistryUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update [package...]",
		Short: "Re-resolve tag-following dependencies to their newest versions",
		Long: `Re-resolves the dependencies declared in bino.toml and rewrites bino.lock.
Entries that follow a tag move with it; exact-version pins are held. With
package arguments, only those packages are re-resolved and every other direct
dependency is held at its locked version.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			res, err := registryUpdate(cmd.Context(), p, args)
			if err != nil {
				return err
			}
			if len(res.Changes) == 0 {
				p.Out.Info("No dependencies declared in bino.toml")
				return nil
			}

			changes := 0
			for _, c := range res.Changes {
				switch {
				case c.After == "":
					p.Out.List(fmt.Sprintf("%s %s removed", c.Name, c.Before))
				case c.Before == "":
					p.Out.List(fmt.Sprintf("%s %s%s added", c.Name, c.After, tagSuffix(c.Tag)))
				case c.Before != c.After:
					p.Out.List(fmt.Sprintf("%s %s -> %s%s", c.Name, c.Before, c.After, tagSuffix(c.Tag)))
				default:
					continue
				}
				changes++
			}
			for _, w := range res.Warnings {
				p.Out.Warning(w)
			}
			if changes == 0 {
				p.Out.Success("Everything up to date")
			} else {
				p.Out.Success(fmt.Sprintf("Updated %s (%d change(s))", registry.LockfileName, changes))
			}
			return nil
		},
	}
}

// registryUpdate re-resolves the declared dependencies (only args when given,
// holding every other direct dependency at its locked version) and rewrites
// the lock. With nothing declared it touches nothing and returns no changes.
// It never prints: the MCP server calls it too.
func registryUpdate(ctx context.Context, p registryProject, args []string) (mcp.RegistryMutationResult, error) {
	roots, err := dependencyRoots(p.Cfg)
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}
	if len(roots) == 0 {
		return mcp.RegistryMutationResult{Changes: []mcp.RegistryChange{}}, nil
	}
	oldLock, err := registry.LoadLockfile(p.Root)
	if err != nil {
		return mcp.RegistryMutationResult{}, ConfigError(err)
	}

	if len(args) > 0 {
		selected := map[string]bool{}
		for _, arg := range args {
			spec, err := registry.ParseSpec(arg)
			if err != nil {
				return mcp.RegistryMutationResult{}, ConfigError(err)
			}
			if _, declared := p.Cfg.Dependencies[spec.Name]; !declared {
				return mcp.RegistryMutationResult{}, ConfigErrorf("%s is not a declared dependency in bino.toml", spec.Name)
			}
			selected[spec.Name] = true
		}
		// Hold every unselected direct dependency at its locked version.
		for i, r := range roots {
			if selected[r.Name] {
				continue
			}
			if e := oldLock.Get(r.Name); e != nil {
				roots[i].Ref = e.Version
			}
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
	plans, err := registrySync(ctx, p, client, plan)
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}
	return mcp.RegistryMutationResult{
		Changes:  lockChanges(oldLock, plan),
		Warnings: registryCompatWarnings(p, planEntries(plans)),
	}, nil
}
