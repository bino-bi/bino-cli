package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveV2ClassifiesATreeAndALegacyVersion(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantFormat string
	}{
		{
			name: "multi file tree",
			body: `{"package":"@acme/kit","version":"1.0.0","digest":"sha256:manifest",
				"kinds":["Table","LayoutPage"],"dependencies":[],
				"files":[{"path":"kit.yaml","type":"document","digest":"sha256:a","size":10},
				         {"path":"resources/logo.png","type":"resource","digest":"sha256:b","size":20}]}`,
			wantFormat: FormatTree,
		},
		{
			// The server renders a v1 version as one document file whose
			// digest IS the version digest, and that file keeps the v1
			// single-document digest rule.
			name: "legacy single document",
			body: `{"package":"@acme/old","version":"2.0.0","digest":"sha256:v1",
				"kinds":["Table"],"dependencies":[],
				"files":[{"path":"old.yml","type":"document","digest":"sha256:v1","size":10}]}`,
			wantFormat: FormatDocument,
		},
		{
			// A genuine one-file tree: the manifest digest differs from the
			// file's, so it must not be mistaken for a v1 version.
			name: "single file tree",
			body: `{"package":"@acme/one","version":"1.0.0","digest":"sha256:manifest",
				"kinds":["Table"],"dependencies":[],
				"files":[{"path":"one.yaml","type":"document","digest":"sha256:a","size":10}]}`,
			wantFormat: FormatTree,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/api/registry/v2/resolve/") {
					t.Errorf("path = %s", r.URL.Path)
				}
				io.WriteString(w, tt.body) //nolint:errcheck // test handler
			}))
			res, err := c.ResolveV2(context.Background(), "acme", "kit", "")
			if err != nil {
				t.Fatalf("ResolveV2: %v", err)
			}
			if res.Format != tt.wantFormat {
				t.Errorf("format = %s, want %s", res.Format, tt.wantFormat)
			}
			if c.v2.Load() != v2Supported {
				t.Error("a successful v2 resolve should record v2 support")
			}
		})
	}
}

// A registry without the v2 routes answers with a bare router 404, which the
// client must read as "no v2 here" and not as "package missing".
func TestResolveV2RecordsAnAbsentRoute(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	if _, err := c.ResolveV2(context.Background(), "acme", "kit", ""); err == nil {
		t.Fatal("expected an error")
	}
	if c.v2.Load() != v2Absent {
		t.Fatalf("v2 state = %d, want v2Absent", c.v2.Load())
	}
	if c.SupportsV2(context.Background(), "acme", "kit", "") {
		t.Error("SupportsV2 = true after a 404 route probe")
	}
}

// A 404 that carries a registry error code is a real answer from a real v2
// route, so it must not disable v2 for the rest of the closure.
func TestResolveV2KeepsV2AfterAPackageNotFound(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"code":"package_not_found","message":"package not found"}`) //nolint:errcheck // test handler
	}))
	if _, err := c.ResolveV2(context.Background(), "acme", "kit", ""); err == nil {
		t.Fatal("expected an error")
	}
	if c.v2.Load() == v2Absent {
		t.Error("a package_not_found must not be read as a missing v2 route")
	}
}

func TestDownloadFileBuildsTheURLLocally(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("ETag", `"sha256:abc"`)
		io.WriteString(w, "body") //nolint:errcheck // test handler
	}))
	body, digest, err := c.DownloadFile(context.Background(), "acme", "kit", "1.0.0", "components/sales.yaml")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if want := "/api/registry/v2/files/acme/kit/1.0.0/components/sales.yaml"; gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
	if string(body) != "body" || digest != "sha256:abc" {
		t.Errorf("body = %q, digest = %q", body, digest)
	}
}

func TestDownloadFileRejectsATraversalPath(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the client must not reach the network for %s", r.URL.Path)
	}))
	if _, _, err := c.DownloadFile(context.Background(), "acme", "kit", "1.0.0", "../../etc/passwd"); err == nil {
		t.Fatal("expected the path to be rejected")
	}
}

func TestPackageExistsClassifiesTheAnswer(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		exists, know bool
	}{
		{"resolves", 200, `{"package":"@acme/kit"}`, true, true},
		{"package missing", 404, `{"code":"package_not_found"}`, false, true},
		{"scope missing", 404, `{"code":"scope_not_found"}`, false, true},
		{"exists but has no version", 404, `{"code":"version_not_found"}`, true, true},
		{"forbidden is inconclusive", 403, `{"code":"forbidden"}`, false, false},
		{"server error is inconclusive", 503, `{"code":"validator_unavailable"}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body) //nolint:errcheck // test handler
			}))
			exists, known := c.PackageExists(context.Background(), "acme", "kit")
			if exists != tt.exists || known != tt.know {
				t.Errorf("= (%v, %v), want (%v, %v)", exists, known, tt.exists, tt.know)
			}
		})
	}
}

