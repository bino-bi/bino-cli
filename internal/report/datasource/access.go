package datasource

import (
	"os"
	"path/filepath"
	"strings"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
)

// Access lists what the views RegisterViews creates for docs read at query
// time, so a locked-down session can allow exactly that.
type Access struct {
	// Files are the local files a view reads by exact path.
	Files []string
	// Prefixes are the local directories and URL prefixes a directory, glob
	// or URL source reads, each ending in a slash.
	Prefixes []string
	// Remote reports that a view reads an http(s) or s3 URL, which needs the
	// httpfs extension loaded.
	Remote bool
}

// AccessFor returns the files and the file and URL prefixes the DataSource
// documents in docs read. Inline and database sources read no path and add
// nothing.
func AccessFor(docs []config.Document) Access {
	var acc Access
	seen := map[string]bool{}
	addPrefix := func(p string) {
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		if !seen[p] {
			seen[p] = true
			acc.Prefixes = append(acc.Prefixes, p)
		}
	}
	addFile := func(p string) {
		if !seen[p] {
			seen[p] = true
			acc.Files = append(acc.Files, p)
		}
	}
	// DuckDB compares the path as written; a symlinked dir (macOS /tmp) can appear either way.
	withReal := func(p string, add func(string)) {
		add(p)
		if real, err := filepath.EvalSymlinks(p); err == nil && filepath.ToSlash(real) != p {
			add(filepath.ToSlash(real))
		}
	}
	for _, doc := range docs {
		if doc.Kind != "DataSource" {
			continue
		}
		spec, err := parseSpec(doc.Raw)
		if err != nil {
			continue
		}
		path := strings.TrimSpace(spec.Path)
		if path == "" {
			continue
		}
		if pathutil.IsURL(path) {
			acc.Remote = true
			addPrefix(urlPrefix(path))
			continue
		}
		resolved, err := pathutil.Resolve(filepath.Dir(doc.File), path)
		if err != nil {
			continue
		}
		if dir, ok := localDir(resolved); ok {
			withReal(dir, addPrefix)
		} else {
			withReal(resolved, addFile)
		}
	}
	return acc
}

// localDir returns the directory a directory or glob source reads, and
// false for a single file.
func localDir(p string) (string, bool) {
	if pathutil.HasGlob(p) {
		p = p[:strings.IndexAny(p, "*?[")]
		if i := strings.LastIndex(p, "/"); i >= 0 {
			return p[:i], true
		}
		return ".", true
	}
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return p, true
	}
	return "", false
}

// urlPrefix cuts a URL back to its directory, before any glob character.
func urlPrefix(u string) string {
	if i := strings.IndexAny(u, "*?["); i >= 0 {
		u = u[:i]
	}
	scheme := strings.Index(u, "://") + 3
	if i := strings.LastIndex(u, "/"); i >= scheme {
		return u[:i+1]
	}
	return u + "/"
}
