package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/text/unicode/norm"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/filehash"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/watchers"
)

// watchRefs holds the non-manifest files the loaded documents read. Globs are
// kept as patterns, not expanded, so a new file matching one still counts.
// All paths are absolute and slash-separated.
type watchRefs struct {
	files   map[string]struct{} // keyed by watchKey
	globs   []string
	mdGlobs []string // DocumentArtefact sources: only .md files match
}

func collectWatchRefs(docs []config.Document) *watchRefs {
	r := &watchRefs{files: map[string]struct{}{}}
	for _, doc := range docs {
		baseDir := filepath.Dir(doc.File)
		switch doc.Kind {
		case "DataSource":
			r.addDataSource(baseDir, doc.Raw)
		case "DocumentArtefact":
			r.addDocumentArtefact(baseDir, doc.Raw)
		case "DataSet":
			r.addDataSet(baseDir, doc.Raw)
		}
		for _, p := range lint.LocalFileRefs(lint.Document{File: doc.File, Kind: doc.Kind, Raw: doc.Raw}) {
			r.addFile(baseDir, p)
		}
	}
	return r
}

// watchKey folds a path for matching. On macOS and Windows the manifest and
// the disk may differ in case, and macOS may store names in NFD while the
// manifest is NFC. On other disks the cost is at most an extra refresh.
func watchKey(path string) string {
	return strings.ToLower(norm.NFC.String(filepath.ToSlash(path)))
}

// globMatch tries the pattern as written first: folding a negated class like
// [!A-Z] changes what it matches.
func globMatch(pattern, path, key string) bool {
	return doublestar.MatchUnvalidated(pattern, path) || doublestar.MatchUnvalidated(watchKey(pattern), key)
}

// addDataSource follows the path rules of filehash.ResolveAndHashFiles.
func (r *watchRefs) addDataSource(baseDir string, raw json.RawMessage) {
	var payload struct {
		Spec struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"spec"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	sourceType := strings.ToLower(strings.TrimSpace(payload.Spec.Type))
	switch sourceType {
	case "csv", "excel", "parquet":
	default:
		return
	}
	path := strings.TrimSpace(payload.Spec.Path)
	if path == "" || pathutil.IsURL(path) {
		return
	}
	resolved, err := pathutil.Resolve(baseDir, path)
	if err != nil {
		return
	}
	if pathutil.HasGlob(path) {
		r.globs = append(r.globs, resolved)
		return
	}
	dirGlob := resolved + "/" + filehash.DefaultGlobForType(sourceType)
	info, err := os.Stat(resolved)
	switch {
	case err != nil:
		// Not there yet: it may come back as a file or as a directory.
		r.files[watchKey(resolved)] = struct{}{}
		r.globs = append(r.globs, dirGlob)
	case info.IsDir():
		r.globs = append(r.globs, dirGlob)
	default:
		r.files[watchKey(resolved)] = struct{}{}
	}
}

// addDocumentArtefact follows the source rules of mdscan.ResolveSourceFiles.
func (r *watchRefs) addDocumentArtefact(baseDir string, raw json.RawMessage) {
	var payload struct {
		Spec config.DocumentArtefactSpec `json:"spec"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	for _, src := range payload.Spec.Sources {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}
		resolved, err := pathutil.Resolve(baseDir, src)
		if err != nil {
			continue
		}
		if strings.ContainsAny(src, "*?[") {
			r.mdGlobs = append(r.mdGlobs, resolved)
		} else {
			r.files[watchKey(resolved)] = struct{}{}
		}
	}
	r.addFile(baseDir, payload.Spec.Stylesheet)
}

func (r *watchRefs) addDataSet(baseDir string, raw json.RawMessage) {
	var payload struct {
		Spec struct {
			Query spec.QueryField `json:"query"`
			Prql  spec.QueryField `json:"prql"`
		} `json:"spec"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	r.addFile(baseDir, payload.Spec.Query.File)
	r.addFile(baseDir, payload.Spec.Prql.File)
}

func (r *watchRefs) addFile(baseDir, path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if resolved, err := pathutil.Resolve(baseDir, path); err == nil {
		r.files[watchKey(resolved)] = struct{}{}
	}
}

// relevant reports whether evt can change the loaded project: a manifest, a
// project config file, anything under .bino/registry, or a referenced file.
func (r *watchRefs) relevant(evt watchers.Event) bool {
	switch strings.ToLower(filepath.Ext(evt.Path)) {
	case ".yaml", ".yml":
		return true
	}
	switch filepath.Base(evt.Path) {
	case "bino.toml", registry.LockfileName, ".bnignore":
		return true
	}
	if strings.HasPrefix(filepath.ToSlash(evt.RelativePath), ".bino/registry/") {
		return true
	}
	p := filepath.ToSlash(evt.Path)
	key := watchKey(p)
	if _, ok := r.files[key]; ok {
		return true
	}
	for _, g := range r.globs {
		if globMatch(g, p, key) {
			return true
		}
	}
	if strings.EqualFold(filepath.Ext(p), ".md") {
		for _, g := range r.mdGlobs {
			if globMatch(g, p, key) {
				return true
			}
		}
	}
	return false
}
