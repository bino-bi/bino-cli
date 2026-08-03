package serve

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
)

// PWA serving emits only RELATIVE URLs into the manifest JSON, the service
// worker, and the injected head tags. bino cloud hosts live artefacts behind
// a reverse proxy at /l/<slug>/ and injects <base href="/l/<slug>/"> into
// HTML, but does NOT rewrite manifest JSON or JS bodies — relative URLs
// resolve correctly both locally and behind the proxy. Behind the proxy the
// injected <base> makes head-tag URLs resolve on every route; served locally
// (no <base>) they resolve against the page's directory, so on a
// multi-segment route like /reports/q1 the manifest link and service worker
// registration 404. Depth-aware ../ prefixes are not an option: they would
// escape the <base> prefix behind the proxy.

// PWAContent holds the generated PWA payloads for a LiveReportArtefact and
// the icon files that must be exposed through the HTTP server.
type PWAContent struct {
	Manifest      []byte
	ServiceWorker []byte
	LocalAssets   []render.LocalAsset
}

// BuildPWAContent resolves the spec.pwa icons against the project's Asset
// documents and generates the web app manifest and service worker.
// It returns nil when the artefact has no pwa block.
func BuildPWAContent(liveArtefact config.LiveArtefact, docs []config.Document, engineVersion string) (*PWAContent, error) {
	pwa := liveArtefact.Spec.PWA
	if pwa == nil {
		return nil, nil
	}
	icons, locals, err := resolvePWAIcons(liveArtefact.Document.Name, pwa.Icons, docs)
	if err != nil {
		return nil, err
	}
	manifest, err := buildPWAManifest(liveArtefact.Spec.PWA, liveArtefact.Spec.Routes, icons)
	if err != nil {
		return nil, err
	}
	return &PWAContent{
		Manifest:      manifest,
		ServiceWorker: buildServiceWorker(engineVersion),
		LocalAssets:   locals,
	}, nil
}

// pwaIconRef ties a spec.pwa icon to its resolved serving URL and media type.
type pwaIconRef struct {
	src       string
	sizes     string
	mediaType string
	purpose   string
}

// resolvePWAIcons resolves each pwa icon to its relative serving URL.
// Validation restricts icons to localPath-sourced image assets, so each
// resolves to assets/files/<name> (the leading "/" is stripped); a non-local
// source is rejected here too so the invariant does not depend on
// ValidateLiveArtefact having run first. Icon files are stat'ed during
// resolution, so a missing file fails at serve startup instead of 404ing at
// request time.
func resolvePWAIcons(artefactName string, icons []config.PWAIcon, docs []config.Document) ([]pwaIconRef, []render.LocalAsset, error) {
	refs := make([]pwaIconRef, 0, len(icons))
	var locals []render.LocalAsset
	for i, icon := range icons {
		value, mediaType, local, err := render.ResolveNamedAsset(docs, icon.Asset)
		if err != nil {
			return nil, nil, fmt.Errorf("LiveReportArtefact %q: pwa icon[%d]: %w", artefactName, i, err)
		}
		if local == nil {
			return nil, nil, fmt.Errorf("LiveReportArtefact %q: pwa icon[%d] Asset %q must use a source.localPath (PWA icons are served as local files)", artefactName, i, icon.Asset)
		}
		refs = append(refs, pwaIconRef{
			src:       strings.TrimPrefix(value, "/"),
			sizes:     icon.Sizes,
			mediaType: mediaType,
			purpose:   icon.Purpose,
		})
		locals = append(locals, *local)
	}
	return refs, locals, nil
}

// webManifest mirrors the manifest.webmanifest JSON shape.
type webManifest struct {
	Name            string             `json:"name"`
	ShortName       string             `json:"short_name"`
	Description     string             `json:"description,omitempty"`
	StartURL        string             `json:"start_url"`
	Scope           string             `json:"scope"`
	ID              string             `json:"id"`
	Display         string             `json:"display"`
	ThemeColor      string             `json:"theme_color,omitempty"`
	BackgroundColor string             `json:"background_color,omitempty"`
	Icons           []manifestIcon     `json:"icons"`
	Shortcuts       []manifestShortcut `json:"shortcuts,omitempty"`
}

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}

type manifestShortcut struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// buildPWAManifest generates the manifest.webmanifest JSON. start_url, scope
// and id are relative so the manifest works both when served at "/" and
// behind a reverse-proxy path prefix.
func buildPWAManifest(pwa *config.PWASpec, routes map[string]config.LiveRouteSpec, icons []pwaIconRef) ([]byte, error) {
	m := webManifest{
		Name:            pwa.Name,
		ShortName:       pwa.ShortName,
		Description:     pwa.Description,
		StartURL:        "./",
		Scope:           "./",
		ID:              "./",
		Display:         pwa.Display,
		ThemeColor:      pwa.ThemeColor,
		BackgroundColor: pwa.BackgroundColor,
		Icons:           make([]manifestIcon, 0, len(icons)),
	}
	for _, icon := range icons {
		m.Icons = append(m.Icons, manifestIcon{
			Src:     icon.src,
			Sizes:   icon.sizes,
			Type:    icon.mediaType,
			Purpose: icon.purpose,
		})
	}
	m.Shortcuts = buildManifestShortcuts(routes)
	return json.Marshal(m)
}