// The multipart shape is a wire contract: "manifest" must arrive as a form
// VALUE, every file part's NAME must be its tree path, and the manifest's file
// list must match the parts.
func TestPublishSendsTheContractedMultipartShape(t *testing.T) {
	var (
		gotManifest PublishManifest
		gotFiles    = map[string]string{}
		gotAuth     string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		if err := json.Unmarshal([]byte(r.FormValue("manifest")), &gotManifest); err != nil {
			t.Errorf("manifest: %v", err)
		}
		for name, headers := range r.MultipartForm.File {
			f, err := headers[0].Open()
			if err != nil {
				t.Errorf("open %s: %v", name, err)
				continue
			}
			var buf bytes.Buffer
			io.Copy(&buf, f) //nolint:errcheck // test handler
			f.Close()        //nolint:errcheck // test handler
			gotFiles[name] = buf.String()
		}
		io.WriteString(w, `{"package":"@acme/kit","version":"1.0.1","digest":"sha256:d","tag":"latest","kinds":["Table"],"unchanged":false,"files":[],"warnings":[]}`) //nolint:errcheck // test handler
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{URL: srv.URL, Token: "jwt-token"})

	m := PublishManifest{
		Name: "@acme/kit", Bump: "patch",
		Files: []FileEntry{
			{Path: "kit.yaml", Type: FileDocument, Digest: "sha256:a"},
			{Path: "resources/logo.png", Type: FileResource, Digest: "sha256:b"},
		},
	}
	files := []PublishFile{
		{Path: "resources/logo.png", Open: openString("png-bytes")},
		{Path: "kit.yaml", Open: openString("kind: Table")},
	}
	res, err := c.Publish(context.Background(), m, files)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Version != "1.0.1" {
		t.Errorf("version = %s", res.Version)
	}
	if gotAuth != "Bearer jwt-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotManifest.Name != "@acme/kit" || gotManifest.Bump != "patch" || gotManifest.DryRun {
		t.Errorf("manifest = %+v", gotManifest)
	}
	if len(gotFiles) != 2 || gotFiles["kit.yaml"] != "kind: Table" || gotFiles["resources/logo.png"] != "png-bytes" {
		t.Errorf("file parts = %v", gotFiles)
	}
}

func TestPublishDryRunDecodesTheOtherResponseShape(t *testing.T) {
	var gotDryRun bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		var m PublishManifest
		json.Unmarshal([]byte(r.FormValue("manifest")), &m) //nolint:errcheck // test handler
		gotDryRun = m.DryRun
		io.WriteString(w, `{"dryRun":true,"digest":"sha256:d","version":"1.0.1","files":[{"path":"kit.yaml","type":"document","digest":"sha256:a"}],"warnings":[]}`) //nolint:errcheck // test handler
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{URL: srv.URL, Token: "jwt"})

	res, err := c.PublishDryRun(context.Background(), PublishManifest{Name: "@acme/kit"}, nil)
	if err != nil {
		t.Fatalf("PublishDryRun: %v", err)
	}
	if !gotDryRun {
		t.Error("the manifest did not carry dryRun")
	}
	if !res.DryRun || res.Version != "1.0.1" || len(res.Files) != 1 {
		t.Errorf("result = %+v", res)
	}
}

func TestPublishRefusesWithoutAToken(t *testing.T) {
	c := NewClient(Config{URL: "http://example.invalid"})
	if _, err := c.Publish(context.Background(), PublishManifest{Name: "@acme/kit"}, nil); err == nil {
		t.Fatal("an anonymous publish must be refused before any request")
	}
}

// A schema_invalid rejection carries the gate's own bino/engine next to its
// findings, so the CLI can show both sides of a version skew.
func TestGateDetailsDecodeFromASchemaInvalidError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"code":"schema_invalid","message":"document failed schema validation",
			"details":[{"bino":"0.92.5","engine":"1.0.0","findings":[{"severity":"error","rule":"schema","message":"bad name","file":"kit.yaml","line":3}]}]}`) //nolint:errcheck // test handler
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{URL: srv.URL, Token: "jwt"})

	_, err := c.Publish(context.Background(), PublishManifest{Name: "@acme/kit"}, nil)
	var apiErr *APIError
	if !errorsAs(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Code != "schema_invalid" {
		t.Fatalf("code = %s", apiErr.Code)
	}
	details := apiErr.GateDetails()
	if len(details) != 1 || details[0].Bino != "0.92.5" || details[0].Engine != "1.0.0" {
		t.Fatalf("details = %+v", details)
	}
	if len(details[0].Findings) != 1 || details[0].Findings[0].Message != "bad name" {
		t.Fatalf("findings = %+v", details[0].Findings)
	}
}

func errorsAs(err error, target **APIError) bool { return errors.As(err, target) }

func openString(s string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(s)), nil }
}
