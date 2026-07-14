package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// LockfileName is the lock file's name at the project root.
const LockfileName = "bino.lock"

// CurrentLockfileVersion is the lockfile format version this CLI writes.
const CurrentLockfileVersion = 1

// ErrMalformedLockfile reports a bino.lock that exists but cannot be parsed.
var ErrMalformedLockfile = errors.New("malformed " + LockfileName)

// Entry records one resolved package in the lock file.
type Entry struct {
	Name         string          `toml:"name"`          // "@scope/name" (dedup key)
	Version      string          `toml:"version"`       // resolved immutable version
	Tag          string          `toml:"tag,omitempty"` // followed tag; omitted when pinned
	Digest       string          `toml:"digest"`        // "sha256:<hex>" over the canonical document
	Kind         string          `toml:"kind"`
	Path         string          `toml:"path"` // project-relative, slash-form
	Direct       bool            `toml:"direct"`
	Dependencies []string        `toml:"dependencies"`        // direct edges, bare "@scope/name"
	Resources    []ResourceEntry `toml:"resources,omitempty"` // bundled resources, version-pinned alongside the document
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
	path := filepath.Join(projectRoot, LockfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Lockfile{LockfileVersion: CurrentLockfileVersion}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	lf := &Lockfile{}
	if err := toml.Unmarshal(data, lf); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedLockfile, err)
	}
	if lf.LockfileVersion == 0 {
		lf.LockfileVersion = CurrentLockfileVersion
	}
	return lf, nil
}

// SaveLockfile writes <projectRoot>/bino.lock atomically with deterministic
// ordering: packages sorted by name, each dependencies slice sorted.
func SaveLockfile(projectRoot string, lf *Lockfile) error {
	sort.Slice(lf.Packages, func(i, j int) bool { return lf.Packages[i].Name < lf.Packages[j].Name })
	for i := range lf.Packages {
		sort.Strings(lf.Packages[i].Dependencies)
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
