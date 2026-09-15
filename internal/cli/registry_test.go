package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bino-bi/bino-plugin-sdk/registrydigest"

	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
)

// fakeResource is one binary resource bundled with a fakePackage version.
type fakeResource struct {
	name string
	body []byte
}

// fakeTreeFile is one file of a v2 (file-tree) package version.
type fakeTreeFile struct {
	path string
	body []byte
}

// fakePackage is one published version served by the fake registry. A package
// with tree files is served through the v2 routes; one without is a v1
// single-document package, which a v2 registry still renders as a synthetic
// one-file tree.
type fakePackage struct {
	tag          string // resolved tag for bare/tag refs; "" only via pin refs
	version      string
	kind         string
	kinds        []string
	dependencies []string
	body         []byte
	digest       string
	resources    []fakeResource
	tree         []fakeTreeFile
	compatEngine string
	compatCLI    string

	// fileDownloads counts every served file of this package, so a test can
	// assert that a warm store costs no bandwidth.
	fileDownloads atomic.Int64
}

// treeEntries digests a fake package's tree the way the registry does.
func (p *fakePackage) treeEntries(t *testing.T) []registry.FileEntry {
	t.Helper()
	out := make([]registry.FileEntry, len(p.tree))
	for i, f := range p.tree {
		typ := registry.FileTypeForPath(f.path)
		digest := registry.ResourceDigest(f.body)
		if typ == registry.FileDocument {
			d, _, err := registry.DocumentDigest(f.body)
			if err != nil {
				t.Fatalf("digest %s: %v", f.path, err)
			}
			digest = d
		}
		out[i] = registry.FileEntry{Path: f.path, Type: typ, Digest: digest}
	}
	return out
}

// manifestDigest is the version digest of a fake tree package.
func (p *fakePackage) manifestDigest(t *testing.T) string {
	t.Helper()
	d, _, err := registry.ManifestDigest(p.treeEntries(t))
	if err != nil {
		t.Fatalf("manifest digest: %v", err)
	}
	return d
}

// sha256Hex formats b's sha256 as "sha256:<hex>", the same form the registry
// server uses for a resource's content_hash and ETag.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// fakeRegistryServer serves resolve/download/resources for a static package
// set and counts resolve calls and resource-download calls.
func fakeRegistryServer(t *testing.T, packages map[string]*fakePackage) (srv *httptest.Server, resolveCalls, resourceDownloadCalls *atomic.Int64) {
	t.Helper()
	resolveCalls = &atomic.Int64{}
	resourceDownloadCalls = &atomic.Int64{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/registry/resolve/{scope}/{name}", func(w http.ResponseWriter, r *http.Request) {
		resolveCalls.Add(1)
		name := "@" + r.PathValue("scope") + "/" + r.PathValue("name")
		pkg, ok := packages[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":"package_not_found","message":"not found"}`)
			return
		}
		ref := r.URL.Query().Get("ref")
		tag := pkg.tag
		if registry.IsPin(ref) {
			if ref != pkg.version {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"code":"version_not_found","message":"not found"}`)
				return
			}
			tag = ""
		}
		resp := map[string]any{
			"package": name, "tag": tag, "version": pkg.version, "kind": pkg.kind,
			"digest": pkg.digest, "dependencies": pkg.dependencies,
			"downloadUrl": "/api/registry/download/" + r.PathValue("scope") + "/" + r.PathValue("name") + "/" + pkg.version,
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("GET /api/registry/download/{scope}/{name}/{version}", func(w http.ResponseWriter, r *http.Request) {
		name := "@" + r.PathValue("scope") + "/" + r.PathValue("name")
		pkg, ok := packages[name]
		if !ok || pkg.version != r.PathValue("version") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":"version_not_found","message":"not found"}`)
			return
		}
		pkg.fileDownloads.Add(1)
		w.Header().Set("ETag", fmt.Sprintf("%q", pkg.digest))
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(pkg.body)
	})
	mux.HandleFunc("GET /api/registry/resources/{scope}/{name}/{version}", func(w http.ResponseWriter, r *http.Request) {
		name := "@" + r.PathValue("scope") + "/" + r.PathValue("name")
		pkg, ok := packages[name]
		if !ok || pkg.version != r.PathValue("version") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":"version_not_found","message":"not found"}`)
			return
		}
		type resourceMeta struct {
			Name        string `json:"name"`
			ContentHash string `json:"content_hash"`
			Size        int64  `json:"size"`
			MimeType    string `json:"mime_type"`
		}
		metas := make([]resourceMeta, 0, len(pkg.resources))
		for _, res := range pkg.resources {
			metas = append(metas, resourceMeta{Name: res.name, ContentHash: sha256Hex(res.body), Size: int64(len(res.body)), MimeType: "application/octet-stream"})
		}
		json.NewEncoder(w).Encode(metas)
	})
	mux.HandleFunc("GET /api/registry/resources/{scope}/{name}/{version}/{resourceName}", func(w http.ResponseWriter, r *http.Request) {
		resourceDownloadCalls.Add(1)
		name := "@" + r.PathValue("scope") + "/" + r.PathValue("name")
		pkg, ok := packages[name]
		if !ok || pkg.version != r.PathValue("version") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":"version_not_found","message":"not found"}`)
			return
		}
		for _, res := range pkg.resources {
			if res.name == r.PathValue("resourceName") {
				pkg.fileDownloads.Add(1)
				w.Header().Set("ETag", fmt.Sprintf("%q", sha256Hex(res.body)))
				w.Write(res.body)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":"resource_not_found","message":"not found"}`)
	})
	registerFakeV2Routes(t, mux, packages, resolveCalls)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, resolveCalls, resourceDownloadCalls
}

