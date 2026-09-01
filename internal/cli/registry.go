package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/version"
)

// errResourceMismatch flags a resource download whose bytes do not match the
// hash the caller expected, so callers can map it onto a re-resolve hint
// without string-matching the error text.
var errResourceMismatch = errors.New("resource content hash mismatch")

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
		if errors.Is(err, pathutil.ErrProjectRootNotFound) {
			return registryProject{}, ConfigError(err)
		}
		return registryProject{}, ConfigErrorf("resolve project root: %w", err)
	}
	cfg, err := pathutil.LoadProjectConfig(root)
	if err != nil {
		return registryProject{}, ConfigErrorf("loading bino.toml: %w", err)
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
			return nil, ConfigErrorf("bino.toml [dependencies]: %w", err)
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
	if err := registry.VerifyFile(registry.FormatDocument, registry.FileDocument, body, wantDigest); err != nil {
		return nil, fmt.Errorf("download %s@%s: %w — the registry returned content that does not match its digest", name, ver, err)
	}
	return body, nil
}

// downloadVerifiedResource fetches one bundled resource of a single-document
// package and verifies its bytes' own sha256 against the expected content
// hash. Unlike documents, resources are opaque binaries with no canonical form
// to recompute — a plain sha256 over the raw bytes is the whole check.
func downloadVerifiedResource(ctx context.Context, client *registry.Client, name, ver, resourceName, wantHash string) ([]byte, error) {
	scope, base, err := registry.ParseName(name)
	if err != nil {
		return nil, err
	}
	body, etag, err := client.DownloadResource(ctx, scope, base, ver, resourceName)
	if err != nil {
		return nil, fmt.Errorf("download resource %s of %s@%s: %w", resourceName, name, ver, err)
	}
	if etag != "" && etag != wantHash {
		return nil, fmt.Errorf("%w: download resource %s of %s@%s: ETag %s does not match expected content hash %s", errResourceMismatch, resourceName, name, ver, etag, wantHash)
	}
	if err := registry.VerifyFile(registry.FormatDocument, registry.FileResource, body, wantHash); err != nil {
		return nil, fmt.Errorf("%w: download resource %s of %s@%s: %w — the registry returned content that does not match its advertised hash", errResourceMismatch, resourceName, name, ver, err)
	}
	return body, nil
}

// downloadVerifiedTreeFile fetches one file of a file-tree package from the v2
// route and verifies both the ETag and the recomputed digest. Which digest
// rule applies is decided by the package's format, never guessed: trying both
// would give a registry two chances to satisfy one check.
func downloadVerifiedTreeFile(ctx context.Context, client *registry.Client, name, ver string, f registry.FileEntry) ([]byte, error) {
	scope, base, err := registry.ParseName(name)
	if err != nil {
		return nil, err
	}
	body, etag, err := client.DownloadFile(ctx, scope, base, ver, f.Path)
	if err != nil {
		return nil, fmt.Errorf("download %s of %s@%s: %w", f.Path, name, ver, err)
	}
	if etag != "" && etag != f.Digest {
		return nil, fmt.Errorf("download %s of %s@%s: ETag %s does not match expected digest %s", f.Path, name, ver, etag, f.Digest)
	}
	if err := registry.VerifyFile(registry.FormatTree, f.Type, body, f.Digest); err != nil {
		return nil, fmt.Errorf("download %s of %s@%s: %w — the registry returned content that does not match its digest", f.Path, name, ver, err)
	}
	return body, nil
}

// packagePlan is one package's fully verified contents, ready to be written.
// Bodies are keyed by the file's path inside the package directory; a file
// already on disk with a matching digest is absent from the map and left
// alone.
type packagePlan struct {
	entry  registry.Entry
	bodies map[string][]byte
}

// maxTreeMemoryBytes caps how much of one package the CLI holds in memory
// while verifying it before anything is written. The server's own per-version
// ceiling is far higher (500 MB), but the verify-everything-first ordering
// means a whole package is resident at once, so the local budget matches the
// archive extractor's (internal/archive.DefaultLimits) instead.
const maxTreeMemoryBytes = 128 << 20

