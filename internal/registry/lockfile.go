package registry

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// LockfileName is the lock file's name at the project root.
const LockfileName = "bino.lock"

// CurrentLockfileVersion is the lockfile format version this CLI writes.
// Version 2 records a package as a file tree: a kinds set and a per-file list
// replace the singular kind, and the entry digest is the tree's manifest
// digest. Version 1 entries are read and upgraded in memory on load.
const CurrentLockfileVersion = 2

// ErrMalformedLockfile reports a bino.lock that exists but cannot be parsed.
var ErrMalformedLockfile = errors.New("malformed " + LockfileName)

// Entry records one resolved package in the lock file.
//
// Format decides how the entry is read, and therefore which digest rule
// verifies its documents. It is derived once, from the resolve response, and
// is never re-inferred from the entry itself. An empty Format in a version-1
// lock means "document"; an empty Format in a version-2 lock means an older
// CLI rewrote the file and stripped the field, which checkLockDrift reports.
type Entry struct {
	Name    string `toml:"name"`          // "@scope/name" (dedup key)
	Version string `toml:"version"`       // resolved immutable version
	Tag     string `toml:"tag,omitempty"` // followed tag; omitted when pinned
	Digest  string `toml:"digest"`        // "sha256:<hex>": the document digest, or a tree's manifest digest
	Format  string `toml:"format"`        // "document" | "tree"
	Kind    string `toml:"kind"`          // the primary kind; also the only kind of a v1 package
	Path    string `toml:"path"`          // the primary document, project-relative, slash-form
	Direct  bool   `toml:"direct"`

	Dependencies []string `toml:"dependencies"` // direct edges, bare "@scope/name"

	// Kinds and Files describe a file-tree package. Both are empty for a
	// single-document ("document") package, which keeps using Kind, Path and
	// Resources so a lock this CLI rewrites stays installable by an older one.
	Kinds []string    `toml:"kinds,omitempty"`
	Files []FileEntry `toml:"files,omitempty"`

	// Resources are the bundled resources of a single-document package,
	// version-pinned alongside it. A tree carries its resources in Files.
	Resources []ResourceEntry `toml:"resources,omitempty"`

	// CompatEngine and CompatCLI are the semver ranges the package declares.
	// They are warn-only and are recorded so 'bino registry install', which
	// works from the lock alone, can still report a mismatch.
	CompatEngine string `toml:"compat_engine,omitempty"`
	CompatCLI    string `toml:"compat_cli,omitempty"`
}

// IsTree reports whether the entry records a file-tree package.
func (e Entry) IsTree() bool { return e.Format == FormatTree }

// TreeFiles returns the entry's file list in the uniform shape callers want,
// synthesizing it for a single-document package so materialization, pruning
// and verification have one code path. The synthesized list is exactly what a
// v1 package occupies on disk: its document plus its bundled resources.
func (e Entry) TreeFiles() []FileEntry {
	if e.IsTree() {
		return e.Files
	}
	files := make([]FileEntry, 0, 1+len(e.Resources))
	if e.Path != "" {
		files = append(files, FileEntry{Path: path.Base(e.Path), Type: FileDocument, Digest: e.Digest})
	}
	for _, r := range e.Resources {
		files = append(files, FileEntry{Path: r.Name, Type: FileResource, Digest: r.ContentHash})
	}
	return files
}

// ResourceEntry records one bundled resource of a locked package.
type ResourceEntry struct {
	Name        string `toml:"name"`
	ContentHash string `toml:"content_hash"` // "sha256:<hex>" over the raw bytes
}

// IsPinned reports whether the entry pins an exact version rather than
// following a tag.
func (e Entry) IsPinned() bool { return e.Tag == "" }

// Lockfile is the parsed bino.lock.
type Lockfile struct {
	LockfileVersion int     `toml:"lockfile_version"`
	Packages        []Entry `toml:"package,omitempty"`
}

// LoadLockfile reads <projectRoot>/bino.lock. A missing file yields an empty,
// current-version lockfile. An unknown future lockfile_version loads what is
// understood and round-trips the version on save.
func LoadLockfile(projectRoot string) (*Lockfile, error) {
	lockPath := filepath.Join(projectRoot, LockfileName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Lockfile{LockfileVersion: CurrentLockfileVersion}, nil
		}
		return nil, fmt.Errorf("read %s: %w", lockPath, err)
	}
	lf := &Lockfile{}
	if err := toml.Unmarshal(data, lf); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedLockfile, err)
	}
	if lf.LockfileVersion == 0 {
		lf.LockfileVersion = 1
	}
	if lf.LockfileVersion < 2 {
		// A version-1 lock predates the format marker, so every entry it
		// holds is a single-document package by definition. Upgrading in
		// memory keeps the file on disk untouched until something rewrites
		// it, so an install against an old lock produces no diff.
		for i := range lf.Packages {
			if lf.Packages[i].Format == "" {
				lf.Packages[i].Format = FormatDocument
			}
		}
	}
	return lf, nil
}

// SaveLockfile writes <projectRoot>/bino.lock atomically with deterministic
// ordering: packages sorted by name, each dependencies slice sorted.
func SaveLockfile(projectRoot string, lf *Lockfile) error {
	sort.Slice(lf.Packages, func(i, j int) bool { return lf.Packages[i].Name < lf.Packages[j].Name })
	for i := range lf.Packages {
		sort.Strings(lf.Packages[i].Dependencies)
		sort.Strings(lf.Packages[i].Kinds)
		sort.Slice(lf.Packages[i].Files, func(a, b int) bool {
			return lf.Packages[i].Files[a].Path < lf.Packages[i].Files[b].Path
		})
		sort.Slice(lf.Packages[i].Resources, func(a, b int) bool {
			return lf.Packages[i].Resources[a].Name < lf.Packages[i].Resources[b].Name
		})
	}
	data, err := toml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("encode %s: %w", LockfileName, err)
	}
	if err := writeFileAtomic(filepath.Join(projectRoot, LockfileName), data); err != nil {
		return fmt.Errorf("write %s: %w", LockfileName, err)
	}
	return nil
}

// Get returns the entry for name, or nil.
func (lf *Lockfile) Get(name string) *Entry {
	for i := range lf.Packages {
		if lf.Packages[i].Name == name {
			return &lf.Packages[i]
		}
	}
	return nil
}

// Upsert replaces the entry with the same name or appends a new one.
func (lf *Lockfile) Upsert(e Entry) {
	if existing := lf.Get(e.Name); existing != nil {
		*existing = e
		return
	}
	lf.Packages = append(lf.Packages, e)
}

// Remove drops the entry for name, reporting whether it existed.
func (lf *Lockfile) Remove(name string) bool {
	for i := range lf.Packages {
		if lf.Packages[i].Name == name {
			lf.Packages = append(lf.Packages[:i], lf.Packages[i+1:]...)
			return true
		}
	}
	return false
}
