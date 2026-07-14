package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistryAddRejectsMaliciousResourceName is an adversarial,
// end-to-end proof that a hostile (or merely buggy) registry server cannot
// use a resource's "name" field to write outside the project's
// .bino/registry store. It drives the real "bino registry add" command
// against a fake server whose resource listing advertises a
// path-traversal / absolute-path name, and asserts:
//   - the command fails loudly rather than silently dropping the resource
//   - nothing is ever written outside the project directory (in particular
//     not at the sentinel absolute path the payload targets)
func TestRegistryAddRejectsMaliciousResourceName(t *testing.T) {
	malicious := []string{
		"../../../../../../tmp/bino-escape-poc",
		"/tmp/bino-escape-poc",
		"..",
		"a/../../b",
		"..\\..\\bino-escape-poc",
	}

	for _, name := range malicious {
		t.Run(name, func(t *testing.T) {
			body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
			packages := map[string]*fakePackage{
				"@acme/revenue": {
					tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
					resources: []fakeResource{{name: name, body: []byte("pwned")}},
				},
			}
			srv, _, _ := fakeRegistryServer(t, packages)
			dir := newRegistryTestProject(t, srv.URL)

			err := runRegistry(t, "add", "@acme/revenue")
			if err == nil {
				t.Fatalf("expected add to fail on malicious resource name %q, but it succeeded", name)
			}

			// The sentinel absolute-path target used above must never be
			// created on disk.
			if _, statErr := os.Stat("/tmp/bino-escape-poc"); statErr == nil {
				os.Remove("/tmp/bino-escape-poc")
				t.Fatal("resource escaped the store: /tmp/bino-escape-poc was created")
			}

			// Simulate where a naive (unsafe) implementation would have
			// written this resource — joined straight onto the package
			// dir with no containment check — and confirm nothing landed
			// there, whether inside or outside the project directory.
			naive := filepath.Clean(filepath.Join(dir, ".bino", "registry", "acme", "revenue", name))
			sep := string(filepath.Separator)
			if rel, relErr := filepath.Rel(dir, naive); relErr == nil && (rel == ".." || strings.HasPrefix(rel, ".."+sep)) {
				if _, statErr := os.Stat(naive); statErr == nil {
					t.Fatalf("resource escaped the project directory at %s (from name %q)", naive, name)
				}
			}
		})
	}
}
