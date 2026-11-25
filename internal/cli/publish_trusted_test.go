package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/bino-bi/bino-plugin-sdk/registryattest"

	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/registry/attesttest"
)

// actionsTokenEndpoint is a fake GitHub Actions OIDC token endpoint. Every
// request gets a new token naming its audience.
type actionsTokenEndpoint struct {
	mu        sync.Mutex
	audiences []string
	auths     []string
}

func (a *actionsTokenEndpoint) requests() ([]string, []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.audiences...), append([]string(nil), a.auths...)
}

// onGitHubActions points the Actions token variables at a fake endpoint.
func onGitHubActions(t *testing.T) *actionsTokenEndpoint {
	t.Helper()
	a := &actionsTokenEndpoint{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.audiences = append(a.audiences, r.URL.Query().Get("audience"))
		a.auths = append(a.auths, r.Header.Get("Authorization"))
		n := len(a.audiences)
		a.mu.Unlock()
		fmt.Fprintf(w, `{"value":"gh-oidc-%d-%s"}`, n, r.URL.Query().Get("audience"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/token?api-version=2.0")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "runner-request-token")
	return a
}

// trustedRegistry adds the OIDC exchange to a fake publish registry and
// records the Authorization of every publish and resolve request.
type trustedRegistry struct {
	mu           sync.Mutex
	status       int // of the exchange; 0 is 200
	exchanged    []string
	packages     []string
	publishAuths []string
	resolveAuths []string
}

func (tr *trustedRegistry) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		switch {
		case r.URL.Path == "/api/registry/auth/oidc/exchange":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
				http.Error(w, "exchange: "+r.Method+" with an Authorization header", http.StatusBadRequest)
				return
			}
			var body struct{ Token, Package string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			tr.exchanged = append(tr.exchanged, body.Token)
			tr.packages = append(tr.packages, body.Package)
			if tr.status != 0 {
				w.WriteHeader(tr.status)
				fmt.Fprintf(w, `{"code":"forbidden","message":"no trusted publisher of @acme matches"}`)
				return
			}
			fmt.Fprintf(w, `{"token":"publish-jwt-%d","expires":"2026-11-25T12:15:00Z","package":%q}`, len(tr.exchanged), body.Package)
			return
		case r.URL.Path == "/api/registry/v2/publish":
			tr.publishAuths = append(tr.publishAuths, r.Header.Get("Authorization"))
		case strings.HasPrefix(r.URL.Path, "/api/registry/v2/resolve/"):
			tr.resolveAuths = append(tr.resolveAuths, r.Header.Get("Authorization"))
		}
		next.ServeHTTP(w, r)
	})
}

// newTrustedKitProject is a public @acme/kit project with no registry token
// anywhere: not in bino.toml, the environment or a stored login.
func newTrustedKitProject(t *testing.T, registryURL, token string) {
	t.Helper()
	isolateTrustHome(t)
	isolateSigningEnv(t)
	t.Setenv(registry.EnvToken, "")
	toml := fmt.Sprintf("report-id = \"test\"\n\n[registry]\nurl = %q\n", registryURL)
	if token != "" {
		toml += fmt.Sprintf("token = %q\n", token)
	}
	toml += "\n[package]\nname = \"@acme/kit\"\nvisibility = \"public\"\nlicense = \"MIT\"\n"
	newPredefTestProject(t, registryURL, map[string]string{
		"bino.toml": toml, "components/table.yaml": "", "components/kit.yaml": signedKitDoc,
	})
}

// assertNoTokens fails when a token value leaks into what bino printed.
func assertNoTokens(t *testing.T, texts ...string) {
	t.Helper()
	for _, s := range texts {
		for _, secret := range []string{"gh-oidc-", "publish-jwt-", "runner-request-token"} {
			if strings.Contains(s, secret) {
				t.Errorf("output carries a token (%s):\n%s", secret, s)
			}
		}
	}
}

