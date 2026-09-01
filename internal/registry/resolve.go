package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrDependencyConflict reports two paths demanding incompatible exact
// versions of the same package.
var ErrDependencyConflict = errors.New("dependency conflict")

// ErrCycle reports a dependency cycle (also rejected server-side at publish).
var ErrCycle = errors.New("dependency cycle")

// Root is one direct dependency demand from bino.toml.
type Root struct {
	Name string // "@scope/name"
	Ref  string // version, tag, or "" (default tag)
}

// Resolved is one package in a fully resolved closure.
type Resolved struct {
	Name    string
	Version string
	Tag     string // "" when pinned
	Kind    string // primary kind; the only kind of a single-document package
	Digest  string
	Format  string // "document" | "tree"

	Kinds        []string   // every kind the version ships
	Files        []FileMeta // the version's file tree
	Dependencies []string   // bare identities of direct edges
	Direct       bool

	CompatEngine string // warn-only semver ranges declared by the package
	CompatCLI    string
}

// FileEntries reduces the resolved file list to the manifest shape recorded in
// the lock.
func (r Resolved) FileEntries() []FileEntry {
	out := make([]FileEntry, len(r.Files))
	for i, f := range r.Files {
		out[i] = FileEntry{Path: f.Path, Type: f.Type, Digest: f.Digest}
	}
	return out
}

// depResolver is the single client method the resolver needs; narrowed for
// testing with a fake. It answers in the v2 file-tree shape, which the client
// also synthesizes for a v1-only registry, so the closure walk has one path
// whatever generation the server is.
type depResolver interface {
	ResolveTree(ctx context.Context, scope, name, ref string) (ResolveV2Result, error)
}

// ResolveClosure resolves the transitive closure of roots with flat
// deduplication: exactly one version per package. A pin demand that differs
// from the already-resolved version is ErrDependencyConflict; tag and bare
// demands unify silently (first resolution wins). Returns entries sorted by
// name; on any error nothing partial is returned.
func ResolveClosure(ctx context.Context, r depResolver, roots []Root) ([]Resolved, error) {
	sorted := make([]Root, len(roots))
	copy(sorted, roots)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	resolved := map[string]*Resolved{}
	var stack []string

	var visit func(name, ref string, direct bool) error
	visit = func(name, ref string, direct bool) error {
		for _, anc := range stack {
			if anc == name {
				return fmt.Errorf("%w: %s -> %s", ErrCycle, strings.Join(stack, " -> "), name)
			}
		}
		if existing, ok := resolved[name]; ok {
			if IsPin(ref) && ref != existing.Version {
				return fmt.Errorf("%w: %s is required at both %s and %s", ErrDependencyConflict, name, existing.Version, ref)
			}
			existing.Direct = existing.Direct || direct
			return nil
		}
		scope, base, err := ParseName(name)
		if err != nil {
			return err
		}
		resp, err := r.ResolveTree(ctx, scope, base, ref)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
		deps := make([]string, 0, len(resp.Dependencies))
		depSpecs := make([]Spec, 0, len(resp.Dependencies))
		for _, d := range resp.Dependencies {
			spec, err := ParseSpec(d)
			if err != nil {
				return fmt.Errorf("resolve %s: dependency %q: %w", name, d, err)
			}
			deps = append(deps, spec.Name)
			depSpecs = append(depSpecs, spec)
		}
		sort.Slice(depSpecs, func(i, j int) bool { return depSpecs[i].Name < depSpecs[j].Name })
		resolved[name] = &Resolved{
			Name:         name,
			Version:      resp.Version,
			Tag:          resp.Tag,
			Kind:         primaryKind(resp.Kinds),
			Digest:       resp.Digest,
			Format:       resp.Format,
			Kinds:        resp.Kinds,
			Files:        resp.Files,
			Dependencies: deps,
			Direct:       direct,
			CompatEngine: resp.CompatEngine,
			CompatCLI:    resp.CompatCli,
		}
		stack = append(stack, name)
		defer func() { stack = stack[:len(stack)-1] }()
		for _, dep := range depSpecs {
			if err := visit(dep.Name, dep.Ref, false); err != nil {
				return err
			}
		}
		return nil
	}

	for _, root := range sorted {
		if err := visit(root.Name, root.Ref, true); err != nil {
			return nil, err
		}
	}

	out := make([]Resolved, 0, len(resolved))
	for _, r := range resolved {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// primaryKind is the kind a package is listed and faceted under: the first of
// its sorted kind set. A single-document package has exactly one.
func primaryKind(kinds []string) string {
	if len(kinds) == 0 {
		return ""
	}
	return kinds[0]
}
