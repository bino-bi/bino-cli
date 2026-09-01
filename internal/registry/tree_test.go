package registry

import (
	"errors"
	"strings"
	"testing"
)

// The fixtures and digests below are the digest contract between this CLI and
// the registry. They are copied verbatim from the server's
// internal/publishv2/docdigest_test.go, where they are frozen for the same
// reason: a change to any of them is a wire break that must land on both sides
// at once.

const oneDoc = `apiVersion: bino.bi/v1
kind: Table
metadata:
  name: "@acme/kit/sales"
  labels:
    registry.visibility: public
spec:
  dataset: sales
  columns:
    - region
    - revenue
`

// oneDocReformatted is oneDoc with comments added, mapping keys reordered and
// the indentation changed. Semantically identical => same digest.
const oneDocReformatted = `# the sales table, reformatted
spec:
    columns: [region, revenue]   # inline sequence
    dataset: sales
metadata:
    labels: {registry.visibility: public}
    name: '@acme/kit/sales'
kind: Table
apiVersion: bino.bi/v1
`

const threeDocs = `apiVersion: bino.bi/v1
kind: LayoutPage
metadata:
  name: "@acme/kit"
spec:
  blocks:
    - ref: "@acme/kit/sales"
---
apiVersion: bino.bi/v1
kind: Table
metadata:
  name: "@acme/kit/sales"
spec:
  dataset: sales
---
apiVersion: bino.bi/v1
kind: DataSet
metadata:
  name: "@acme/kit/sales_data"
spec:
  sql: SELECT region, revenue FROM sales
`

// anchorsDoc uses a YAML anchor and a merge key; registrydigest rejects both.
const anchorsDoc = `apiVersion: bino.bi/v1
kind: Table
metadata: &m
  name: "@acme/kit/sales"
spec:
  <<: *m
  dataset: sales
`

// nelDoc carries U+0085 (NEL) inside a string. The per-document pass keeps it
// (JCS escapes only C0 controls), but re-reading the canonical array as YAML
// folds NEL to a space, so the second pass sees different content.
const nelDoc = `apiVersion: bino.bi/v1
kind: Table
metadata:
  name: "@acme/kit/sales"
spec:
  note: "x\u0085y"
`

// delDoc carries U+007F (DEL) inside a string: kept by the per-document pass,
// rejected as a control character when the canonical array is re-read as YAML.
const delDoc = `apiVersion: bino.bi/v1
kind: Table
metadata:
  name: "@acme/kit/sales"
spec:
  note: "x\u007Fy"
`

const (
	oneDocDigest    = "sha256:f1758d01482d1b360d769f0ae3fa692554f6202bac96987e4848ecc7f172372f"
	threeDocsDigest = "sha256:d4e2a570ca630c0ccce5129e41913dc83ef916d6965b53aac5830b724bbcf6ae"
	manifestDigest  = "sha256:b18268a415209eab0cef1cc789c823c46bef22c4147e3141b88331bce7b731af"
)

var manifestEntries = []FileEntry{
	{Path: "kit.yaml", Type: FileDocument, Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
	{Path: "components/sales.yaml", Type: FileDocument, Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
	{Path: "data/sales.csv", Type: FileResource, Digest: "sha256:3333333333333333333333333333333333333333333333333333333333333333"},
	{Path: "assets/logo.png", Type: FileResource, Digest: "sha256:4444444444444444444444444444444444444444444444444444444444444444"},
}

func TestDocumentDigestMatchesServerGoldens(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		docs int
	}{
		{"one document", oneDoc, oneDocDigest, 1},
		{"same document reformatted and commented", oneDocReformatted, oneDocDigest, 1},
		{"three document stream", threeDocs, threeDocsDigest, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, docs, err := DocumentDigest([]byte(tt.raw))
			if err != nil {
				t.Fatalf("DocumentDigest: %v", err)
			}
			if got != tt.want {
				t.Errorf("digest = %s, want %s", got, tt.want)
			}
			if len(docs) != tt.docs {
				t.Errorf("documents = %d, want %d", len(docs), tt.docs)
			}
		})
	}
}

// A single-document file is digested as a one-element array, which can never
// equal the v1 single-document digest. Both rules stay reachable, so the
// format marker has to select between them.
func TestDocumentDigestIsNotTheV1Digest(t *testing.T) {
	tree, _, err := DocumentDigest([]byte(oneDoc))
	if err != nil {
		t.Fatalf("DocumentDigest: %v", err)
	}
	if err := VerifyFile(FormatDocument, FileDocument, []byte(oneDoc), tree); err == nil {
		t.Fatal("the v1 rule accepted the tree digest; the two rules must not collide")
	}
	if err := VerifyFile(FormatTree, FileDocument, []byte(oneDoc), tree); err != nil {
		t.Fatalf("the tree rule rejected its own digest: %v", err)
	}
}

func TestDocumentDigestRejectsNonCanonicalizable(t *testing.T) {
	tests := []struct{ name, raw string }{
		{"anchors and merge keys", anchorsDoc},
		{"U+0085 folds on the re-read", nelDoc},
		{"U+007F is rejected on the re-read", delDoc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := DocumentDigest([]byte(tt.raw)); !errors.Is(err, ErrNotCanonical) {
				t.Fatalf("err = %v, want ErrNotCanonical", err)
			}
		})
	}
}

func TestDocumentDigestRejectsEmptyStream(t *testing.T) {
	for _, raw := range []string{"", "# just a comment\n"} {
		if _, _, err := DocumentDigest([]byte(raw)); !errors.Is(err, ErrEmptyDocument) {
			t.Fatalf("raw %q: err = %v, want ErrEmptyDocument", raw, err)
		}
	}
}

