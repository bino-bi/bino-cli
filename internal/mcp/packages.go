package mcp

import (
	"context"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/registry"
)

// Packages is the read-only package-registry surface. It is implemented by
// the CLI layer (which owns the project's registry configuration) and injected
// via Deps.Packages. When nil, the registry tools and the bino://packages
// resource are not registered. The interface lives here so the CLI can depend
// on internal/mcp without internal/mcp depending on internal/cli.
type Packages interface {
	Packages(ctx context.Context) ([]daemon.RegistryPackage, error)
	Search(ctx context.Context, params registry.SearchParams) (registry.SearchResult, error)
	Info(ctx context.Context, spec registry.Spec) (daemon.RegistryInfoResult, error)
	AuthStatus(ctx context.Context) (RegistryAuthStatus, error)
}

// RegistryAuthStatus reports whether the registry can be used authenticated.
// It never carries the token.
type RegistryAuthStatus struct {
	URL             string `json:"url"`
	Authenticated   bool   `json:"authenticated"`
	CredentialsPath string `json:"credentialsPath"`
	Hint            string `json:"hint,omitempty"`
}

type registrySearchInput struct {
	Query   string   `json:"query" jsonschema:"full-text search over package names and descriptions"`
	Kinds   []string `json:"kinds,omitempty" jsonschema:"restrict to manifest kinds, e.g. Table"`
	Scopes  []string `json:"scopes,omitempty" jsonschema:"restrict to scopes without the leading @, e.g. acme"`
	Tags    []string `json:"tags,omitempty" jsonschema:"restrict to packages carrying these tags"`
	Page    int      `json:"page,omitempty" jsonschema:"1-based result page (default 1)"`
	PerPage int      `json:"per_page,omitempty" jsonschema:"results per page (registry default when 0)"`
}

type registryInfoInput struct {
	Spec string `json:"spec" jsonschema:"package spec @scope/name[@version|@tag], e.g. @acme/kpi-card or @acme/kpi-card@1.2.0"`
}

// registryPackagesOutput is the same envelope the daemon's GET /registry/packages serves.
type registryPackagesOutput struct {
	Packages []daemon.RegistryPackage `json:"packages"`
}
