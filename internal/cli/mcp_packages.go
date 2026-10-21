package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/report/config"
)

// cliPackages implements mcp.Packages over the daemon's registry computation
// and the lifted `bino registry` write bodies. It is bound to one project, so
// the root is the server's fixed project root and never the process working
// directory.
type cliPackages struct {
	root  string
	state func(ctx context.Context) (*daemon.State, error)
}

func newCLIPackages(state *daemon.State) *cliPackages {
	return &cliPackages{root: state.ProjectRoot(), state: func(context.Context) (*daemon.State, error) { return state, nil }}
}

// newLazyCLIPackages loads the State only for the calls that read documents.
func newLazyCLIPackages(root string, state func(ctx context.Context) (*daemon.State, error)) *cliPackages {
	return &cliPackages{root: root, state: state}
}

func (p *cliPackages) Packages(ctx context.Context) ([]daemon.RegistryPackage, error) {
	st, err := p.state(ctx)
	if err != nil {
		return nil, err
	}
	return daemon.RegistryPackages(st.ProjectRoot(), st.Documents())
}

func (p *cliPackages) Search(ctx context.Context, params registry.SearchParams) (registry.SearchResult, error) {
	return daemon.RegistrySearch(ctx, p.root, params)
}

func (p *cliPackages) Info(ctx context.Context, spec registry.Spec) (daemon.RegistryInfoResult, error) {
	return daemon.RegistryInfo(ctx, p.root, spec)
}

// AuthStatus reports whether a credential resolves for the project's registry.
// The token itself is never returned or logged.
func (p *cliPackages) AuthStatus(_ context.Context) (mcp.RegistryAuthStatus, error) {
	proj, err := registryProjectFor(p.root)
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

// mutate runs one registry write against the server's fixed project root and
// reloads the State before returning, so the agent's next describe_project or
// validate_project already sees the new closure instead of waiting for the
// asynchronous file watcher.
func (p *cliPackages) mutate(ctx context.Context, op func(registryProject) (mcp.RegistryMutationResult, error)) (mcp.RegistryMutationResult, error) {
	proj, err := registryProjectFor(p.root)
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}
	res, err := op(proj)
	if err != nil {
		return mcp.RegistryMutationResult{}, err
	}
	st, err := p.state(ctx)
	if errors.Is(err, mcp.ErrNotInitialized) {
		return res, nil
	}
	if err == nil {
		err = st.Refresh(ctx)
	}
	if err != nil {
		res.Warnings = append(res.Warnings, "project reload failed after the write: "+err.Error())
	}
	return res, nil
}

func (p *cliPackages) Add(ctx context.Context, specs []string) (mcp.RegistryMutationResult, error) {
	res, err := p.mutate(ctx, func(proj registryProject) (mcp.RegistryMutationResult, error) {
		return registryAdd(ctx, proj, specs)
	})
	if err != nil {
		return res, err
	}
	if st, err := p.state(ctx); err == nil {
		res.NameCollisions = packageNameCollisions(p.root, st.Documents())
	}
	return res, nil
}

func (p *cliPackages) Update(ctx context.Context, packages []string) (mcp.RegistryMutationResult, error) {
	return p.mutate(ctx, func(proj registryProject) (mcp.RegistryMutationResult, error) {
		return registryUpdate(ctx, proj, packages)
	})
}

func (p *cliPackages) Remove(ctx context.Context, packages []string) (mcp.RegistryMutationResult, error) {
	return p.mutate(ctx, func(proj registryProject) (mcp.RegistryMutationResult, error) {
		return registryRemove(proj, packages)
	})
}

func (p *cliPackages) Install(ctx context.Context) (mcp.RegistryMutationResult, error) {
	return p.mutate(ctx, func(proj registryProject) (mcp.RegistryMutationResult, error) {
		return registryInstall(ctx, proj)
	})
}

// packageNameCollisions reports installed package documents that share kind and
// name with a local document. The build's duplicate-name validation rejects the
// pair, and only the local document can be renamed.
func packageNameCollisions(root string, docs []config.Document) []mcp.RegistryNameCollision {
	store := registry.StoreDir(root) + string(filepath.Separator)
	unique := config.UniqueNameKinds(nil) // the build validates with a nil provider too
	type key struct{ kind, name string }
	local := map[key]string{}
	for _, d := range docs {
		if _, ok := unique[d.Kind]; ok && !strings.HasPrefix(d.File, store) {
			local[key{d.Kind, d.Name}] = d.File
		}
	}
	var out []mcp.RegistryNameCollision
	for _, d := range docs {
		if !strings.HasPrefix(d.File, store) {
			continue
		}
		localFile, ok := local[key{d.Kind, d.Name}]
		if !ok {
			continue
		}
		seg := strings.SplitN(filepath.ToSlash(strings.TrimPrefix(d.File, store)), "/", 3) // scope/base/...
		pkg := ""
		if len(seg) >= 2 {
			pkg = "@" + seg[0] + "/" + seg[1]
		}
		out = append(out, mcp.RegistryNameCollision{
			Package:     pkg,
			Kind:        d.Kind,
			Name:        d.Name,
			PackageFile: d.File,
			LocalFile:   localFile,
			Hint:        fmt.Sprintf("rename the local %s %q in %s; the installed package document cannot be renamed", d.Kind, d.Name, localFile),
		})
	}
	return out
}
