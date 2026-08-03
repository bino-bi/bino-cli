package cli

import (
	"context"
	"fmt"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/serve"
)

func TestBuildCacheKey(t *testing.T) {
	tests := []struct {
		name         string
		artefactName string
		params       map[string]string
		want         string
	}{
		{
			name:         "no params",
			artefactName: "report",
			params:       nil,
			want:         "report",
		},
		{
			name:         "empty params",
			artefactName: "report",
			params:       map[string]string{},
			want:         "report",
		},
		{
			name:         "single param",
			artefactName: "report",
			params:       map[string]string{"foo": "bar"},
			want:         "report?foo=bar",
		},
		{
			name:         "multiple params sorted",
			artefactName: "report",
			params:       map[string]string{"z": "3", "a": "1", "m": "2"},
			want:         "report?a=1?m=2?z=3",
		},
		{
			name:         "params with special chars",
			artefactName: "my-report",
			params:       map[string]string{"date": "2024-01-01", "name": "hello world"},
			want:         "my-report?date=2024-01-01?name=hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCacheKey(tt.artefactName, tt.params)
			if got != tt.want {
				t.Errorf("buildCacheKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildLayoutPagesCacheKey(t *testing.T) {
	tests := []struct {
		name        string
		layoutPages config.LayoutPagesOrRefs
		params      map[string]string
		want        string
	}{
		{
			name:        "single page no params",
			layoutPages: config.LayoutPagesOrRefs{{Page: "page1"}},
			params:      nil,
			want:        "layoutPages:page1",
		},
		{
			name:        "multiple pages sorted",
			layoutPages: config.LayoutPagesOrRefs{{Page: "page3"}, {Page: "page1"}, {Page: "page2"}},
			params:      nil,
			want:        "layoutPages:page1;page2;page3",
		},
		{
			name:        "with params",
			layoutPages: config.LayoutPagesOrRefs{{Page: "page1"}},
			params:      map[string]string{"foo": "bar"},
			want:        "layoutPages:page1?foo=bar",
		},
		{
			name:        "multiple pages with multiple params",
			layoutPages: config.LayoutPagesOrRefs{{Page: "b"}, {Page: "a"}},
			params:      map[string]string{"z": "3", "a": "1"},
			want:        "layoutPages:a;b?a=1&z=3",
		},
		{
			name:        "page ref with params",
			layoutPages: config.LayoutPagesOrRefs{{Page: "sales", Params: map[string]string{"REGION": "EU"}}},
			params:      nil,
			want:        "layoutPages:sales#REGION=EU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLayoutPagesCacheKey(tt.layoutPages, tt.params)
			if got != tt.want {
				t.Errorf("buildLayoutPagesCacheKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServeRenderCache_LRUBound(t *testing.T) {
	cache := newServeRenderCache()

	// Fill past the cap; the oldest entries must be evicted.
	for i := 0; i < maxServeRenderCacheEntries+10; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), &serveRenderEntry{frameHTML: []byte{byte(i)}})
	}
	if got := cache.lru.Len(); got != maxServeRenderCacheEntries {
		t.Fatalf("lru length = %d, want %d", got, maxServeRenderCacheEntries)
	}
	if len(cache.cache) != maxServeRenderCacheEntries {
		t.Fatalf("map length = %d, want %d", len(cache.cache), maxServeRenderCacheEntries)
	}
	for i := 0; i < 10; i++ {
		if _, ok := cache.Get(fmt.Sprintf("key-%d", i)); ok {
			t.Fatalf("key-%d should have been evicted", i)
		}
	}
	if _, ok := cache.Get("key-10"); !ok {
		t.Fatal("key-10 should still be cached")
	}

	// Get must refresh recency: touch the oldest entry, add one more, and
	// verify the touched entry survived while the next-oldest was evicted.
	if _, ok := cache.Get("key-11"); !ok {
		t.Fatal("key-11 should still be cached")
	}
	cache.Set("key-new", &serveRenderEntry{})
	if _, ok := cache.Get("key-11"); !ok {
		t.Fatal("key-11 was evicted despite being recently used")
	}
	if _, ok := cache.Get("key-12"); ok {
		t.Fatal("key-12 should have been evicted as least recently used")
	}

	// Set on an existing key must update in place, not grow the cache.
	cache.Set("key-new", &serveRenderEntry{frameHTML: []byte("updated")})
	if got := cache.lru.Len(); got != maxServeRenderCacheEntries {
		t.Fatalf("lru length after update = %d, want %d", got, maxServeRenderCacheEntries)
	}
	entry, ok := cache.Get("key-new")
	if !ok || string(entry.frameHTML) != "updated" {
		t.Fatalf("key-new not updated in place: ok=%v entry=%v", ok, entry)
	}
}

func TestSetupServeRoutesPWA(t *testing.T) {
	live := config.LiveArtefact{
		Spec: config.LiveReportArtefactSpec{
			Routes: map[string]config.LiveRouteSpec{},
		},
	}

	t.Run("registers manifest and sw routes when PWA set", func(t *testing.T) {
		setup, err := setupServeRoutes(serveRouteConfig{
			LiveArtefact: live,
			PWA: &serve.PWAContent{
				Manifest:      []byte(`{"name":"x"}`),
				ServiceWorker: []byte("// sw"),
			},
		})
		if err != nil {
			t.Fatalf("setupServeRoutes: %v", err)
		}

		manifestFn, ok := setup.RouteMap["/manifest.webmanifest"]
		if !ok {
			t.Fatal("/manifest.webmanifest not registered")
		}
		body, contentType, err := manifestFn(context.Background())
		if err != nil {
			t.Fatalf("manifest content func: %v", err)
		}
		if contentType != "application/manifest+json" {
			t.Errorf("manifest content type = %q, want application/manifest+json", contentType)
		}
		if string(body) != `{"name":"x"}` {
			t.Errorf("manifest body = %q", body)
		}

		swFn, ok := setup.RouteMap["/sw.js"]
		if !ok {
			t.Fatal("/sw.js not registered")
		}
		body, contentType, err = swFn(context.Background())
		if err != nil {
			t.Fatalf("sw content func: %v", err)
		}
		if contentType != "text/javascript; charset=utf-8" {
			t.Errorf("sw content type = %q, want text/javascript; charset=utf-8", contentType)
		}
		if string(body) != "// sw" {
			t.Errorf("sw body = %q", body)
		}
	})

	t.Run("no PWA routes when nil", func(t *testing.T) {
		setup, err := setupServeRoutes(serveRouteConfig{LiveArtefact: live})
		if err != nil {
			t.Fatalf("setupServeRoutes: %v", err)
		}
		if _, ok := setup.RouteMap["/manifest.webmanifest"]; ok {
			t.Error("/manifest.webmanifest registered despite nil PWA")
		}
		if _, ok := setup.RouteMap["/sw.js"]; ok {
			t.Error("/sw.js registered despite nil PWA")
		}
	})
}
