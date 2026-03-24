package plugin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// AssetCache stores plugin assets indexed by URL path for fast HTTP serving.
type AssetCache struct {
	assets map[string]AssetFile
}

// BuildAssetCache collects assets from all plugins that provide them.
func BuildAssetCache(ctx context.Context, registry *PluginRegistry, renderMode string) *AssetCache {
	cache := &AssetCache{assets: make(map[string]AssetFile)}
	for _, p := range registry.PluginsWithAssets() {
		scripts, styles, err := p.GetAssets(ctx, renderMode)
		if err != nil {
			continue
		}
		for _, a := range scripts {
			cache.assets[a.URLPath] = a
		}
		for _, a := range styles {
			cache.assets[a.URLPath] = a
		}
	}
	return cache
}

// Get returns an asset by URL path.
func (c *AssetCache) Get(urlPath string) (AssetFile, bool) {
	a, ok := c.assets[urlPath]
	return a, ok
}

// AllScripts returns all cached script assets.
func (c *AssetCache) AllScripts() []AssetFile {
	var scripts []AssetFile
	for _, a := range c.assets {
		if isScriptPath(a.URLPath) {
			scripts = append(scripts, a)
		}
	}
	return scripts
}

// AllStyles returns all cached style assets.
func (c *AssetCache) AllStyles() []AssetFile {
	var styles []AssetFile
	for _, a := range c.assets {
		if !isScriptPath(a.URLPath) {
			styles = append(styles, a)
		}
	}
	return styles
}

// NewAssetHandler creates an HTTP handler that serves plugin assets from the cache.
func NewAssetHandler(cache *AssetCache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset, ok := cache.Get(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		mediaType := asset.MediaType
		if mediaType == "" {
			mediaType = inferMediaType(r.URL.Path)
		}
		w.Header().Set("Content-Type", mediaType)

		if len(asset.Content) > 0 {
			_, _ = w.Write(asset.Content)
			return
		}
		if asset.FilePath != "" {
			http.ServeFile(w, r, asset.FilePath)
			return
		}
		http.NotFound(w, r)
	})
}

// RenderAssetTags generates HTML <script> and <link> tags for plugin assets.
func RenderAssetTags(cache *AssetCache) string {
	if cache == nil || len(cache.assets) == 0 {
		return ""
	}
	var buf strings.Builder
	for _, script := range cache.AllScripts() {
		if script.IsModule {
			fmt.Fprintf(&buf, "<script type=\"module\" src=\"%s\"></script>\n", escapeAttr(script.URLPath))
		} else {
			fmt.Fprintf(&buf, "<script src=\"%s\"></script>\n", escapeAttr(script.URLPath))
		}
	}
	for _, style := range cache.AllStyles() {
		fmt.Fprintf(&buf, "<link rel=\"stylesheet\" href=\"%s\">\n", escapeAttr(style.URLPath))
	}
	return buf.String()
}

func isScriptPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".mjs")
}

func inferMediaType(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".mjs"):
		return "application/javascript"
	case strings.HasSuffix(lower, ".css"):
		return "text/css"
	case strings.HasSuffix(lower, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(lower, ".woff"):
		return "font/woff"
	default:
		return "application/octet-stream"
	}
}

// escapeAttr performs minimal HTML attribute escaping.
func escapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