// buildManifestShortcuts turns the artefact's titled non-root routes into
// manifest shortcuts (the long-press/right-click jump list of the installed
// app). URLs are relative to the manifest so they stay inside a reverse-proxy
// path prefix, and the slice is sorted by route path because map iteration
// order would otherwise leak into the manifest bytes.
func buildManifestShortcuts(routes map[string]config.LiveRouteSpec) []manifestShortcut {
	paths := make([]string, 0, len(routes))
	for path, route := range routes {
		if path == "/" || route.Title == "" {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	shortcuts := make([]manifestShortcut, 0, len(paths))
	for _, path := range paths {
		shortcuts = append(shortcuts, manifestShortcut{
			Name: routes[path].Title,
			URL:  "./" + strings.TrimPrefix(path, "/"),
		})
	}
	return shortcuts
}

// serviceWorkerTemplate is the service worker source. The __BINO_ENGINE_VERSION__
// placeholder is replaced at generation time. It must not contain absolute
// paths: everything is computed relative to self.registration.scope so the
// worker behaves identically behind a reverse-proxy path prefix.
const serviceWorkerTemplate = `// bino serve service worker. Generated at startup; the cache name embeds the
// engine version so engine upgrades roll the cache.
const CACHE_NAME = 'bino-serve-__BINO_ENGINE_VERSION__';

const OFFLINE_HTML = '<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">' +
  '<meta name="viewport" content="width=device-width, initial-scale=1"><title>Offline</title>' +
  '<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;' +
  'justify-content:center;min-height:100vh;margin:0;color:#333}' +
  'main{text-align:center;padding:2rem}h1{font-size:1.5rem}</style></head>' +
  '<body><main><h1>You are offline</h1>' +
  '<p>This report needs a connection. Please reconnect and try again.</p></main></body></html>';

self.addEventListener('install', () => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const names = await caches.keys();
    await Promise.all(names.filter((name) => name !== CACHE_NAME).map((name) => caches.delete(name)));
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (request.method !== 'GET') {
    return;
  }
  // Documents are credentialed and Cache-Control: no-store — never cache
  // them. Offline navigations get a friendly fallback page instead.
  if (request.mode === 'navigate') {
    event.respondWith(fetch(request).catch(() => new Response(OFFLINE_HTML, {
      status: 503,
      headers: { 'Content-Type': 'text/html; charset=utf-8' },
    })));
    return;
  }
  const scope = self.registration.scope;
  if (!request.url.startsWith(scope)) {
    return;
  }
  const path = request.url.slice(scope.length);
  // Report data is never cached: it would persist tenant data in durable
  // Cache Storage and grow without bound as content hashes change.
  if (path.startsWith('__bino/data/')) {
    return;
  }
  if (path.startsWith('cdn/') || path.startsWith('__bino/')) {
    event.respondWith((async () => {
      const cache = await caches.open(CACHE_NAME);
      const cached = await cache.match(request);
      if (cached) {
        return cached;
      }
      const response = await fetch(request);
      if (response.ok) {
        cache.put(request, response.clone());
      }
      return response;
    })());
  }
});
`

// buildServiceWorker generates the service worker source for the given engine
// version.
func buildServiceWorker(engineVersion string) []byte {
	return []byte(strings.ReplaceAll(serviceWorkerTemplate, "__BINO_ENGINE_VERSION__", engineVersion))
}

// buildPWAHeadTags renders the head tags that make the served page an
// installable PWA: manifest link, optional theme-color, apple-touch-icon for
// the largest purpose "any" icon, and the service worker registration.
func buildPWAHeadTags(pwa *config.PWASpec) string {
	var b strings.Builder
	b.WriteString("\n" + `<link rel="manifest" href="manifest.webmanifest">`)
	if pwa.ThemeColor != "" {
		b.WriteString("\n" + `<meta name="theme-color" content="` + html.EscapeString(pwa.ThemeColor) + `">`)
	}
	if icon := largestAnyIcon(pwa.Icons); icon != nil {
		b.WriteString("\n" + `<link rel="apple-touch-icon" href="assets/files/` + url.PathEscape(icon.Asset) + `">`)
	}
	b.WriteString("\n" + `<script>if ('serviceWorker' in navigator) { navigator.serviceWorker.register('sw.js'); }</script>` + "\n")
	return b.String()
}

// largestAnyIcon returns the purpose "any" icon with the largest area, or nil
// when there is none. An empty purpose counts as "any" (the config default).
func largestAnyIcon(icons []config.PWAIcon) *config.PWAIcon {
	var best *config.PWAIcon
	bestArea := -1
	for i := range icons {
		icon := &icons[i]
		if icon.Purpose != "" && icon.Purpose != "any" {
			continue
		}
		if area := iconArea(icon.Sizes); area > bestArea {
			bestArea = area
			best = icon
		}
	}
	return best
}

// iconArea parses a WIDTHxHEIGHT sizes value into its area; malformed values
// count as zero.
func iconArea(sizes string) int {
	width, height, ok := strings.Cut(sizes, "x")
	if !ok {
		return 0
	}
	w, err := strconv.Atoi(width)
	if err != nil {
		return 0
	}
	h, err := strconv.Atoi(height)
	if err != nil {
		return 0
	}
	return w * h
}