// On GitHub Actions without a configured token, publish requests an OIDC
// token for the registry's URL, exchanges it, and publishes with the result.
// The publish token is sent on publish requests only.
func TestPublishUsesTrustedPublishingOnGitHubActions(t *testing.T) {
	srv, capture := fakeSignedPublishServer(t, "public")
	tr := &trustedRegistry{}
	srv.Config.Handler = tr.wrap(srv.Config.Handler)
	// A trailing slash is not part of the audience.
	newTrustedKitProject(t, srv.URL+"/", "")
	actions := onGitHubActions(t)

	var out string
	stderr, err := captureStderr(t, func() error {
		var err error
		out, err = execPublish(t, "--bump", "patch", "--json")
		return err
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	audiences, auths := actions.requests()
	if len(audiences) != 1 || audiences[0] != srv.URL || auths[0] != "Bearer runner-request-token" {
		t.Errorf("token requests: audiences %q, auth %q; want one for %q", audiences, auths, srv.URL)
	}
	if len(tr.exchanged) != 1 || tr.exchanged[0] != "gh-oidc-1-"+srv.URL || tr.packages[0] != "@acme/kit" {
		t.Errorf("exchanged %q for %q", tr.exchanged, tr.packages)
	}
	if capture.calls != 1 || capture.auth != "Bearer publish-jwt-1" {
		t.Errorf("%d publish call(s) with %q, want one with the publish token", capture.calls, capture.auth)
	}
	for _, a := range tr.resolveAuths {
		if a != "" {
			t.Errorf("the visibility probe sent Authorization %q, want none", a)
		}
	}
	assertNoTokens(t, out, stderr)
}

// A dry run also authenticates with the publish token.
func TestPublishDryRunUsesTrustedPublishing(t *testing.T) {
	srv, _ := fakeSignedPublishServer(t, "public")
	tr := &trustedRegistry{}
	srv.Config.Handler = tr.wrap(srv.Config.Handler)
	newTrustedKitProject(t, srv.URL, "")
	onGitHubActions(t)

	out, err := execPublish(t, "--dry-run", "--json")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(tr.publishAuths) != 1 || tr.publishAuths[0] != "Bearer publish-jwt-1" {
		t.Errorf("dry run sent %q, want the publish token", tr.publishAuths)
	}
	assertNoTokens(t, out)
}

// A configured token wins on GitHub Actions; --trusted-publishing skips it.
func TestPublishPrefersAConfiguredToken(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		env         string
		args        []string
		want        string
	}{
		{name: "bino.toml", token: "toml-jwt", want: "Bearer toml-jwt"},
		{name: "environment", env: "env-jwt", want: "Bearer env-jwt"},
		{name: "forced", token: "toml-jwt", args: []string{"--trusted-publishing"}, want: "Bearer publish-jwt-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, capture := fakeSignedPublishServer(t, "public")
			tr := &trustedRegistry{}
			srv.Config.Handler = tr.wrap(srv.Config.Handler)
			newTrustedKitProject(t, srv.URL, tc.token)
			t.Setenv(registry.EnvToken, tc.env)
			actions := onGitHubActions(t)

			if _, err := execPublish(t, append([]string{"--bump", "patch"}, tc.args...)...); err != nil {
				t.Fatalf("publish: %v", err)
			}
			if capture.auth != tc.want {
				t.Errorf("publish sent %q, want %q", capture.auth, tc.want)
			}
			audiences, _ := actions.requests()
			if forced := len(tc.args) > 0; forced != (len(audiences) == 1 && len(tr.exchanged) == 1) {
				t.Errorf("%d token request(s), %d exchange(s)", len(audiences), len(tr.exchanged))
			}
		})
	}
}

