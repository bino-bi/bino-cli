package mcp

import (
	"context"
	"errors"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/registry"
)

func (h *handlers) registerPackagesTools(srv *mcpsdk.Server) {
	p := h.deps.Packages
	if p == nil {
		return
	}

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_packages",
		Description: "List the project's package dependencies fully offline: bino.lock entries merged with bino.toml [dependencies], whether each is installed under .bino/registry/, and the params each package declares. A package declared in bino.toml but missing from bino.lock is listed with installed=false (the human runs `bino registry install`).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, registryPackagesOutput, error) {
		pkgs, err := p.Packages(ctx)
		if err != nil {
			return errorResult(err), registryPackagesOutput{}, nil
		}
		return nil, registryPackagesOutput{Packages: pkgs}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_search",
		Description: "Search the package registry (network). Use it before authoring a component from scratch: a published predef may already provide it.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in registrySearchInput) (*mcpsdk.CallToolResult, registry.SearchResult, error) {
		res, err := p.Search(ctx, registry.SearchParams{
			Query:   in.Query,
			Kinds:   in.Kinds,
			Scopes:  in.Scopes,
			Tags:    in.Tags,
			Page:    in.Page,
			PerPage: in.PerPage,
		})
		if err != nil {
			return errorResult(err), registry.SearchResult{}, nil
		}
		return nil, res, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_info",
		Description: "Resolve one package spec (@scope/name[@version|@tag]) against the registry (network): resolved version, kinds, files, dependencies, and installedVersion when the project already locks it.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in registryInfoInput) (*mcpsdk.CallToolResult, daemon.RegistryInfoResult, error) {
		spec, err := registry.ParseSpec(in.Spec)
		if err != nil {
			return errorResult(err), daemon.RegistryInfoResult{}, nil
		}
		res, err := p.Info(ctx, spec)
		if err != nil {
			return errorResult(err), daemon.RegistryInfoResult{}, nil
		}
		return nil, res, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_auth_status",
		Description: "Report the resolved registry URL, whether a credential is available, and where credentials are stored. Never returns the token. When not authenticated, the human must run `bino registry login`; do not try to obtain a token.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, RegistryAuthStatus, error) {
		res, err := p.AuthStatus(ctx)
		if err != nil {
			return errorResult(err), RegistryAuthStatus{}, nil
		}
		return nil, res, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_add",
		Description: "Add registry packages as project dependencies (network; writes bino.toml, bino.lock and .bino/registry/). Each spec is @scope/name[@version|@tag]; a bare name follows the latest tag. Resolves the transitive closure, verifies every download before writing anything, and reloads the project before returning. The result lists every package in the closure with its locked version before and after, non-blocking compat warnings, and nameCollisions: an installed document that shares kind and name with a local document fails the build's duplicate-name check — rename the local document.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in registryAddInput) (*mcpsdk.CallToolResult, RegistryMutationResult, error) {
		if len(in.Specs) == 0 {
			return errorResult(errors.New("specs is required")), RegistryMutationResult{}, nil
		}
		return h.mutatePackages(func() (RegistryMutationResult, error) { return p.Add(ctx, in.Specs) })
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_update",
		Description: "Re-resolve tag-following dependencies to their newest versions and rewrite bino.lock (network). Pinned versions are held. With packages given, only those are re-resolved and every other direct dependency is held at its locked version.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in registryUpdateInput) (*mcpsdk.CallToolResult, RegistryMutationResult, error) {
		return h.mutatePackages(func() (RegistryMutationResult, error) { return p.Update(ctx, in.Packages) })
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_remove",
		Description: "Remove declared dependencies from bino.toml and sweep every locked package no longer reachable from the remaining declarations (offline). A transitive still required by another package is kept.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in registryRemoveInput) (*mcpsdk.CallToolResult, RegistryMutationResult, error) {
		if len(in.Packages) == 0 {
			return errorResult(errors.New("packages is required")), RegistryMutationResult{}, nil
		}
		return h.mutatePackages(func() (RegistryMutationResult, error) { return p.Remove(ctx, in.Packages) })
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_install",
		Description: "Re-materialize .bino/registry/ exactly as pinned in bino.lock without re-resolving (idempotent). Fails when bino.lock disagrees with bino.toml [dependencies]; then run registry_update.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, RegistryMutationResult, error) {
		return h.mutatePackages(func() (RegistryMutationResult, error) { return p.Install(ctx) })
	})
}

// mutatePackages runs one registry write under buildMu: two writes, or a write
// and a build, must never interleave on bino.lock and .bino/registry/. On
// success it tells the daemon's editor clients.
func (h *handlers) mutatePackages(op func() (RegistryMutationResult, error)) (*mcpsdk.CallToolResult, RegistryMutationResult, error) {
	buildMu.Lock()
	defer buildMu.Unlock()
	res, err := op()
	if err != nil {
		return errorResult(err), RegistryMutationResult{}, nil
	}
	if h.deps.RegistryChanged != nil {
		h.deps.RegistryChanged()
	}
	return nil, res, nil
}
