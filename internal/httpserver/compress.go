// Package httpserver provides a preview HTTP server for report development.
package httpserver

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
)

// compressionLevel defines the default compression level.
// For brotli: 4 is a good balance between speed and compression ratio.
// For gzip: gzip.DefaultCompression is used.
const brotliCompressionLevel = 4

// compressibleTypes lists content types eligible for compression.
var compressibleTypes = map[string]bool{
	"text/html":                true,
	"text/plain":               true,
	"text/css":                 true,
	"text/javascript":          true,
	"application/javascript":   true,
	"application/json":         true,
	"application/xml":          true,
	"text/xml":                 true,
	"image/svg+xml":            true,
	"text/event-stream":        true,
	"application/octet-stream": false, // Explicitly not compressed
}

// writerPools for reusing compression writers.
var (
	brotliWriterPool = sync.Pool{
		New: func() any {
			return brotli.NewWriterLevel(nil, brotliCompressionLevel)
		},
	}
	gzipWriterPool = sync.Pool{
		New: func() any {
			w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
			return w
		},
	}
)

// compressionType represents the type of compression to use.
type compressionType int

const (
	compressionNone compressionType = iota
	compressionBrotli
	compressionGzip
)

// selectCompression returns the best compression type based on Accept-Encoding header.
// Prefers Brotli over Gzip when both are supported.
func selectCompression(acceptEncoding string) compressionType {
	if acceptEncoding == "" {
		return compressionNone
	}
	// Parse the Accept-Encoding header, checking for br and gzip
	hasBrotli := false
	hasGzip := false
	for _, part := range strings.Split(acceptEncoding, ",") {
		encoding := strings.TrimSpace(strings.Split(part, ";")[0])
		switch encoding {
		case "br":
			hasBrotli = true
		case "gzip":
			hasGzip = true
		}
	}
	// Prefer Brotli over Gzip
	if hasBrotli {
		return compressionBrotli
	}
	if hasGzip {
		return compressionGzip
	}
	return compressionNone
}

// isCompressibleContentType checks if the content type is eligible for compression.
func isCompressibleContentType(contentType string) bool {
	// Extract the base content type without charset or other parameters
	base := strings.TrimSpace(strings.Split(contentType, ";")[0])
	if compressible, ok := compressibleTypes[base]; ok {
		return compressible
	}
	// Default: compress text types, don't compress binary
	return strings.HasPrefix(base, "text/")
}

// compressedResponseWriter wraps an http.ResponseWriter with compression.
type compressedResponseWriter struct {
	http.ResponseWriter
	writer          io.Writer
	compType        compressionType
	headerWritten   bool
	statusCode      int
	skipCompression bool
}

// Write compresses data if appropriate.
func (c *compressedResponseWriter) Write(b []byte) (int, error) {
	if !c.headerWritten {
		c.writeHeader(http.StatusOK)
	}
	return c.writer.Write(b)
}

// WriteHeader captures the status code and sets compression headers if needed.
func (c *compressedResponseWriter) WriteHeader(statusCode int) {
	if c.headerWritten {
		return
	}
	c.statusCode = statusCode
	c.writeHeader(statusCode)
}

func (c *compressedResponseWriter) writeHeader(statusCode int) {
	if c.headerWritten {
		return
	}
	c.headerWritten = true
	c.statusCode = statusCode

	// Check if compression should be skipped
	if c.skipCompression {
		c.writer = c.ResponseWriter
		c.ResponseWriter.WriteHeader(statusCode)
		return
	}

	// Check content type for compressibility
	contentType := c.ResponseWriter.Header().Get("Content-Type")
	if !isCompressibleContentType(contentType) {
		c.writer = c.ResponseWriter
		c.ResponseWriter.WriteHeader(statusCode)
		return
	}

	// Remove Content-Length as compression changes the size
	c.ResponseWriter.Header().Del("Content-Length")

	// Set appropriate Content-Encoding and initialize writer
	switch c.compType {
	case compressionBrotli:
		c.ResponseWriter.Header().Set("Content-Encoding", "br")
		c.ResponseWriter.Header().Add("Vary", "Accept-Encoding")
		bw, _ := brotliWriterPool.Get().(*brotli.Writer)
		bw.Reset(c.ResponseWriter)
		c.writer = bw
	case compressionGzip:
		c.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		c.ResponseWriter.Header().Add("Vary", "Accept-Encoding")
		gw, _ := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(c.ResponseWriter)
		c.writer = gw
	default:
		c.writer = c.ResponseWriter
	}

	c.ResponseWriter.WriteHeader(statusCode)
}

