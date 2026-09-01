package lsp

import (
	"path/filepath"

	"bino.bi/bino/internal/registry"
)

// pkgOrigin identifies the registry package an installed file came from.
type pkgOrigin struct {
	Name    string
	Version string
	Tag     string
}

// getLockfile returns the cached bino.lock, loading it on first use. Errors
// degrade to an empty lockfile so completion/hover never fail on lock state.
func (s *Server) getLockfile() *registry.Lockfile {
	s.mu.RLock()
	lf := s.lock
	s.mu.RUnlock()
	if lf != nil {
		return lf
	}
	lf, err := registry.LoadLockfile(s.root)
	if err != nil {
		s.log.Debugf("lockfile unavailable: %v", err)
		lf = &registry.Lockfile{}
	}
	s.mu.Lock()
	s.lock = lf
	s.mu.Unlock()
	return lf
}

// packageOrigins maps each installed package file to its registry origin.
//
// A package is a file tree, so it contributes one key per file rather than
// one per package: every document of a multi-file package has to carry the
// same "(registry)" annotation in completion and hover, not just whichever one
// the lock names as the package's primary document. Keys are normalized with
// normPath so they match index paths regardless of how the root was given.
func (s *Server) packageOrigins() map[string]pkgOrigin {
	lf := s.getLockfile()
	if len(lf.Packages) == 0 {
		return nil
	}
	out := make(map[string]pkgOrigin, len(lf.Packages))
	for _, e := range lf.Packages {
		origin := pkgOrigin{Name: e.Name, Version: e.Version, Tag: e.Tag}
		dirAbs, _, err := registry.PackageDir(s.root, e.Name)
		if !e.IsTree() || err != nil {
			// A single-document package is addressed by the path the lock
			// recorded, which also keeps locks written before the store gained
			// a per-package directory annotating correctly.
			out[normPath(filepath.Join(s.root, filepath.FromSlash(e.Path)))] = origin
			continue
		}
		for _, f := range e.Files {
			out[normPath(filepath.Join(dirAbs, filepath.FromSlash(f.Path)))] = origin
		}
	}
	return out
}

// normPath normalizes a path to an absolute, cleaned form for map keying.
func normPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
