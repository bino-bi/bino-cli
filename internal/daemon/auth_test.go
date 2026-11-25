package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
)

// startAuthServer runs a real daemon listener with a fake /mcp handler.
func startAuthServer(t *testing.T) *Server {
	t.Helper()
	mcp := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv, err := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0", MCPHandler: mcp})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Start(ctx) }()
	return srv
}

func doAuth(t *testing.T, srv *Server, method, path string, header map[string]string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, fmt.Sprintf("http://127.0.0.1:%d%s", srv.Port(), path), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestAuthRequiresToken(t *testing.T) {
	srv := startAuthServer(t)
	good := map[string]string{"Authorization": "Bearer " + srv.Token()}

	tests := []struct {
		name   string
		method string
		path   string
		header map[string]string
		want   int
	}{
		{"no token", http.MethodGet, "/health", nil, http.StatusUnauthorized},
		{"wrong token", http.MethodGet, "/health", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"token without scheme", http.MethodGet, "/health", map[string]string{"Authorization": srv.Token()}, http.StatusUnauthorized},
		{"token in query is ignored", http.MethodGet, "/health?token=" + srv.Token(), nil, http.StatusUnauthorized},
		{"right token", http.MethodGet, "/health", good, http.StatusOK},
		{"shutdown without token", http.MethodPost, "/shutdown", nil, http.StatusUnauthorized},
		{"mcp without token", http.MethodPost, "/mcp", nil, http.StatusUnauthorized},
		{"mcp with token", http.MethodPost, "/mcp", good, http.StatusOK},
		{"events without token", http.MethodGet, "/events", nil, http.StatusUnauthorized},
		{"browser origin", http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer " + srv.Token(), "Origin": "https://evil.example"}, http.StatusForbidden},
		{"webview origin", http.MethodGet, "/health", map[string]string{"Authorization": "Bearer " + srv.Token(), "Origin": "vscode-webview://abc"}, http.StatusForbidden},
		{"rebound host", http.MethodGet, "/health", map[string]string{"Authorization": "Bearer " + srv.Token(), "Host": fmt.Sprintf("evil.example:%d", srv.Port())}, http.StatusForbidden},
		{"localhost host", http.MethodGet, "/health", map[string]string{"Authorization": "Bearer " + srv.Token(), "Host": fmt.Sprintf("localhost:%d", srv.Port())}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := doAuth(t, srv, tt.method, tt.path, tt.header); got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTokensDiffer(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != 64 {
		t.Fatalf("tokens must be random 32-byte hex: %q %q", a, b)
	}
}

func TestLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:4000": true, "localhost:4000": true, "[::1]:4000": true, "LOCALHOST": true,
		"evil.example:4000": false, "10.0.0.1:4000": false, "": false,
	} {
		if got := loopbackHost(host); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestPortFileHoldsTokenPrivately(t *testing.T) {
	dir := t.TempDir()
	// An older daemon left a world-readable file; the rewrite must narrow it.
	if err := os.MkdirAll(dir+"/.bino", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PortFilePath(dir), []byte("{}"), 0o644); err != nil { //nolint:gosec // test setup
		t.Fatal(err)
	}
	if err := WritePortFile(dir, 4000, "s3cret"); err != nil {
		t.Fatalf("WritePortFile: %v", err)
	}
	pf, err := ReadPortFile(dir)
	if err != nil || pf == nil {
		t.Fatalf("ReadPortFile: %v %v", pf, err)
	}
	if pf.Token != "s3cret" {
		t.Errorf("token = %q, want s3cret", pf.Token)
	}
	if runtime.GOOS == "windows" {
		return // no unix permission bits
	}
	info, err := os.Stat(PortFilePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("port file mode = %o, want 600", mode)
	}
}

func TestConfiguredTokenAndReadOnlyRoutes(t *testing.T) {
	srv, err := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0", Token: "fixed", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Start(ctx) }()

	if got := doAuth(t, srv, "GET", "/health", map[string]string{"Authorization": "Bearer other"}); got != http.StatusUnauthorized {
		t.Errorf("other token: %d", got)
	}
	auth := map[string]string{"Authorization": "Bearer fixed"}
	for _, route := range []string{
		"POST /introspect-draft", "POST /preview-dataset", "GET /registry/packages", "GET /registry/search",
		"GET /registry/info", "POST /preview/start", "POST /preview/stop", "GET /preview/status", "POST /build",
	} {
		method, path, _ := strings.Cut(route, " ")
		if got := doAuth(t, srv, method, path, auth); got != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", route, got)
		}
	}
}
