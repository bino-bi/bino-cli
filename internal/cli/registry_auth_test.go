package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"bino.bi/bino/internal/registry"
)

const (
	fakeSessionJWT  = "session-jwt"
	fakeExchangeJWT = "exchanged-jwt"
	fakePAT         = "bino_pat_0123456789012345678901234567890123456789"
)

type fakeAuthState struct {
	exchangeCalls atomic.Int64
	revoked       []string
	failRevoke    bool
	mfaID         string // non-empty: password auth demands TOTP
}

// fakeAuthServer implements the auth endpoints the login/logout/token
// commands drive.
func fakeAuthServer(t *testing.T, state *fakeAuthState) *httptest.Server {
	t.Helper()
	requireAuth := func(w http.ResponseWriter, r *http.Request) bool {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+fakeSessionJWT && auth != "Bearer "+fakeExchangeJWT {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"code":"unauthorized","message":"authentication required"}`))
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Identity, Password string }
		json.NewDecoder(r.Body).Decode(&req)
		if req.Password != "password123" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message":"Failed to authenticate."}`))
			return
		}
		if state.mfaID != "" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"mfaId":"` + state.mfaID + `"}`))
			return
		}
		w.Write([]byte(`{"token":"` + fakeSessionJWT + `"}`))
	})
	mux.HandleFunc("POST /api/registry/auth/totp/verify", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ MfaID, Code string }
		json.NewDecoder(r.Body).Decode(&req)
		if req.MfaID != state.mfaID || req.Code != "123456" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"code":"invalid_totp","message":"invalid authenticator code"}`))
			return
		}
		w.Write([]byte(`{"token":"` + fakeSessionJWT + `"}`))
	})
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
	mux.HandleFunc("POST /api/registry/auth/pat", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		var req struct {
			Name      string `json:"name"`
			ExpiresIn int64  `json:"expiresIn"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		expires := ""
		if req.ExpiresIn > 0 {
			expires = "2026-10-10 00:00:00.000Z"
		}
		w.Write([]byte(`{"id":"pat1","name":"` + req.Name + `","token":"` + fakePAT + `","prefix":"bino_pat_01234567","expires":"` + expires + `","created":"2026-07-12 10:00:00.000Z"}`))
	})
	mux.HandleFunc("GET /api/registry/auth/pat", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		w.Write([]byte(`{"items":[{"id":"pat1","name":"ci","prefix":"bino_pat_01234567","created":"2026-07-12 10:00:00.000Z","lastUsed":"","expires":""}]}`))
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

// authTestEnv isolates HOME (credentials) and cwd (no project).
func authTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(registry.EnvToken, "")
	t.Chdir(t.TempDir())
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

func TestRegistryLoginPasswordStdin(t *testing.T) {
	authTestEnv(t)
	state := &fakeAuthState{}
	srv := fakeAuthServer(t, state)

	err := runRegistryIn(t, "password123\n", "login", "--registry", srv.URL, "--email", "dev@example.com", "--password-stdin", "--expires", "90d")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	cred, ok := storedCredential(t, srv.URL)
	if !ok || cred.PAT != fakePAT || cred.PATID != "pat1" || cred.User != "dev@example.com" {
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
}

func TestRegistryLoginWrongPassword(t *testing.T) {
	authTestEnv(t)
	srv := fakeAuthServer(t, &fakeAuthState{})

	err := runRegistryIn(t, "wrong\n", "login", "--registry", srv.URL, "--email", "dev@example.com", "--password-stdin")
	if err == nil {
		t.Fatal("expected login failure")
	}
	if _, ok := storedCredential(t, srv.URL); ok {
		t.Error("credential stored despite failed login")
	}
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

	// A bogus token is rejected and not stored.
	if err := runRegistryIn(t, "not-a-pat\n", "login", "--registry", srv.URL, "--with-token"); err == nil {
		t.Fatal("expected rejection of a non-PAT token")
	}
}

func TestRegistryLoginNonInteractiveNeedsFlags(t *testing.T) {
	authTestEnv(t)
	srv := fakeAuthServer(t, &fakeAuthState{})
	err := runRegistryIn(t, "", "login", "--registry", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "non-interactive") {
		t.Fatalf("expected non-interactive guidance, got %v", err)
	}
}

func TestRegistryLoginMFANonInteractive(t *testing.T) {
	authTestEnv(t)
	srv := fakeAuthServer(t, &fakeAuthState{mfaID: "mfa1"})
	err := runRegistryIn(t, "password123\n", "login", "--registry", srv.URL, "--email", "dev@example.com", "--password-stdin")
	if err == nil || !strings.Contains(err.Error(), "authenticator code") {
		t.Fatalf("expected MFA guidance, got %v", err)
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

func TestRegistryTokenCommands(t *testing.T) {
	authTestEnv(t)
	state := &fakeAuthState{}
	srv := fakeAuthServer(t, state)

	// Authenticate via the stored-credential chain.
	if err := runRegistryIn(t, fakePAT+"\n", "login", "--registry", srv.URL, "--with-token"); err != nil {
		t.Fatal(err)
	}

	if err := runRegistryIn(t, "", "token", "create", "--registry", srv.URL, "--name", "ci", "--expires", "90d"); err != nil {
		t.Fatalf("token create: %v", err)
	}
	if err := runRegistryIn(t, "", "token", "list", "--registry", srv.URL); err != nil {
		t.Fatalf("token list: %v", err)
	}
	if err := runRegistryIn(t, "", "token", "revoke", "pat1", "--registry", srv.URL); err != nil {
		t.Fatalf("token revoke: %v", err)
	}
	if len(state.revoked) == 0 || state.revoked[len(state.revoked)-1] != "pat1" {
		t.Errorf("revoked = %v", state.revoked)
	}
}

func TestRegistryTokenRequiresAuth(t *testing.T) {
	authTestEnv(t)
	srv := fakeAuthServer(t, &fakeAuthState{})
	err := runRegistryIn(t, "", "token", "list", "--registry", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("expected not-logged-in error, got %v", err)
	}
}

func TestParseExpiry(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "", want: 0},
		{in: "never", want: 0},
		{in: "90d", want: 90 * 24 * 3600},
		{in: "1d", want: 86400},
		{in: "12h", want: 12 * 3600},
		{in: "30m", want: 1800},
		{in: "0d", wantErr: true},
		{in: "-5d", wantErr: true},
		{in: "-12h", wantErr: true},
		{in: "soon", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseExpiry(tc.in)
		if tc.wantErr != (err != nil) || got != tc.want {
			t.Errorf("parseExpiry(%q) = %d, %v; want %d, wantErr=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}