// registerFakeV2Routes adds the v2 resolve and per-file routes. A package
// without tree files is served as the synthetic one-file tree a real registry
// renders for a v1 version — same path, same digest — so a mixed closure
// exercises both formats against one server.
func registerFakeV2Routes(t *testing.T, mux *http.ServeMux, packages map[string]*fakePackage, resolveCalls *atomic.Int64) {
	t.Helper()
	mux.HandleFunc("GET /api/registry/v2/resolve/{scope}/{name}", func(w http.ResponseWriter, r *http.Request) {
		resolveCalls.Add(1)
		base := r.PathValue("name")
		name := "@" + r.PathValue("scope") + "/" + base
		pkg, ok := packages[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":"package_not_found","message":"not found"}`)
			return
		}
		ref := r.URL.Query().Get("ref")
		tag := pkg.tag
		if registry.IsPin(ref) {
			if ref != pkg.version {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"code":"version_not_found","message":"not found"}`)
				return
			}
			tag = ""
		}
		digest, kinds := pkg.digest, pkg.kinds
		files := []map[string]any{{
			"path": base + ".yml", "type": "document", "digest": pkg.digest, "size": len(pkg.body),
		}}
		if len(pkg.tree) > 0 {
			digest = pkg.manifestDigest(t)
			files = files[:0]
			for i, e := range pkg.treeEntries(t) {
				files = append(files, map[string]any{
					"path": e.Path, "type": e.Type, "digest": e.Digest, "size": len(pkg.tree[i].body),
				})
			}
		}
		if len(kinds) == 0 {
			kinds = []string{pkg.kind}
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck // test handler
			"package": name, "tag": tag, "version": pkg.version, "digest": digest,
			"kinds": kinds, "dependencies": pkg.dependencies,
			"compatEngine": pkg.compatEngine, "compatCli": pkg.compatCLI,
			"files": files,
		})
	})
	mux.HandleFunc("GET /api/registry/v2/files/{scope}/{name}/{version}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		base := r.PathValue("name")
		name := "@" + r.PathValue("scope") + "/" + base
		pkg, ok := packages[name]
		if !ok || pkg.version != r.PathValue("version") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":"version_not_found","message":"not found"}`)
			return
		}
		want := r.PathValue("path")
		if len(pkg.tree) == 0 {
			if want != base+".yml" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"code":"file_not_found","message":"not found"}`)
				return
			}
			pkg.fileDownloads.Add(1)
			w.Header().Set("ETag", fmt.Sprintf("%q", pkg.digest))
			w.Write(pkg.body) //nolint:errcheck // test handler
			return
		}
		entries := pkg.treeEntries(t)
		for i, f := range pkg.tree {
			if f.path == want {
				pkg.fileDownloads.Add(1)
				w.Header().Set("ETag", fmt.Sprintf("%q", entries[i].Digest))
				w.Write(f.body) //nolint:errcheck // test handler
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":"file_not_found","message":"not found"}`)
	})
}

