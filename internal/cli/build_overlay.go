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
	// lower maps a lower-case overlay path to its spelling, to find names
	// that differ only in case.
	lower map[string]string
	// inProject is Dir as a path below ProjectRoot when the overlay lives in
	// the project, so the mirror leaves it out. Empty otherwise.
	inProject string
	// tree is Root opened as an os.Root: every write goes through it, so
	// none can follow a link out of the temporary tree.
	tree *os.Root
}

// linkedDirs are linked as a whole instead of walked, unless the overlay has
// a file inside: the manifest loader skips them, and they can hold many
// files. Not below .bino: the loader reads .bino/registry with no name skips,
// and the build writes caches below .bino, which must land in the tree.
var linkedDirs = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {}, "node_modules": {}, "vendor": {}, "venv": {}, ".venv": {},
	"dist": {}, "build": {}, "target": {},
}

// binoCache is left out of the tree: the build writes its caches there, and
// they must go to the tree, not through a link into the project.
const binoCache = ".bino/cache"

// Limits for an overlay: it can come from an untrusted agent. Entries count
// files and directories.
const (
	maxOverlayEntries = 2000
	maxOverlayBytes   = 8 << 20
)

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

	// The overlay is read through an os.Root, so a directory swapped for a
	// link after the walk cannot lead outside it.
	src, err := os.OpenRoot(absDir)
	if err != nil {
		return nil, fmt.Errorf("--overlay %s: %w", dir, err)
	}
	defer src.Close() //nolint:errcheck // read-only
	files, err := overlayFiles(src)
	if err != nil {
		return nil, fmt.Errorf("--overlay %s: %w", dir, err)
	}
	if _, ok := files["bino.toml"]; ok {
		return nil, fmt.Errorf("--overlay %s: bino.toml cannot be replaced, it is always read from the project", dir)
	}

	root, err := os.MkdirTemp("", "bino-overlay-*")
	if err != nil {
		return nil, fmt.Errorf("--overlay: create temporary directory: %w", err)
	}
	o := &buildOverlay{ProjectRoot: projectRoot, Dir: absDir, Root: root, files: files, lower: map[string]string{}}
	for rel := range files {
		o.lower[strings.ToLower(rel)] = rel
	}
	if rel, err := filepath.Rel(resolvedRoot, absDir); err == nil && sameOrInside(absDir, resolvedRoot) {
		o.inProject = filepath.Join(projectRoot, rel)
	}
	if o.tree, err = os.OpenRoot(root); err == nil {
		err = o.mirror(src)
	}
	if err != nil {
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
	if o.tree != nil {
		o.tree.Close() //nolint:errcheck,gosec // the tree is removed next
	}
	return os.RemoveAll(o.Root)
}