func TestManifestDigestMatchesServerGolden(t *testing.T) {
	got, canonical, err := ManifestDigest(manifestEntries)
	if err != nil {
		t.Fatalf("ManifestDigest: %v", err)
	}
	if got != manifestDigest {
		t.Errorf("digest = %s, want %s", got, manifestDigest)
	}
	if !strings.HasPrefix(string(canonical), `[{"digest":`) {
		t.Errorf("canonical bytes are not a JSON array of objects: %s", canonical)
	}
}

// The digest is over the path-sorted manifest, so the order the caller
// assembled it in cannot change the version identity — and the caller's slice
// must come back untouched.
func TestManifestDigestIgnoresEntryOrder(t *testing.T) {
	shuffled := []FileEntry{manifestEntries[2], manifestEntries[0], manifestEntries[3], manifestEntries[1]}
	got, _, err := ManifestDigest(shuffled)
	if err != nil {
		t.Fatalf("ManifestDigest: %v", err)
	}
	if got != manifestDigest {
		t.Errorf("digest = %s, want %s", got, manifestDigest)
	}
	if shuffled[0].Path != "data/sales.csv" {
		t.Errorf("caller's slice was reordered: %s", shuffled[0].Path)
	}
}

func TestManifestDigestOfEmptySetIsNotNull(t *testing.T) {
	_, canonical, err := ManifestDigest(nil)
	if err != nil {
		t.Fatalf("ManifestDigest: %v", err)
	}
	if string(canonical) != "[]" {
		t.Errorf("canonical = %s, want []", canonical)
	}
}

func TestResourceDigestIsRawSHA256(t *testing.T) {
	// sha256("") — a resource has no canonical form, so the raw bytes are the digest.
	const want = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := ResourceDigest(nil); got != want {
		t.Errorf("digest = %s, want %s", got, want)
	}
}

func TestValidateTreePath(t *testing.T) {
	valid := []string{
		"kit.yaml", "components/sales.yaml", "resources/logo.png",
		"a", "A.B_c-d.yaml", "models/rev.png",
	}
	for _, p := range valid {
		if err := ValidateTreePath(p); err != nil {
			t.Errorf("ValidateTreePath(%q) = %v, want nil", p, err)
		}
	}
	invalid := []string{
		"", "/abs.yaml", "../evil.yaml", "a/../../b", "a/b/c.yaml",
		".hidden.yaml", "a//b.yaml", `a\b.yaml`, "a..b.yaml", "a/", "/", "..",
	}
	for _, p := range invalid {
		if err := ValidateTreePath(p); !errors.Is(err, ErrInvalidTreePath) {
			t.Errorf("ValidateTreePath(%q) = %v, want ErrInvalidTreePath", p, err)
		}
	}
}

func TestFileTypeForPath(t *testing.T) {
	document := []string{"a.yaml", "a.yml", "A.YAML", "dir/a.Yml"}
	for _, p := range document {
		if got := FileTypeForPath(p); got != FileDocument {
			t.Errorf("FileTypeForPath(%q) = %s, want %s", p, got, FileDocument)
		}
	}
	resource := []string{"a.png", "a.csv", "a", "a.json", "a.yaml.bak"}
	for _, p := range resource {
		if got := FileTypeForPath(p); got != FileResource {
			t.Errorf("FileTypeForPath(%q) = %s, want %s", p, got, FileResource)
		}
	}
}

func TestResourceExtAllowed(t *testing.T) {
	for _, p := range []string{"a.png", "a.JPG", "d/a.parquet", "a.xlsx", "a.csv"} {
		if !ResourceExtAllowed(p) {
			t.Errorf("ResourceExtAllowed(%q) = false", p)
		}
	}
	for _, p := range []string{"a.svg", "a.xls", "a.pdf", "a", "a.exe"} {
		if ResourceExtAllowed(p) {
			t.Errorf("ResourceExtAllowed(%q) = true", p)
		}
	}
}

func TestCheckTreeQuotas(t *testing.T) {
	if err := CheckTreeQuotas(map[string]int64{"a.yaml": 10, "b.png": 1 << 20}); err != nil {
		t.Fatalf("CheckTreeQuotas: %v", err)
	}
	if err := CheckTreeQuotas(map[string]int64{"a.yaml": MaxDocumentBytes + 1}); !errors.Is(err, ErrTreeFileTooLarge) {
		t.Errorf("oversized document: err = %v, want ErrTreeFileTooLarge", err)
	}
	if err := CheckTreeQuotas(map[string]int64{"a.png": MaxFileBytes + 1}); !errors.Is(err, ErrTreeFileTooLarge) {
		t.Errorf("oversized resource: err = %v, want ErrTreeFileTooLarge", err)
	}
	many := make(map[string]int64, MaxTreeFiles+1)
	for i := 0; i <= MaxTreeFiles; i++ {
		many[string(rune('a'+i%26))+string(rune('a'+i/26))+".png"] = 1
	}
	if err := CheckTreeQuotas(many); !errors.Is(err, ErrTooManyTreeFiles) {
		t.Errorf("too many files: err = %v, want ErrTooManyTreeFiles", err)
	}
}

func TestVerifyFileRejectsUnknownFormat(t *testing.T) {
	err := VerifyFile("", FileDocument, []byte(oneDoc), oneDocDigest)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("err = %v, want ErrUnknownFormat", err)
	}
}
