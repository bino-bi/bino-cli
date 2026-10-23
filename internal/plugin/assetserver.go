package plugin

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	urlpath "path"
	"strings"

	"bino.bi/bino/internal/logx"
)

// AssetCache stores plugin assets indexed by URL path for fast HTTP serving.
// scripts and styles keep the order the plugins returned them, plugin by
// plugin in load order, because a library must load before code that uses it.
type AssetCache struct {
	assets  map[string]AssetFile
	scripts []AssetFile
	styles  []AssetFile
}

// BuildAssetCache collects assets from all plugins that provide them.
func BuildAssetCache(ctx context.Context, registry *PluginRegistry, renderMode string) *AssetCache {
	log := logx.FromContext(ctx)
	cache := &AssetCache{assets: make(map[string]AssetFile)}
	for _, p := range registry.PluginsWithAssets() {
		scripts, styles, err := p.GetAssets(ctx, renderMode)
		if err != nil {
			// Skipping silently shows up as a visual defect (missing plugin
			// CSS/JS) with no diagnostic anywhere.
			log.Warnf("plugin %s: get assets failed, its assets are missing from the render: %v", p.Manifest().Name, err)
			continue
		}
		name := p.Manifest().Name
		warned := make(map[string]bool)
		add := func(list *[]AssetFile, a AssetFile) {
			scope := assetPathScope(name, a.URLPath)
			if (scope == assetForeign || scope == assetUnserved) && !warned[a.URLPath] {
				warned[a.URLPath] = true
				if scope == assetForeign {
					log.Warnf("plugin %s: asset %q is under /plugins/ but not under /plugins/%s/, skipped", name, a.URLPath, name)
				} else {
					log.Warnf("plugin %s: asset %q is not under /plugins/%s/ and will not be served", name, a.URLPath, name)
				}
			}
			if scope == assetForeign {
				// Skipped so one plugin cannot overwrite another plugin's assets.
				return
			}
			if _, dup := cache.assets[a.URLPath]; dup {
				log.Warnf("plugin %s: asset %q is already listed, the first one is kept", name, a.URLPath)
				return
			}
			cache.assets[a.URLPath] = a
			*list = append(*list, a)
		}
		for _, a := range scripts {
			add(&cache.scripts, a)
		}
		for _, a := range styles {
			add(&cache.styles, a)
		}
	}
	return cache
}

type assetScope int

const (
	assetServed   assetScope = iota // under the plugin's own /plugins/<name>/ prefix
	assetExternal                   // absolute http(s) URL, loaded by the browser
	assetUnserved                   // any other path, linked but never served
	assetForeign                    // under /plugins/ but not the plugin's own prefix
)

func assetPathScope(pluginName, urlPath string) assetScope {
	lower := strings.ToLower(urlPath)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return assetExternal
	}
	if urlpath.Clean(urlPath) == urlPath && strings.HasPrefix(urlPath, "/plugins/"+pluginName+"/") {
		return assetServed
	}
	if strings.HasPrefix(urlpath.Clean(urlPath), "/plugins/") {
		return assetForeign
	}
	return assetUnserved
}

// Get returns an asset by URL path.
func (c *AssetCache) Get(urlPath string) (AssetFile, bool) {
	a, ok := c.assets[urlPath]
	return a, ok
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
		// Preview reloads plugins in place, so the browser must not keep old files.
		w.Header().Set("Cache-Control", "no-cache")

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

// RenderAssetTags generates HTML <script> and <link> tags for plugin assets,
// in the order the plugins returned them.
func RenderAssetTags(cache *AssetCache) string {
	if cache == nil {
		return ""
	}
	var buf strings.Builder
	for _, script := range cache.scripts {
		if script.IsModule {
			fmt.Fprintf(&buf, "<script type=\"module\" src=\"%s\"></script>\n", escapeAttr(script.URLPath))
		} else {
			fmt.Fprintf(&buf, "<script src=\"%s\"></script>\n", escapeAttr(script.URLPath))
		}
	}
	for _, style := range cache.styles {
		if linkStyle(style) {
			fmt.Fprintf(&buf, "<link rel=\"stylesheet\" href=\"%s\">\n", escapeAttr(style.URLPath))
		}
	}
	return buf.String()
}

// linkStyle reports whether a style asset gets a stylesheet link. Only known
// non-CSS types are skipped, so an external CSS URL without a suffix stays
// linked. Skipped styles are still served.
func linkStyle(a AssetFile) bool {
	raw := a.MediaType
	if raw == "" {
		raw = inferMediaType(a.URLPath)
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	if strings.HasPrefix(mediaType, "font/") || strings.HasPrefix(mediaType, "image/") {
		return false
	}
	return a.MediaType == "" || mediaType == "text/css"
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
	case strings.HasSuffix(lower, ".ttf"):
		return "font/ttf"
	case strings.HasSuffix(lower, ".otf"):
		return "font/otf"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
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
