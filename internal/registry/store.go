package registry

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
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
	if err := mkdirAllContained(projectRoot, filepath.Dir(abs)); err != nil {
		return "", err
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
	if err := mkdirAllContained(projectRoot, filepath.Dir(abs)); err != nil {
		return err
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
	// storeContain is lexical, so a symlinked scope directory would make this
	// RemoveAll delete outside the store. Resolve before deleting.
	if err := verifyContainedAfterResolve(projectRoot, abs); err != nil {
		return err
	}
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("remove %s: %w", rel, err)
	}
	// Prune the scope dir if empty; best-effort (fails when non-empty).
	_ = os.Remove(filepath.Dir(abs))
	return nil
}

// TreeFilePath returns the absolute and project-relative (slash-form) path
// for one file of a package's materialized tree. The tree path comes from the
// server and is never trusted: it is validated against the same grammar the
// server enforces before any path is built, and the result is checked to
// resolve inside the store.
func TreeFilePath(projectRoot, name, treePath string) (abs, rel string, err error) {
	if err := ValidateTreePath(treePath); err != nil {
		return "", "", err
	}
	_, dirRel, err := PackageDir(projectRoot, name)
	if err != nil {
		return "", "", err
	}
	rel = path.Join(dirRel, treePath)
	abs, err = storeContain(projectRoot, rel)
	if err != nil {
		return "", "", fmt.Errorf("package file %q resolves outside the registry store", treePath)
	}
	return abs, rel, nil
}

// WriteTreeFile atomically writes one file of a package's tree, creating its
// directory. Digest verification is the caller's responsibility — the store is
// plain I/O.
func WriteTreeFile(projectRoot, name, treePath string, body []byte) (rel string, err error) {
	abs, rel, err := TreeFilePath(projectRoot, name, treePath)
	if err != nil {
		return "", err
	}
	if err := mkdirAllContained(projectRoot, filepath.Dir(abs)); err != nil {
		return "", err
	}
	if err := writeFileAtomic(abs, body); err != nil {
		return "", fmt.Errorf("write %s: %w", rel, err)
	}
	return rel, nil
}

// ListPackageFiles returns every regular file materialized for a package, as
// slash-form paths relative to the package directory, sorted. A missing
// package directory yields no files and no error. Symlinks are reported so a
// caller sweeping the directory can remove them rather than follow them.
func ListPackageFiles(projectRoot, name string) ([]string, error) {
	dirAbs, _, err := PackageDir(projectRoot, name)
	if err != nil {
		return nil, err
	}
	var out []string
	walkErr := filepath.WalkDir(dirAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dirAbs, p)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("read %s: %w", dirAbs, walkErr)
	}
	sort.Strings(out)
	return out, nil
}

// RemoveTreeFile deletes one materialized file of a package and prunes the
// directories it leaves empty, stopping at the package directory. A missing
// file is not an error.
func RemoveTreeFile(projectRoot, name, treePath string) error {
	abs, rel, err := TreeFilePath(projectRoot, name, treePath)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", rel, err)
	}
	dirAbs, _, err := PackageDir(projectRoot, name)
	if err != nil {
		return err
	}
	pruneEmptyParents(filepath.Dir(abs), dirAbs)
	return nil
}

// pruneEmptyParents removes now-empty directories from start upwards, stopping
// below stop. os.Remove refuses a non-empty directory, and that refusal is the
// loop's stop condition rather than a failure to report.
func pruneEmptyParents(start, stop string) {
	for parent := start; parent != stop && strings.HasPrefix(parent, stop); parent = filepath.Dir(parent) {
		if os.Remove(parent) != nil {
			return
		}
	}
}

// mkdirAllContained creates dir and then re-checks, with symlinks resolved,
// that it is still inside the store. storeContain is purely lexical, and
// MkdirAll happily traverses an existing symlinked component — so a planted
// ".bino/registry/acme/kit/models -> /etc" would otherwise make the store
// write outside the project. Mirrors internal/archive/zip.go's
// verifyResolvedParent, which guards the same hazard for extracted archives.
//
// The atomic write itself needs no O_NOFOLLOW: os.CreateTemp opens with
// O_CREATE|O_EXCL, which fails on any existing name including a symlink, and
// os.Rename replaces a symlink rather than following it.
func mkdirAllContained(projectRoot, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return verifyContainedAfterResolve(projectRoot, dir)
}

// verifyContainedAfterResolve reports an error unless dir, with every symlink
// resolved, is the store directory or sits inside it.
func verifyContainedAfterResolve(projectRoot, dir string) error {
	resolved, err := filepath.EvalSymlinks(dir)
	if os.IsNotExist(err) {
		// Nothing exists at dir yet, so nothing can redirect a write out of
		// the store; the lexical check in storeContain already covered it.
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}
	root, err := filepath.EvalSymlinks(StoreDir(projectRoot))
	if err != nil {
		return fmt.Errorf("resolve registry store: %w", err)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return fmt.Errorf("%s resolves outside the registry store", dir)
	}
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
			tmp.Close() //nolint:errcheck // best-effort cleanup; the success path checks Close before the rename
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