func fakeDoc(t *testing.T, name, kind string) ([]byte, string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"apiVersion":"bino.bi/v1alpha1","kind":%q,"metadata":{"name":%q},"spec":{"value":"hello"}}`, kind, name))
	digest, err := registrydigest.Digest(body)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	return body, digest
}

func newRegistryTestProject(t *testing.T, registryURL string) string {
	t.Helper()
	dir := t.TempDir()
	toml := fmt.Sprintf("report-id = \"test\"\n\n[registry]\nurl = %q\n", registryURL)
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

func runRegistry(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newRegistryCommand()
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.ExecuteContext(context.Background())
}

func TestRegistryGlobalConfigURL(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/solo", "Text")
	packages := map[string]*fakePackage{
		"@acme/solo": {tag: "latest", version: "1.0.0", kind: "Text", body: body, digest: digest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)

	// The registry URL comes from ~/.bino/config.toml — the project's
	// bino.toml has no [registry] table.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(registry.EnvURL, "")
	if err := os.MkdirAll(filepath.Join(home, ".bino"), 0o700); err != nil {
		t.Fatal(err)
	}
	global := fmt.Sprintf("[registry]\nurl = %q\n", srv.URL)
	if err := os.WriteFile(filepath.Join(home, ".bino", "config.toml"), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("report-id = \"test\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if err := runRegistry(t, "add", "@acme/solo"); err != nil {
		t.Fatalf("add via global config URL: %v", err)
	}
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Get("@acme/solo") == nil {
		t.Errorf("lockfile missing @acme/solo: %+v", lock.Packages)
	}
}

func TestRegistryAddInstallVerifyRemove(t *testing.T) {
	greetingBody, greetingDigest := fakeDoc(t, "@acme/greeting", "Text")
	styleBody, styleDigest := fakeDoc(t, "@acme/style", "ComponentStyle")
	packages := map[string]*fakePackage{
		"@acme/greeting": {tag: "latest", version: "1.2.0", kind: "Text", dependencies: []string{"@acme/style"}, body: greetingBody, digest: greetingDigest},
		"@acme/style":    {tag: "latest", version: "2.0.0", kind: "ComponentStyle", body: styleBody, digest: styleDigest},
	}
	srv, resolveCalls, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	// --- add resolves the closure, writes files, lock, and manifest.
	if err := runRegistry(t, "add", "@acme/greeting"); err != nil {
		t.Fatalf("add: %v", err)
	}
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 2 {
		t.Fatalf("lock has %d packages, want 2: %+v", len(lock.Packages), lock.Packages)
	}
	greeting := lock.Get("@acme/greeting")
	if greeting == nil || greeting.Version != "1.2.0" || greeting.Tag != "latest" || !greeting.Direct {
		t.Errorf("greeting entry: %+v", greeting)
	}
	style := lock.Get("@acme/style")
	if style == nil || style.Direct {
		t.Errorf("style entry should be transitive: %+v", style)
	}
	greetingPath := filepath.Join(dir, ".bino", "registry", "acme", "greeting", "greeting.yml")
	if data, err := os.ReadFile(greetingPath); err != nil || string(data) != string(greetingBody) {
		t.Errorf("materialized file: %v", err)
	}
	tomlData, _ := os.ReadFile(filepath.Join(dir, "bino.toml"))
	if !strings.Contains(string(tomlData), `"@acme/greeting" = "latest"`) {
		t.Errorf("bino.toml missing dependency:\n%s", tomlData)
	}
	if !strings.Contains(string(tomlData), "[dependencies]") {
		t.Errorf("bino.toml missing table:\n%s", tomlData)
	}

	// --- install replays the lock with zero resolve calls.
	if err := os.RemoveAll(filepath.Join(dir, ".bino")); err != nil {
		t.Fatal(err)
	}
	resolveCalls.Store(0)
	if err := runRegistry(t, "install"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if n := resolveCalls.Load(); n != 0 {
		t.Errorf("install made %d resolve calls, want 0", n)
	}
	if data, err := os.ReadFile(greetingPath); err != nil || string(data) != string(greetingBody) {
		t.Errorf("install did not re-materialize identical bytes: %v", err)
	}

	// --- verify passes, then flags a hand-edited file.
	if err := runRegistry(t, "verify"); err != nil {
		t.Fatalf("verify clean: %v", err)
	}
	if err := os.WriteFile(greetingPath, []byte(`{"kind":"Text","tampered":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "verify"); err == nil {
		t.Fatal("verify should fail on a tampered file")
	}
	if err := runRegistry(t, "install"); err != nil {
		t.Fatalf("re-install: %v", err)
	}
	if err := runRegistry(t, "verify"); err != nil {
		t.Fatalf("verify after re-install: %v", err)
	}

	// --- remove sweeps the package and its now-unused transitive.
	if err := runRegistry(t, "remove", "@acme/greeting"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	lock, err = registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 0 {
		t.Errorf("lock not swept: %+v", lock.Packages)
	}
	if _, err := os.Stat(greetingPath); !os.IsNotExist(err) {
		t.Error("greeting file not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "style", "style.yml")); !os.IsNotExist(err) {
		t.Error("transitive style file not swept")
	}
	tomlData, _ = os.ReadFile(filepath.Join(dir, "bino.toml"))
	if strings.Contains(string(tomlData), "@acme/greeting") {
		t.Errorf("dependency not removed from bino.toml:\n%s", tomlData)
	}
}

func TestRegistryRemoveKeepsSharedTransitive(t *testing.T) {
	aBody, aDigest := fakeDoc(t, "@acme/a", "Text")
	bBody, bDigest := fakeDoc(t, "@acme/b", "Text")
	sharedBody, sharedDigest := fakeDoc(t, "@acme/shared", "ComponentStyle")
	packages := map[string]*fakePackage{
		"@acme/a":      {tag: "latest", version: "1.0.0", kind: "Text", dependencies: []string{"@acme/shared"}, body: aBody, digest: aDigest},
		"@acme/b":      {tag: "latest", version: "1.0.0", kind: "Text", dependencies: []string{"@acme/shared"}, body: bBody, digest: bDigest},
		"@acme/shared": {tag: "latest", version: "1.0.0", kind: "ComponentStyle", body: sharedBody, digest: sharedDigest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/a", "@acme/b"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := runRegistry(t, "remove", "@acme/a"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Get("@acme/a") != nil {
		t.Error("@acme/a still locked")
	}
	if lock.Get("@acme/shared") == nil {
		t.Error("shared transitive was swept while still required by @acme/b")
	}
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "shared", "shared.yml")); err != nil {
		t.Error("shared file was deleted")
	}
}

