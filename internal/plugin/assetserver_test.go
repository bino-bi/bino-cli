package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
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
