package registry

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// resourceNameRe mirrors the registry server's own resource-name validator:
// ASCII letters/digits, then letters/digits/"._-", max 255 chars, no leading
// dot. The explicit ".." substring reject below backstops names the regex
// alone would still allow (e.g. "a..b").
var resourceNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)

// StoreDir returns the registry materialization root: <projectRoot>/.bino/registry.
func StoreDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".bino", "registry")
}

// storeContain resolves rel (a project-relative, slash-form path built from
// already-validated segments) to an absolute path and verifies it stays
// inside the store directory.
func storeContain(projectRoot, rel string) (abs string, err error) {
	abs = filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	if !strings.HasPrefix(abs, StoreDir(projectRoot)+string(filepath.Separator)) {
		return "", fmt.Errorf("%q resolves outside the registry store", rel)
	}
	return abs, nil
}

// PackageDir returns the absolute and project-relative (slash-form)
// directory a package's document and bundled resources are materialized
// into: .bino/registry/<scope>/<name>/. Both name segments are validated
// against the shared token grammar before any path is built, so a hostile
// name can never escape the store directory.
func PackageDir(projectRoot, name string) (abs, rel string, err error) {
	scope, base, err := ParseName(name)
	if err != nil {
		return "", "", err
	}
	rel = path.Join(".bino", "registry", scope, base)
	abs, err = storeContain(projectRoot, rel)
	if err != nil {
		return "", "", fmt.Errorf("package %q resolves outside the registry store", name)
	}
	return abs, rel, nil
}

// StorePath returns the absolute and project-relative (slash-form) file path
// for a package's document: .bino/registry/<scope>/<name>/<name>.yml.
func StorePath(projectRoot, name string) (abs, rel string, err error) {
	_, base, err := ParseName(name)
	if err != nil {
		return "", "", err
	}
	dirAbs, dirRel, err := PackageDir(projectRoot, name)
	if err != nil {
		return "", "", err
	}
	rel = path.Join(dirRel, base+".yml")
	abs = filepath.Join(dirAbs, base+".yml")
	return abs, rel, nil
}

// ResourcePath returns the absolute and project-relative (slash-form) file
// path for one of a package's bundled resources: <packageDir>/<resourceName>.
// resourceName comes from the server's response and is never trusted
// blindly: it is validated against the same grammar the server enforces
// before any path is built, and the result is checked to resolve inside the
// store.
func ResourcePath(projectRoot, name, resourceName string) (abs, rel string, err error) {
	if !resourceNameRe.MatchString(resourceName) || strings.Contains(resourceName, "..") {
		return "", "", fmt.Errorf("invalid resource name %q", resourceName)
	}
	_, dirRel, err := PackageDir(projectRoot, name)
	if err != nil {
		return "", "", err
	}
	rel = path.Join(dirRel, resourceName)
	abs, err = storeContain(projectRoot, rel)
	if err != nil {
		return "", "", fmt.Errorf("resource %q resolves outside the registry store", resourceName)
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

// WriteResource atomically writes one of a package's bundled resources into
// the store. Digest verification is the caller's responsibility — the store
// is plain I/O.
func WriteResource(projectRoot, name, resourceName string, body []byte) error {
	abs, rel, err := ResourcePath(projectRoot, name, resourceName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(abs), err)
	}
	if err := writeFileAtomic(abs, body); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	return nil
}

// RemovePackage deletes a package's entire directory — its document and any
// bundled resources — from the store, and prunes the scope directory when it
// becomes empty. A missing directory is not an error.
func RemovePackage(projectRoot, name string) error {
	abs, rel, err := PackageDir(projectRoot, name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(abs); err != nil {
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