func TestRegistryInstallDriftDetection(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/x", "Text")
	packages := map[string]*fakePackage{
		"@acme/x": {tag: "latest", version: "1.0.0", kind: "Text", body: body, digest: digest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/x"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Hand-edit the declaration to a pin the lock does not match.
	if err := registry.SetDependency(dir, "@acme/x", "9.9.9"); err != nil {
		t.Fatal(err)
	}
	err := runRegistry(t, "install")
	if err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("expected drift error, got %v", err)
	}
}

func TestRegistryAddPinned(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/x", "Text")
	packages := map[string]*fakePackage{
		"@acme/x": {tag: "latest", version: "1.0.0", kind: "Text", body: body, digest: digest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/x@1.0.0"); err != nil {
		t.Fatalf("add pinned: %v", err)
	}
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Get("@acme/x")
	if e == nil || !e.IsPinned() || e.Version != "1.0.0" {
		t.Errorf("pinned entry: %+v", e)
	}
	tomlData, _ := os.ReadFile(filepath.Join(dir, "bino.toml"))
	if !strings.Contains(string(tomlData), `"@acme/x" = "1.0.0"`) {
		t.Errorf("bino.toml should record the pin:\n%s", tomlData)
	}
}

func TestRegistryAddDigestMismatchWritesNothing(t *testing.T) {
	body, _ := fakeDoc(t, "@acme/x", "Text")
	packages := map[string]*fakePackage{
		"@acme/x": {tag: "latest", version: "1.0.0", kind: "Text", body: body, digest: "sha256:wrong"},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/x"); err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if _, err := os.Stat(filepath.Join(dir, registry.LockfileName)); !os.IsNotExist(err) {
		t.Error("lock file written despite digest mismatch")
	}
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry")); !os.IsNotExist(err) {
		t.Error("store written despite digest mismatch")
	}
	tomlData, _ := os.ReadFile(filepath.Join(dir, "bino.toml"))
	if strings.Contains(string(tomlData), "dependencies") {
		t.Errorf("bino.toml modified despite digest mismatch:\n%s", tomlData)
	}
}

func TestRegistryUpdateMovesTagHoldsPin(t *testing.T) {
	xBody, xDigest := fakeDoc(t, "@acme/x", "Text")
	yBody, yDigest := fakeDoc(t, "@acme/y", "Text")
	packages := map[string]*fakePackage{
		"@acme/x": {tag: "latest", version: "1.0.0", kind: "Text", body: xBody, digest: xDigest},
		"@acme/y": {tag: "latest", version: "1.0.0", kind: "Text", body: yBody, digest: yDigest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/x", "@acme/y@1.0.0"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// The registry moves on: both packages get a 2.0.0.
	xBody2, _ := fakeDoc(t, "@acme/x", "Text")
	xBody2 = append(xBody2[:len(xBody2)-2], []byte(`,"v":2}}`)...)
	xDigest2, err := registrydigest.Digest(xBody2)
	if err != nil {
		t.Fatal(err)
	}
	packages["@acme/x"].version = "2.0.0"
	packages["@acme/x"].body = xBody2
	packages["@acme/x"].digest = xDigest2
	_ = yBody
	_ = yDigest

	if err := runRegistry(t, "update"); err != nil {
		t.Fatalf("update: %v", err)
	}
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e := lock.Get("@acme/x"); e == nil || e.Version != "2.0.0" {
		t.Errorf("tag follower did not move: %+v", e)
	}
	if e := lock.Get("@acme/y"); e == nil || e.Version != "1.0.0" || !e.IsPinned() {
		t.Errorf("pin did not hold: %+v", e)
	}
}

func TestRegistryAddDownloadsResource(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, resourceCalls := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if n := resourceCalls.Load(); n != 1 {
		t.Errorf("resource download calls = %d, want 1", n)
	}
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Get("@acme/revenue")
	if e == nil || len(e.Resources) != 1 || e.Resources[0].Name != "sales.csv" {
		t.Fatalf("entry: %+v", e)
	}
	if want := sha256Hex([]byte("id,amount\n1,10\n")); e.Resources[0].ContentHash != want {
		t.Errorf("content hash = %q, want %q", e.Resources[0].ContentHash, want)
	}
	resPath := filepath.Join(dir, ".bino", "registry", "acme", "revenue", "sales.csv")
	if data, err := os.ReadFile(resPath); err != nil || string(data) != "id,amount\n1,10\n" {
		t.Fatalf("resource file: %v, %q", err, data)
	}
}

func TestRegistryUpdateSkipsUnchangedResource(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, resourceCalls := fakeRegistryServer(t, packages)
	newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if n := resourceCalls.Load(); n != 1 {
		t.Fatalf("resource download calls after add = %d, want 1", n)
	}

	// Nothing changed upstream: a re-sync must not re-download the resource.
	if err := runRegistry(t, "update"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if n := resourceCalls.Load(); n != 1 {
		t.Errorf("resource download calls after no-op update = %d, want still 1", n)
	}
}

func TestRegistryUpdateDownloadsChangedResource(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, resourceCalls := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// The registry publishes a new version with updated resource content
	// (resources are immutable per version, so a change implies a new one).
	body2, _ := fakeDoc(t, "@acme/revenue", "DataSource")
	body2 = append(body2[:len(body2)-2], []byte(`,"v":2}}`)...)
	digest2, err := registrydigest.Digest(body2)
	if err != nil {
		t.Fatal(err)
	}
	packages["@acme/revenue"].version = "2.0.0"
	packages["@acme/revenue"].body = body2
	packages["@acme/revenue"].digest = digest2
	packages["@acme/revenue"].resources = []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,99\n")}}

	if err := runRegistry(t, "update"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if n := resourceCalls.Load(); n != 2 {
		t.Errorf("resource download calls = %d, want 2 (add + changed-content update)", n)
	}
	resPath := filepath.Join(dir, ".bino", "registry", "acme", "revenue", "sales.csv")
	if data, err := os.ReadFile(resPath); err != nil || string(data) != "id,amount\n1,99\n" {
		t.Fatalf("resource not updated: %v, %q", err, data)
	}
}

func TestRegistryUpdateRemovesStaleResource(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}
	resPath := filepath.Join(dir, ".bino", "registry", "acme", "revenue", "sales.csv")
	if _, err := os.Stat(resPath); err != nil {
		t.Fatalf("resource not materialized: %v", err)
	}

	// The registry publishes a new version that dropped the resource.
	body2, _ := fakeDoc(t, "@acme/revenue", "DataSource")
	body2 = append(body2[:len(body2)-2], []byte(`,"v":2}}`)...)
	digest2, err := registrydigest.Digest(body2)
	if err != nil {
		t.Fatal(err)
	}
	packages["@acme/revenue"].version = "2.0.0"
	packages["@acme/revenue"].body = body2
	packages["@acme/revenue"].digest = digest2
	packages["@acme/revenue"].resources = nil

	if err := runRegistry(t, "update"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(resPath); !os.IsNotExist(err) {
		t.Error("stale resource file not removed")
	}
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e := lock.Get("@acme/revenue"); e == nil || len(e.Resources) != 0 {
		t.Errorf("lock still records the stale resource: %+v", e)
	}
}

func TestRegistryInstallRefetchesResource(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, resourceCalls := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".bino")); err != nil {
		t.Fatal(err)
	}
	resourceCalls.Store(0)

	if err := runRegistry(t, "install"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if n := resourceCalls.Load(); n != 1 {
		t.Errorf("resource download calls during install = %d, want 1", n)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".bino", "registry", "acme", "revenue", "sales.csv"))
	if err != nil || string(data) != "id,amount\n1,10\n" {
		t.Fatalf("resource not reinstalled: %v, %q", err, data)
	}
}

func TestRegistryInstallResourceHashMismatchFails(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Corrupt the locked content hash, simulating drift between the lock
	// and what the registry actually serves for that pinned resource.
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Get("@acme/revenue")
	e.Resources[0].ContentHash = "sha256:" + strings.Repeat("0", 64)
	if err := registry.SaveLockfile(dir, lock); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".bino")); err != nil {
		t.Fatal(err)
	}

	err = runRegistry(t, "install")
	if err == nil {
		t.Fatal("expected install to fail on a resource content-hash mismatch")
	}
	if hint := errorHint(err); !strings.Contains(hint, "bino registry update") {
		t.Errorf("expected a re-resolve hint, got %q (err: %v)", hint, err)
	}
}

func TestRegistryInstallMissingResourceFails(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// The resource disappears from the registry (drift since the lock was
	// written), while the document itself is untouched.
	packages["@acme/revenue"].resources = nil

	if err := os.RemoveAll(filepath.Join(dir, ".bino")); err != nil {
		t.Fatal(err)
	}
	err := runRegistry(t, "install")
	if err == nil {
		t.Fatal("expected install to fail on a missing pinned resource")
	}
	if hint := errorHint(err); !strings.Contains(hint, "bino registry update") {
		t.Errorf("expected a re-resolve hint, got %q (err: %v)", hint, err)
	}
}

func TestRegistryVerifyCatchesTamperedOrMissingResource(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/revenue", "DataSource")
	packages := map[string]*fakePackage{
		"@acme/revenue": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: body, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("id,amount\n1,10\n")}},
		},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/revenue"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := runRegistry(t, "verify"); err != nil {
		t.Fatalf("verify clean: %v", err)
	}

	resPath := filepath.Join(dir, ".bino", "registry", "acme", "revenue", "sales.csv")
	if err := os.WriteFile(resPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "verify"); err == nil {
		t.Fatal("verify should fail on a tampered resource file")
	}

	if err := os.Remove(resPath); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "verify"); err == nil {
		t.Fatal("verify should fail on a missing resource file")
	}
}

