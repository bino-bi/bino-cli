package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/bino-bi/bino-plugin-sdk/registrydigest"
	"gopkg.in/yaml.v3"
)

// Package-format discriminators. A v2 version is a file tree; a v1 version is
// a single document, which the server also serves through the v2 routes as a
// synthetic one-file tree carrying the v1 digest. The two are digested by
// different rules, so every lock entry records which one applies.
const (
	FormatDocument = "document"
	FormatTree     = "tree"
)

// File types, as the registry names them in a version's file manifest.
const (
	FileDocument = "document"
	FileResource = "resource"
)

// Tree quotas, mirroring the server's (internal/resources/validate.go and
// internal/publishv2/ingest.go). They are enforced client-side so an oversized
// package fails with a local message instead of after a full upload.
const (
	MaxTreeFiles     = 50
	MaxDocumentBytes = 1 << 20  // one manifest file, parsed and canonicalized whole
	MaxFileBytes     = 50 << 20 // one file of any type
	MaxTreeBytes     = 500 << 20
)

// Tree errors. Every one maps onto a message the user can act on locally
// rather than a 4xx from the server.
var (
	ErrInvalidTreePath  = errors.New("invalid package file path")
	ErrUnsupportedType  = errors.New("unsupported resource type")
	ErrEmptyDocument    = errors.New("file carries no YAML document")
	ErrNotCanonical     = errors.New("document is not canonicalizable")
	ErrUnknownFormat    = errors.New("unknown package format")
	ErrTooManyTreeFiles = errors.New("package has too many files")
	ErrTreeFileTooLarge = errors.New("package file is too large")
	ErrTreeTooLarge     = errors.New("package is too large")
)

