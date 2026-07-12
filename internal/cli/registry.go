package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bino-bi/bino-plugin-sdk/registrydigest"
	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/version"
)

func newRegistryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage registry packages",
		Long: `Commands for consuming predef packages from a bino registry.

Dependencies are declared in bino.toml's [dependencies] table, resolved
versions are pinned in bino.lock (commit it), and package files are
materialized under .bino/registry/ (gitignore it — 'bino registry install'
re-creates it from bino.lock).

A package whose name collides with a local document of the same kind fails
the build's duplicate-name validation; rename the local document.`,
	}

	cmd.AddCommand(newRegistryAddCommand())
	cmd.AddCommand(newRegistryInstallCommand())
	cmd.AddCommand(newRegistryUpdateCommand())
	cmd.AddCommand(newRegistryRemoveCommand())
	cmd.AddCommand(newRegistryListCommand())
	cmd.AddCommand(newRegistrySearchCommand())
	cmd.AddCommand(newRegistryInfoCommand())
	cmd.AddCommand(newRegistryVerifyCommand())
	cmd.AddCommand(newRegistryLoginCommand())
	cmd.AddCommand(newRegistryLogoutCommand())
	cmd.AddCommand(newRegistryTokenCommand())

	return cmd
}

// registryProject bundles the project context every registry subcommand needs.
type registryProject struct {
	Root string
	Cfg  *pathutil.ProjectConfig
	Out  *Output
}

func registryProjectSetup() (registryProject, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return registryProject{}, RuntimeError(err)
	}
	root, err := pathutil.FindProjectRoot(workdir)
	if err != nil {
		return registryProject{}, ConfigErrorf("no bino project found (missing bino.toml)")
	}
	cfg, err := pathutil.LoadProjectConfig(root)
	if err != nil {
		return registryProject{}, ConfigErrorf("loading bino.toml: %v", err)
	}
	return registryProject{Root: root, Cfg: cfg, Out: NewOutput(OutputConfig{})}, nil
}

func (p registryProject) client() (*registry.Client, error) {
	cfg, err := registry.ResolveConfig(p.Cfg.Registry.URL, p.Cfg.Registry.Token)
	if err != nil {
		return nil, ConfigError(err)
	}
	return registry.NewClient(cfg), nil
}

// dependencyRoots turns bino.toml's [dependencies] table into resolver roots.
func dependencyRoots(cfg *pathutil.ProjectConfig) ([]registry.Root, error) {
	roots := make([]registry.Root, 0, len(cfg.Dependencies))
	for name, ref := range cfg.Dependencies {
		if _, _, err := registry.ParseName(name); err != nil {
			return nil, ConfigErrorf("bino.toml [dependencies]: %v", err)
		}
		roots = append(roots, registry.Root{Name: name, Ref: ref})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	return roots, nil
}

// registryCommandError maps registry-layer errors onto the CLI's exit-code
// classification.
func registryCommandError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, registry.ErrDependencyConflict) || errors.Is(err, registry.ErrCycle) {
		return ConfigError(err)
	}
	return ExternalError(err)
}

// downloadVerified fetches one package version and verifies it three ways:
// the transport ETag and the locally recomputed canonical digest must both
// equal the expected digest. Nothing is trusted before this passes.
func downloadVerified(ctx context.Context, client *registry.Client, name, ver, wantDigest string) ([]byte, error) {
	scope, base, err := registry.ParseName(name)
	if err != nil {
		return nil, err
	}
	body, etag, err := client.Download(ctx, scope, base, ver)
	if err != nil {
		return nil, fmt.Errorf("download %s@%s: %w", name, ver, err)
	}
	if etag != "" && etag != wantDigest {
		return nil, fmt.Errorf("download %s@%s: ETag %s does not match expected digest %s", name, ver, etag, wantDigest)
	}
	actual, err := registrydigest.Digest(body)
	if err != nil {
		return nil, fmt.Errorf("download %s@%s: canonicalize: %w", name, ver, err)
	}
	if actual != wantDigest {
		return nil, fmt.Errorf("download %s@%s: content digest %s does not match expected %s — the registry returned content that does not match its digest", name, ver, actual, wantDigest)
	}
	return body, nil
}

// registrySync materializes a resolved closure: downloads and verifies every
// package into memory first (a failure writes nothing), then writes files,
// rewrites bino.lock to exactly the closure, and sweeps files of packages
// that left it. Returns the downloaded bodies keyed by package name.
func registrySync(ctx context.Context, p registryProject, client *registry.Client, plan []registry.Resolved) (map[string][]byte, error) {
	bodies := make(map[string][]byte, len(plan))
	for _, r := range plan {
		body, err := downloadVerified(ctx, client, r.Name, r.Version, r.Digest)
		if err != nil {
			return nil, ExternalError(err)
		}
		bodies[r.Name] = body
	}

	lock, err := registry.LoadLockfile(p.Root)
	if err != nil {
		return nil, ConfigError(err)
	}
	inPlan := make(map[string]bool, len(plan))
	for _, r := range plan {
		inPlan[r.Name] = true
	}
	for _, e := range append([]registry.Entry(nil), lock.Packages...) {
		if !inPlan[e.Name] {
			if err := registry.RemovePackage(p.Root, e.Name); err != nil {
				return nil, RuntimeError(err)
			}
			lock.Remove(e.Name)
		}
	}

	for _, r := range plan {
		rel, err := registry.WritePackage(p.Root, r.Name, bodies[r.Name])
		if err != nil {
			return nil, RuntimeError(err)
		}
		lock.Upsert(registry.Entry{
			Name:         r.Name,
			Version:      r.Version,
			Tag:          r.Tag,
			Digest:       r.Digest,
			Kind:         r.Kind,
			Path:         rel,
			Direct:       r.Direct,
			Dependencies: r.Dependencies,
		})
	}
	if err := registry.SaveLockfile(p.Root, lock); err != nil {
		return nil, RuntimeError(err)
	}
	return bodies, nil
}

// registryCompatWarnings emits non-blocking registry.compat.* warnings for
// every downloaded package.
func registryCompatWarnings(p registryProject, bodies map[string][]byte, plan []registry.Resolved) {
	engineVersion := resolveEngineVersionForCompat(p.Cfg.EngineVersion)
	for _, r := range plan {
		body, ok := bodies[r.Name]
		if !ok {
			continue
		}
		for _, w := range registry.CompatWarnings(r.Name, r.Version, body, version.Version, engineVersion) {
			p.Out.Warning(w)
		}
	}
}

// registryGitignoreHint warns once when the project is a git repo whose
// .gitignore does not cover .bino/. Warn only — this CLI never edits a
// user's .gitignore.
func registryGitignoreHint(p registryProject) {
	if _, err := os.Stat(filepath.Join(p.Root, ".git")); err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(p.Root, ".gitignore"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.TrimSuffix(strings.TrimPrefix(line, "/"), "/") == ".bino" {
				return
			}
		}
	}
	p.Out.Warning("'.bino/' does not appear to be gitignored — downloaded registry files should not be committed (bino.lock should be)")
}