// overlayFiles lists the overlay's files. Only regular files are accepted: a
// link or a device could make the build read a file outside the overlay.
func overlayFiles(src *os.Root) (map[string]struct{}, error) {
	files := map[string]struct{}{}
	entries := 0
	var total int64
	err := fs.WalkDir(src.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		entries++
		if entries > maxOverlayEntries {
			return fmt.Errorf("more than %d files and directories", maxOverlayEntries)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file (links, devices and pipes are not allowed)", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if total += info.Size(); total > maxOverlayBytes {
			return errOverlayTooLarge
		}
		files[rel] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

var errOverlayTooLarge = fmt.Errorf("more than %d MiB", maxOverlayBytes>>20)

// mirror links the project into Root, then copies the overlay files in.
func (o *buildOverlay) mirror(src *os.Root) error {
	if err := o.Sync(); err != nil {
		return err
	}
	budget := int64(maxOverlayBytes)
	for rel := range o.files {
		if err := o.copyOverlayFile(src, rel, &budget); err != nil {
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
		if o.inProject != "" && sameOrInside(path, o.inProject) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if slashRel == binoCache {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		inBino := slashRel == ".bino" || strings.HasPrefix(slashRel, ".bino/")
		existing, statErr := o.tree.Lstat(rel)
		if d.IsDir() {
			if statErr == nil {
				if existing.Mode()&fs.ModeSymlink != 0 {
					return filepath.SkipDir
				}
				return nil
			}
			if _, link := linkedDirs[d.Name()]; link && !inBino && !o.hasFilesIn(slashRel) {
				if err := o.link(path, rel, true); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return o.tree.Mkdir(rel, 0o755)
		}
		if statErr == nil {
			return nil
		}
		if _, replaced := o.files[slashRel]; replaced {
			return nil
		}
		if spelled, clash := o.lower[strings.ToLower(slashRel)]; clash {
			// one of the two would replace the other on a case-insensitive file system
			return fmt.Errorf("%s differs only in case from the overlay file %s", slashRel, spelled)
		}
		if slashRel == ".bino" && d.Type()&fs.ModeSymlink != 0 {
			return o.linkBinoChildren(path)
		}
		if d.Type()&fs.ModeSymlink != 0 && o.hasFilesIn(slashRel) {
			// an overlay file below a link would be written through it into the link's target
			return fmt.Errorf("%s is a link in the project, the overlay cannot have files below it", slashRel)
		}
		return o.link(path, rel, false)
	})
	if err != nil {
		return fmt.Errorf("--overlay: mirror project: %w", err)
	}
	return nil
}

// linkBinoChildren mirrors a project .bino that is itself a link: a real
// directory with its entries linked, but no cache, as for a plain .bino.
func (o *buildOverlay) linkBinoChildren(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if err := o.tree.Mkdir(".bino", 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	for _, e := range entries {
		rel := filepath.Join(".bino", e.Name())
		if filepath.ToSlash(rel) == binoCache {
			continue
		}
		if err := o.link(filepath.Join(path, e.Name()), rel, e.IsDir()); err != nil {
			return err
		}
	}
	return nil
}

// copyOverlayFile copies one overlay file into the tree, within the byte
// budget. Directories and the file are created through the os.Root of the
// tree, so they cannot go through a link, and the file must be new, which
// also catches a name that only differs in case from a project file on a
// case-insensitive file system.
func (o *buildOverlay) copyOverlayFile(src *os.Root, rel string, budget *int64) error {
	in, err := src.Open(rel)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if dir := filepath.Dir(filepath.FromSlash(rel)); dir != "." {
		if err := o.tree.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("its directory is a link in the project or cannot be created: %w", err)
		}
	}
	out, err := o.tree.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("the project has a file with this name (does the name differ only in case?)")
	}
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(in, *budget+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > *budget {
		return errOverlayTooLarge
	}
	*budget -= n
	return nil
}

// hasFilesIn reports whether the overlay has a file below the
// project-relative dir, in any case.
func (o *buildOverlay) hasFilesIn(dir string) bool {
	prefix := strings.ToLower(dir) + "/"
	for lower := range o.lower {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// link links rel in the tree to the project path. Where the system does not
// allow links (Windows without developer mode) it copies instead.
func (o *buildOverlay) link(path, rel string, dir bool) error {
	err := o.tree.Symlink(path, rel)
	if err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	if dir {
		if err := os.CopyFS(filepath.Join(o.Root, rel), os.DirFS(path)); err != nil {
			return fmt.Errorf("cannot link or copy %s: %w", filepath.ToSlash(rel), err)
		}
		return nil
	}
	return o.copyProjectFile(path, rel)
}

// copyProjectFile copies a project file into the tree. A project link is
// followed, as the build would follow it without --overlay.
func (o *buildOverlay) copyProjectFile(path, rel string) error {
	in, err := os.Open(path) //nolint:gosec // G304: a file of the project
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot link or copy %s: not a regular file", filepath.ToSlash(rel))
	}
	out, err := o.tree.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
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

// checkOutputPath refuses, when confine is set (--overlay), an output file
// name that is absolute or leaves the output directory: a draft must not be
// able to write over a project file.
func checkOutputPath(confine bool, outputDir, filename string) error {
	if !confine {
		return nil
	}
	path := filepath.Join(outputDir, filename)
	if filepath.IsAbs(filename) || path == filepath.Clean(outputDir) || !sameOrInside(path, outputDir) {
		return fmt.Errorf("with --overlay the output file %q must stay inside the output directory %s", filename, outputDir)
	}
	return nil
}
