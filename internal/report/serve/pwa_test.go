package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// localIconAssetDoc writes an icon file into a temp dir and returns an Asset
// document whose source.localPath points at it.
func localIconAssetDoc(t *testing.T, name string) config.Document {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "icon.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	return config.Document{
		Kind: "Asset",
		Name: name,
		File: filepath.Join(dir, "asset.yaml"),
		Raw:  json.RawMessage(`{"spec":{"type":"image","mediaType":"image/png","source":{"localPath":"icon.png"}}}`),
	}
}

func pwaLiveArtefact() config.LiveArtefact {
	return config.LiveArtefact{
		Document: config.Document{Name: "dash"},
		Spec: config.LiveReportArtefactSpec{
			Title:       "Sales Dashboard",
			Description: "Quarterly sales",
			Routes: map[string]config.LiveRouteSpec{
				"/":      {Title: "Home", Artifact: "home"},
				"/sales": {Title: "Sales", Artifact: "sales"},
			},
			PWA: &config.PWASpec{
				Name:            "Sales Dashboard",
				ShortName:       "Sales",
				Description:     "Quarterly sales",
				ThemeColor:      "#0B5FFF",
				BackgroundColor: "#FFFFFF",
				Display:         "standalone",
				Icons: []config.PWAIcon{
					{Asset: "app-icon", Sizes: "512x512", Purpose: "any"},
				},
			},
		},
	}
}

func TestBuildPWAContentNilWithoutPWA(t *testing.T) {
	live := pwaLiveArtefact()
	live.Spec.PWA = nil
	content, err := BuildPWAContent(live, nil, "1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != nil {
		t.Fatalf("content = %+v, want nil when spec.pwa is unset", content)
	}
}

