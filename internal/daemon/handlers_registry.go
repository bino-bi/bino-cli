package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/report/config"
)

// RegistryParam is the served shape of one declared package parameter.
type RegistryParam struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     *string  `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// RegistryPackage is the served shape of one dependency: the union of its
// bino.toml declaration, its bino.lock resolution, and its on-disk install.
type RegistryPackage struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Kind    string `json:"kind,omitempty"`
	// Path is the package's primary document, kept for clients that open a
	// package with a single click. Files lists the whole tree.
	Path         string          `json:"path,omitempty"`
	Kinds        []string        `json:"kinds,omitempty"`
	Files        []string        `json:"files,omitempty"`
	Direct       bool            `json:"direct"`
	DeclaredRef  string          `json:"declaredRef,omitempty"`
	Installed    bool            `json:"installed"`
	Dependencies []string        `json:"dependencies,omitempty"`
	Params       []RegistryParam `json:"params,omitempty"`
}

// handleRegistryPackages reports the project's dependencies fully offline:
// bino.lock entries merged with bino.toml [dependencies] and an install check
// on the store, with params read from the already-loaded documents.
func (s *Server) handleRegistryPackages(w http.ResponseWriter, _ *http.Request) {
	root := s.state.ProjectRoot()
	lf, err := registry.LoadLockfile(root)
	if err != nil {
		s.writeJSON(w, map[string]any{"packages": []RegistryPackage{}, "error": err.Error()})
		return
	}
	declared := map[string]string{}
	if cfg, cfgErr := pathutil.LoadProjectConfig(root); cfgErr == nil && cfg != nil && cfg.Dependencies != nil {
		declared = cfg.Dependencies
	}
	paramsByPath := packageParamsByPath(root, s.state.Documents())

	packages := make([]RegistryPackage, 0, len(lf.Packages)+len(declared))
	locked := make(map[string]bool, len(lf.Packages))
	for _, e := range lf.Packages {
		locked[e.Name] = true
		files, installed := packageFileState(root, e)
		packages = append(packages, RegistryPackage{
			Name:         e.Name,
			Version:      e.Version,
			Tag:          e.Tag,
			Kind:         e.Kind,
			Path:         e.Path,
			Kinds:        e.Kinds,
			Files:        files,
			Direct:       e.Direct,
			DeclaredRef:  declared[e.Name],
			Installed:    installed,
			Dependencies: e.Dependencies,
			Params:       packageParams(root, files, paramsByPath),
		})
	}
	// Declared-but-unlocked dependencies (bino.toml edited by hand, `bino
	// registry add` not run yet) still show up so the client can nudge a sync.
	for name, ref := range declared {
		if !locked[name] {
			packages = append(packages, RegistryPackage{Name: name, DeclaredRef: ref, Direct: true})
		}
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	s.writeJSON(w, map[string]any{"packages": packages})
}

// handleRegistrySearch proxies the registry's full-text search using the
// project's resolved registry config (URL chain + token/credentials).
func (s *Server) handleRegistrySearch(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.registryClientConfig()
	if err != nil {
		s.writeRegistryError(w, err)
		return
	}
	q := r.URL.Query()
	params := registry.SearchParams{
		Query:   q.Get("q"),
		Kinds:   q["kind"],
		Scopes:  q["scope"],
		Tags:    q["tag"],
		Page:    atoiOrZero(q.Get("page")),
		PerPage: atoiOrZero(q.Get("perPage")),
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := registry.NewClient(cfg).Search(ctx, params)
	if err != nil {
		s.writeRegistryError(w, err)
		return
	}
	s.writeJSON(w, res)
}

// handleRegistryInfo resolves a package spec ("@scope/name[@ref]") against the
// registry and annotates the locally locked version, if any.
func (s *Server) handleRegistryInfo(w http.ResponseWriter, r *http.Request) {
	spec, err := registry.ParseSpec(r.URL.Query().Get("spec"))
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err, "")
		return
	}
	cfg, err := s.registryClientConfig()
	if err != nil {
		s.writeRegistryError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := registry.NewClient(cfg).ResolveTree(ctx, spec.Scope, spec.Base, spec.Ref)
	if err != nil {
		s.writeRegistryError(w, err)
		return
	}
	installed := ""
	if lf, lockErr := registry.LoadLockfile(s.state.ProjectRoot()); lockErr == nil {
		if e := lf.Get(spec.Name); e != nil {
			installed = e.Version
		}
	}
	s.writeJSON(w, struct {
		registry.ResolveV2Result
		InstalledVersion string `json:"installedVersion,omitempty"`
	}{res, installed})
}

