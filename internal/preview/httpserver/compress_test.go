package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
)

func TestSelectCompression(t *testing.T) {
	tests := []struct {
		name           string
		acceptEncoding string
		want           compressionType
	}{
		{
			name:           "no accept-encoding",
			acceptEncoding: "",
			want:           compressionNone,
		},
		{
			name:           "only brotli",
			acceptEncoding: "br",
			want:           compressionBrotli,
		},
		{
			name:           "only gzip",
			acceptEncoding: "gzip",
			want:           compressionGzip,
		},
		{
			name:           "both brotli and gzip prefers brotli",
			acceptEncoding: "gzip, deflate, br",
			want:           compressionBrotli,
		},
		{
			name:           "br before gzip",
			acceptEncoding: "br, gzip",
			want:           compressionBrotli,
		},
		{
			name:           "gzip before br still prefers brotli",
			acceptEncoding: "gzip, br",
			want:           compressionBrotli,
		},
		{
			name:           "identity only",
			acceptEncoding: "identity",
			want:           compressionNone,
		},
		{
			name:           "with quality values",
			acceptEncoding: "gzip;q=0.8, br;q=1.0",
			want:           compressionBrotli,
		},
		{
			name:           "deflate only unsupported",
			acceptEncoding: "deflate",
			want:           compressionNone,
		},
		{
			name:           "wildcard",
			acceptEncoding: "*",
			want:           compressionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectCompression(tt.acceptEncoding)
			if got != tt.want {
				t.Errorf("selectCompression(%q) = %v, want %v", tt.acceptEncoding, got, tt.want)
			}
		})
	}
}

func TestIsCompressibleContentType(t *testing.T) {
	tests := []struct {
		contentType  string
		compressible bool
	}{
		{"text/html", true},
		{"text/html; charset=utf-8", true},
		{"text/plain", true},
		{"text/css", true},
		{"text/javascript", true},
		{"application/javascript", true},
		{"application/json", true},
		{"application/xml", true},
		{"text/xml", true},
		{"image/svg+xml", true},
		{"text/event-stream", true},
		{"application/octet-stream", false},
		{"image/png", false},
		{"image/jpeg", false},
		{"video/mp4", false},
		{"text/unknown", true}, // text/* defaults to compressible
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			got := isCompressibleContentType(tt.contentType)
			if got != tt.compressible {
				t.Errorf("isCompressibleContentType(%q) = %v, want %v", tt.contentType, got, tt.compressible)
			}
		})
	}
}

func TestCompressionMiddleware_Brotli(t *testing.T) {
	testBody := "<html><body><h1>Hello World</h1></body></html>"

	handler := compressionHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testBody))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "br" {
		t.Errorf("expected Content-Encoding: br, got %q", resp.Header.Get("Content-Encoding"))
	}

	if resp.Header.Get("Vary") != "Accept-Encoding" {
		t.Errorf("expected Vary: Accept-Encoding, got %q", resp.Header.Get("Vary"))
	}

	// Decompress and verify
	br := brotli.NewReader(resp.Body)
	decompressed, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("failed to decompress brotli: %v", err)
	}

	if string(decompressed) != testBody {
		t.Errorf("decompressed body = %q, want %q", string(decompressed), testBody)
	}
}

func TestCompressionMiddleware_Gzip(t *testing.T) {
	testBody := "<html><body><h1>Hello World</h1></body></html>"

	handler := compressionHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testBody))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got %q", resp.Header.Get("Content-Encoding"))
	}

	// Decompress and verify
	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to decompress gzip: %v", err)
	}

	if string(decompressed) != testBody {
		t.Errorf("decompressed body = %q, want %q", string(decompressed), testBody)
	}
}

func TestCompressionMiddleware_NoCompression(t *testing.T) {
	testBody := "<html><body><h1>Hello World</h1></body></html>"

	handler := compressionHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testBody))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Accept-Encoding header

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding, got %q", resp.Header.Get("Content-Encoding"))
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != testBody {
		t.Errorf("body = %q, want %q", string(body), testBody)
	}
}

func TestCompressionMiddleware_NonCompressibleContentType(t *testing.T) {
	testBody := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header bytes

	handler := compressionHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(testBody)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	// Should not compress binary content
	if resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding for image/png, got %q", resp.Header.Get("Content-Encoding"))
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(testBody) {
		t.Errorf("body was modified when it shouldn't be")
	}
}

func TestSSECompressedWriter_Brotli(t *testing.T) {
	rec := httptest.NewRecorder()

	// Set SSE headers
	rec.Header().Set("Content-Type", "text/event-stream")

	sw := newSSECompressedWriter(rec, compressionBrotli)
	defer sw.Close()

	// Write SSE event
	event := "data: hello world\n\n"
	n, err := sw.Write([]byte(event))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	if n != len(event) {
		t.Errorf("wrote %d bytes, want %d", n, len(event))
	}

	sw.Flush()

	// Verify Content-Encoding was set
	if rec.Header().Get("Content-Encoding") != "br" {
		t.Errorf("expected Content-Encoding: br, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestSSECompressedWriter_Gzip(t *testing.T) {
	rec := httptest.NewRecorder()

	// Set SSE headers
	rec.Header().Set("Content-Type", "text/event-stream")

	sw := newSSECompressedWriter(rec, compressionGzip)
	defer sw.Close()

	// Write SSE event
	event := "data: hello world\n\n"
	n, err := sw.Write([]byte(event))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	if n != len(event) {
		t.Errorf("wrote %d bytes, want %d", n, len(event))
	}

	sw.Flush()

	// Verify Content-Encoding was set
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_LargeBody(t *testing.T) {
	// Create a large body that will benefit from compression
	testBody := strings.Repeat("<div>This is repeated content for testing compression.</div>", 1000)

	handler := compressionHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testBody))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	compressedBody, _ := io.ReadAll(resp.Body)

	// Verify compression actually reduced size
	if len(compressedBody) >= len(testBody) {
		t.Errorf("compression did not reduce size: compressed=%d, original=%d", len(compressedBody), len(testBody))
	}

	t.Logf("Compression ratio: %.2f%% (%d -> %d bytes)",
		float64(len(compressedBody))/float64(len(testBody))*100,
		len(testBody), len(compressedBody))
}

func TestCompressionMiddleware_JSON(t *testing.T) {
	testBody := `{"name":"test","data":[1,2,3,4,5],"nested":{"key":"value"}}`

	handler := compressionHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testBody))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "br" {
		t.Errorf("expected Content-Encoding: br for JSON, got %q", resp.Header.Get("Content-Encoding"))
	}

	// Decompress and verify
	br := brotli.NewReader(resp.Body)
	decompressed, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("failed to decompress brotli: %v", err)
	}

	if string(decompressed) != testBody {
		t.Errorf("decompressed body = %q, want %q", string(decompressed), testBody)
	}
}
