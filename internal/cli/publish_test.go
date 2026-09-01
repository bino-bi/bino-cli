package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bino.bi/bino/internal/registry"
)

// publishCapture records what the fake registry received, so a test can assert
// the multipart contract rather than just the happy path.
type publishCapture struct {
	manifest registry.PublishManifest
	parts    map[string]string
	auth     string
	calls    int
}

// fakePublishServer serves POST /api/registry/v2/publish, answering with
// respond (a JSON body) and status.
func fakePublishServer(t *testing.T, status int, respond func(m registry.PublishManifest) string) (*httptest.Server, *publishCapture) {
	t.Helper()
	capture := &publishCapture{parts: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/registry/v2/publish", func(w http.ResponseWriter, r *http.Request) {
		capture.calls++
		capture.auth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		if err := json.Unmarshal([]byte(r.FormValue("manifest")), &capture.manifest); err != nil {
			t.Errorf("manifest: %v", err)
			return
		}
		for name, headers := range r.MultipartForm.File {
			capture.parts[name] = readPart(t, headers[0])
		}
		w.WriteHeader(status)
		io.WriteString(w, respond(capture.manifest)) //nolint:errcheck // test handler
	})
	// The visibility probe resolves the package first; "not found" means this
	// would be a first publish.
	mux.HandleFunc("GET /api/registry/v2/resolve/{scope}/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"code":"package_not_found","message":"package not found"}`) //nolint:errcheck // test handler
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, capture
}

func readPart(t *testing.T, h *multipart.FileHeader) string {
	t.Helper()
	f, err := h.Open()
	if err != nil {
		t.Fatalf("open part: %v", err)
	}
	defer f.Close() //nolint:errcheck // test helper
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, f); err != nil {
		t.Fatalf("read part: %v", err)
	}
	return buf.String()
}

// newPredefTestProject writes a minimal publishable project and chdirs into
// it. extra adds or overrides files, keyed by project-relative slash path.
func newPredefTestProject(t *testing.T, registryURL string, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"bino.toml": fmt.Sprintf(`report-id = "test"

[registry]
url = %q
token = "test-token"

[package]
name = "@acme/kit"
description = "a kit"
tags = ["starter"]
category = "components"
visibility = "private"
`, registryURL),
		"components/table.yaml": `apiVersion: bino.bi/v1
kind: Table
metadata:
  name: "@acme/kit/table"
spec:
  dataset: sales
`,
	}
	for k, v := range extra {
		if v == "" {
			delete(files, k)
			continue
		}
		files[k] = v
	}
	for rel, body := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return dir
}

func execPublish(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newPublishCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestPublishSendsTheManifestAndOnePartPerFile(t *testing.T) {
	srv, capture := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string {
		return `{"package":"@acme/kit","version":"1.0.0","digest":"sha256:d","tag":"latest","kinds":["Table"],"unchanged":false,"files":[],"warnings":[]}`
	})
	newPredefTestProject(t, srv.URL, map[string]string{
		"resources/logo.png": "\x89PNG\r\n\x1a\npayload",
	})

	stdout, err := execPublish(t, "--bump", "patch", "--json")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if capture.auth != "Bearer test-token" {
		t.Errorf("auth = %q", capture.auth)
	}
	m := capture.manifest
	if m.Name != "@acme/kit" || m.Bump != "patch" || m.DryRun {
		t.Errorf("manifest = %+v", m)
	}
	if m.Description != "a kit" || m.Category != "components" || len(m.Tags) != 1 {
		t.Errorf("package metadata not forwarded: %+v", m)
	}
	// The package does not exist yet, so visibility is declared.
	if m.Visibility != "private" {
		t.Errorf("visibility = %q, want private on a first publish", m.Visibility)
	}
	// One part per manifest entry, named by the tree path; the type comes
	// from the extension, as the server recomputes it.
	if len(m.Files) != 2 || len(capture.parts) != 2 {
		t.Fatalf("files = %+v, parts = %v", m.Files, capture.parts)
	}
	byPath := map[string]registry.FileEntry{}
	for _, f := range m.Files {
		byPath[f.Path] = f
	}
	if byPath["components/table.yaml"].Type != registry.FileDocument {
		t.Errorf("yaml classified as %q", byPath["components/table.yaml"].Type)
	}
	if byPath["resources/logo.png"].Type != registry.FileResource {
		t.Errorf("png classified as %q", byPath["resources/logo.png"].Type)
	}
	if !strings.HasPrefix(capture.parts["resources/logo.png"], "\x89PNG") {
		t.Errorf("resource part = %q", capture.parts["resources/logo.png"])
	}
	// Every declared digest must be the one the server would recompute.
	for _, f := range m.Files {
		body := capture.parts[f.Path]
		if err := registry.VerifyFile(registry.FormatTree, f.Type, []byte(body), f.Digest); err != nil {
			t.Errorf("%s: %v", f.Path, err)
		}
	}

	var outcome publishOutcome
	if err := json.Unmarshal([]byte(stdout), &outcome); err != nil {
		t.Fatalf("--json output %q: %v", stdout, err)
	}
	if outcome.Version != "1.0.0" || outcome.Unchanged || outcome.DryRun {
		t.Errorf("outcome = %+v", outcome)
	}
}