// --trusted-publishing outside GitHub Actions fails before any request.
func TestPublishTrustedPublishingNeedsGitHubActions(t *testing.T) {
	srv, capture := fakeSignedPublishServer(t, "public")
	tr := &trustedRegistry{}
	srv.Config.Handler = tr.wrap(srv.Config.Handler)
	newTrustedKitProject(t, srv.URL, "")

	_, err := execPublish(t, "--bump", "patch", "--trusted-publishing")
	if err == nil || !strings.Contains(err.Error(), "GitHub Actions OIDC token") || !strings.Contains(hintOf(err), "id-token: write") {
		t.Fatalf("err = %v (hint %q), want a refusal off GitHub Actions", err, hintOf(err))
	}
	if capture.calls != 0 || len(tr.exchanged) != 0 || len(tr.resolveAuths) != 0 {
		t.Errorf("registry contacted: %d publish, %d exchange, %d resolve", capture.calls, len(tr.exchanged), len(tr.resolveAuths))
	}
}

// Without a configured token or Actions, publish stays as it was: it asks
// for a login.
func TestPublishWithoutCredentialsAsksForALogin(t *testing.T) {
	srv, capture := fakeSignedPublishServer(t, "public")
	newTrustedKitProject(t, srv.URL, "")

	_, err := execPublish(t, "--bump", "patch")
	if err == nil || !strings.Contains(err.Error(), "bino registry login") {
		t.Fatalf("err = %v, want a login request", err)
	}
	if capture.calls != 0 {
		t.Errorf("%d publish call(s), want none", capture.calls)
	}
}

func TestPublishTrustedPublishingErrors(t *testing.T) {
	for _, tc := range []struct {
		status    int
		want, has string
	}{
		{status: http.StatusForbidden, want: "no trusted publisher", has: "trusted publisher in the settings of scope @acme"},
		{status: http.StatusUnauthorized, want: "trusted publishing", has: "not pull request jobs"},
		{status: http.StatusNotFound, want: "does not support Trusted Publishing", has: "upgrade the registry"},
		{status: http.StatusMethodNotAllowed, want: "does not support Trusted Publishing", has: "upgrade the registry"},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			srv, capture := fakeSignedPublishServer(t, "public")
			tr := &trustedRegistry{status: tc.status}
			srv.Config.Handler = tr.wrap(srv.Config.Handler)
			newTrustedKitProject(t, srv.URL, "")
			onGitHubActions(t)

			out, err := execPublish(t, "--bump", "patch", "--json")
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(hintOf(err), tc.has) {
				t.Fatalf("err = %v (hint %q), want %q with hint %q", err, hintOf(err), tc.want, tc.has)
			}
			if capture.calls != 0 {
				t.Errorf("%d publish call(s), want none", capture.calls)
			}
			assertNoTokens(t, out, err.Error(), hintOf(err))
		})
	}
}

