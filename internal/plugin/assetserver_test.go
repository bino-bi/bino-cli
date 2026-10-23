package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetCache_BuildAndGet(t *testing.T) {
	cache := &AssetCache{assets: map[string]AssetFile{
		"/plugins/sf/chart.js":  {URLPath: "/plugins/sf/chart.js", Content: []byte("js"), IsModule: true},
		"/plugins/sf/style.css": {URLPath: "/plugins/sf/style.css", Content: []byte("css")},
	}}

	a, ok := cache.Get("/plugins/sf/chart.js")
	if !ok || string(a.Content) != "js" {
		t.Fatal("expected to find chart.js")
	}

	_, ok = cache.Get("/plugins/sf/missing.js")
	if ok {
		t.Fatal("should not find missing asset")
	}
}

func TestAssetHandler_Found(t *testing.T) {
	cache := &AssetCache{assets: map[string]AssetFile{
		"/plugins/sf/chart.js": {URLPath: "/plugins/sf/chart.js", Content: []byte("console.log('hi')"), MediaType: "application/javascript"},
	}}

	handler := NewAssetHandler(cache)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/plugins/sf/chart.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/javascript" {
		t.Fatalf("wrong content type: %s", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "console.log('hi')" {
		t.Fatal("wrong body")
	}
}

func TestAssetHandler_NotFound(t *testing.T) {
	cache := &AssetCache{assets: map[string]AssetFile{}}
	handler := NewAssetHandler(cache)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/plugins/sf/missing.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRenderAssetTags_Empty(t *testing.T) {
	tags := RenderAssetTags(nil)
	if tags != "" {
		t.Fatal("nil cache should produce empty tags")
	}
}

func TestRenderAssetTags_Mixed(t *testing.T) {
	cache := &AssetCache{
		scripts: []AssetFile{{URLPath: "/plugins/sf/chart.js", IsModule: true}},
		styles:  []AssetFile{{URLPath: "/plugins/sf/style.css"}},
	}

	tags := RenderAssetTags(cache)
	if !strings.Contains(tags, `type="module"`) {
		t.Fatal("expected module script tag")
	}
	if !strings.Contains(tags, `rel="stylesheet"`) {
		t.Fatal("expected stylesheet link tag")
	}
}

func TestInferMediaType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/a.js", "application/javascript"},
		{"/a.mjs", "application/javascript"},
		{"/a.css", "text/css"},
		{"/a.woff2", "font/woff2"},
		{"/a.ttf", "font/ttf"},
		{"/a.png", "image/png"},
		{"/a.svg", "image/svg+xml"},
		{"/a.bin", "application/octet-stream"},
	}
	for _, tt := range tests {
		if got := inferMediaType(tt.path); got != tt.want {
			t.Errorf("inferMediaType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestAssetHandler_FilePathAndContent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "chart.js")
	if err := os.WriteFile(file, []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &AssetCache{assets: map[string]AssetFile{
		"/plugins/p/chart.js":  {URLPath: "/plugins/p/chart.js", FilePath: file},
		"/plugins/p/style.css": {URLPath: "/plugins/p/style.css", Content: []byte("inline")},
	}}
	handler := NewAssetHandler(cache)

	for path, want := range map[string]struct{ body, ctype string }{
		"/plugins/p/chart.js":  {"from file", "application/javascript"},
		"/plugins/p/style.css": {"inline", "text/css"},
	} {
		req := httptest.NewRequestWithContext(context.Background(), "GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != want.body {
			t.Errorf("%s = %d %q, want 200 %q", path, rec.Code, rec.Body.String(), want.body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, want.ctype) {
			t.Errorf("%s Content-Type = %q, want %q", path, ct, want.ctype)
		}
	}
}

func TestBuildAssetCache_PathRules(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&assetsPlugin{newMock("p", withAssets()), []AssetFile{
		{URLPath: "chart.js"},
		{URLPath: "chart.js"},
		{URLPath: "/plugins/p/chart.js"},
		{URLPath: "https://example.com/x.js"},
		{URLPath: "/plugins/other/x.js"},
		{URLPath: "/plugins/p/../other/y.js"},
	}})

	ctx, buf := captureContext()
	cache := BuildAssetCache(ctx, reg, "build")
	logged := buf.String()

	if n := strings.Count(logged, `"chart.js" is not under`); n != 1 {
		t.Errorf("path warning for chart.js logged %d times, want 1; logged: %q", n, logged)
	}
	if n := strings.Count(logged, `"chart.js" is already listed`); n != 1 {
		t.Errorf("duplicate warning for chart.js logged %d times, want 1; logged: %q", n, logged)
	}
	if _, ok := cache.Get("chart.js"); !ok {
		t.Error("chart.js should still be linked")
	}
	for _, quiet := range []string{"/plugins/p/chart.js", "https://example.com/x.js"} {
		if strings.Contains(logged, `"`+quiet+`"`) {
			t.Errorf("unexpected warning for %s; logged: %q", quiet, logged)
		}
		if _, ok := cache.Get(quiet); !ok {
			t.Errorf("%s missing from the cache", quiet)
		}
	}
	for _, foreign := range []string{"/plugins/other/x.js", "/plugins/p/../other/y.js"} {
		if !strings.Contains(logged, `"`+foreign+`"`) {
			t.Errorf("no warning for %s; logged: %q", foreign, logged)
		}
		if _, ok := cache.Get(foreign); ok {
			t.Errorf("%s is outside the plugin's prefix and must be skipped", foreign)
		}
	}
}

func TestBuildRenderOptions_SetsAssetHandler(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&assetsPlugin{newMock("p", withAssets()), []AssetFile{
		{URLPath: "/plugins/p/a.js", Content: []byte("js")},
	}})
	opts := BuildRenderOptions(context.Background(), reg, t.TempDir(), "build")
	if opts.AssetHandler == nil {
		t.Fatal("AssetHandler is nil")
	}
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/plugins/p/a.js", nil)
	rec := httptest.NewRecorder()
	opts.AssetHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "js" {
		t.Errorf("asset = %d %q, want 200 js", rec.Code, rec.Body.String())
	}
}

// assetListPlugin returns fixed script and style lists from GetAssets.
type assetListPlugin struct {
	*mockPlugin
	scripts, styles []AssetFile
}

func (p *assetListPlugin) GetAssets(context.Context, string) ([]AssetFile, []AssetFile, error) {
	return p.scripts, p.styles, nil
}

func TestRenderAssetTags_Order(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&assetListPlugin{mockPlugin: newMock("x", withAssets()), scripts: []AssetFile{
		{URLPath: "/plugins/x/a.js"},
		{URLPath: "/plugins/x/b.js"},
	}})
	// Registered after x on purpose: tags follow load order, not names.
	reg.Register(&assetListPlugin{mockPlugin: newMock("w", withAssets()), scripts: []AssetFile{
		{URLPath: "/plugins/w/c.js"},
	}})

	want := "<script src=\"/plugins/x/a.js\"></script>\n" +
		"<script src=\"/plugins/x/b.js\"></script>\n" +
		"<script src=\"/plugins/w/c.js\"></script>\n"
	for i := range 50 {
		if got := RenderAssetTags(BuildAssetCache(context.Background(), reg, "build")); got != want {
			t.Fatalf("run %d: tags = %q, want %q", i, got, want)
		}
	}
}

func TestRenderAssetTags_Types(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&assetListPlugin{
		mockPlugin: newMock("x", withAssets()),
		scripts: []AssetFile{
			{URLPath: "/plugins/x/hc", MediaType: "application/javascript", Content: []byte("js")},
			{URLPath: "/plugins/x/hc", MediaType: "application/javascript"},
		},
		styles: []AssetFile{
			{URLPath: "/plugins/x/f.woff2", Content: []byte("font")},
			{URLPath: "/plugins/x/logo.png", Content: []byte("png")},
			{URLPath: "/plugins/x/data.json", MediaType: "application/json"},
			{URLPath: "https://fonts.googleapis.com/css2?family=X"},
			{URLPath: "/plugins/x/theme", MediaType: "text/css; charset=utf-8"},
			{URLPath: "/plugins/x/style.css"},
			{URLPath: "/plugins/x/style.css"},
		},
	})
	ctx, _ := captureContext()
	cache := BuildAssetCache(ctx, reg, "build")
	tags := RenderAssetTags(cache)

	want := "<script src=\"/plugins/x/hc\"></script>\n" +
		"<link rel=\"stylesheet\" href=\"https://fonts.googleapis.com/css2?family=X\">\n" +
		"<link rel=\"stylesheet\" href=\"/plugins/x/theme\">\n" +
		"<link rel=\"stylesheet\" href=\"/plugins/x/style.css\">\n"
	if tags != want {
		t.Fatalf("tags = %q, want %q", tags, want)
	}

	// Unlinked styles are still served.
	handler := NewAssetHandler(cache)
	for path, ctype := range map[string]string{"/plugins/x/f.woff2": "font/woff2", "/plugins/x/logo.png": "image/png"} {
		req := httptest.NewRequestWithContext(context.Background(), "GET", path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != ctype {
			t.Errorf("%s = %d %q, want 200 %q", path, rec.Code, rec.Header().Get("Content-Type"), ctype)
		}
	}
}
