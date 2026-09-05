package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/mcp"
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
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			res, err := registryInstall(cmd.Context(), p)
			if err != nil {
				return err
			}
			if len(res.Changes) == 0 {
				p.Out.Info("Nothing to install")
				return nil
			}
			for _, w := range res.Warnings {
				p.Out.Warning(w)
			}
			registryGitignoreHint(p)
			p.Out.Success(fmt.Sprintf("Installed %d package(s) from %s", len(res.Changes), registry.LockfileName))
			return nil
		},
	}
}

// registryInstall re-materializes .bino/registry/ from bino.lock without
// re-resolving, refusing a lock that disagrees with bino.toml. Every locked
// package is reported with Before == After. It never prints: the MCP server
// calls it too.
func registryInstall(ctx context.Context, p registryProject) (mcp.RegistryMutationResult, error) {
	lock, err := registry.LoadLockfile(p.Root)
	if err != nil {
		return mcp.RegistryMutationResult{}, ConfigError(err)
	}
	if len(lock.Packages) == 0 {
		if len(p.Cfg.Dependencies) == 0 {
			return mcp.RegistryMutationResult{Changes: []mcp.RegistryChange{}}, nil
		}
		return mcp.RegistryMutationResult{}, ConfigErrorf("bino.toml declares dependencies but %s has no packages — run 'bino registry update' to create it", registry.LockfileName)
	}
	if err := checkLockDrift(p, lock); err != nil {
		return mcp.RegistryMutationResult{}, err
	}

	client, err := p.client()
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}

	entries := append([]registry.Entry(nil), lock.Packages...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	plans := make([]packagePlan, 0, len(entries))
	for _, e := range entries {
		pp, err := fetchLockedPackage(ctx, p, client, e)
		if err != nil {
			return mcp.RegistryMutationResult{}, err
		}
		plans = append(plans, pp)
	}
	// The lock is authoritative here — it is not rewritten, so a v1
	// lock stays a v1 lock on disk and re-installing produces no diff.
	if err := materialize(p, plans, false); err != nil {
		return mcp.RegistryMutationResult{}, err
	}

	changes := make([]mcp.RegistryChange, 0, len(entries))
	for _, e := range entries {
		changes = append(changes, mcp.RegistryChange{Name: e.Name, Before: e.Version, After: e.Version, Tag: e.Tag, Direct: e.Direct})
	}
	return mcp.RegistryMutationResult{
		Changes:  changes,
		Warnings: registryCompatWarnings(p, entries),
	}, nil
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
		// LoadLockfile fills the format of every entry of a version-1 lock,
		// so an empty one here means the file claims version 2 while an older
		// bino rewrote it and dropped the fields this CLI needs.
		if e.Format == "" {
			return ConfigErrorf("%s records %s without a package format — it was probably rewritten by an older bino; run 'bino registry update'", registry.LockfileName, e.Name)
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

// fetchLockedPackage downloads and verifies one package exactly as bino.lock
// pins it, mapping the two failures a stale lock produces onto an actionable
// hint. It re-uses any file already materialized with a matching digest, so a
// warm store costs no bandwidth.
func fetchLockedPackage(ctx context.Context, p registryProject, client *registry.Client, e registry.Entry) (packagePlan, error) {
	plan := packagePlan{entry: e, bodies: map[string][]byte{}}
	for _, f := range e.TreeFiles() {
		if reuseOnDisk(p.Root, e.Format, e.Name, f) {
			continue
		}
		body, err := fetchLockedFile(ctx, client, e, f)
		if err != nil {
			return packagePlan{}, err
		}
		plan.bodies[f.Path] = body
	}
	return plan, nil
}

// fetchLockedFile downloads one file of a locked package over the routes its
// format lives on: a file-tree package through the v2 file route, a
// single-document package through the v1 document and resource routes, whose
// bundled resources are not part of the one-file tree a v2 registry renders
// for it.
func fetchLockedFile(ctx context.Context, client *registry.Client, e registry.Entry, f registry.FileEntry) ([]byte, error) {
	if e.IsTree() {
		body, err := downloadVerifiedTreeFile(ctx, client, e.Name, e.Version, f)
		if err != nil {
			return nil, lockedFetchError(e, f, err)
		}
		return body, nil
	}
	if f.Type == registry.FileResource {
		body, err := downloadVerifiedResource(ctx, client, e.Name, e.Version, f.Path, f.Digest)
		if err != nil {
			return nil, lockedFetchError(e, f, err)
		}
		return body, nil
	}
	body, err := downloadVerified(ctx, client, e.Name, e.Version, f.Digest)
	if err != nil {
		return nil, lockedFetchError(e, f, err)
	}
	return body, nil
}

// lockedFetchError turns a download failure during install into the one thing
// the user can do about it. Install never re-resolves, so anything the
// registry no longer serves as pinned means the lock has to be refreshed.
func lockedFetchError(e registry.Entry, f registry.FileEntry, err error) error {
	var apiErr *registry.APIError
	isAPI := errors.As(err, &apiErr)
	switch {
	case isAPI && apiErr.Status == http.StatusGone:
		return ExternalErrorWithHint(
			fmt.Errorf("%s@%s is pinned in %s but has been yanked from the registry", e.Name, e.Version, registry.LockfileName),
			"run 'bino registry update' to re-resolve to an available version")
	case isAPI && apiErr.Code == "requires_newer_client":
		return ExternalErrorWithHint(
			fmt.Errorf("%s@%s is a multi-file package that this project's %s records as a single document", e.Name, e.Version, registry.LockfileName),
			"run 'bino registry update' to re-resolve it in the current format")
	case errors.Is(err, errResourceMismatch) || (isAPI && apiErr.Status == http.StatusNotFound):
		return ExternalErrorWithHint(
			fmt.Errorf("%s of %s@%s is pinned in %s but no longer matches what the registry serves", f.Path, e.Name, e.Version, registry.LockfileName),
			"run 'bino registry update' to re-resolve to the package's current state")
	}
	return ExternalError(err)
}
