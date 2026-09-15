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

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
)

// collectedFile is one file the project publishes: where it lives on disk,
// where it sits in the package tree, and the digest the registry will verify
// it against.
type collectedFile struct {
	TreePath string // relative, slash-form, at most one directory level
	AbsPath  string
	Type     string
	Digest   string
	Size     int64
	Kinds    []string // the document's kinds, in stream order; nil for a resource
}

// Errors the collector reports. Each names something the author can fix
// locally, before anything is uploaded.
var (
	errPublishEmpty      = errors.New("the package includes no files")
	errPublishPathDepth  = errors.New("a package file may sit at most one directory deep")
	errPublishSymlink    = errors.New("a package may not include a symbolic link")
	errPublishCredential = errors.New("a package may not include credentials")
)

// credentialDirs are never publishable, whatever the include list says.
// secrets/ and signing/ are in the default include set, and the predef lint
// rule that flags credential kinds only sees YAML — so a secrets/*.csv would
// otherwise pass the resource allow-list and be uploaded. The directory is the
// marker here, independently of what the files contain.
var credentialDirs = []string{"secrets", "signing"}

// credentialKinds may never be published even from a non-credential directory.
var credentialKinds = []string{"ConnectionSecret", "SigningProfile"}

// collectPackageFiles walks the project and returns the files [package].include
// selects, digested and ready to publish, sorted by tree path.
//
// The walk is deliberately stricter than the loader's: it refuses symlinks
// rather than following them (an included symlink is an exfiltration primitive
// — the file the author sees is not the file that would be uploaded), refuses
// credentials, and refuses anything the registry's path grammar or resource
// allow-list would reject, so a mistake costs a local error instead of a
// rejected upload.
func collectPackageFiles(projectRoot string, cfg *pathutil.PackageConfig) ([]collectedFile, error) {
	include := cfg.IncludeSet(projectRoot)
	ignore := loadPublishIgnore(projectRoot)

	var files []collectedFile
	walkErr := filepath.WalkDir(projectRoot, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(projectRoot, abs)
		if relErr != nil {
			return relErr
		}
		slashRel := filepath.ToSlash(rel)
		if d.IsDir() {
			if slashRel == "." {
				return nil
			}
			if skipPublishDir(abs, slashRel, include, ignore) {
				return filepath.SkipDir
			}
			return nil
		}
		if !include.Contains(abs) || ignore.matches(slashRel) {
			return nil
		}
		// WalkDir reports a symlink as a plain entry, so this is the only
		// place it can be caught before the bytes are read.
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", errPublishSymlink, slashRel)
		}
		f, fileErr := collectOneFile(abs, slashRel)
		if fileErr != nil {
			return fileErr
		}
		files = append(files, f)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: nothing under %s", errPublishEmpty, strings.Join(includeDirs(cfg), ", "))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].TreePath < files[j].TreePath })

	sizes := make(map[string]int64, len(files))
	for _, f := range files {
		sizes[f.TreePath] = f.Size
	}
	if err := registry.CheckTreeQuotas(sizes); err != nil {
		return nil, err
	}
	return files, nil
}

// skipPublishDir prunes a directory the walk can never yield a file from, so a
// large mocks/ or reports/ tree is not traversed at all.
func skipPublishDir(abs, slashRel string, include *pathutil.IncludeSet, ignore *publishIgnore) bool {
	if ignore.matches(slashRel) {
		return true
	}
	base := filepath.Base(slashRel)
	if base == ".git" || base == ".bino" {
		return true
	}
	// A directory is worth descending into when a file inside it could be
	// included. Contains is a pure shape test over an absolute path, so
	// probing with a name that cannot exist is enough and costs no I/O.
	return !include.Contains(filepath.Join(abs, "probe"))
}

// collectOneFile classifies, reads and digests one included file.
func collectOneFile(abs, slashRel string) (collectedFile, error) {
	if err := registry.ValidateTreePath(slashRel); err != nil {
		if strings.Count(slashRel, "/") > 1 {
			return collectedFile{}, fmt.Errorf("%w: %s", errPublishPathDepth, slashRel)
		}
		return collectedFile{}, fmt.Errorf("%s cannot be published: %w", slashRel, err)
	}
	if isCredentialPath(slashRel) {
		return collectedFile{}, fmt.Errorf("%w: %s — exclude it with [package].include", errPublishCredential, slashRel)
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return collectedFile{}, fmt.Errorf("read %s: %w", slashRel, err)
	}
	f := collectedFile{
		TreePath: slashRel,
		AbsPath:  abs,
		Type:     registry.FileTypeForPath(slashRel),
		Size:     int64(len(body)),
	}
	if f.Type == registry.FileResource {
		if !registry.ResourceExtAllowed(slashRel) {
			return collectedFile{}, fmt.Errorf("%w: %s — the registry accepts %s",
				registry.ErrUnsupportedType, slashRel, strings.Join(registry.ResourceExtensions(), ", "))
		}
		f.Digest = registry.ResourceDigest(body)
		return f, nil
	}
	digest, docs, err := registry.DocumentDigest(body)
	if err != nil {
		return collectedFile{}, fmt.Errorf("%s: %w", slashRel, err)
	}
	f.Digest = digest
	f.Kinds = documentKinds(docs)
	for _, k := range f.Kinds {
		for _, forbidden := range credentialKinds {
			if k == forbidden {
				return collectedFile{}, fmt.Errorf("%w: %s declares a %s", errPublishCredential, slashRel, k)
			}
		}
	}
	return f, nil
}

// isCredentialPath reports whether a tree path sits in a directory that never
// ships, whatever it holds.
func isCredentialPath(slashRel string) bool {
	first, _, _ := strings.Cut(slashRel, "/")
	for _, d := range credentialDirs {
		if first == d {
			return true
		}
	}
	return false
}

// includeDirs is the effective include list, for an error message that tells
// the author where the collector looked.
func includeDirs(cfg *pathutil.PackageConfig) []string {
	if len(cfg.Include) > 0 {
		return cfg.Include
	}
	return pathutil.DefaultPackageIncludeDirs()
}

// publishFiles turns the collected set into the client's upload list and the
// manifest's file entries.
func publishFiles(files []collectedFile) ([]registry.PublishFile, []registry.FileEntry) {
	uploads := make([]registry.PublishFile, len(files))
	entries := make([]registry.FileEntry, len(files))
	for i, f := range files {
		abs := f.AbsPath
		uploads[i] = registry.PublishFile{
			Path: f.TreePath,
			Open: func() (io.ReadCloser, error) { return os.Open(abs) },
		}
		entries[i] = registry.FileEntry{Path: f.TreePath, Type: f.Type, Digest: f.Digest}
	}
	return uploads, entries
}
