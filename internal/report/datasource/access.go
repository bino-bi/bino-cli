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
	// Prefixes are the local directories and URL prefixes the views read,
	// each ending in a slash.
	Prefixes []string
	// Remote reports that a view reads an http(s) or s3 URL, which needs the
	// httpfs extension loaded.
	Remote bool
}

// AccessFor returns the file and URL prefixes the DataSource documents in docs
// read. Inline and database sources read no path and add nothing.
func AccessFor(docs []config.Document) Access {
	var acc Access
	seen := map[string]bool{}
	add := func(p string) {
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		if !seen[p] {
			seen[p] = true
			acc.Prefixes = append(acc.Prefixes, p)
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
			add(urlPrefix(path))
			continue
		}
		resolved, err := pathutil.Resolve(filepath.Dir(doc.File), path)
		if err != nil {
			continue
		}
		dir := localDir(resolved)
		add(dir)
		// DuckDB compares the path as written; a symlinked dir (macOS /tmp) can appear either way.
		if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
			add(filepath.ToSlash(real))
		}
	}
	return acc
}

// localDir is the directory a local path, directory or glob reads from.
func localDir(p string) string {
	if pathutil.HasGlob(p) {
		p = p[:strings.IndexAny(p, "*?[")]
		if i := strings.LastIndex(p, "/"); i >= 0 {
			return p[:i]
		}
		return "."
	}
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return p
	}
	return filepath.ToSlash(filepath.Dir(p))
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