// fetchPackage downloads and verifies everything one resolved package needs,
// returning the plan to write. Nothing is written here — a failure anywhere in
// the closure must leave the store untouched.
func fetchPackage(ctx context.Context, p registryProject, client *registry.Client, r registry.Resolved) (packagePlan, error) {
	entry := registry.Entry{
		Name:         r.Name,
		Version:      r.Version,
		Tag:          r.Tag,
		Digest:       r.Digest,
		Format:       r.Format,
		Kind:         r.Kind,
		Direct:       r.Direct,
		Dependencies: r.Dependencies,
		CompatEngine: r.CompatEngine,
		CompatCLI:    r.CompatCLI,
	}
	if r.Format == registry.FormatTree {
		entry.Kinds = r.Kinds
		entry.Files = r.FileEntries()
	}
	plan := packagePlan{entry: entry, bodies: map[string][]byte{}}

	if r.Format == registry.FormatTree {
		if err := checkTreeBudget(r); err != nil {
			return packagePlan{}, fmt.Errorf("%s@%s: %w", r.Name, r.Version, err)
		}
		for _, f := range entry.Files {
			if reuseOnDisk(p.Root, registry.FormatTree, r.Name, f) {
				continue
			}
			body, err := downloadVerifiedTreeFile(ctx, client, r.Name, r.Version, f)
			if err != nil {
				return packagePlan{}, err
			}
			plan.bodies[f.Path] = body
		}
		plan.entry.Path = treeRootDocument(p.Root, r.Name, entry.Files)
		return plan, nil
	}

	// A single-document package keeps the v1 routes: its bundled resources
	// are not part of the one-file tree a v2 registry renders for it, so the
	// v2 file route has nothing to serve for them.
	body, err := downloadVerified(ctx, client, r.Name, r.Version, r.Digest)
	if err != nil {
		return packagePlan{}, err
	}
	_, base, err := registry.ParseName(r.Name)
	if err != nil {
		return packagePlan{}, err
	}
	docName := base + ".yml"
	plan.bodies[docName] = body

	resources, err := fetchResources(ctx, p, client, r.Name, r.Version, plan.bodies)
	if err != nil {
		return packagePlan{}, err
	}
	plan.entry.Resources = resources
	_, rel, err := registry.StorePath(p.Root, r.Name)
	if err != nil {
		return packagePlan{}, err
	}
	plan.entry.Path = rel
	return plan, nil
}

// checkTreeBudget refuses a package whose advertised size would not fit the
// in-memory verification budget, before a single byte is requested.
func checkTreeBudget(r registry.Resolved) error {
	sizes := make(map[string]int64, len(r.Files))
	var total int64
	for _, f := range r.Files {
		sizes[f.Path] = f.Size
		total += f.Size
	}
	if err := registry.CheckTreeQuotas(sizes); err != nil {
		return err
	}
	if total > maxTreeMemoryBytes {
		return fmt.Errorf("package is %d bytes, more than this CLI verifies at once (%d)", total, maxTreeMemoryBytes)
	}
	return nil
}

// reuseOnDisk reports whether the materialized file already matches the digest
// the registry advertises, so it need not be downloaded again. This mirrors
// the server's own content-addressed dedup and is what makes a re-install of
// an unchanged closure nearly free.
//
// format selects the digest rule, exactly as it does for a downloaded file: a
// single-document package's document is digested the way it was published,
// which is not the rule a tree's documents use.
func reuseOnDisk(projectRoot, format, name string, f registry.FileEntry) bool {
	abs, _, err := registry.TreeFilePath(projectRoot, name, f.Path)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return false
	}
	return registry.VerifyFile(format, f.Type, data, f.Digest) == nil
}

