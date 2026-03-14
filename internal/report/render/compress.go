package render

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"hash/fnv"
)

// CompressContent gzip-compresses data, base64-encodes it, and prepends an
// FNV-1a hash.  The result format is "<hash>:<base64_gzip>" which the
// bn-template-engine decodes when raw="false".
func CompressContent(data []byte) (string, error) {
	// Compute hash of the raw content.
	h := fnv.New64a()
	h.Write(data)
	hashStr := fmt.Sprintf("%x", h.Sum64())

	// Gzip compress.
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	// Base64 encode.
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	return hashStr + ":" + encoded, nil
}
