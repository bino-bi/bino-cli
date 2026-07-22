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

	"bino.bi/bino/internal/registry"
)

const (
	fakeExchangeJWT = "exchanged-jwt"
	fakePAT         = "bino_pat_0123456789012345678901234567890123456789"
)

type fakeAuthState struct {
	exchangeCalls atomic.Int64
	revoked       []string
	failRevoke    bool
}

// fakeAuthServer implements the auth endpoints the login/logout commands
// drive (PAT exchange + revocation).
func fakeAuthServer(t *testing.T, state *fakeAuthState) *httptest.Server {
	t.Helper()
	requireAuth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+fakeExchangeJWT {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"code":"unauthorized","message":"authentication required"}`))
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/registry/auth/pat/exchange", func(w http.ResponseWriter, r *http.Request) {
		state.exchangeCalls.Add(1)
		var req struct{ Token string }
		json.NewDecoder(r.Body).Decode(&req)
		if req.Token != fakePAT {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"code":"invalid_token","message":"invalid or expired token"}`))
			return
		}
		w.Write([]byte(`{"token":"` + fakeExchangeJWT + `","expires":"2026-01-01T00:00:00Z","id":"pat1"}`))
	})
	mux.HandleFunc("DELETE /api/registry/auth/pat/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		if state.failRevoke {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"code":"internal","message":"boom"}`))
			return
		}
		state.revoked = append(state.revoked, r.PathValue("id"))
		w.Write([]byte(`{"status":"ok"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// authTestEnv isolates HOME (credentials, global config), the registry env
// vars, and cwd (no project).
func authTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(registry.EnvToken, "")
	t.Setenv(registry.EnvURL, "")
	t.Chdir(t.TempDir())
}

func TestAuthRegistryURLResolution(t *testing.T) {
	const globalToml = "[registry]\nurl = \"https://global.example.com\"\n"
	tests := []struct {
		name    string
		flag    string
		project string // bino.toml [registry].url; empty = no [registry] table
		env     string
		global  string // ~/.bino/config.toml content; empty = no file
		want    string
		wantErr bool
	}{
		{name: "flag wins over everything", flag: "https://flag.example.com/", project: "https://project.example.com", env: "https://env.example.com", global: globalToml, want: "https://flag.example.com"},
		{name: "project beats env and global", project: "https://project.example.com", env: "https://env.example.com", global: globalToml, want: "https://project.example.com"},
		{name: "env beats global", env: "https://env.example.com", global: globalToml, want: "https://env.example.com"},
		{name: "global beats default", global: globalToml, want: "https://global.example.com"},
		{name: "default when nothing set", want: registry.DefaultURL},
		{name: "malformed global errors", global: "[[registry\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv(registry.EnvURL, tt.env)
			if tt.global != "" {
				if err := os.MkdirAll(filepath.Join(home, ".bino"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, ".bino", "config.toml"), []byte(tt.global), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			dir := t.TempDir()
			if tt.project != "" {
				content := fmt.Sprintf("report-id = \"test\"\n\n[registry]\nurl = %q\n", tt.project)
				if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(dir)

			got, err := authRegistryURL(tt.flag)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func runRegistryIn(t *testing.T, stdin string, args ...string) error {
	t.Helper()
	cmd := newRegistryCommand()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.ExecuteContext(context.Background())
}

func storedCredential(t *testing.T, url string) (registry.Credential, bool) {
	t.Helper()
	creds, err := registry.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return creds.Get(url)
}

func TestRegistryLoginWithToken(t *testing.T) {
	authTestEnv(t)
	state := &fakeAuthState{}
	srv := fakeAuthServer(t, state)

	if err := runRegistryIn(t, fakePAT+"\n", "login", "--registry", srv.URL, "--with-token"); err != nil {
		t.Fatalf("login --with-token: %v", err)
	}
	if state.exchangeCalls.Load() != 1 {
		t.Errorf("exchange calls = %d, want 1 (validation)", state.exchangeCalls.Load())
	}
	cred, ok := storedCredential(t, srv.URL)
	if !ok || cred.PAT != fakePAT || cred.PATID != "pat1" {
		t.Errorf("stored credential = %+v, %v", cred, ok)
	}
	path, _ := registry.CredentialsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("credentials mode = %o, want 0600", info.Mode().Perm())
	}

	// A bogus token is rejected and not stored.
	if err := runRegistryIn(t, "not-a-pat\n", "login", "--registry", srv.URL, "--with-token"); err == nil {
		t.Fatal("expected rejection of a non-PAT token")
	}
}

func TestRegistryLoginNonInteractiveEmptyStdin(t *testing.T) {
	authTestEnv(t)
	srv := fakeAuthServer(t, &fakeAuthState{})
	// Non-interactive with no piped token: readPAT reads empty stdin and rejects
	// it as not-a-token before any request.
	err := runRegistryIn(t, "", "login", "--registry", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "personal access token") {
		t.Fatalf("expected personal-access-token guidance, got %v", err)
	}
	if _, ok := storedCredential(t, srv.URL); ok {
		t.Error("credential stored despite empty token")
	}
}

func TestRegistryLogout(t *testing.T) {
	authTestEnv(t)
	state := &fakeAuthState{}
	srv := fakeAuthServer(t, state)

	if err := runRegistryIn(t, fakePAT+"\n", "login", "--registry", srv.URL, "--with-token"); err != nil {
		t.Fatal(err)
	}
	if err := runRegistryIn(t, "", "logout", "--registry", srv.URL); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, ok := storedCredential(t, srv.URL); ok {
		t.Error("credential still stored after logout")
	}
	if len(state.revoked) != 1 || state.revoked[0] != "pat1" {
		t.Errorf("revoked = %v, want [pat1]", state.revoked)
	}

	// Logging out again is a friendly no-op.
	if err := runRegistryIn(t, "", "logout", "--registry", srv.URL); err != nil {
		t.Errorf("second logout: %v", err)
	}
}

func TestRegistryLogoutRemovesEvenWhenRevokeFails(t *testing.T) {
	authTestEnv(t)
	state := &fakeAuthState{failRevoke: true}
	srv := fakeAuthServer(t, state)

	if err := runRegistryIn(t, fakePAT+"\n", "login", "--registry", srv.URL, "--with-token"); err != nil {
		t.Fatal(err)
	}
	if err := runRegistryIn(t, "", "logout", "--registry", srv.URL); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, ok := storedCredential(t, srv.URL); ok {
		t.Error("credential must be removed even when server revocation fails")
	}
}
