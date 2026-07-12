package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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
			removed := make([]string, 0, len(args))
			for _, arg := range args {
				spec, err := registry.ParseSpec(arg)
				if err != nil {
					return ConfigError(err)
				}
				if spec.Ref != "" {
					return ConfigErrorf("remove takes package names without a version: %s", arg)
				}
				if _, declared := p.Cfg.Dependencies[spec.Name]; !declared {
					return ConfigErrorf("%s is not a declared dependency in bino.toml", spec.Name)
				}
				removed = append(removed, spec.Name)
			}

			lock, err := registry.LoadLockfile(p.Root)
			if err != nil {
				return ConfigError(err)
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

			swept := 0
			for _, e := range append([]registry.Entry(nil), lock.Packages...) {
				if reachable[e.Name] {
					continue
				}
				if err := registry.RemovePackage(p.Root, e.Name); err != nil {
					return RuntimeError(err)
				}
				lock.Remove(e.Name)
				p.Out.List(fmt.Sprintf("removed %s %s", e.Name, e.Version))
				swept++
			}
			// A removed declaration that survives as someone's transitive
			// dependency is no longer direct.
			for _, name := range removed {
				if e := lock.Get(name); e != nil {
					e.Direct = false
					p.Out.List(fmt.Sprintf("kept %s (still required by another package)", name))
				}
			}
			if err := registry.SaveLockfile(p.Root, lock); err != nil {
				return RuntimeError(err)
			}
			for _, name := range removed {
				if err := registry.RemoveDependency(p.Root, name); err != nil {
					return ConfigError(err)
				}
			}
			p.Out.Success(fmt.Sprintf("Removed %d package(s), swept %d file(s)", len(removed), swept))
			return nil
		},
	}
}