func TestBuildPWAContentManifest(t *testing.T) {
	live := pwaLiveArtefact()
	docs := []config.Document{localIconAssetDoc(t, "app-icon")}

	content, err := BuildPWAContent(live, docs, "1.2.3")
	if err != nil {
		t.Fatalf("BuildPWAContent: %v", err)
	}

	var manifest struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		Description     string `json:"description"`
		StartURL        string `json:"start_url"`
		Scope           string `json:"scope"`
		ID              string `json:"id"`
		Display         string `json:"display"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		Icons           []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Type    string `json:"type"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
		Shortcuts []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"shortcuts"`
	}
	if err := json.Unmarshal(content.Manifest, &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v\n%s", err, content.Manifest)
	}

	if manifest.StartURL != "./" || manifest.Scope != "./" || manifest.ID != "./" {
		t.Errorf("start_url/scope/id = %q/%q/%q, want ./ for all", manifest.StartURL, manifest.Scope, manifest.ID)
	}
	if manifest.Name != "Sales Dashboard" || manifest.ShortName != "Sales" {
		t.Errorf("name/short_name = %q/%q", manifest.Name, manifest.ShortName)
	}
	if manifest.Display != "standalone" || manifest.ThemeColor != "#0B5FFF" || manifest.BackgroundColor != "#FFFFFF" {
		t.Errorf("display/theme_color/background_color = %q/%q/%q", manifest.Display, manifest.ThemeColor, manifest.BackgroundColor)
	}

	if len(manifest.Icons) != 1 {
		t.Fatalf("got %d icons, want 1", len(manifest.Icons))
	}
	icon := manifest.Icons[0]
	if icon.Src != "assets/files/app-icon" {
		t.Errorf("icon src = %q, want assets/files/app-icon", icon.Src)
	}
	if icon.Type != "image/png" || icon.Sizes != "512x512" || icon.Purpose != "any" {
		t.Errorf("icon type/sizes/purpose = %q/%q/%q", icon.Type, icon.Sizes, icon.Purpose)
	}

	// Titled non-root routes become app shortcuts; the root never does.
	if len(manifest.Shortcuts) != 1 {
		t.Fatalf("got %d shortcuts, want 1", len(manifest.Shortcuts))
	}
	if manifest.Shortcuts[0].Name != "Sales" || manifest.Shortcuts[0].URL != "./sales" {
		t.Errorf("shortcut = %+v, want {Sales ./sales}", manifest.Shortcuts[0])
	}

	// No emitted URL may be absolute.
	for _, ic := range manifest.Icons {
		if strings.HasPrefix(ic.Src, "/") {
			t.Errorf("icon src %q has a leading /", ic.Src)
		}
	}
	for _, sc := range manifest.Shortcuts {
		if strings.HasPrefix(sc.URL, "/") {
			t.Errorf("shortcut url %q has a leading /", sc.URL)
		}
	}

	// The icon file must be registered for local serving.
	if len(content.LocalAssets) != 1 {
		t.Fatalf("got %d local assets, want 1", len(content.LocalAssets))
	}
	if content.LocalAssets[0].URLPath != "/assets/files/app-icon" {
		t.Errorf("local asset URLPath = %q, want /assets/files/app-icon", content.LocalAssets[0].URLPath)
	}
}

func TestBuildPWAManifestOmitsEmptyOptionals(t *testing.T) {
	live := pwaLiveArtefact()
	live.Spec.PWA.Description = ""
	live.Spec.PWA.ThemeColor = ""
	live.Spec.PWA.BackgroundColor = ""
	docs := []config.Document{localIconAssetDoc(t, "app-icon")}

	content, err := BuildPWAContent(live, docs, "1.2.3")
	if err != nil {
		t.Fatalf("BuildPWAContent: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(content.Manifest, &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	for _, key := range []string{"description", "theme_color", "background_color"} {
		if _, ok := manifest[key]; ok {
			t.Errorf("manifest contains %q, want key omitted when empty", key)
		}
	}
}

func TestBuildManifestShortcuts(t *testing.T) {
	routes := map[string]config.LiveRouteSpec{
		"/":         {Title: "Home"}, // root: excluded even when titled
		"/untitled": {},              // no title: nothing to name the shortcut
		"/b-page":   {Title: "B Page"},
		"/a-page":   {Title: "A Page"},
	}
	shortcuts := buildManifestShortcuts(routes)
	if len(shortcuts) != 2 {
		t.Fatalf("got %d shortcuts, want 2", len(shortcuts))
	}
	// Sorted by route path, not map order.
	if shortcuts[0].Name != "A Page" || shortcuts[0].URL != "./a-page" {
		t.Errorf("shortcuts[0] = %+v, want {A Page ./a-page}", shortcuts[0])
	}
	if shortcuts[1].Name != "B Page" || shortcuts[1].URL != "./b-page" {
		t.Errorf("shortcuts[1] = %+v, want {B Page ./b-page}", shortcuts[1])
	}
}

func TestBuildPWAContentMissingIconFile(t *testing.T) {
	live := pwaLiveArtefact()
	dir := t.TempDir()
	docs := []config.Document{{
		Kind: "Asset",
		Name: "app-icon",
		File: filepath.Join(dir, "asset.yaml"),
		Raw:  json.RawMessage(`{"spec":{"type":"image","mediaType":"image/png","source":{"localPath":"does-not-exist.png"}}}`),
	}}

	if _, err := BuildPWAContent(live, docs, "1.2.3"); err == nil {
		t.Fatal("expected error for missing icon file, got nil")
	}
}

func TestBuildPWAContentNonLocalIconAsset(t *testing.T) {
	// ValidateLiveArtefact restricts pwa icons to localPath-sourced assets;
	// BuildPWAContent must reject non-local sources on its own so the
	// invariant does not depend on validation having run first.
	live := pwaLiveArtefact()
	docs := []config.Document{{
		Kind: "Asset",
		Name: "app-icon",
		Raw:  json.RawMessage(`{"spec":{"type":"image","mediaType":"image/png","source":{"remoteURL":"https://example.com/icon.png"}}}`),
	}}

	_, err := BuildPWAContent(live, docs, "1.2.3")
	if err == nil {
		t.Fatal("expected error for non-local icon asset, got nil")
	}
	if !strings.Contains(err.Error(), "localPath") {
		t.Errorf("error %q does not name source.localPath", err)
	}
}

func TestBuildPWAContentUnknownIconAsset(t *testing.T) {
	live := pwaLiveArtefact()
	_, err := BuildPWAContent(live, nil, "1.2.3")
	if err == nil {
		t.Fatal("expected error for unknown icon asset, got nil")
	}
	if !strings.Contains(err.Error(), "app-icon") {
		t.Errorf("error %q does not name the missing asset", err)
	}
}

func TestBuildServiceWorker(t *testing.T) {
	sw := string(buildServiceWorker("1.0.0-alpha.19"))

	if !strings.Contains(sw, "bino-serve-1.0.0-alpha.19") {
		t.Errorf("cache name does not embed the engine version:\n%s", sw)
	}
	// The worker must not contain absolute-path literals; everything is
	// computed relative to self.registration.scope.
	for _, forbidden := range []string{`"/assets`, `'/assets`, `"/cdn`, `'/cdn`, `"/__bino`, `'/__bino`} {
		if strings.Contains(sw, forbidden) {
			t.Errorf("service worker contains absolute-path literal %s", forbidden)
		}
	}
	if !strings.Contains(sw, "self.registration.scope") {
		t.Errorf("service worker does not compute paths from self.registration.scope")
	}
	// Report data must be excluded from the cacheable set.
	if !strings.Contains(sw, "path.startsWith('__bino/data/')") {
		t.Errorf("service worker does not exclude __bino/data/ from caching:\n%s", sw)
	}
}

func TestInjectScriptPWATags(t *testing.T) {
	frame := []byte(`<html><head><title>x</title></head><body></body></html>`)
	routeSpec := config.LiveRouteSpec{}

	t.Run("tags present when PWA set", func(t *testing.T) {
		got := string(injectScript(frame, pwaLiveArtefact(), "/", routeSpec, "", "", nil, nil))
		for _, want := range []string{
			`<link rel="manifest" href="manifest.webmanifest">`,
			`<meta name="theme-color" content="#0B5FFF">`,
			`<link rel="apple-touch-icon" href="assets/files/app-icon">`,
			`navigator.serviceWorker.register('sw.js')`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %s in:\n%s", want, got)
			}
		}
		// Tags must land inside <head>.
		headClose := strings.Index(got, "</head>")
		if idx := strings.Index(got, "manifest.webmanifest"); idx == -1 || idx > headClose {
			t.Errorf("manifest link not injected before </head>")
		}
	})

	t.Run("absent when PWA nil", func(t *testing.T) {
		live := pwaLiveArtefact()
		live.Spec.PWA = nil
		got := string(injectScript(frame, live, "/", routeSpec, "", "", nil, nil))
		for _, forbidden := range []string{"manifest.webmanifest", "theme-color", "apple-touch-icon", "serviceWorker"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("unexpected %s in output without spec.pwa", forbidden)
			}
		}
	})

	t.Run("theme-color omitted when empty", func(t *testing.T) {
		live := pwaLiveArtefact()
		live.Spec.PWA.ThemeColor = ""
		got := string(injectScript(frame, live, "/", routeSpec, "", "", nil, nil))
		if strings.Contains(got, "theme-color") {
			t.Errorf("theme-color emitted despite empty ThemeColor:\n%s", got)
		}
	})

	t.Run("apple-touch-icon picks largest any icon", func(t *testing.T) {
		live := pwaLiveArtefact()
		live.Spec.PWA.Icons = []config.PWAIcon{
			{Asset: "small", Sizes: "192x192", Purpose: "any"},
			{Asset: "masked", Sizes: "1024x1024", Purpose: "maskable"},
			{Asset: "large", Sizes: "512x512", Purpose: "any"},
		}
		got := string(injectScript(frame, live, "/", routeSpec, "", "", nil, nil))
		if !strings.Contains(got, `<link rel="apple-touch-icon" href="assets/files/large">`) {
			t.Errorf("apple-touch-icon did not pick the largest any icon:\n%s", got)
		}
	})

	t.Run("no apple-touch-icon without any-purpose icon", func(t *testing.T) {
		live := pwaLiveArtefact()
		live.Spec.PWA.Icons = []config.PWAIcon{
			{Asset: "masked", Sizes: "512x512", Purpose: "maskable"},
		}
		got := string(injectScript(frame, live, "/", routeSpec, "", "", nil, nil))
		if strings.Contains(got, "apple-touch-icon") {
			t.Errorf("apple-touch-icon emitted without a purpose any icon:\n%s", got)
		}
	})
}

func TestBuildMissingParamsHTMLIncludesPWATags(t *testing.T) {
	live := pwaLiveArtefact()
	routeSpec := config.LiveRouteSpec{
		QueryParams: []config.LiveQueryParamSpec{{Name: "region"}},
	}

	got := string(BuildMissingParamsHTML(live, "/", routeSpec, "", []string{"region"}, nil))
	if !strings.Contains(got, `<link rel="manifest" href="manifest.webmanifest">`) {
		t.Errorf("missing-params page lacks manifest link:\n%s", got)
	}
	if !strings.Contains(got, `navigator.serviceWorker.register('sw.js')`) {
		t.Errorf("missing-params page lacks service worker registration:\n%s", got)
	}
}
