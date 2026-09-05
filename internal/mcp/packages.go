package mcp

import (
	"context"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/registry"
)

// Packages is the package-registry surface: the read-only queries plus the
// four writes that mirror `bino registry add/update/remove/install`. It is
// implemented by the CLI layer (which owns the project's registry
// configuration) and injected via Deps.Packages. When nil, the registry tools
// and the bino://packages resource are not registered. The interface lives
// here so the CLI can depend on internal/mcp without internal/mcp depending on
// internal/cli.
type Packages interface {
	Packages(ctx context.Context) ([]daemon.RegistryPackage, error)
	Search(ctx context.Context, params registry.SearchParams) (registry.SearchResult, error)
	Info(ctx context.Context, spec registry.Spec) (daemon.RegistryInfoResult, error)
	AuthStatus(ctx context.Context) (RegistryAuthStatus, error)
	Add(ctx context.Context, specs []string) (RegistryMutationResult, error)
	Update(ctx context.Context, packages []string) (RegistryMutationResult, error)
	Remove(ctx context.Context, packages []string) (RegistryMutationResult, error)
	Install(ctx context.Context) (RegistryMutationResult, error)
}

// RegistryChange is one package's transition through a registry write.
type RegistryChange struct {
	Name   string `json:"name"`
	Before string `json:"before,omitempty"` // locked version before the call; empty when it was not locked
	After  string `json:"after,omitempty"`  // locked version after the call; empty when it was removed
	Tag    string `json:"tag,omitempty"`    // followed tag; empty when pinned
	Direct bool   `json:"direct"`
}

// RegistryNameCollision is an installed package document whose kind and name
// a local document also uses.
type RegistryNameCollision struct {
	Package     string `json:"package"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	PackageFile string `json:"packageFile"`
	LocalFile   string `json:"localFile"`
	Hint        string `json:"hint"`
}

// RegistryMutationResult reports what a registry write changed.
type RegistryMutationResult struct {
	Changes        []RegistryChange        `json:"changes"`
	Warnings       []string                `json:"warnings,omitempty"`
	NameCollisions []RegistryNameCollision `json:"nameCollisions,omitempty"`
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

type registryAddInput struct {
	Specs []string `json:"specs" jsonschema:"package specs @scope/name[@version|@tag]; a bare name follows the latest tag"`
}

type registryUpdateInput struct {
	Packages []string `json:"packages,omitempty" jsonschema:"declared packages to re-resolve (default: all); every other direct dependency is held at its locked version"`
}

type registryRemoveInput struct {
	Packages []string `json:"packages" jsonschema:"declared package names to remove, without a version"`
}

// registryPackagesOutput is the same envelope the daemon's GET /registry/packages serves.
type registryPackagesOutput struct {
	Packages []daemon.RegistryPackage `json:"packages"`
}
