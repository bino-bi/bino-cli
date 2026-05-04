package render

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"hash/fnv"
)

// ContentHash returns the FNV-1a 64-bit hex hash of data. It is the canonical
// content fingerprint used both in the inline "<hash>:<base64_gzip>" embed
// format and in the URL form "?hash=<hash>" served by previewhttp.Server.
func ContentHash(data []byte) string {
	h := fnv.New64a()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum64())
}

// CompressContent gzip-compresses data, base64-encodes it, and prepends an
// FNV-1a hash.  The result format is "<hash>:<base64_gzip>" which the
// bn-template-engine decodes when raw="false".
func CompressContent(data []byte) (string, error) {
	hashStr := ContentHash(data)

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