// treePathRe mirrors the registry server's resources.ValidatePath: one
// optional directory level, each segment following the same grammar as a flat
// resource name. Backslashes, absolute paths, empty segments and a third level
// are all outside the grammar; ".." is rejected separately, as a substring,
// exactly as the server does.
var treePathRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}(/[A-Za-z0-9][A-Za-z0-9._-]{0,254})?$`)

// ValidateTreePath reports whether p is safe to use as a relative path inside
// a package's file tree. It is a deliberate byte-for-byte mirror of the
// server's validator: a path this accepts and the server rejects (or the
// reverse) is a wire break, not a local nicety.
func ValidateTreePath(p string) error {
	if p == "" || !treePathRe.MatchString(p) || strings.Contains(p, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidTreePath, p)
	}
	return nil
}

// documentExts are the extensions the server reads as manifest documents.
var documentExts = []string{".yaml", ".yml"}

// resourceExts is the server's closed binary allow-list
// (internal/resources/validate.go allowedTypes). Anything else is refused at
// publish rather than uploaded and rejected.
var resourceExts = []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".csv", ".xlsx", ".parquet"}

// FileTypeForPath derives a file's type the way the server does: from the
// extension alone. A .yaml file that does not parse as a bino manifest is
// still a document — the server recomputes this and rejects a manifest entry
// that disagrees, so content-based classification would be a 400.
func FileTypeForPath(p string) string {
	ext := strings.ToLower(path.Ext(p))
	for _, e := range documentExts {
		if ext == e {
			return FileDocument
		}
	}
	return FileResource
}

// ResourceExtAllowed reports whether a resource file's extension is on the
// server's allow-list.
func ResourceExtAllowed(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	for _, e := range resourceExts {
		if ext == e {
			return true
		}
	}
	return false
}

// ResourceExtensions returns the allowed resource extensions, for error
// messages that tell the author what would work.
func ResourceExtensions() []string {
	return append([]string(nil), resourceExts...)
}

// FileEntry is one entry of a package's file manifest. The JSON tags are the
// wire contract shared with the registry: the canonical JSON of the
// path-sorted entry array is the version digest, so a tag change here is a
// digest change.
type FileEntry struct {
	Path   string `json:"path" toml:"path"`
	Type   string `json:"type" toml:"type"`
	Digest string `json:"digest" toml:"digest"`
}

// SplitDocuments decodes a "---" YAML stream into one re-encoded document per
// stream element. Re-encoding goes through yaml.Node so anchors, aliases and
// explicit tags survive into the returned bytes and are rejected by
// registrydigest rather than silently resolved away.
func SplitDocuments(raw []byte) ([][]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var docs [][]byte
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrNotCanonical, err)
		}
		out, encErr := yaml.Marshal(&node)
		if encErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrNotCanonical, encErr)
		}
		docs = append(docs, out)
	}
	if len(docs) == 0 {
		return nil, ErrEmptyDocument
	}
	return docs, nil
}

// DocumentDigest computes the content digest of one document file of a v2
// tree and returns the file's documents, split and re-encoded.
//
// The digest is sha256 over the JCS canonical JSON *array* of the file's
// documents: every element of the "---" stream is canonicalized on its own
// (registrydigest.Canonicalize rejects multi-document input) and the canonical
// objects are joined into "[a,b,c]" in stream order. That join IS the
// canonical JSON array, so it is hashed verbatim — never over a re-parse of
// it. A one-document file is digested as a ONE-ELEMENT ARRAY; it is
// deliberately not registrydigest.Digest(raw), because the unit is the file,
// not the document.
//
// Re-canonicalizing the assembled array is not idempotent for every input (a
// U+0085 inside a string folds to a space on a YAML re-read, a U+007F is
// rejected outright), so the second pass is a byte-for-byte self-check that
// errors rather than a silently different digest. This mirrors the server's
// internal/publishv2.DigestDocumentFile exactly.
func DocumentDigest(raw []byte) (digest string, docs [][]byte, err error) {
	docs, err = SplitDocuments(raw)
	if err != nil {
		return "", nil, err
	}
	var arr bytes.Buffer
	arr.WriteByte('[')
	for i, doc := range docs {
		canon, cErr := registrydigest.Canonicalize(doc)
		if cErr != nil {
			return "", nil, fmt.Errorf("%w: document %d: %w", ErrNotCanonical, i+1, cErr)
		}
		if i > 0 {
			arr.WriteByte(',')
		}
		arr.Write(canon)
	}
	arr.WriteByte(']')

	recanon, rcErr := registrydigest.Canonicalize(arr.Bytes())
	if rcErr != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrNotCanonical, rcErr)
	}
	if !bytes.Equal(recanon, arr.Bytes()) {
		return "", nil, fmt.Errorf("%w: array idempotency self-check failed", ErrNotCanonical)
	}
	return sha256Digest(arr.Bytes()), docs, nil
}

// ResourceDigest is the digest of a resource file: sha256 over the raw bytes.
// Resources are opaque binaries with no canonical form.
func ResourceDigest(raw []byte) string { return sha256Digest(raw) }

// ManifestDigest returns a version's digest — sha256 over the JCS canonical
// JSON of the entries sorted by path — plus those canonical bytes. Sorting is
// by path only, over a copy, so the caller's slice keeps its order and
// shuffling the manifest cannot change the digest.
func ManifestDigest(entries []FileEntry) (digest string, canonical []byte, err error) {
	sorted := make([]FileEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	raw, err := json.Marshal(sorted)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrNotCanonical, err)
	}
	digest, canonical, err = registrydigest.DigestWithCanonical(raw)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrNotCanonical, err)
	}
	return digest, canonical, nil
}

// VerifyFile recomputes a downloaded file's digest and compares it with the
// one the lock (or the resolve response) pins. format selects the document
// rule: a tree's documents are digested as a canonical JSON array, while a v1
// document — which the server also serves through the v2 file route, as a
// one-file tree — keeps the single-document digest it was published with.
// The rule is never guessed: trying both would give a hostile server two
// chances to satisfy one check.
func VerifyFile(format, fileType string, body []byte, want string) error {
	var actual string
	switch {
	case fileType == FileResource:
		actual = ResourceDigest(body)
	case format == FormatTree:
		d, _, err := DocumentDigest(body)
		if err != nil {
			return err
		}
		actual = d
	case format == FormatDocument:
		d, err := registrydigest.Digest(body)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrNotCanonical, err)
		}
		actual = d
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, format)
	}
	if actual != want {
		return fmt.Errorf("content digest %s does not match expected %s", actual, want)
	}
	return nil
}

// CheckTreeQuotas enforces the server's per-version limits on a file set
// before any upload or download starts.
func CheckTreeQuotas(sizes map[string]int64) error {
	if len(sizes) > MaxTreeFiles {
		return fmt.Errorf("%w: %d files exceeds the limit of %d", ErrTooManyTreeFiles, len(sizes), MaxTreeFiles)
	}
	var total int64
	for p, n := range sizes {
		limit := int64(MaxFileBytes)
		if FileTypeForPath(p) == FileDocument {
			limit = MaxDocumentBytes
		}
		if n > limit {
			return fmt.Errorf("%w: %q is %d bytes, the limit is %d", ErrTreeFileTooLarge, p, n, limit)
		}
		total += n
	}
	if total > MaxTreeBytes {
		return fmt.Errorf("%w: %d bytes exceeds the limit of %d", ErrTreeTooLarge, total, MaxTreeBytes)
	}
	return nil
}

func sha256Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