func TestPublishDryRunMintsNothing(t *testing.T) {
	srv, capture := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string {
		return `{"dryRun":true,"digest":"sha256:d","version":"1.0.0","files":[{"path":"components/table.yaml","type":"document","digest":"sha256:a"}],"warnings":[]}`
	})
	newPredefTestProject(t, srv.URL, nil)

	stdout, err := execPublish(t, "--dry-run", "--json")
	if err != nil {
		t.Fatalf("publish --dry-run: %v", err)
	}
	if !capture.manifest.DryRun {
		t.Error("the manifest did not carry dryRun")
	}
	var outcome publishOutcome
	if err := json.Unmarshal([]byte(stdout), &outcome); err != nil {
		t.Fatalf("--json output %q: %v", stdout, err)
	}
	if !outcome.DryRun || outcome.Version != "1.0.0" {
		t.Errorf("outcome = %+v", outcome)
	}
}

// Republishing identical content is not a failure: the registry recognizes it
// and names the version that already carries it.
func TestPublishUnchangedSucceeds(t *testing.T) {
	srv, _ := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string {
		return `{"package":"@acme/kit","version":"1.0.0","digest":"sha256:d","tag":"latest","kinds":["Table"],"unchanged":true,"files":[],"warnings":[]}`
	})
	newPredefTestProject(t, srv.URL, nil)

	stdout, err := execPublish(t, "--bump", "patch", "--json")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	var outcome publishOutcome
	if err := json.Unmarshal([]byte(stdout), &outcome); err != nil {
		t.Fatalf("--json output %q: %v", stdout, err)
	}
	if !outcome.Unchanged {
		t.Errorf("outcome = %+v, want unchanged", outcome)
	}
}

// A gate rejection must surface the registry's findings and the bino/engine it
// ran them with, which is what makes version skew visible.
func TestPublishReportsGateSkew(t *testing.T) {
	srv, _ := fakePublishServer(t, http.StatusUnprocessableEntity, func(registry.PublishManifest) string {
		return `{"code":"schema_invalid","message":"document failed schema validation","details":[{"bino":"0.92.5","engine":"1.0.0","findings":[{"severity":"error","rule":"schema","message":"metadata.name is not allowed","file":"components/table.yaml","line":4}]}]}`
	})
	newPredefTestProject(t, srv.URL, nil)

	printed, err := captureStderr(t, func() error {
		_, err := execPublish(t, "--bump", "patch")
		return err
	})
	if err == nil {
		t.Fatal("expected the publish to fail")
	}
	for _, want := range []string{"metadata.name is not allowed", "components/table.yaml:4", "0.92.5", "(registry)", "(here)"} {
		if !strings.Contains(printed, want) {
			t.Errorf("output does not mention %q:\n%s", want, printed)
		}
	}
}

func TestPublishRefusesOutsideAPackageProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("report-id = \"test\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if _, err := execPublish(t, "--bump", "patch"); err == nil ||
		!strings.Contains(err.Error(), "[package]") {
		t.Fatalf("err = %v, want a [package] complaint", err)
	}
}

func TestPublishRefusesCredentialsAndUnpublishableFiles(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "a credential kind",
			files: map[string]string{"components/secret.yaml": "apiVersion: bino.bi/v1\nkind: ConnectionSecret\nmetadata:\n  name: \"@acme/kit/s\"\nspec: {}\n"},
			want:  "credentials",
		},
		{
			// The kind check only sees YAML, so the directory itself has to
			// be the marker — secrets/ is in the default include set.
			name:  "a non-YAML file under secrets/",
			files: map[string]string{"secrets/keys.csv": "user,token\na,b\n"},
			want:  "credentials",
		},
		{
			name:  "a resource type the registry does not accept",
			files: map[string]string{"resources/diagram.svg": "<svg/>"},
			want:  "unsupported resource type",
		},
		{
			name:  "a file more than one directory deep",
			files: map[string]string{"resources/assets/logo.png": "\x89PNG"},
			want:  "one directory deep",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newPredefTestProject(t, "http://example.invalid", tt.files)
			_, err := execPublish(t, "--bump", "patch")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// An included symlink is an exfiltration primitive: the file the author sees
// is not the file that would be uploaded.
func TestPublishRefusesASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	outside := filepath.Join(t.TempDir(), "secret.yaml")
	if err := os.WriteFile(outside, []byte("kind: Secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := newPredefTestProject(t, "http://example.invalid", nil)
	if err := os.Symlink(outside, filepath.Join(dir, "components", "leak.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := execPublish(t, "--bump", "patch"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err = %v, want a symlink refusal", err)
	}
}

// Visibility only takes effect when a package is created, and the registry
// rejects a value that differs from an existing package's — so it must not be
// sent for a package that already exists.
func TestPublishOmitsVisibilityForAnExistingPackage(t *testing.T) {
	srv, capture := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string {
		return `{"package":"@acme/kit","version":"1.0.1","digest":"sha256:d","unchanged":false,"files":[],"warnings":[]}`
	})
	// Override the probe: the package resolves, so it already exists.
	newPredefTestProject(t, srv.URL, nil)
	srv.Config.Handler = withResolve(srv.Config.Handler, http.StatusOK, `{"package":"@acme/kit","version":"1.0.0","digest":"sha256:x","kinds":["Table"],"files":[]}`)

	if _, err := execPublish(t, "--bump", "patch", "--json"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if capture.manifest.Visibility != "" {
		t.Errorf("visibility = %q, want it omitted for an existing package", capture.manifest.Visibility)
	}
}

// An inconclusive probe must not fall through to "send nothing": a first
// publish that silently lands with the registry's default visibility cannot
// be undone.
func TestPublishRefusesWhenVisibilityCannotBeDecided(t *testing.T) {
	srv, _ := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string { return `{}` })
	newPredefTestProject(t, srv.URL, nil)
	srv.Config.Handler = withResolve(srv.Config.Handler, http.StatusServiceUnavailable, `{"code":"validator_unavailable"}`)

	_, err := execPublish(t, "--bump", "patch")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want a refusal to guess the visibility", err)
	}
}

// withResolve overrides the v2 resolve route on an existing handler.
func withResolve(next http.Handler, status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/registry/v2/resolve/") {
			w.WriteHeader(status)
			io.WriteString(w, body) //nolint:errcheck // test handler
			return
		}
		next.ServeHTTP(w, r)
	})
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// was written. The publish command's progress output goes through
// NewOutput(OutputConfig{}), which resolves os.Stderr at call time, so this is
// the only seam without threading a writer through registryProjectSetup.
func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r) //nolint:errcheck // the pipe closes when fn returns
		done <- buf.String()
	}()
	fnErr := fn()
	os.Stderr = orig
	w.Close() //nolint:errcheck // best effort; the reader drains on close
	out := <-done
	r.Close() //nolint:errcheck // best effort
	return out, fnErr
}