// The registry takes each OIDC token once, so every exchange gets its own.
func TestPublishFetchesAFreshOIDCTokenPerExchange(t *testing.T) {
	srv, _ := fakeSignedPublishServer(t, "public")
	tr := &trustedRegistry{}
	srv.Config.Handler = tr.wrap(srv.Config.Handler)
	newTrustedKitProject(t, srv.URL, "")
	actions := onGitHubActions(t)

	for range 2 {
		if _, err := execPublish(t, "--bump", "patch"); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	audiences, _ := actions.requests()
	if len(audiences) != 2 || len(tr.exchanged) != 2 || tr.exchanged[0] == tr.exchanged[1] {
		t.Errorf("%d token request(s), exchanged %q; want a new token per exchange", len(audiences), tr.exchanged)
	}
	if want := []string{"Bearer publish-jwt-1", "Bearer publish-jwt-2"}; strings.Join(tr.publishAuths, ",") != strings.Join(want, ",") {
		t.Errorf("publishes sent %q, want %q", tr.publishAuths, want)
	}
}

// A keyless signature under Trusted Publishing claims the oidc channel with
// the run fields, and its token for Sigstore is a separate one.
func TestPublishKeylessUnderTrustedPublishingClaimsOIDC(t *testing.T) {
	srv, capture := fakeSignedPublishServer(t, "public")
	tr := &trustedRegistry{}
	srv.Config.Handler = tr.wrap(srv.Config.Handler)
	newTrustedKitProject(t, srv.URL, "")
	actions := onGitHubActions(t)
	for k, v := range map[string]string{
		"GITHUB_SERVER_URL":   "https://github.com",
		"GITHUB_REPOSITORY":   "acme/reports",
		"GITHUB_WORKFLOW_REF": "acme/reports/.github/workflows/publish.yml@refs/tags/v1.2.0",
		"GITHUB_REF":          "refs/tags/v1.2.0",
		"GITHUB_SHA":          "0123456789abcdef0123456789abcdef01234567",
		"GITHUB_RUN_ID":       "42",
		"GITHUB_RUN_ATTEMPT":  "2",
	} {
		t.Setenv(k, v)
	}
	kl := attesttest.NewKeyless(t)
	tokens := kl.InstallSigner(t, "https://github.com/acme/reports/.github/workflows/publish.yml@refs/tags/v1.2.0", trustIssuer)

	out, err := execPublish(t, "--bump", "patch", "--sign", "--keyless", "--json")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	audiences, _ := actions.requests()
	if strings.Join(audiences, ",") != srv.URL+",sigstore" {
		t.Errorf("token audiences %q, want the registry then sigstore", audiences)
	}
	if len(*tokens) != 1 || (*tokens)[0] != "gh-oidc-2-sigstore" {
		t.Errorf("Fulcio was asked with %q, want the sigstore token", *tokens)
	}
	if want := []string{"Bearer publish-jwt-1", "Bearer publish-jwt-1"}; strings.Join(tr.publishAuths, ",") != strings.Join(want, ",") {
		t.Errorf("dry run and publish sent %q, want the publish token on both", tr.publishAuths)
	}
	want := registryattest.PublishChannel{
		Channel:    registryattest.ChannelOIDC,
		Repository: "acme/reports",
		Workflow:   ".github/workflows/publish.yml",
		Ref:        "refs/tags/v1.2.0",
		Commit:     "0123456789abcdef0123456789abcdef01234567",
		RunURL:     "https://github.com/acme/reports/actions/runs/42/attempts/2",
	}
	if ch := publishedChannel(t, capture); ch != want {
		t.Errorf("channel = %+v\nwant      %+v", ch, want)
	}
	assertNoTokens(t, out)
}

// --trusted-publishing ignores a bino.toml token whose variable is unset, as
// after a workflow dropped the secret.
func TestPublishForcedTrustedPublishingIgnoresAnUnsetTokenVariable(t *testing.T) {
	srv, capture := fakeSignedPublishServer(t, "public")
	tr := &trustedRegistry{}
	srv.Config.Handler = tr.wrap(srv.Config.Handler)
	newTrustedKitProject(t, srv.URL, "${BINO_TP_TEST_UNSET}")
	onGitHubActions(t)

	if _, err := execPublish(t, "--bump", "patch"); err == nil {
		t.Fatal("publish without --trusted-publishing: want the unset variable to fail")
	}
	if _, err := execPublish(t, "--bump", "patch", "--trusted-publishing"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if capture.auth != "Bearer publish-jwt-1" {
		t.Errorf("publish sent %q, want the publish token", capture.auth)
	}
}

// The anonymous probe cannot see a private package, so Trusted Publishing
// sends "private" without claiming the package is new.
func TestPublishTrustedPublishingSkipsTheFirstPublishNote(t *testing.T) {
	srv, capture := fakeSignedPublishServer(t, "public")
	tr := &trustedRegistry{}
	srv.Config.Handler = tr.wrap(srv.Config.Handler)
	newTrustedKitProject(t, srv.URL, "")
	toml := fmt.Sprintf("report-id = \"test\"\n\n[registry]\nurl = %q\n\n[package]\nname = \"@acme/kit\"\nlicense = \"MIT\"\n", srv.URL)
	if err := os.WriteFile("bino.toml", []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	onGitHubActions(t)

	stderr, err := captureStderr(t, func() error {
		_, err := execPublish(t, "--bump", "patch", "--json")
		return err
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if capture.manifest.Visibility != "private" {
		t.Errorf("visibility = %q, want private", capture.manifest.Visibility)
	}
	if strings.Contains(stderr, "does not exist yet") {
		t.Errorf("the note claims the package is new:\n%s", stderr)
	}
}
