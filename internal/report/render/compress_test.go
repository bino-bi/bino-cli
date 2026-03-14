package render

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func TestCompressContent(t *testing.T) {
	data := []byte(`[{"id":1,"value":42},{"id":2,"value":99}]`)

	result, err := CompressContent(data)
	if err != nil {
		t.Fatalf("CompressContent returned error: %v", err)
	}

	// Must contain exactly one colon separating hash from base64.
	idx := strings.Index(result, ":")
	if idx == -1 {
		t.Fatal("result missing ':' separator")
	}

	hashPart := result[:idx]
	b64Part := result[idx+1:]

	if hashPart == "" {
		t.Fatal("hash part is empty")
	}

	// Decode base64.
	compressed, err := base64.StdEncoding.DecodeString(b64Part)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	// Decompress gzip.
	gr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip reader failed: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip read failed: %v", err)
	}
	gr.Close()

	if !bytes.Equal(decompressed, data) {
		t.Fatalf("round-trip mismatch: got %q, want %q", decompressed, data)
	}
}

func TestCompressContentDeterministic(t *testing.T) {
	data := []byte(`{"key":"value"}`)

	r1, _ := CompressContent(data)
	r2, _ := CompressContent(data)

	// Hash portion must be identical.
	h1 := r1[:strings.Index(r1, ":")]
	h2 := r2[:strings.Index(r2, ":")]
	if h1 != h2 {
		t.Fatalf("hashes differ for same input: %q vs %q", h1, h2)
	}
}
