// Package web embeds and serves the CSS/JS assets for preview and serve modes.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed shared/*.css shared/*.js preview/*.css preview/*.js preview/components/*.js serve/*.css serve/*.js serve/components/*.js
var assets embed.FS

// mimeTypes maps file extensions to MIME types for embedded assets.
var mimeTypes = map[string]string{
	".css": "text/css; charset=utf-8",
	".js":  "application/javascript; charset=utf-8",
}

// Handler returns an http.Handler that serves embedded web assets under the
// given prefix (e.g. "/__bino/"). All responses include Cache-Control: no-cache
// since this is a development tool where caching is counterproductive.
func Handler(prefix string) http.Handler {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the prefix to get the relative path within the embed.FS
		relPath := strings.TrimPrefix(r.URL.Path, prefix)
		if relPath == "" || relPath == r.URL.Path {
			http.NotFound(w, r)
			return
		}

		data, err := fs.ReadFile(assets, relPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Set Content-Type from extension
		ext := path.Ext(relPath)
		if ct, ok := mimeTypes[ext]; ok {
			w.Header().Set("Content-Type", ct)
		}

		// Development tool: disable caching so edits during goreleaser --snapshot
		// iterations are picked up immediately after rebuild.
		w.Header().Set("Cache-Control", "no-cache")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}