// registryClientConfig resolves the registry connection for this project:
// bino.toml [registry] → env → global config → default, token → credentials.
func (s *Server) registryClientConfig() (registry.Config, error) {
	var rawURL, rawToken string
	if cfg, err := pathutil.LoadProjectConfig(s.state.ProjectRoot()); err == nil && cfg != nil {
		rawURL, rawToken = cfg.Registry.URL, cfg.Registry.Token
	}
	return registry.ResolveConfig(rawURL, rawToken)
}

// writeRegistryError maps a registry failure onto a JSON error response,
// preserving the upstream status for structured API errors.
func (s *Server) writeRegistryError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	code := ""
	var apiErr *registry.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Status >= 400 {
			status = apiErr.Status
		}
		code = apiErr.Code
	}
	s.writeJSONError(w, status, err, code)
}

// packageFileState lists a locked package's project-relative files and reports
// whether every one of them is on disk. A package is a file tree, so stat-ing
// the package directory would not do: os.Stat succeeds on an empty directory,
// which would report a half-installed package as installed.
func packageFileState(root string, e registry.Entry) (files []string, installed bool) {
	entryFiles := e.TreeFiles()
	if len(entryFiles) == 0 {
		return nil, false
	}
	_, dirRel, err := registry.PackageDir(root, e.Name)
	if err != nil {
		return nil, false
	}
	files = make([]string, 0, len(entryFiles))
	installed = true
	for _, f := range entryFiles {
		rel := path.Join(dirRel, f.Path)
		if !e.IsTree() && f.Type == registry.FileDocument && e.Path != "" {
			// A single-document package's document is addressed by the path
			// the lock recorded, which also keeps locks written before the
			// store gained a per-package directory reporting correctly. Its
			// bundled resources sit beside it, in the package directory.
			rel = e.Path
		}
		files = append(files, rel)
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); statErr != nil {
			installed = false
		}
	}
	return files, installed
}

// packageParams collects the declared params of every document a package
// ships. A package used to be one document, so its params could be looked up
// by that one path; a tree contributes them from all of its files.
func packageParams(root string, files []string, byPath map[string][]RegistryParam) []RegistryParam {
	var out []RegistryParam
	seen := map[string]bool{}
	for _, rel := range files {
		for _, p := range byPath[filepath.Join(root, filepath.FromSlash(rel))] {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			out = append(out, p)
		}
	}
	return out
}

// packageParamsByPath maps each loaded document's absolute file path to its
// declared params (documents without params are skipped).
func packageParamsByPath(root string, docs []config.Document) map[string][]RegistryParam {
	out := make(map[string][]RegistryParam)
	for _, d := range docs {
		if len(d.Params) == 0 {
			continue
		}
		abs := d.File
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		// A manifest file may hold several "---" documents, each with its own
		// params, so this accumulates rather than overwrites.
		out[abs] = append(out[abs], toRegistryParams(d.Params)...)
	}
	return out
}

func toRegistryParams(specs []config.LayoutPageParamSpec) []RegistryParam {
	out := make([]RegistryParam, 0, len(specs))
	for _, p := range specs {
		rp := RegistryParam{
			Name:        p.Name,
			Type:        p.Type,
			Required:    p.Required,
			Default:     p.Default,
			Description: p.Description,
		}
		if p.Options != nil {
			for _, it := range p.Options.Items {
				rp.Options = append(rp.Options, it.Value)
			}
		}
		out = append(out, rp)
	}
	return out
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// RegistryReason reports whether any watcher refresh reason touches registry
// state (bino.lock, bino.toml, or the .bino/registry store) — the trigger for
// the registry-changed SSE event.
func RegistryReason(reasons []string) bool {
	for _, r := range reasons {
		if strings.Contains(r, registry.LockfileName) ||
			strings.Contains(r, "bino.toml") ||
			strings.Contains(r, ".bino/registry") {
			return true
		}
	}
	return false
}
