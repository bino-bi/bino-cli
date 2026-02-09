package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPlatformKey(t *testing.T) {
	key, err := platformKey()
	if err != nil {
		t.Skipf("unsupported platform for test: %v", err)
	}

	expected := map[string]string{
		"darwin/arm64":  "mac-arm64",
		"darwin/amd64":  "mac-x64",
		"linux/amd64":   "linux64",
		"windows/amd64": "win64",
		"windows/386":   "win32",
	}

	goos := runtime.GOOS + "/" + runtime.GOARCH
	want, ok := expected[goos]
	if !ok {
		t.Skipf("no expected value for %s", goos)
	}
	if key != want {
		t.Errorf("platformKey() = %q, want %q", key, want)
	}
}

func TestResolveExecPath_EnvVar(t *testing.T) {
	// Create a temp file to act as the chrome binary
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "chrome-headless-shell")
	if err := os.WriteFile(fakeBin, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CHROME_PATH", fakeBin)

	mgr := NewManagerWithClient(t.TempDir(), http.DefaultClient)
	got, err := mgr.ResolveExecPath()
	if err != nil {
		t.Fatalf("ResolveExecPath() error = %v", err)
	}
	if got != fakeBin {
		t.Errorf("ResolveExecPath() = %q, want %q", got, fakeBin)
	}
}

func TestResolveExecPath_EnvVar_InvalidPath(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/path/chrome")

	mgr := NewManagerWithClient(t.TempDir(), http.DefaultClient)
	_, err := mgr.ResolveExecPath()
	if err == nil {
		t.Fatal("ResolveExecPath() should return error for nonexistent CHROME_PATH")
	}
}

func TestResolveExecPath_CachedVersion(t *testing.T) {
	cacheDir := t.TempDir()

	// Create a fake cached version
	versionDir := filepath.Join(cacheDir, "133.0.6943.53")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binName := "chrome-headless-shell"
	if runtime.GOOS == "windows" {
		binName = "chrome-headless-shell.exe"
	}
	fakeBin := filepath.Join(versionDir, binName)
	if err := os.WriteFile(fakeBin, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CHROME_PATH", "") // ensure env var is unset

	mgr := NewManagerWithClient(cacheDir, http.DefaultClient)
	got, err := mgr.ResolveExecPath()
	if err != nil {
		t.Fatalf("ResolveExecPath() error = %v", err)
	}
	if got != fakeBin {
		t.Errorf("ResolveExecPath() = %q, want %q", got, fakeBin)
	}
}

func TestResolveExecPath_NotFound(t *testing.T) {
	t.Setenv("CHROME_PATH", "")

	mgr := NewManagerWithClient(t.TempDir(), http.DefaultClient)
	_, err := mgr.ResolveExecPath()
	if err == nil {
		t.Fatal("ResolveExecPath() should return error when no binary is available")
	}
}

func TestListLocalVersions(t *testing.T) {
	cacheDir := t.TempDir()

	binName := "chrome-headless-shell"
	if runtime.GOOS == "windows" {
		binName = "chrome-headless-shell.exe"
	}

	// Create two fake cached versions
	for _, v := range []string{"130.0.6723.58", "133.0.6943.53"} {
		vDir := filepath.Join(cacheDir, v)
		if err := os.MkdirAll(vDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vDir, binName), []byte("fake"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create a directory without a binary (should be skipped)
	if err := os.MkdirAll(filepath.Join(cacheDir, "incomplete"), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := NewManagerWithClient(cacheDir, http.DefaultClient)
	versions, err := mgr.ListLocalVersions()
	if err != nil {
		t.Fatalf("ListLocalVersions() error = %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("ListLocalVersions() returned %d versions, want 2", len(versions))
	}
	// Should be sorted newest first
	if versions[0].Version != "133.0.6943.53" {
		t.Errorf("first version = %q, want %q", versions[0].Version, "133.0.6943.53")
	}
	if versions[1].Version != "130.0.6723.58" {
		t.Errorf("second version = %q, want %q", versions[1].Version, "130.0.6723.58")
	}
}

func TestFetchLatestStableVersion(t *testing.T) {
	platform, err := platformKey()
	if err != nil {
		t.Skipf("unsupported platform: %v", err)
	}

	resp := lastKnownGoodResponse{}
	resp.Channels.Stable.Version = "133.0.6943.53"
	resp.Channels.Stable.Downloads.ChromeHeadlessShell = []downloadEntry{
		{Platform: platform, URL: "https://example.com/chrome-headless-shell.zip"},
	}
	body, _ := json.Marshal(resp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	mgr := NewManagerWithClient(t.TempDir(), srv.Client())
	// Override the URL by using a custom client that rewrites requests
	origFetch := mgr.httpClient
	mgr.httpClient = &http.Client{
		Transport: &rewriteTransport{base: origFetch.Transport, targetURL: srv.URL},
	}

	version, err := mgr.FetchLatestStableVersion(context.Background())
	if err != nil {
		t.Fatalf("FetchLatestStableVersion() error = %v", err)
	}
	if version != "133.0.6943.53" {
		t.Errorf("FetchLatestStableVersion() = %q, want %q", version, "133.0.6943.53")
	}
}

// rewriteTransport redirects all requests to a test server.
type rewriteTransport struct {
	base      http.RoundTripper
	targetURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.targetURL[len("http://"):]
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
