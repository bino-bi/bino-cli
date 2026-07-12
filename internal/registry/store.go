package registry

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// StoreDir returns the registry materialization root: <projectRoot>/.bino/registry.
func StoreDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".bino", "registry")
}

// StorePath returns the absolute and project-relative (slash-form) file path
// for a package name. Both name segments are validated against the shared
// token grammar before any path is built, so a hostile name can never escape
// the store directory.
func StorePath(projectRoot, name string) (abs, rel string, err error) {
	scope, base, err := ParseName(name)
	if err != nil {
		return "", "", err
	}
	rel = path.Join(".bino", "registry", scope, base+".yml")
	abs = filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	if !strings.HasPrefix(abs, StoreDir(projectRoot)+string(filepath.Separator)) {
		return "", "", fmt.Errorf("package %q resolves outside the registry store", name)
	}
	return abs, rel, nil
}

// WritePackage atomically writes a package document into the store and
// returns its project-relative path. Digest verification is the caller's
// responsibility — the store is plain I/O.
func WritePackage(projectRoot, name string, body []byte) (rel string, err error) {
	abs, rel, err := StorePath(projectRoot, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(abs), err)
	}
	if err := writeFileAtomic(abs, body); err != nil {
		return "", fmt.Errorf("write %s: %w", rel, err)
	}
	return rel, nil
}

// RemovePackage deletes a package file from the store and prunes the scope
// directory when it becomes empty. A missing file is not an error.
func RemovePackage(projectRoot, name string) error {
	abs, rel, err := StorePath(projectRoot, name)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", rel, err)
	}
	// Prune the scope dir if empty; best-effort (fails when non-empty).
	_ = os.Remove(filepath.Dir(abs))
	return nil
}

// writeFileAtomic writes data via a temp file in the target directory,
// fsyncs, and renames into place so readers never observe a partial file.
func writeFileAtomic(target string, data []byte) error {
	return writeFileAtomicMode(target, data, 0o644) //nolint:gosec // G302: manifest files need standard read perms
}

func writeFileAtomicMode(target string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if tmp != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	name := tmp.Name()
	tmp = nil
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
