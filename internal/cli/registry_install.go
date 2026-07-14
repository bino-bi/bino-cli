package cli

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/registry"
)

func newRegistryInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install packages exactly as pinned in bino.lock",
		Long: `Re-materializes .bino/registry/ from bino.lock without re-resolving
anything, so CI and fresh checkouts reproduce the exact locked versions.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			lock, err := registry.LoadLockfile(p.Root)
			if err != nil {
				return ConfigError(err)
			}
			if len(lock.Packages) == 0 {
				if len(p.Cfg.Dependencies) == 0 {
					p.Out.Info("Nothing to install")
					return nil
				}
				return ConfigErrorf("bino.toml declares dependencies but %s has no packages — run 'bino registry update' to create it", registry.LockfileName)
			}
			if err := checkLockDrift(p, lock); err != nil {
				return err
			}

			client, err := p.client()
			if err != nil {
				return err
			}

			entries := append([]registry.Entry(nil), lock.Packages...)
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
			bodies := make(map[string][]byte, len(entries))
			for _, e := range entries {
				body, err := downloadVerified(ctx, client, e.Name, e.Version, e.Digest)
				if err != nil {
					var apiErr *registry.APIError
					if errors.As(err, &apiErr) && apiErr.Status == http.StatusGone {
						return ExternalErrorWithHint(
							fmt.Errorf("%s@%s is pinned in %s but has been yanked from the registry", e.Name, e.Version, registry.LockfileName),
							"run 'bino registry update' to re-resolve to an available version")
					}
					return ExternalError(err)
				}
				bodies[e.Name] = body
			}
			for _, e := range entries {
				if _, err := registry.WritePackage(p.Root, e.Name, bodies[e.Name]); err != nil {
					return RuntimeError(err)
				}
				for _, r := range e.Resources {
					body, err := downloadVerifiedResource(ctx, client, e.Name, e.Version, r.Name, r.ContentHash)
					if err != nil {
						var apiErr *registry.APIError
						if errors.Is(err, errResourceMismatch) || (errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound) {
							return ExternalErrorWithHint(
								fmt.Errorf("resource %q of %s@%s is pinned in %s but no longer matches what the registry serves", r.Name, e.Name, e.Version, registry.LockfileName),
								"run 'bino registry update' to re-resolve to the resource's current state")
						}
						return ExternalError(err)
					}
					if err := registry.WriteResource(p.Root, e.Name, r.Name, body); err != nil {
						return RuntimeError(err)
					}
				}
			}

			plan := make([]registry.Resolved, 0, len(entries))
			for _, e := range entries {
				plan = append(plan, registry.Resolved{Name: e.Name, Version: e.Version, Tag: e.Tag, Kind: e.Kind, Digest: e.Digest})
			}
			registryCompatWarnings(p, bodies, plan)
			registryGitignoreHint(p)
			p.Out.Success(fmt.Sprintf("Installed %d package(s) from %s", len(entries), registry.LockfileName))
			return nil
		},
	}
}

// checkLockDrift verifies bino.lock still reflects bino.toml's [dependencies]
// declarations, so install never silently materializes a stale closure.
func checkLockDrift(p registryProject, lock *registry.Lockfile) error {
	drift := func(format string, args ...any) error {
		return ConfigErrorf("%s is out of date with bino.toml (%s) — run 'bino registry update'", registry.LockfileName, fmt.Sprintf(format, args...))
	}
	direct := map[string]bool{}
	for _, e := range lock.Packages {
		if e.Direct {
			direct[e.Name] = true
		}
	}
	for name, ref := range p.Cfg.Dependencies {
		e := lock.Get(name)
		if e == nil || !e.Direct {
			return drift("%s is declared but not locked", name)
		}
		if ref == "" {
			ref = "latest"
		}
		if registry.IsPin(ref) {
			if e.Version != ref {
				return drift("%s is declared at %s but locked at %s", name, ref, e.Version)
			}
		} else if e.Tag != ref {
			return drift("%s follows tag %q but the lock records %q", name, ref, e.Tag)
		}
		delete(direct, name)
	}
	for name := range direct {
		return drift("%s is locked as a direct dependency but not declared", name)
	}
	return nil
}