// TestRegistryPulledDataSourceResourceResolves is the key proof for this
// feature: a DataSource predef bundling a CSV resource is pulled from a fake
// registry via the real "add" path, landing at
// .bino/registry/<scope>/<name>/<name>.yml with the resource as a sibling
// file. It then exercises the REAL, unmodified consuming code
// (datasource.Collect) against that on-disk tree with a relative
// path: "sales.csv" — proving the resource resolves via
// filepath.Dir(doc.File), with zero changes to the path-resolution engine.
func TestRegistryPulledDataSourceResourceResolves(t *testing.T) {
	docBody, digest := fakeDoc(t, "@acme/revenue-table", "DataSource")
	csvBody := []byte("id,name,amount\n1,Widget,10\n2,Gadget,20\n")
	packages := map[string]*fakePackage{
		"@acme/revenue-table": {
			tag: "latest", version: "1.0.0", kind: "DataSource", body: docBody, digest: digest,
			resources: []fakeResource{{name: "sales.csv", body: csvBody}},
		},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/revenue-table"); err != nil {
		t.Fatalf("add: %v", err)
	}

	docPath := filepath.Join(dir, ".bino", "registry", "acme", "revenue-table", "revenue-table.yml")
	if _, err := os.Stat(docPath); err != nil {
		t.Fatalf("pulled document missing: %v", err)
	}
	csvPath := filepath.Join(dir, ".bino", "registry", "acme", "revenue-table", "sales.csv")
	if data, err := os.ReadFile(csvPath); err != nil || string(data) != string(csvBody) {
		t.Fatalf("pulled resource missing or wrong: %v, %q", err, data)
	}

	raw, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"type": "csv",
			"path": "sales.csv",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	docs := []config.Document{
		{Kind: "DataSource", Name: "revenue_table", File: docPath, Raw: raw},
	}

	results, diags, err := datasource.Collect(context.Background(), docs, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", diags)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	var rows []map[string]any
	if err := json.Unmarshal(results[0].Data, &rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}
	if name, ok := rows[0]["name"].(string); !ok || name != "Widget" {
		t.Errorf("unexpected first row: %+v", rows[0])
	}
}

// fakeTree builds a multi-document, multi-file package for @acme/kit: a main
// document, a second manifest in a subdirectory, and a binary resource.
func fakeTree() []fakeTreeFile {
	const name = "@acme/kit"
	return []fakeTreeFile{
		{path: "kit.yaml", body: []byte("apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: \"" + name + "\"\nspec:\n  value: main\n---\napiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: \"" + name + "/second\"\nspec:\n  value: two\n")},
		{path: "components/extra.yaml", body: []byte("apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: \"" + name + "/extra\"\nspec:\n  value: three\n")},
		{path: "resources/logo.png", body: []byte("\x89PNG\r\n\x1a\nlogo")},
	}
}

// A dependency closure may mix generations. Each package must materialize
// under the rule its own format implies, and bino.lock must record which one
// applies — the tree's manifest digest cannot be checked with the v1 rule, or
// the reverse.
func TestRegistryAddMixedV1AndV2Closure(t *testing.T) {
	v1Body, v1Digest := fakeDoc(t, "@bino/style_a", "ComponentStyle")
	packages := map[string]*fakePackage{
		"@acme/kit": {
			tag: "latest", version: "2.0.0", kind: "Text", kinds: []string{"Text"},
			dependencies: []string{"@bino/style_a"}, tree: fakeTree(),
			compatEngine: ">=99.0.0",
		},
		"@bino/style_a": {
			tag: "latest", version: "1.4.0", kind: "ComponentStyle",
			body: v1Body, digest: v1Digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("region,revenue\n")}},
		},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}

	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lock.LockfileVersion != registry.CurrentLockfileVersion {
		t.Errorf("lockfile_version = %d, want %d", lock.LockfileVersion, registry.CurrentLockfileVersion)
	}

	kit := lock.Get("@acme/kit")
	if kit == nil || !kit.IsTree() {
		t.Fatalf("@acme/kit = %+v, want a tree entry", kit)
	}
	if len(kit.Files) != 3 {
		t.Errorf("tree files = %+v, want 3", kit.Files)
	}
	if kit.Digest != packages["@acme/kit"].manifestDigest(t) {
		t.Errorf("tree digest = %s, want the manifest digest", kit.Digest)
	}
	if kit.CompatEngine != ">=99.0.0" {
		t.Errorf("compat range not locked: %+v", kit)
	}
	if kit.Path != ".bino/registry/acme/kit/kit.yaml" {
		t.Errorf("primary document = %q", kit.Path)
	}

	style := lock.Get("@bino/style_a")
	if style == nil || style.IsTree() {
		t.Fatalf("@bino/style_a = %+v, want a document entry", style)
	}
	if style.Format != registry.FormatDocument || len(style.Resources) != 1 {
		t.Errorf("document entry = %+v", style)
	}

	// The tree materializes verbatim, subdirectories and all.
	for _, rel := range []string{
		".bino/registry/acme/kit/kit.yaml",
		".bino/registry/acme/kit/components/extra.yaml",
		".bino/registry/acme/kit/resources/logo.png",
		".bino/registry/bino/style_a/style_a.yml",
		".bino/registry/bino/style_a/sales.csv",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// Verify re-checks both formats offline.
	if err := runRegistry(t, "verify"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// A fresh checkout re-materializes the same tree from the lock alone.
	if err := os.RemoveAll(filepath.Join(dir, ".bino")); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "install"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := runRegistry(t, "verify"); err != nil {
		t.Fatalf("verify after install: %v", err)
	}
}

// A file the lock does not record is reported rather than ignored: the build's
// second pass over .bino/registry would otherwise load an injected document.
func TestRegistryVerifyReportsAnExtraFile(t *testing.T) {
	packages := map[string]*fakePackage{
		"@acme/kit": {tag: "latest", version: "1.0.0", kind: "Text", kinds: []string{"Text"}, tree: fakeTree()},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	injected := filepath.Join(dir, ".bino", "registry", "acme", "kit", "injected.yaml")
	if err := os.WriteFile(injected, []byte("apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: \"@acme/kit/evil\"\nspec: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "verify"); err == nil {
		t.Fatal("verify accepted a file bino.lock does not record")
	}
}

// A package that changes format must not leave its old document beside the new
// tree: two documents with the same metadata.name is a duplicate-name build
// failure the user cannot fix from their own files.
func TestRegistryUpdateFlipsDocumentToTreeWithoutLeavingResidue(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/kit", "Text")
	pkg := &fakePackage{tag: "latest", version: "1.0.0", kind: "Text", body: body, digest: digest}
	packages := map[string]*fakePackage{"@acme/kit": pkg}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	stale := filepath.Join(dir, ".bino", "registry", "acme", "kit", "kit.yml")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("v1 document not materialized: %v", err)
	}

	// The next version of the same package is a file tree.
	pkg.version = "2.0.0"
	pkg.tree = fakeTree()
	pkg.kinds = []string{"Text"}

	if err := runRegistry(t, "update"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the v1 document survived beside the new tree")
	}
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "kit", "kit.yaml")); err != nil {
		t.Errorf("tree not materialized: %v", err)
	}
	if err := runRegistry(t, "verify"); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// An older bino rewriting a v2 lock drops the fields this CLI needs while
// keeping lockfile_version = 2. Install must say so instead of failing with a
// digest mismatch nobody can act on.
func TestRegistryInstallDetectsAStrippedLock(t *testing.T) {
	packages := map[string]*fakePackage{
		"@acme/kit": {tag: "latest", version: "1.0.0", kind: "Text", kinds: []string{"Text"}, tree: fakeTree()},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	lockPath := filepath.Join(dir, registry.LockfileName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stripped := regexp.MustCompile(`(?m)^format = .*\n`).ReplaceAll(data, nil)
	if bytes.Equal(stripped, data) {
		t.Fatal("the lock has no format key to strip")
	}
	if err := os.WriteFile(lockPath, stripped, 0o644); err != nil {
		t.Fatal(err)
	}
	err = runRegistry(t, "install")
	if err == nil || !strings.Contains(err.Error(), "package format") {
		t.Fatalf("err = %v, want a stripped-lock complaint", err)
	}
}

// A warm store costs no bandwidth: install re-checks each file's digest on
// disk and only fetches what is missing or changed. The digest rule is the one
// the package was published under, so a single-document package must be
// recognized as unchanged just as a tree is.
func TestRegistryInstallReusesAWarmStore(t *testing.T) {
	v1Body, v1Digest := fakeDoc(t, "@bino/style_a", "ComponentStyle")
	packages := map[string]*fakePackage{
		"@acme/kit": {
			tag: "latest", version: "2.0.0", kind: "Text", kinds: []string{"Text"},
			dependencies: []string{"@bino/style_a"}, tree: fakeTree(),
		},
		"@bino/style_a": {
			tag: "latest", version: "1.4.0", kind: "ComponentStyle",
			body: v1Body, digest: v1Digest,
			resources: []fakeResource{{name: "sales.csv", body: []byte("region,revenue\n")}},
		},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	for name, pkg := range packages {
		if pkg.fileDownloads.Load() == 0 {
			t.Fatalf("%s served no files on the first install", name)
		}
		pkg.fileDownloads.Store(0)
	}

	if err := runRegistry(t, "install"); err != nil {
		t.Fatalf("install: %v", err)
	}
	for name, pkg := range packages {
		if n := pkg.fileDownloads.Load(); n != 0 {
			t.Errorf("%s re-downloaded %d file(s) into a warm store", name, n)
		}
	}
}