// Close flushes and closes the compression writer.
func (c *compressedResponseWriter) Close() error {
	if c.writer == nil {
		return nil
	}

	switch c.compType {
	case compressionBrotli:
		if bw, ok := c.writer.(*brotli.Writer); ok {
			err := bw.Close()
			bw.Reset(nil)
			brotliWriterPool.Put(bw)
			return err
		}
	case compressionGzip:
		if gw, ok := c.writer.(*gzip.Writer); ok {
			err := gw.Close()
			gw.Reset(nil)
			gzipWriterPool.Put(gw)
			return err
		}
	default:
	}
	return nil
}

// Flush implements http.Flusher.
func (c *compressedResponseWriter) Flush() {
	// Ensure header is written first
	if !c.headerWritten {
		c.writeHeader(http.StatusOK)
	}

	// Flush the compression writer if it supports it
	switch c.compType {
	case compressionBrotli:
		if bw, ok := c.writer.(*brotli.Writer); ok {
			_ = bw.Flush()
		}
	case compressionGzip:
		if gw, ok := c.writer.(*gzip.Writer); ok {
			_ = gw.Flush()
		}
	default:
	}

	// Flush the underlying response writer
	if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker.
func (c *compressedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := c.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push implements http.Pusher.
func (c *compressedResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := c.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// compressionHandlerFunc wraps an http.HandlerFunc with compression support.
func compressionHandlerFunc(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Determine compression type from Accept-Encoding header
		compType := selectCompression(r.Header.Get("Accept-Encoding"))

		if compType == compressionNone {
			fn(w, r)
			return
		}

		cw := &compressedResponseWriter{
			ResponseWriter: w,
			compType:       compType,
			writer:         w, // Default to original writer until header is written
		}
		defer cw.Close()

		fn(cw, r)
	}
}

// sseCompressedWriter is a specialized compressed writer for SSE streams.
// Unlike the regular compressedResponseWriter, this writes headers immediately
// and supports streaming with proper flushing.
type sseCompressedWriter struct {
	http.ResponseWriter
	writer   io.Writer
	compType compressionType
}

// newSSECompressedWriter creates a compressed writer for SSE streams.
// It immediately sets up compression headers and initializes the writer.
func newSSECompressedWriter(w http.ResponseWriter, compType compressionType) *sseCompressedWriter {
	sw := &sseCompressedWriter{
		ResponseWriter: w,
		compType:       compType,
	}

	// Remove Content-Length as compression changes the size
	w.Header().Del("Content-Length")

	// Set appropriate Content-Encoding
	switch compType {
	case compressionBrotli:
		w.Header().Set("Content-Encoding", "br")
		w.Header().Add("Vary", "Accept-Encoding")
		bw, _ := brotliWriterPool.Get().(*brotli.Writer)
		bw.Reset(w)
		sw.writer = bw
	case compressionGzip:
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gw, _ := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(w)
		sw.writer = gw
	default:
		sw.writer = w
	}

	return sw
}

// Write writes data to the compressed stream.
func (s *sseCompressedWriter) Write(b []byte) (int, error) {
	return s.writer.Write(b)
}

// Flush flushes the compression writer and the underlying response writer.
func (s *sseCompressedWriter) Flush() {
	// Flush the compression writer
	switch s.compType {
	case compressionBrotli:
		if bw, ok := s.writer.(*brotli.Writer); ok {
			_ = bw.Flush()
		}
	case compressionGzip:
		if gw, ok := s.writer.(*gzip.Writer); ok {
			_ = gw.Flush()
		}
	default:
	}

	// Flush the underlying response writer
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Close flushes and closes the compression writer.
func (s *sseCompressedWriter) Close() error {
	if s.writer == nil {
		return nil
	}

	switch s.compType {
	case compressionBrotli:
		if bw, ok := s.writer.(*brotli.Writer); ok {
			err := bw.Close()
			bw.Reset(nil)
			brotliWriterPool.Put(bw)
			return err
		}
	case compressionGzip:
		if gw, ok := s.writer.(*gzip.Writer); ok {
			err := gw.Close()
			gw.Reset(nil)
			gzipWriterPool.Put(gw)
			return err
		}
	default:
	}
	return nil
}

// Header returns the header map.
func (s *sseCompressedWriter) Header() http.Header {
	return s.ResponseWriter.Header()
}

// WriteHeader is a no-op for SSE as headers are set during initialization.
func (s *sseCompressedWriter) WriteHeader(statusCode int) {
	s.ResponseWriter.WriteHeader(statusCode)
}
