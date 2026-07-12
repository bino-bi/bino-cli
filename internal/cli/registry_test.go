package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bino-bi/bino-plugin-sdk/registrydigest"

	"bino.bi/bino/internal/registry"
)

// fakePackage is one published version served by the fake registry.
type fakePackage struct {
	tag          string // resolved tag for bare/tag refs; "" only via pin refs
	version      string
	kind         string
	dependencies []string
	body         []byte
	digest       string
}

// fakeRegistryServer serves resolve/download for a static package set and
// counts resolve calls.
func fakeRegistryServer(t *testing.T, packages map[string]*fakePackage) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var resolveCalls atomic.Int64
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
		w.Header().Set("ETag", fmt.Sprintf("%q", pkg.digest))
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(pkg.body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &resolveCalls
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

func TestRegistryAddInstallVerifyRemove(t *testing.T) {
	greetingBody, greetingDigest := fakeDoc(t, "@acme/greeting", "Text")
	styleBody, styleDigest := fakeDoc(t, "@acme/style", "ComponentStyle")
	packages := map[string]*fakePackage{
		"@acme/greeting": {tag: "latest", version: "1.2.0", kind: "Text", dependencies: []string{"@acme/style"}, body: greetingBody, digest: greetingDigest},
		"@acme/style":    {tag: "latest", version: "2.0.0", kind: "ComponentStyle", body: styleBody, digest: styleDigest},
	}
	srv, resolveCalls := fakeRegistryServer(t, packages)
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
	greetingPath := filepath.Join(dir, ".bino", "registry", "acme", "greeting.yml")
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
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "style.yml")); !os.IsNotExist(err) {
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
	srv, _ := fakeRegistryServer(t, packages)
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
	if _, err := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "shared.yml")); err != nil {
		t.Error("shared file was deleted")
	}
}

func TestRegistryInstallDriftDetection(t *testing.T) {
	body, digest := fakeDoc(t, "@acme/x", "Text")
	packages := map[string]*fakePackage{
		"@acme/x": {tag: "latest", version: "1.0.0", kind: "Text", body: body, digest: digest},
	}
	srv, _ := fakeRegistryServer(t, packages)
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
	srv, _ := fakeRegistryServer(t, packages)
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
	srv, _ := fakeRegistryServer(t, packages)
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
	srv, _ := fakeRegistryServer(t, packages)
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
