package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/spec"
)

// buildOverlay is the project as one build of `bino build --overlay` sees it:
// a temporary tree where every project file is a link to the original, and
// every file of the overlay directory replaces or adds the file at the same
// relative path. Nothing is written to the project.
//
// A temporary tree, and not a file lookup inside the loader, so that every
// path a manifest names (a CSV, a SQL file, an image, a markdown source)
// resolves the same way it does in the project, and an overlay file of any
// kind wins over the project file.
type buildOverlay struct {
	// ProjectRoot is the real project.
	ProjectRoot string
	// Dir is the overlay directory.
	Dir string
	// Root is the temporary tree the build reads instead of ProjectRoot.
	Root string
	// files are the overlay files, as slash-separated paths relative to Dir.
	files map[string]struct{}
	// realRoot is Root with links resolved, to check writes stay inside it.
	realRoot string
	// inProject is Dir as a path below ProjectRoot when the overlay lives in
	// the project, so the mirror leaves it out. Empty otherwise.
	inProject string
}

// linkedDirs are linked as a whole instead of walked, unless the overlay has
// a file inside: the manifest loader skips them (it still reads
// .bino/registry through the link), and they can hold many files.
var linkedDirs = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {}, ".bino": {}, "node_modules": {}, "vendor": {}, "venv": {}, ".venv": {},
	"dist": {}, "build": {}, "target": {},
}

