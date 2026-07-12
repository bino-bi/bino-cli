package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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
			ctx := cmd.Context()
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}

			specs := make([]registry.Spec, 0, len(args))
			for _, arg := range args {
				spec, err := registry.ParseSpec(arg)
				if err != nil {
					return ConfigError(err)
				}
				if spec.Ref == "" {
					spec.Ref = "latest"
				}
				specs = append(specs, spec)
			}

			roots, err := dependencyRoots(p.Cfg)
			if err != nil {
				return err
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
				return err
			}
			plan, err := registry.ResolveClosure(ctx, client, roots)
			if err != nil {
				return registryCommandError(err)
			}
			bodies, err := registrySync(ctx, p, client, plan)
			if err != nil {
				return err
			}

			for _, spec := range specs {
				if err := registry.SetDependency(p.Root, spec.Name, spec.Ref); err != nil {
					return ConfigError(err)
				}
			}

			added := make(map[string]bool, len(specs))
			for _, spec := range specs {
				added[spec.Name] = true
			}
			for _, r := range plan {
				if added[r.Name] {
					p.Out.Success(fmt.Sprintf("Added %s %s%s", r.Name, r.Version, tagSuffix(r.Tag)))
				}
			}
			for _, r := range plan {
				if !added[r.Name] {
					p.Out.List(fmt.Sprintf("%s %s (dependency)", r.Name, r.Version))
				}
			}
			registryCompatWarnings(p, bodies, plan)
			registryGitignoreHint(p)
			return nil
		},
	}
}

func tagSuffix(tag string) string {
	if tag == "" {
		return " (pinned)"
	}
	return fmt.Sprintf(" (%s)", tag)
}
