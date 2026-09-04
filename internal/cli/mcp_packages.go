package cli

import (
	"context"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
)

// cliPackages implements mcp.Packages over the daemon's registry computation.
// It is bound to one project State, so the root is the server's fixed project
// root and never the process working directory.
type cliPackages struct {
	state *daemon.State
}

func newCLIPackages(state *daemon.State) *cliPackages { return &cliPackages{state: state} }

func (p *cliPackages) Packages(_ context.Context) ([]daemon.RegistryPackage, error) {
	return daemon.RegistryPackages(p.state.ProjectRoot(), p.state.Documents())
}

func (p *cliPackages) Search(ctx context.Context, params registry.SearchParams) (registry.SearchResult, error) {
	return daemon.RegistrySearch(ctx, p.state.ProjectRoot(), params)
}

func (p *cliPackages) Info(ctx context.Context, spec registry.Spec) (daemon.RegistryInfoResult, error) {
	return daemon.RegistryInfo(ctx, p.state.ProjectRoot(), spec)
}

// AuthStatus reports whether a credential resolves for the project's registry.
// The token itself is never returned or logged.
func (p *cliPackages) AuthStatus(_ context.Context) (mcp.RegistryAuthStatus, error) {
	proj, err := registryProjectFor(p.state.ProjectRoot())
	if err != nil {
		return mcp.RegistryAuthStatus{}, err
	}
	cfg, err := registry.ResolveConfig(proj.Cfg.Registry.URL, proj.Cfg.Registry.Token)
	if err != nil {
		return mcp.RegistryAuthStatus{}, err
	}
	credPath, err := registry.CredentialsPath()
	if err != nil {
		return mcp.RegistryAuthStatus{}, err
	}
	out := mcp.RegistryAuthStatus{URL: cfg.URL, Authenticated: cfg.Token != "", CredentialsPath: credPath}
	if !out.Authenticated {
		out.Hint = "not logged in to " + cfg.URL + ": the human must run `bino registry login`; do not try to obtain a token"
	}
	return out, nil
}
