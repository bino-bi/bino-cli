package mcp

import (
	"context"

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
}