// newBuildOverlay builds the temporary tree. The caller must Close it.
func newBuildOverlay(projectRoot, dir string) (*buildOverlay, error) {
	absDir, err := filepath.Abs(dir)
	if err == nil {
		absDir, err = filepath.EvalSymlinks(absDir)
	}
	if err != nil {
		return nil, fmt.Errorf("--overlay %s: %w", dir, err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("--overlay %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("--overlay %s: not a directory", dir)
	}
	resolvedRoot := projectRoot
	if r, err := filepath.EvalSymlinks(projectRoot); err == nil {
		resolvedRoot = r
	}
	if sameOrInside(resolvedRoot, absDir) {
		return nil, fmt.Errorf("--overlay %s: must not contain the project", dir)
	}

	files, err := overlayFiles(absDir)
	if err != nil {
		return nil, err
	}
	for rel := range files {
		if rel == "bino.toml" {
			return nil, fmt.Errorf("--overlay %s: bino.toml cannot be replaced, it is always read from the project", dir)
		}
	}

	root, err := os.MkdirTemp("", "bino-overlay-*")
	if err != nil {
		return nil, fmt.Errorf("--overlay: create temporary directory: %w", err)
	}
	o := &buildOverlay{ProjectRoot: projectRoot, Dir: absDir, Root: root, files: files, realRoot: root}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		o.realRoot = r
	}
	if rel, err := filepath.Rel(resolvedRoot, absDir); err == nil && sameOrInside(absDir, resolvedRoot) {
		o.inProject = filepath.Join(projectRoot, rel)
	}
	if err := o.mirror(); err != nil {
		o.Close() //nolint:errcheck,gosec // the mirror error is the one to report
		return nil, err
	}
	return o, nil
}

// Close removes the temporary tree. The project and the overlay stay as they are.
func (o *buildOverlay) Close() error {
	if o == nil {
		return nil
	}
	return os.RemoveAll(o.Root)
}

// Limits for an overlay: it can come from an untrusted agent.
const (
	maxOverlayFiles = 2000
	maxOverlayBytes = 8 << 20
)

// overlayFiles lists the overlay's files. Only regular files are accepted: a
// link or a device could make the build read a file outside the overlay.
func overlayFiles(dir string) (map[string]struct{}, error) {
	files := map[string]struct{}{}
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file (links, devices and pipes are not allowed)", filepath.ToSlash(rel))
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if len(files) >= maxOverlayFiles || total > maxOverlayBytes {
			return fmt.Errorf("more than %d files or %d MiB", maxOverlayFiles, maxOverlayBytes>>20)
		}
		files[filepath.ToSlash(rel)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("--overlay %s: %w", dir, err)
	}
	return files, nil
}

// mirror links the project into Root, then copies the overlay files in.
func (o *buildOverlay) mirror() error {
	if err := o.Sync(); err != nil {
		return err
	}
	for rel := range o.files {
		if err := o.copyOverlayFile(rel); err != nil {
			return fmt.Errorf("--overlay: %s: %w", rel, err)
		}
	}
	return nil
}

// Sync links every project file that is not in Root yet. It runs again
// after hooks, so files a hook writes into the project are seen.
// Directories are created for real, because the manifest loader does not
// follow a linked directory.
func (o *buildOverlay) Sync() error {
	if o == nil {
		return nil
	}
	err := filepath.WalkDir(o.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(o.ProjectRoot, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(o.Root, rel)
		if o.inProject != "" && sameOrInside(path, o.inProject) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		existing, statErr := os.Lstat(target)
		if d.IsDir() {
			if statErr == nil {
				if existing.Mode()&fs.ModeSymlink != 0 {
					return filepath.SkipDir
				}
				return nil
			}
			if _, link := linkedDirs[d.Name()]; link && !o.hasFilesIn(rel) {
				if err := linkOrCopy(path, target, true); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return os.Mkdir(target, 0o755)
		}
		if statErr == nil {
			return nil
		}
		if _, replaced := o.files[filepath.ToSlash(rel)]; replaced {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 && o.hasFilesIn(rel) {
			// an overlay file below a link would be written through it into the link's target
			return fmt.Errorf("%s is a link in the project, the overlay cannot have files below it", filepath.ToSlash(rel))
		}
		return linkOrCopy(path, target, false)
	})
	if err != nil {
		return fmt.Errorf("--overlay: mirror project: %w", err)
	}
	return nil
}

// copyOverlayFile copies one overlay file into Root. It never writes
// through a link: the parent must resolve inside Root and the file must not
// exist yet, which also catches a name that only differs in case from a
// project file on a case-insensitive file system.
func (o *buildOverlay) copyOverlayFile(rel string) error {
	src := filepath.Join(o.Dir, filepath.FromSlash(rel))
	target := filepath.Join(o.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return err
	}
	if !sameOrInside(parent, o.realRoot) {
		return fmt.Errorf("its directory is a link in the project, the overlay cannot have files below it")
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("the project has a file with this name (does the name differ only in case?)")
	}
	return copyFile(src, target)
}

// hasFilesIn reports whether the overlay has a file below the project-relative dir.
func (o *buildOverlay) hasFilesIn(dir string) bool {
	prefix := filepath.ToSlash(dir) + "/"
	for rel := range o.files {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// linkOrCopy links target to path, and copies a file where the system does
// not allow links (Windows without developer mode).
func linkOrCopy(path, target string, dir bool) error {
	err := os.Symlink(path, target)
	if err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	if dir {
		return os.CopyFS(target, os.DirFS(path))
	}
	return copyFile(path, target)
}

// copyFile copies a regular file to a new file. It refuses a source that is
// not a regular file, or that changed into one between the check and the
// open, and a destination that exists.
func copyFile(src, dst string) error {
	before, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	in, err := os.Open(src) //nolint:gosec // G304: a file of the project or of the overlay the user named
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only
	opened, err := in.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, opened) {
		return fmt.Errorf("%s changed while it was read", src)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // G304: inside the temporary tree
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, opened.Size())); err != nil {
		out.Close() //nolint:errcheck,gosec // the copy error is the one to report
		return err
	}
	return out.Close()
}

func sameOrInside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ProjectPath maps a path in the temporary tree to the same path in the
// project. Other paths, and a nil overlay, return p unchanged.
func (o *buildOverlay) ProjectPath(p string) string {
	if o == nil || p == "" {
		return p
	}
	rel, err := filepath.Rel(o.Root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return filepath.Join(o.ProjectRoot, rel)
}

// FromOverlay reports whether a path (in the tree or in the project) is a
// file of the overlay, and returns the overlay file.
func (o *buildOverlay) FromOverlay(p string) (string, bool) {
	if o == nil || p == "" {
		return "", false
	}
	p = o.ProjectPath(p)
	rel, err := filepath.Rel(o.ProjectRoot, p)
	if err != nil {
		return "", false
	}
	if _, ok := o.files[filepath.ToSlash(rel)]; !ok {
		return "", false
	}
	return filepath.Join(o.Dir, rel), true
}

// WrapError names project paths instead of temporary ones in err, and notes
// which of the named files came from the overlay.
func (o *buildOverlay) WrapError(err error) error {
	if o == nil || err == nil {
		return err
	}
	o.rewriteErrorFiles(err)
	return &overlayError{err: err, overlay: o}
}

// rewriteErrorFiles points the file fields of the located errors in the
// chain at the project path, which the error output and the JSON log read.
func (o *buildOverlay) rewriteErrorFiles(err error) {
	for err != nil {
		switch e := err.(type) { //nolint:errorlint // walks the chain one level at a time
		case *spec.SchemaValidationError:
			e.File = o.ProjectPath(e.File)
		case *config.DocumentError:
			e.File = o.ProjectPath(e.File)
		case *config.SourceError:
			e.File = o.ProjectPath(e.File)
		}
		if multi, ok := err.(interface{ Unwrap() []error }); ok {
			for _, child := range multi.Unwrap() {
				o.rewriteErrorFiles(child)
			}
			return
		}
		err = errors.Unwrap(err)
	}
}

// overlayError is an error of a build with --overlay.
type overlayError struct {
	err     error
	overlay *buildOverlay
}

func (e *overlayError) Error() string {
	return strings.ReplaceAll(e.err.Error(), e.overlay.Root, e.overlay.ProjectRoot)
}

func (e *overlayError) Unwrap() error { return e.err }

// OverlayFiles lists the overlay files the error names, as project path to
// overlay path, sorted by project path.
func (e *overlayError) OverlayFiles() [][2]string {
	msg := e.Error()
	named := errorFiles(e.err)
	var out [][2]string
	seen := map[string]bool{}
	for rel := range e.overlay.files {
		project := filepath.Join(e.overlay.ProjectRoot, filepath.FromSlash(rel))
		hit := strings.Contains(msg, project)
		for _, f := range named {
			hit = hit || f == project
		}
		if hit && !seen[project] {
			seen[project] = true
			out = append(out, [2]string{project, filepath.Join(e.overlay.Dir, filepath.FromSlash(rel))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// Note is the line the error output adds: which files were read from the overlay.
func (e *overlayError) Note() string {
	files := e.OverlayFiles()
	if len(files) == 0 {
		return ""
	}
	parts := make([]string, 0, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(e.overlay.ProjectRoot, f[0])
		if err != nil {
			rel = f[0]
		}
		parts = append(parts, fmt.Sprintf("%s from %s", filepath.ToSlash(rel), f[1]))
	}
	return "Content read from the overlay, not from the project: " + strings.Join(parts, ", ")
}

// overlaySource is the overlay file of a project path the error names.
func overlaySource(err error, projectPath string) (string, bool) {
	var oe *overlayError
	if !errors.As(err, &oe) {
		return "", false
	}
	return oe.overlay.FromOverlay(projectPath)
}

// errorFiles are the file fields of the located errors in the chain.
func errorFiles(err error) []string {
	var out []string
	for err != nil {
		switch e := err.(type) { //nolint:errorlint // walks the chain one level at a time
		case *spec.SchemaValidationError:
			out = append(out, e.File)
		case *config.DocumentError:
			out = append(out, e.File)
		case *config.SourceError:
			out = append(out, e.File)
		}
		if multi, ok := err.(interface{ Unwrap() []error }); ok {
			for _, child := range multi.Unwrap() {
				out = append(out, errorFiles(child)...)
			}
			return out
		}
		err = errors.Unwrap(err)
	}
	return out
}

// projectDocuments returns docs with project paths, for the build logs.
func (o *buildOverlay) projectDocuments(docs []config.Document) []config.Document {
	if o == nil {
		return docs
	}
	out := make([]config.Document, len(docs))
	for i, doc := range docs {
		doc.File = o.ProjectPath(doc.File)
		out[i] = doc
	}
	return out
}

// projectFindings returns lint findings with project paths, for the JSON build log.
func (o *buildOverlay) projectFindings(findings []lint.Finding) []lint.Finding {
	if o == nil {
		return findings
	}
	out := make([]lint.Finding, len(findings))
	for i, f := range findings {
		f.File = o.ProjectPath(f.File)
		out[i] = f
	}
	return out
}
