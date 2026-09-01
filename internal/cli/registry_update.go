package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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
			ctx := cmd.Context()
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			roots, err := dependencyRoots(p.Cfg)
			if err != nil {
				return err
			}
			if len(roots) == 0 {
				p.Out.Info("No dependencies declared in bino.toml")
				return nil
			}
			oldLock, err := registry.LoadLockfile(p.Root)
			if err != nil {
				return ConfigError(err)
			}

			if len(args) > 0 {
				selected := map[string]bool{}
				for _, arg := range args {
					spec, err := registry.ParseSpec(arg)
					if err != nil {
						return ConfigError(err)
					}
					if _, declared := p.Cfg.Dependencies[spec.Name]; !declared {
						return ConfigErrorf("%s is not a declared dependency in bino.toml", spec.Name)
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
				return err
			}
			plan, err := registry.ResolveClosure(ctx, client, roots)
			if err != nil {
				return registryCommandError(err)
			}
			plans, err := registrySync(ctx, p, client, plan)
			if err != nil {
				return err
			}

			changes := 0
			inPlan := map[string]bool{}
			for _, r := range plan {
				inPlan[r.Name] = true
				old := oldLock.Get(r.Name)
				switch {
				case old == nil:
					p.Out.List(fmt.Sprintf("%s %s%s added", r.Name, r.Version, tagSuffix(r.Tag)))
					changes++
				case old.Version != r.Version:
					p.Out.List(fmt.Sprintf("%s %s -> %s%s", r.Name, old.Version, r.Version, tagSuffix(r.Tag)))
					changes++
				}
			}
			for _, e := range oldLock.Packages {
				if !inPlan[e.Name] {
					p.Out.List(fmt.Sprintf("%s %s removed", e.Name, e.Version))
					changes++
				}
			}
			registryCompatWarnings(p, planEntries(plans))
			if changes == 0 {
				p.Out.Success("Everything up to date")
			} else {
				p.Out.Success(fmt.Sprintf("Updated %s (%d change(s))", registry.LockfileName, changes))
			}
			return nil
		},
	}
}
