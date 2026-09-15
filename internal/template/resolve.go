package template

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"bino.bi/bino/internal/pathutil"
)

// SourceKind classifies a parsed template source.
type SourceKind int

const (
	// SourceBuiltin is a //go:embed'd template (minimal, predef, standard).
	SourceBuiltin SourceKind = iota
	// SourceCurated is a baked-in short-name pinned to a repo + commit.
	SourceCurated
	// SourceShorthand is owner/repo[/subdir][#ref].
	SourceShorthand
	// SourceURL is a host-generic archive URL.
	SourceURL
	// SourceLocal is a local directory or file:// path.
	SourceLocal
)

// Source is a parsed template source.
type Source struct {
	Kind   SourceKind
	Raw    string
	Name   string // builtin or curated short-name
	Owner  string
	Repo   string
	Subdir string
	Ref    string // branch | tag | commit SHA; empty means default branch
	URL    string
	Path   string
}

// curatedEntry pins a curated short-name to a specific repo + commit.
type curatedEntry struct {
	Owner string
	Repo  string
	SHA   string
}

// curated maps short-names to pinned repos. It is intentionally empty until the
// reference repo (bino-bi/report-template) is published; the resolver and tests
// exercise the mechanism through this same map. When the repo is published, add:
//
//	"report": {Owner: "bino-bi", Repo: "report-template", SHA: "<pinned commit>"},
var curated = map[string]curatedEntry{}

// CuratedNames returns the curated short-names, for discovery surfaces.
func CuratedNames() []string {
	names := make([]string, 0, len(curated))
	for name := range curated {
		names = append(names, name)
	}
	return names
}

var shorthandRE = regexp.MustCompile(`^[\w.-]+/[\w.-]+(?:/[\w./-]+?)?(?:#.+)?$`)

// ParseSource classifies a SOURCE argument. An empty source is the built-in
// minimal template. Resolution order: built-in name, curated short-name,
// file:// / URL, local path, owner/repo shorthand.
func ParseSource(raw string) (Source, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Source{Kind: SourceBuiltin, Raw: raw, Name: "minimal"}, nil
	}
	if IsBuiltin(raw) {
		return Source{Kind: SourceBuiltin, Raw: raw, Name: raw}, nil
	}
	if _, ok := curated[raw]; ok {
		return Source{Kind: SourceCurated, Raw: raw, Name: raw}, nil
	}
	if rest, ok := strings.CutPrefix(raw, "file://"); ok {
		return Source{Kind: SourceLocal, Raw: raw, Path: rest}, nil
	}
	if pathutil.HasScheme(raw) {
		return Source{Kind: SourceURL, Raw: raw, URL: raw}, nil
	}
	if isLocalPathish(raw) {
		return Source{Kind: SourceLocal, Raw: raw, Path: raw}, nil
	}
	if shorthandRE.MatchString(raw) {
		owner, repo, subdir, ref := parseShorthand(raw)
		return Source{Kind: SourceShorthand, Raw: raw, Owner: owner, Repo: repo, Subdir: subdir, Ref: ref}, nil
	}
	return Source{}, fmt.Errorf("unrecognized template source %q (use a built-in name, owner/repo[/subdir]#ref, a URL, or ./local-path)", raw)
}

// isLocalPathish reports whether raw is clearly a local filesystem path rather
// than an owner/repo shorthand.
func isLocalPathish(raw string) bool {
	switch {
	case raw == ".", raw == "..":
		return true
	case strings.HasPrefix(raw, "./"), strings.HasPrefix(raw, "../"):
		return true
	case strings.HasPrefix(raw, "/"), strings.HasPrefix(raw, "~"):
		return true
	default:
		return false
	}
}

func parseShorthand(raw string) (owner, repo, subdir, ref string) {
	if i := strings.Index(raw, "#"); i >= 0 {
		ref = raw[i+1:]
		raw = raw[:i]
	}
	parts := strings.SplitN(raw, "/", 3)
	owner = parts[0]
	repo = parts[1]
	if len(parts) == 3 {
		subdir = parts[2]
	}
	return owner, repo, subdir, ref
}

// isHexSHA reports whether s is a full 40-character hex commit SHA.
func isHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// LoadTree reads and parses the bino.template.yaml at the root of fsys and
// returns the render root. If a template/ subdirectory exists it is the render
// root (g8-style src layout); otherwise the root itself is, with the manifest
// skipped during rendering.
func LoadTree(fsys fs.FS) (*ProjectTemplate, fs.FS, error) {
	data, err := fs.ReadFile(fsys, manifestFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", manifestFile, err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return nil, nil, err
	}
	if info, statErr := fs.Stat(fsys, "template"); statErr == nil && info.IsDir() {
		sub, subErr := fs.Sub(fsys, "template")
		if subErr != nil {
			return nil, nil, subErr
		}
		return manifest, sub, nil
	}
	return manifest, fsys, nil
}
