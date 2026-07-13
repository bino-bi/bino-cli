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

// packageOrigins maps each installed package file to its registry origin,
// keyed off bino.lock's project-relative paths. Keys are normalized with
// normPath so they match index paths regardless of how the root was given.
func (s *Server) packageOrigins() map[string]pkgOrigin {
	lf := s.getLockfile()
	if len(lf.Packages) == 0 {
		return nil
	}
	out := make(map[string]pkgOrigin, len(lf.Packages))
	for _, e := range lf.Packages {
		key := normPath(filepath.Join(s.root, filepath.FromSlash(e.Path)))
		out[key] = pkgOrigin{Name: e.Name, Version: e.Version, Tag: e.Tag}
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
