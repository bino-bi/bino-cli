package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
)

func newRegistryRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <package>...",
		Short: "Remove declared dependencies and their unused transitives",
		Long: `Removes packages from bino.toml's [dependencies] table, then deletes every
locked package no longer reachable from the remaining declarations. Works
offline. A transitive dependency still required by another package is kept.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			res, err := registryRemove(p, args)
			if err != nil {
				return err
			}
			swept := 0
			for _, c := range res.Changes {
				if c.After == "" {
					p.Out.List(fmt.Sprintf("removed %s %s", c.Name, c.Before))
					swept++
				} else {
					p.Out.List(fmt.Sprintf("kept %s (still required by another package)", c.Name))
				}
			}
			p.Out.Success(fmt.Sprintf("Removed %d package(s), swept %d file(s)", len(args), swept))
			return nil
		},
	}
}

// registryRemove drops args from bino.toml's [dependencies] and sweeps every
// locked package no longer reachable from the remaining declarations. Swept
// packages come first (After empty), then the removed declarations that
// survive as someone else's transitive. Offline; it never prints.
func registryRemove(p registryProject, args []string) (mcp.RegistryMutationResult, error) {
	removed := make([]string, 0, len(args))
	for _, arg := range args {
		spec, err := registry.ParseSpec(arg)
		if err != nil {
			return mcp.RegistryMutationResult{}, ConfigError(err)
		}
		if spec.Ref != "" {
			return mcp.RegistryMutationResult{}, ConfigErrorf("remove takes package names without a version: %s", arg)
		}
		if _, declared := p.Cfg.Dependencies[spec.Name]; !declared {
			return mcp.RegistryMutationResult{}, ConfigErrorf("%s is not a declared dependency in bino.toml", spec.Name)
		}
		removed = append(removed, spec.Name)
	}

	lock, err := registry.LoadLockfile(p.Root)
	if err != nil {
		return mcp.RegistryMutationResult{}, ConfigError(err)
	}

	// Mark-and-sweep from the remaining declared roots over the
	// lock's recorded dependency edges.
	removedSet := map[string]bool{}
	for _, name := range removed {
		removedSet[name] = true
	}
	reachable := map[string]bool{}
	var mark func(name string)
	mark = func(name string) {
		if reachable[name] {
			return
		}
		reachable[name] = true
		if e := lock.Get(name); e != nil {
			for _, dep := range e.Dependencies {
				mark(dep)
			}
		}
	}
	for name := range p.Cfg.Dependencies {
		if !removedSet[name] {
			mark(name)
		}
	}

	changes := []mcp.RegistryChange{}
	for _, e := range append([]registry.Entry(nil), lock.Packages...) {
		if reachable[e.Name] {
			continue
		}
		if err := registry.RemovePackage(p.Root, e.Name); err != nil {
			return mcp.RegistryMutationResult{}, RuntimeError(err)
		}
		lock.Remove(e.Name)
		changes = append(changes, mcp.RegistryChange{Name: e.Name, Before: e.Version, Tag: e.Tag, Direct: e.Direct})
	}
	// A removed declaration that survives as someone's transitive
	// dependency is no longer direct.
	for _, name := range removed {
		if e := lock.Get(name); e != nil {
			e.Direct = false
			changes = append(changes, mcp.RegistryChange{Name: name, Before: e.Version, After: e.Version, Tag: e.Tag})
		}
	}
	if err := registry.SaveLockfile(p.Root, lock); err != nil {
		return mcp.RegistryMutationResult{}, RuntimeError(err)
	}
	for _, name := range removed {
		if err := registry.RemoveDependency(p.Root, name); err != nil {
			return mcp.RegistryMutationResult{}, ConfigError(err)
		}
	}
	return mcp.RegistryMutationResult{Changes: changes}, nil
}
