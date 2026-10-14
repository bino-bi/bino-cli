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
	cache := &AssetCache{assets: map[string]AssetFile{
		"/plugins/sf/chart.js":  {URLPath: "/plugins/sf/chart.js", IsModule: true},
		"/plugins/sf/style.css": {URLPath: "/plugins/sf/style.css"},
	}}

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

	if n := strings.Count(logged, `"chart.js"`); n != 1 {
		t.Errorf("warning for chart.js logged %d times, want 1; logged: %q", n, logged)
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
