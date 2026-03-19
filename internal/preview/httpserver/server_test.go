package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeCDNPath(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name:    "simple path",
			raw:     "/cdn/assets/file.js",
			want:    "assets/file.js",
			wantErr: false,
		},
		{
			name:    "nested path",
			raw:     "/cdn/a/b/c/file.css",
			want:    "a/b/c/file.css",
			wantErr: false,
		},
		{
			name:    "path with extra leading slashes",
			raw:     "/cdn///file.js",
			want:    "file.js",
			wantErr: false,
		},
		{
			name:    "empty path after prefix",
			raw:     "/cdn/",
			want:    "",
			wantErr: true,
		},
		{
			name:    "only cdn prefix without slash",
			raw:     "/cdn",
			want:    "cdn", // Results in "cdn" which is a valid path segment
			wantErr: false,
		},
		{
			name:    "path traversal with double dots",
			raw:     "/cdn/../etc/passwd",
			want:    "",
			wantErr: true,
		},
		{
			name:    "path traversal in middle",
			raw:     "/cdn/assets/../../../etc/passwd",
			want:    "",
			wantErr: true,
		},
		{
			name:    "encoded path traversal",
			raw:     "/cdn/..%2F..%2Fetc/passwd",
			want:    "",
			wantErr: true, // Contains ".." literal
		},
		{
			name:    "path becomes dot after clean",
			raw:     "/cdn/./",
			want:    "",
			wantErr: true,
		},
		{
			name:    "double dot at start after clean",
			raw:     "/cdn/../file",
			want:    "",
			wantErr: true,
		},
		{
			name:    "valid path with dots in filename",
			raw:     "/cdn/file.min.js",
			want:    "file.min.js",
			wantErr: false,
		},
		{
			name:    "path with version numbers",
			raw:     "/cdn/v1.2.3/bundle.js",
			want:    "v1.2.3/bundle.js",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeCDNPath(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeCDNPath(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("sanitizeCDNPath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCacheBypassed(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "no query params",
			url:  "/cdn/file.js",
			want: false,
		},
		{
			name: "cache=0",
			url:  "/cdn/file.js?cache=0",
			want: true,
		},
		{
			name: "cache=1",
			url:  "/cdn/file.js?cache=1",
			want: false,
		},
		{
			name: "typo chache=0 also works",
			url:  "/cdn/file.js?chache=0",
			want: true,
		},
		{
			name: "both params set",
			url:  "/cdn/file.js?cache=0&chache=0",
			want: true,
		},
		{
			name: "other params only",
			url:  "/cdn/file.js?v=123",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tt.url, nil)
			got := cacheBypassed(req)
			if got != tt.want {
				t.Errorf("cacheBypassed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadUpstreamBody(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		limit   int64
		want    string
		wantErr bool
	}{
		{
			name:    "body within limit",
			body:    "hello world",
			limit:   100,
			want:    "hello world",
			wantErr: false,
		},
		{
			name:    "body exactly at limit",
			body:    "12345",
			limit:   5,
			want:    "12345",
			wantErr: false,
		},
		{
			name:    "body exceeds limit",
			body:    "123456",
			limit:   5,
			want:    "",
			wantErr: true,
		},
		{
			name:    "no limit",
			body:    "any content",
			limit:   0,
			want:    "any content",
			wantErr: false,
		},
		{
			name:    "negative limit treated as no limit",
			body:    "any content",
			limit:   -1,
			want:    "any content",
			wantErr: false,
		},
		{
			name:    "empty body",
			body:    "",
			limit:   100,
			want:    "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.body)
			got, err := readUpstreamBody(reader, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Errorf("readUpstreamBody() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("readUpstreamBody() = %q, want %q", string(got), tt.want)
			}
			if tt.wantErr && !errors.Is(err, errBodyTooLarge) {
				t.Errorf("readUpstreamBody() expected errBodyTooLarge, got %v", err)
			}
		})
	}
}

func TestCopyHeaders(t *testing.T) {
	tests := []struct {
		name   string
		src    http.Header
		keys   []string
		expect map[string][]string
	}{
		{
			name: "copy specific headers",
			src: http.Header{
				"Content-Type":   {"text/plain"},
				"Cache-Control":  {"max-age=3600"},
				"X-Custom":       {"value"},
				"Content-Length": {"100"},
			},
			keys: []string{"Content-Type", "Cache-Control"},
			expect: map[string][]string{
				"Content-Type":  {"text/plain"},
				"Cache-Control": {"max-age=3600"},
			},
		},
		{
			name: "missing key is ignored",
			src: http.Header{
				"Content-Type": {"text/plain"},
			},
			keys: []string{"Content-Type", "Missing-Header"},
			expect: map[string][]string{
				"Content-Type": {"text/plain"},
			},
		},
		{
			name:   "empty source",
			src:    http.Header{},
			keys:   []string{"Content-Type"},
			expect: map[string][]string{},
		},
		{
			name: "multiple values for header",
			src: http.Header{
				"Accept": {"text/html", "application/json"},
			},
			keys: []string{"Accept"},
			expect: map[string][]string{
				"Accept": {"text/html", "application/json"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := http.Header{}
			copyHeaders(dst, tt.src, tt.keys...)
			for key, wantVals := range tt.expect {
				gotVals := dst[key]
				if len(gotVals) != len(wantVals) {
					t.Errorf("header %q: got %d values, want %d", key, len(gotVals), len(wantVals))
					continue
				}
				for i, v := range wantVals {
					if gotVals[i] != v {
						t.Errorf("header %q[%d]: got %q, want %q", key, i, gotVals[i], v)
					}
				}
			}
			// Ensure no extra headers were copied
			for key := range dst {
				if _, ok := tt.expect[key]; !ok {
					t.Errorf("unexpected header %q in destination", key)
				}
			}
		})
	}
}

func TestFormatSSE(t *testing.T) {
	tests := []struct {
		name  string
		event string
		data  []byte
		want  string
	}{
		{
			name:  "event with data",
			event: "message",
			data:  []byte(`{"key":"value"}`),
			want:  "event: message\ndata: {\"key\":\"value\"}\n\n",
		},
		{
			name:  "event with empty data",
			event: "ping",
			data:  nil,
			want:  "event: ping\ndata:\n\n",
		},
		{
			name:  "empty event with data",
			event: "",
			data:  []byte("hello"),
			want:  "data: hello\n\n",
		},
		{
			name:  "multiline data",
			event: "update",
			data:  []byte("line1\nline2\nline3"),
			want:  "event: update\ndata: line1\ndata: line2\ndata: line3\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSSE(tt.event, tt.data)
			if string(got) != tt.want {
				t.Errorf("FormatSSE(%q, %q) = %q, want %q", tt.event, tt.data, string(got), tt.want)
			}
		})
	}
}

func TestServerNew(t *testing.T) {
	t.Run("default configuration", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if srv == nil {
			t.Fatal("New() returned nil server")
		}
		if srv.URL() == "" {
			t.Error("server URL is empty")
		}
		if !strings.HasPrefix(srv.URL(), "http://127.0.0.1:") {
			t.Errorf("unexpected server URL: %s", srv.URL())
		}
	})

	t.Run("custom listen address", func(t *testing.T) {
		srv, err := New(Config{ListenAddr: "127.0.0.1:0"})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if srv.URL() == "" {
			t.Error("server URL is empty")
		}
	})

	t.Run("CDN base URL without trailing slash", func(t *testing.T) {
		srv, err := New(Config{CDNBaseURL: "https://example.com"})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if !strings.HasSuffix(srv.cfg.CDNBaseURL, "/") {
			t.Error("CDN base URL should have trailing slash")
		}
	})
}

func TestHandleRoot(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		contentFn  ContentFunc
		routes     map[string]ContentFunc
		wantStatus int
		wantBody   string
		wantType   string
	}{
		{
			name:       "default content function",
			path:       "/",
			contentFn:  StaticContent([]byte("hello"), "text/plain"),
			routes:     nil,
			wantStatus: http.StatusOK,
			wantBody:   "hello",
			wantType:   "text/plain",
		},
		{
			name:       "route match",
			path:       "/page",
			contentFn:  StaticContent([]byte("default"), "text/plain"),
			routes:     map[string]ContentFunc{"/page": StaticContent([]byte("page content"), "text/html")},
			wantStatus: http.StatusOK,
			wantBody:   "page content",
			wantType:   "text/html",
		},
		{
			name:       "route not found with routes defined",
			path:       "/missing",
			contentFn:  StaticContent([]byte("default"), "text/plain"),
			routes:     map[string]ContentFunc{"/page": StaticContent([]byte("page"), "text/html")},
			wantStatus: http.StatusNotFound,
			wantBody:   "",
			wantType:   "",
		},
		{
			name:       "root path with routes defined",
			path:       "/",
			contentFn:  StaticContent([]byte("root"), "text/plain"),
			routes:     map[string]ContentFunc{"/page": StaticContent([]byte("page"), "text/html")},
			wantStatus: http.StatusOK,
			wantBody:   "root",
			wantType:   "text/plain",
		},
		{
			name: "content function error",
			path: "/",
			contentFn: func(context.Context) ([]byte, string, error) {
				return nil, "", errors.New("render failed")
			},
			routes:     nil,
			wantStatus: http.StatusInternalServerError,
			wantBody:   "",
			wantType:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := New(Config{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			srv.SetContentFunc(tt.contentFn)
			if tt.routes != nil {
				srv.SetContentRoutes(tt.routes)
			}

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			srv.handleRoot(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantBody != "" {
				body, _ := io.ReadAll(resp.Body)
				if string(body) != tt.wantBody {
					t.Errorf("body = %q, want %q", string(body), tt.wantBody)
				}
			}
			if tt.wantType != "" && resp.Header.Get("Content-Type") != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", resp.Header.Get("Content-Type"), tt.wantType)
			}
		})
	}
}

func TestHandleAsset(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("asset content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name       string
		urlPath    string
		assets     []LocalAsset
		wantStatus int
		wantBody   string
	}{
		{
			name:    "asset found",
			urlPath: "/assets/test.txt",
			assets: []LocalAsset{
				{URLPath: "/assets/test.txt", FilePath: testFile, MediaType: "text/plain"},
			},
			wantStatus: http.StatusOK,
			wantBody:   "asset content",
		},
		{
			name:       "asset not found",
			urlPath:    "/assets/missing.txt",
			assets:     []LocalAsset{},
			wantStatus: http.StatusNotFound,
			wantBody:   "",
		},
		{
			name:    "asset file missing from disk",
			urlPath: "/assets/gone.txt",
			assets: []LocalAsset{
				{URLPath: "/assets/gone.txt", FilePath: "/nonexistent/file.txt", MediaType: "text/plain"},
			},
			wantStatus: http.StatusNotFound,
			wantBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := New(Config{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			srv.SetLocalAssets(tt.assets)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tt.urlPath, nil)
			w := httptest.NewRecorder()
			srv.handleAsset(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantBody != "" {
				body, _ := io.ReadAll(resp.Body)
				if string(body) != tt.wantBody {
					t.Errorf("body = %q, want %q", string(body), tt.wantBody)
				}
			}
		})
	}
}

func TestHandleCDN_CacheHit(t *testing.T) {
	cacheDir := t.TempDir()
	cachedFile := filepath.Join(cacheDir, "cached", "file.js")
	if err := os.MkdirAll(filepath.Dir(cachedFile), 0o755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	if err := os.WriteFile(cachedFile, []byte("cached content"), 0o644); err != nil {
		t.Fatalf("failed to create cached file: %v", err)
	}

	srv, err := New(Config{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/cached/file.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "cached content" {
		t.Errorf("body = %q, want %q", string(body), "cached content")
	}
}

func TestHandleCDN_CacheMiss(t *testing.T) {
	cacheDir := t.TempDir()

	// Create a mock CDN server
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cdn content"))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CacheDir:   cacheDir,
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/new/file.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "cdn content" {
		t.Errorf("body = %q, want %q", string(body), "cdn content")
	}

	// Verify file was cached
	cachedPath := filepath.Join(cacheDir, "new", "file.js")
	cachedContent, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Errorf("cache file not written: %v", err)
	} else if string(cachedContent) != "cdn content" {
		t.Errorf("cached content = %q, want %q", string(cachedContent), "cdn content")
	}
}

func TestHandleCDN_CacheBypass(t *testing.T) {
	cacheDir := t.TempDir()

	// Pre-populate cache with stale content
	cachedFile := filepath.Join(cacheDir, "file.js")
	if err := os.WriteFile(cachedFile, []byte("stale cached"), 0o644); err != nil {
		t.Fatalf("failed to create cached file: %v", err)
	}

	// Mock CDN returns fresh content
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fresh from cdn"))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CacheDir:   cacheDir,
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/file.js?cache=0", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "fresh from cdn" {
		t.Errorf("body = %q, want %q (should bypass cache)", string(body), "fresh from cdn")
	}
}

func TestHandleCDN_InvalidPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "path traversal attempt",
			path:       "/cdn/../../../etc/passwd",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty path",
			path:       "/cdn/",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "double dot in path",
			path:       "/cdn/foo/../bar/../../secret",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := New(Config{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			srv.handleCDN(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestHandleCDN_UpstreamError(t *testing.T) {
	// Mock CDN that returns errors
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("cdn unavailable"))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/file.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should forward upstream status
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleCDN_BodyTooLarge(t *testing.T) {
	// Mock CDN that returns large body
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write more than the limit
		_, _ = w.Write(make([]byte, 1000))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	srv.maxCDNBytes = 100 // Set small limit

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/large.bin", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

func TestHandleEvents(t *testing.T) {
	t.Run("method not allowed", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__preview/events", nil)
		w := httptest.NewRecorder()
		srv.handleEvents(w, req)

		resp := w.Result()
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
		}
	})
}

func TestStaticContent(t *testing.T) {
	t.Run("returns cloned bytes", func(t *testing.T) {
		original := []byte("original")
		fn := StaticContent(original, "text/plain")

		// Modify original after creating the function
		original[0] = 'X'

		body, contentType, err := fn(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if contentType != "text/plain" {
			t.Errorf("contentType = %q, want %q", contentType, "text/plain")
		}
		// Should return original bytes, not modified
		if string(body) != "original" {
			t.Errorf("body = %q, want %q", string(body), "original")
		}
	})

	t.Run("default content type", func(t *testing.T) {
		fn := StaticContent([]byte("test"), "")
		_, contentType, _ := fn(context.Background())
		if contentType != "text/plain; charset=utf-8" {
			t.Errorf("default contentType = %q, want %q", contentType, "text/plain; charset=utf-8")
		}
	})
}

func TestSetContentRoutes(t *testing.T) {
	t.Run("normalizes paths", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		routes := map[string]ContentFunc{
			"page":  StaticContent([]byte("page1"), "text/plain"),
			"/path": StaticContent([]byte("page2"), "text/plain"),
		}
		srv.SetContentRoutes(routes)

		// Should be able to look up both with leading slash
		fn1, ok1 := srv.lookupContentFunc("/page")
		fn2, ok2 := srv.lookupContentFunc("/path")

		if !ok1 {
			t.Error("failed to find /page route")
		}
		if !ok2 {
			t.Error("failed to find /path route")
		}

		if fn1 != nil {
			body, _, _ := fn1(context.Background())
			if string(body) != "page1" {
				t.Errorf("/page body = %q, want %q", string(body), "page1")
			}
		}
		if fn2 != nil {
			body, _, _ := fn2(context.Background())
			if string(body) != "page2" {
				t.Errorf("/path body = %q, want %q", string(body), "page2")
			}
		}
	})

	t.Run("ignores nil functions", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		routes := map[string]ContentFunc{
			"/valid": StaticContent([]byte("valid"), "text/plain"),
			"/nil":   nil,
		}
		srv.SetContentRoutes(routes)

		_, ok := srv.lookupContentFunc("/nil")
		if ok {
			t.Error("nil function should not be stored")
		}
	})

	t.Run("ignores empty paths", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		routes := map[string]ContentFunc{
			"":       StaticContent([]byte("empty"), "text/plain"),
			"/valid": StaticContent([]byte("valid"), "text/plain"),
		}
		srv.SetContentRoutes(routes)

		srv.contentMu.RLock()
		count := len(srv.routes)
		srv.contentMu.RUnlock()

		if count != 1 {
			t.Errorf("route count = %d, want 1", count)
		}
	})

	t.Run("nil clears routes", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		srv.SetContentRoutes(map[string]ContentFunc{"/page": StaticContent([]byte("page"), "text/plain")})
		srv.SetContentRoutes(nil)

		srv.contentMu.RLock()
		count := len(srv.routes)
		srv.contentMu.RUnlock()

		if count != 0 {
			t.Errorf("route count = %d, want 0 after clearing", count)
		}
	})
}

func TestSetLocalAssets(t *testing.T) {
	t.Run("ignores empty paths", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		assets := []LocalAsset{
			{URLPath: "", FilePath: "/some/path"},
			{URLPath: "/valid", FilePath: ""},
			{URLPath: "/good", FilePath: "/file.txt"},
		}
		srv.SetLocalAssets(assets)

		srv.assetMu.RLock()
		count := len(srv.localAssets)
		srv.assetMu.RUnlock()

		if count != 1 {
			t.Errorf("asset count = %d, want 1", count)
		}
	})
}

func TestBroadcastContent(t *testing.T) {
	t.Run("normalizes path", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		// Subscribe to receive broadcasts
		ch := srv.sse.Subscribe()
		defer srv.sse.Unsubscribe(ch)

		// Broadcast with path without leading slash
		srv.BroadcastContent("page", []byte("<html></html>"))

		select {
		case msg := <-ch:
			if !strings.Contains(string(msg), `"path":"/page"`) {
				t.Errorf("message should contain normalized path: %s", string(msg))
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout waiting for broadcast")
		}
	})

	t.Run("ignores empty html", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		ch := srv.sse.Subscribe()
		defer srv.sse.Unsubscribe(ch)

		srv.BroadcastContent("/page", []byte{})

		select {
		case <-ch:
			t.Error("should not broadcast empty content")
		case <-time.After(50 * time.Millisecond):
			// Expected: no message
		}
	})

	t.Run("nil server is safe", func(t *testing.T) {
		var srv *Server
		// Should not panic
		srv.BroadcastContent("/page", []byte("html"))
	})
}

func TestSSEHub(t *testing.T) {
	t.Run("subscribe and unsubscribe", func(t *testing.T) {
		hub := NewSSEHub()
		ch := hub.Subscribe()
		if ch == nil {
			t.Fatal("Subscribe returned nil channel")
		}

		hub.Unsubscribe(ch)

		// Channel should be closed
		_, ok := <-ch
		if ok {
			t.Error("channel should be closed after unsubscribe")
		}
	})

	t.Run("double unsubscribe is safe", func(t *testing.T) {
		hub := NewSSEHub()
		ch := hub.Subscribe()
		hub.Unsubscribe(ch)
		hub.Unsubscribe(ch) // Should not panic
	})

	t.Run("unsubscribe nil is safe", func(t *testing.T) {
		hub := NewSSEHub()
		hub.Unsubscribe(nil) // Should not panic
	})

	t.Run("broadcast to multiple clients", func(t *testing.T) {
		hub := NewSSEHub()
		ch1 := hub.Subscribe()
		ch2 := hub.Subscribe()
		defer hub.Unsubscribe(ch1)
		defer hub.Unsubscribe(ch2)

		hub.Broadcast([]byte("message"))

		select {
		case msg := <-ch1:
			if string(msg) != "message" {
				t.Errorf("ch1 got %q, want %q", string(msg), "message")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout on ch1")
		}

		select {
		case msg := <-ch2:
			if string(msg) != "message" {
				t.Errorf("ch2 got %q, want %q", string(msg), "message")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout on ch2")
		}
	})

	t.Run("empty broadcast is ignored", func(t *testing.T) {
		hub := NewSSEHub()
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		hub.Broadcast(nil)
		hub.Broadcast([]byte{})

		select {
		case <-ch:
			t.Error("should not receive empty broadcasts")
		case <-time.After(50 * time.Millisecond):
			// Expected
		}
	})
}

func TestLookupContentFunc(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*Server)
		path      string
		wantOK    bool
		wantBody  string
	}{
		{
			name: "empty path defaults to root",
			setupFunc: func(s *Server) {
				s.SetContentFunc(StaticContent([]byte("root"), "text/plain"))
			},
			path:     "",
			wantOK:   true,
			wantBody: "root",
		},
		{
			name: "explicit root path",
			setupFunc: func(s *Server) {
				s.SetContentFunc(StaticContent([]byte("root"), "text/plain"))
			},
			path:     "/",
			wantOK:   true,
			wantBody: "root",
		},
		{
			name: "route takes precedence",
			setupFunc: func(s *Server) {
				s.SetContentFunc(StaticContent([]byte("default"), "text/plain"))
				s.SetContentRoutes(map[string]ContentFunc{
					"/special": StaticContent([]byte("special"), "text/plain"),
				})
			},
			path:     "/special",
			wantOK:   true,
			wantBody: "special",
		},
		{
			name: "no match when routes exist",
			setupFunc: func(s *Server) {
				s.SetContentFunc(StaticContent([]byte("default"), "text/plain"))
				s.SetContentRoutes(map[string]ContentFunc{
					"/page": StaticContent([]byte("page"), "text/plain"),
				})
			},
			path:   "/other",
			wantOK: false,
		},
		{
			name:      "no content function set",
			setupFunc: func(s *Server) {},
			path:      "/",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := New(Config{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			// Clear default content function
			srv.contentMu.Lock()
			srv.contentFn = nil
			srv.contentMu.Unlock()

			tt.setupFunc(srv)

			fn, ok := srv.lookupContentFunc(tt.path)
			if ok != tt.wantOK {
				t.Errorf("lookupContentFunc(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
				return
			}
			if ok && tt.wantBody != "" {
				body, _, _ := fn(context.Background())
				if string(body) != tt.wantBody {
					t.Errorf("body = %q, want %q", string(body), tt.wantBody)
				}
			}
		})
	}
}

func TestServerURL(t *testing.T) {
	t.Run("returns empty for nil listener", func(t *testing.T) {
		srv := &Server{}
		if srv.URL() != "" {
			t.Errorf("URL() = %q, want empty", srv.URL())
		}
	})
}

func TestSetContentFunc(t *testing.T) {
	t.Run("nil function is ignored", func(t *testing.T) {
		srv, err := New(Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		// Verify content function is not nil before
		if srv.contentFn == nil {
			t.Fatal("default content function should not be nil")
		}

		srv.SetContentFunc(nil)

		// Verify content function is still not nil after
		if srv.contentFn == nil {
			t.Error("nil should not replace existing content function")
		}
	})
}

func TestHandleCDN_NoCacheDir(t *testing.T) {
	// Mock CDN server
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cdn content"))
	}))
	defer mockCDN.Close()

	// Server without cache dir
	srv, err := New(Config{
		CacheDir:   "", // No cache directory
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/file.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "cdn content" {
		t.Errorf("body = %q, want %q", string(body), "cdn content")
	}
}

func TestHandleCDN_NonOKResponseNotCached(t *testing.T) {
	cacheDir := t.TempDir()

	// Mock CDN returns 404
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CacheDir:   cacheDir,
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/missing.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should return 404
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Verify file was NOT cached
	cachedPath := filepath.Join(cacheDir, "missing.js")
	if _, err := os.Stat(cachedPath); !os.IsNotExist(err) {
		t.Error("404 response should not be cached")
	}
}

func TestHandleCDN_ContentLengthHeaderExceedsLimit(t *testing.T) {
	// Mock CDN that declares large content length
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
		// Don't actually write that much
		_, _ = w.Write([]byte("small body"))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	srv.maxCDNBytes = 100

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/file.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should reject based on Content-Length header
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

func TestHandleCDN_TemplateEngine_ServesFromLocalCache(t *testing.T) {
	cacheDir := t.TempDir()

	// Pre-populate cache with content for the template engine
	cachedFile := filepath.Join(cacheDir, "bn-template-engine", "v1.0.0", "bn-template-engine.esm.js")
	if err := os.MkdirAll(filepath.Dir(cachedFile), 0o755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	if err := os.WriteFile(cachedFile, []byte("local template engine"), 0o644); err != nil {
		t.Fatalf("failed to create cached file: %v", err)
	}

	// Mock CDN that would return different content (should NOT be called for template engine)
	cdnCalled := false
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cdn content"))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CacheDir:   cacheDir,
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/bn-template-engine/v1.0.0/bn-template-engine.esm.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should serve from local cache
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "local template engine" {
		t.Errorf("body = %q, want %q", string(body), "local template engine")
	}

	// CDN should NOT be called for template engine files
	if cdnCalled {
		t.Error("CDN was called for template engine file, but should serve from local cache only")
	}
}

func TestHandleCDN_TemplateEngine_NotFoundLocally(t *testing.T) {
	cacheDir := t.TempDir()

	// No template engine in cache
	srv, err := New(Config{
		CacheDir:   cacheDir,
		CDNBaseURL: "https://example.com/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/bn-template-engine/v1.0.0/bn-template-engine.esm.js", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should return 404 when not found locally
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandleCDN_CacheFirst_UsesCache(t *testing.T) {
	cacheDir := t.TempDir()

	// Pre-populate cache with content for a regular file
	cachedFile := filepath.Join(cacheDir, "some", "style.css")
	if err := os.MkdirAll(filepath.Dir(cachedFile), 0o755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	if err := os.WriteFile(cachedFile, []byte("cached css"), 0o644); err != nil {
		t.Fatalf("failed to create cached file: %v", err)
	}

	// Mock CDN that would return different content
	mockCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fresh css from cdn"))
	}))
	defer mockCDN.Close()

	srv, err := New(Config{
		CacheDir:   cacheDir,
		CDNBaseURL: mockCDN.URL + "/",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cdn/some/style.css", nil)
	w := httptest.NewRecorder()
	srv.handleCDN(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// For cache-first files, should get cached content
	if string(body) != "cached css" {
		t.Errorf("body = %q, want %q (cache-first files should use cache)", string(body), "cached css")
	}
}