// fetchResources verifies the bundled resources of a single-document package,
// reusing any on-disk file whose sha256 already matches. Downloaded bodies are
// added to bodies; the returned entries are sorted by name.
func fetchResources(ctx context.Context, p registryProject, client *registry.Client, name, ver string, bodies map[string][]byte) ([]registry.ResourceEntry, error) {
	scope, base, err := registry.ParseName(name)
	if err != nil {
		return nil, err
	}
	metas, err := client.ListResources(ctx, scope, base, ver)
	if err != nil {
		return nil, fmt.Errorf("list resources for %s@%s: %w", name, ver, err)
	}
	entries := make([]registry.ResourceEntry, 0, len(metas))
	for _, m := range metas {
		entries = append(entries, registry.ResourceEntry{Name: m.Name, ContentHash: m.ContentHash})
		if reuseOnDisk(p.Root, registry.FormatDocument, name, registry.FileEntry{Path: m.Name, Type: registry.FileResource, Digest: m.ContentHash}) {
			continue
		}
		body, err := downloadVerifiedResource(ctx, client, name, ver, m.Name, m.ContentHash)
		if err != nil {
			return nil, err
		}
		bodies[m.Name] = body
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// treeRootDocument picks the document that stands for a tree package: the one
// named after the package itself, else the first document by path order. It is
// what 'bino registry list' shows and what the editor opens.
func treeRootDocument(projectRoot, name string, files []registry.FileEntry) string {
	_, base, err := registry.ParseName(name)
	if err != nil {
		return ""
	}
	_, dirRel, err := registry.PackageDir(projectRoot, name)
	if err != nil {
		return ""
	}
	sorted := append([]registry.FileEntry(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	fallback := ""
	for _, f := range sorted {
		if f.Type != registry.FileDocument {
			continue
		}
		if stem := strings.TrimSuffix(strings.TrimSuffix(f.Path, ".yaml"), ".yml"); stem == base {
			return path.Join(dirRel, f.Path)
		}
		if fallback == "" {
			fallback = path.Join(dirRel, f.Path)
		}
	}
	return fallback
}

// materialize writes a verified closure to disk and rewrites bino.lock to
// exactly that closure.
//
// Order matters. Every package was verified into memory first, so this
// function cannot fail on content. It then removes the files a package no
// longer has before writing the ones it does — a package that changes format
// must not leave its old document beside the new tree, which would be a
// duplicate-name build failure the user cannot fix from their own files —
// and only deletes paths the previous lock recorded, so a file the CLI never
// installed is never silently removed. bino.lock is written last, so an
// interrupted run re-materializes cleanly instead of claiming to be done.
func materialize(p registryProject, plans []packagePlan, writeLock bool) error {
	lock, err := registry.LoadLockfile(p.Root)
	if err != nil {
		return ConfigError(err)
	}
	inPlan := make(map[string]bool, len(plans))
	for _, pl := range plans {
		inPlan[pl.entry.Name] = true
	}
	for _, e := range append([]registry.Entry(nil), lock.Packages...) {
		if !inPlan[e.Name] {
			if err := registry.RemovePackage(p.Root, e.Name); err != nil {
				return RuntimeError(err)
			}
			lock.Remove(e.Name)
		}
	}

	for _, pl := range plans {
		if err := pruneDepartedFiles(p.Root, lock.Get(pl.entry.Name), pl.entry); err != nil {
			return RuntimeError(err)
		}
		for _, f := range pl.entry.TreeFiles() {
			body, ok := pl.bodies[f.Path]
			if !ok {
				continue // already on disk with a matching digest
			}
			if _, err := registry.WriteTreeFile(p.Root, pl.entry.Name, f.Path, body); err != nil {
				return RuntimeError(err)
			}
		}
		lock.Upsert(pl.entry)
	}
	if !writeLock {
		return nil
	}
	lock.LockfileVersion = registry.CurrentLockfileVersion
	if err := registry.SaveLockfile(p.Root, lock); err != nil {
		return RuntimeError(err)
	}
	return nil
}

// pruneDepartedFiles removes the files the previous lock recorded for a
// package that its new contents no longer have. It is deliberately driven by
// the old lock rather than by a directory listing: a file this CLI did not
// install is not its to delete.
func pruneDepartedFiles(projectRoot string, old *registry.Entry, next registry.Entry) error {
	if old == nil {
		return nil
	}
	keep := make(map[string]bool)
	for _, f := range next.TreeFiles() {
		keep[f.Path] = true
	}
	for _, f := range old.TreeFiles() {
		if keep[f.Path] {
			continue
		}
		if err := registry.RemoveTreeFile(projectRoot, next.Name, f.Path); err != nil {
			return err
		}
	}
	return nil
}

// registrySync materializes a resolved closure: it verifies every package into
// memory first (a failure writes nothing), then writes files, rewrites
// bino.lock to exactly the closure, and sweeps packages that left it.
func registrySync(ctx context.Context, p registryProject, client *registry.Client, plan []registry.Resolved) ([]packagePlan, error) {
	plans := make([]packagePlan, 0, len(plan))
	for _, r := range plan {
		pp, err := fetchPackage(ctx, p, client, r)
		if err != nil {
			return nil, ExternalError(err)
		}
		plans = append(plans, pp)
	}
	if err := materialize(p, plans, true); err != nil {
		return nil, err
	}
	return plans, nil
}

// registryCompatWarnings emits the non-blocking compat warnings a package
// declares. The ranges come from the resolve response and are recorded in
// bino.lock, so an install that never resolves still reports them.
func registryCompatWarnings(p registryProject, entries []registry.Entry) {
	engineVersion := resolveEngineVersionForCompat(p.Cfg.EngineVersion)
	for _, e := range entries {
		for _, w := range registry.CompatWarnings(e.Name, e.Version, e.CompatEngine, e.CompatCLI, version.Version, engineVersion) {
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

// planEntries is the lock entries of a materialized closure, for the callers
// that only need the metadata.
func planEntries(plans []packagePlan) []registry.Entry {
	out := make([]registry.Entry, len(plans))
	for i, pl := range plans {
		out[i] = pl.entry
	}
	return out
}
