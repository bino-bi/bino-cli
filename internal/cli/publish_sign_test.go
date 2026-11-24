package cli

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bino-bi/bino-plugin-sdk/registryattest"

	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/registry/attesttest"
)

const signedKitDoc = "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: \"@acme/kit\"\nspec:\n  value: main\n"

// isolateSigningEnv keeps the CI's own OIDC token and run out of a test.
func isolateSigningEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envSigstoreIDToken, "ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"GITHUB_SERVER_URL", "GITHUB_REPOSITORY", "GITHUB_WORKFLOW_REF", "GITHUB_REF", "GITHUB_SHA", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT"} {
		t.Setenv(k, "")
	}
}

// fakeSignedPublishServer answers a dry run with the version 1.0.0, the
// digest of the manifest it got and visibility, and a publish with that
// version.
func fakeSignedPublishServer(t *testing.T, visibility string) (*httptest.Server, *publishCapture) {
	t.Helper()
	return fakePublishServer(t, http.StatusOK, func(m registry.PublishManifest) string {
		d, _, err := registry.ManifestDigest(m.Files)
		if err != nil {
			t.Errorf("manifest digest: %v", err)
		}
		if m.DryRun {
			return fmt.Sprintf(`{"dryRun":true,"digest":%q,"version":"1.0.0","visibility":%q,"files":[],"warnings":[]}`, d, visibility)
		}
		return fmt.Sprintf(`{"package":"@acme/kit","version":"1.0.0","digest":%q,"tag":"latest","kinds":["Text"],"files":[],"warnings":[]}`, d)
	})
}

func newSignedKitProject(t *testing.T, registryURL string) string {
	t.Helper()
	return newPredefTestProject(t, registryURL, map[string]string{"components/table.yaml": "", "components/kit.yaml": signedKitDoc})
}

func writeKeyPEM(t *testing.T, path string, key crypto.Signer) {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePublicKeyPEM(t *testing.T, path string, pub crypto.PublicKey) {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// installPublished serves what the fake registry received as @acme/kit and
// adds it to a fresh project under the [trust] rule, which require = "signed"
// makes the verifier enforce.
func installPublished(t *testing.T, capture *publishCapture, rootKind, rule string, files map[string][]byte) *registry.Entry {
	t.Helper()
	kit := &fakePackage{tag: "latest", version: "1.0.0", kind: "Text", kinds: []string{"Text"}}
	for path, body := range capture.parts {
		kit.tree = append(kit.tree, fakeTreeFile{path: path, body: []byte(body)})
	}
	kit.attestations = []registry.Attestation{attesttest.Attestation("publish", rootKind, json.RawMessage(capture.attestation))}
	srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{"@acme/kit": kit})
	dir := newRegistryTestProject(t, srv.URL)
	for rel, body := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	appendTrust(t, dir, "[trust]\nrequire = \"signed\"\n[[trust.signer]]\nscope = \"@acme/*\"\n"+rule+"\n")
	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add the signed package: %v", err)
	}
	return lockedKit(t, dir)
}

// publishedChannel checks the statement the publish signed names exactly the
// release the registry got, and returns its channel record.
func publishedChannel(t *testing.T, capture *publishCapture) registryattest.PublishChannel {
	t.Helper()
	var b struct {
		DsseEnvelope struct {
			Payload string `json:"payload"`
		} `json:"dsseEnvelope"`
	}
	if err := json.Unmarshal([]byte(capture.attestation), &b); err != nil {
		t.Fatal(err)
	}
	payload, err := base64.StdEncoding.DecodeString(b.DsseEnvelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	digest, _, err := registry.ManifestDigest(capture.manifest.Files)
	if err != nil {
		t.Fatal(err)
	}
	if err := registryattest.CheckStatement(payload, "acme", "kit", "1.0.0", digest); err != nil {
		t.Fatalf("statement: %v", err)
	}
	ch, err := registryattest.ReadPublishChannel(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// Each signing model: publish signs the version and digest the dry run
// reported, and the existing verifier accepts the bundle on install.
func TestPublishSignedInstallsAndVerifies(t *testing.T) {
	isolateTrustHome(t)
	isolateSigningEnv(t)
	kl := attesttest.NewKeyless(t)
	company := attesttest.NewCA(t)
	_, edKey, _ := ed25519.GenerateKey(rand.Reader)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rootPEM := filepath.Join(t.TempDir(), "root.pem")
	company.WriteRoot(t, rootPEM)
	rootBytes, _ := os.ReadFile(rootPEM)

	pubPEM := func(pub crypto.PublicKey) []byte {
		p := filepath.Join(t.TempDir(), "k.pub")
		writePublicKeyPEM(t, p, pub)
		b, _ := os.ReadFile(p)
		return b
	}
	fp := func(pub crypto.PublicKey) string { s, _ := registry.KeyFingerprint(pub); return s }

	for _, tc := range []struct {
		name, visibility, rootKind, signer, rule string
		args                                     func(t *testing.T, dir string) []string
		trustFiles                               map[string][]byte
	}{
		{
			name: "keyless", visibility: "public", rootKind: "keyless", signer: "alice@example.com",
			rule: "oidc-issuer = \"" + trustIssuer + "\"\nidentity = \"alice@example.com\"",
			args: func(t *testing.T, _ string) []string {
				kl.InstallSigner(t, "alice@example.com", trustIssuer)
				t.Setenv(envSigstoreIDToken, "a-token")
				return []string{"--keyless"}
			},
		},
		{
			name: "ed25519", visibility: "private", rootKind: "key", signer: fp(edKey.Public()),
			rule:       "key-file = \"trust/k.pub\"\nkey = \"" + fp(edKey.Public()) + "\"",
			trustFiles: map[string][]byte{"trust/k.pub": pubPEM(edKey.Public())},
			args: func(t *testing.T, dir string) []string {
				writeKeyPEM(t, filepath.Join(dir, "signing.pem"), edKey)
				return []string{"--key", "signing.pem"}
			},
		},
		{
			name: "p256", visibility: "private", rootKind: "key", signer: fp(ecKey.Public()),
			rule:       "key-file = \"trust/k.pub\"",
			trustFiles: map[string][]byte{"trust/k.pub": pubPEM(ecKey.Public())},
			args: func(t *testing.T, dir string) []string {
				writeKeyPEM(t, filepath.Join(dir, "signing.pem"), ecKey)
				return []string{"--key", "signing.pem"}
			},
		},
		{
			name: "x509", visibility: "private", rootKind: "x509", signer: "release@acme.example",
			rule:       "x509-root = \"trust/ca.pem\"\nidentity = \"release@acme.example\"",
			trustFiles: map[string][]byte{"trust/ca.pem": rootBytes},
			args: func(t *testing.T, dir string) []string {
				writeKeyPEM(t, filepath.Join(dir, "signing.pem"), leafKey)
				chain := company.ChainPEM(t, "release@acme.example", leafKey.Public())
				if err := os.WriteFile(filepath.Join(dir, "chain.pem"), chain, 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--key", "signing.pem", "--cert-chain", "chain.pem"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, capture := fakeSignedPublishServer(t, tc.visibility)
			dir := newSignedKitProject(t, srv.URL)
			args := append([]string{"--bump", "patch", "--json", "--sign"}, tc.args(t, dir)...)
			stdout, err := execPublish(t, args...)
			if err != nil {
				t.Fatalf("publish: %v", err)
			}
			if capture.calls != 2 || capture.manifest.DryRun || capture.manifest.Bump != "patch" {
				t.Fatalf("calls = %d, last manifest = %+v; want a dry run, then the publish", capture.calls, capture.manifest)
			}
			var outcome publishOutcome
			if err := json.Unmarshal([]byte(stdout), &outcome); err != nil {
				t.Fatalf("--json output %q: %v", stdout, err)
			}
			if outcome.RootKind != tc.rootKind || outcome.Signer != tc.signer {
				t.Errorf("outcome signer = %q (%s), want %q (%s)", outcome.Signer, outcome.RootKind, tc.signer, tc.rootKind)
			}
			if ch := publishedChannel(t, capture); ch != (registryattest.PublishChannel{Channel: registryattest.ChannelPAT}) {
				t.Errorf("channel = %+v, want pat with no CI run", ch)
			}

			e := installPublished(t, capture, tc.rootKind, tc.rule, tc.trustFiles)
			if e.Signer == nil || e.Signer.Kind != tc.rootKind {
				t.Fatalf("locked signer = %+v, want %s", e.Signer, tc.rootKind)
			}
		})
	}
}

// On GitHub Actions the token comes from the runner for the audience
// "sigstore", and the channel record names the run exactly as Fulcio does.
func TestPublishKeylessOnGitHubActions(t *testing.T) {
	isolateTrustHome(t)
	isolateSigningEnv(t)
	var gotAudience, gotAuth string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAudience, gotAuth = r.URL.Query().Get("audience"), r.Header.Get("Authorization")
		io.WriteString(w, `{"value":"gh-oidc-token"}`) //nolint:errcheck // test handler
	}))
	t.Cleanup(tokenSrv.Close)
	for k, v := range map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   tokenSrv.URL + "/token?api-version=2.0",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "runner-request-token",
		"GITHUB_SERVER_URL":              "https://github.com",
		"GITHUB_REPOSITORY":              "acme/reports",
		"GITHUB_WORKFLOW_REF":            "acme/reports/.github/workflows/publish.yml@refs/tags/v1.2.0",
		"GITHUB_REF":                     "refs/tags/v1.2.0",
		"GITHUB_SHA":                     "0123456789abcdef0123456789abcdef01234567",
		"GITHUB_RUN_ID":                  "42",
		"GITHUB_RUN_ATTEMPT":             "2",
	} {
		t.Setenv(k, v)
	}
	kl := attesttest.NewKeyless(t)
	tokens := kl.InstallSigner(t, "alice@example.com", trustIssuer)
	srv, capture := fakeSignedPublishServer(t, "public")
	newSignedKitProject(t, srv.URL)

	if _, err := execPublish(t, "--bump", "patch", "--sign", "--keyless"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if gotAudience != "sigstore" || gotAuth != "Bearer runner-request-token" {
		t.Errorf("token request: audience %q, auth %q", gotAudience, gotAuth)
	}
	if len(*tokens) != 1 || (*tokens)[0] != "gh-oidc-token" {
		t.Errorf("Fulcio was asked with %q, want the runner's token", *tokens)
	}
	want := registryattest.PublishChannel{
		Channel:    registryattest.ChannelPAT,
		Repository: "acme/reports",
		Workflow:   ".github/workflows/publish.yml",
		Ref:        "refs/tags/v1.2.0",
		Commit:     "0123456789abcdef0123456789abcdef01234567",
		RunURL:     "https://github.com/acme/reports/actions/runs/42/attempts/2",
	}
	if ch := publishedChannel(t, capture); ch != want {
		t.Errorf("channel = %+v\nwant      %+v", ch, want)
	}
}

// A keyless signature lands in the public Rekor log, so a package that is not
// confirmed public is refused before anything is signed or published.
func TestPublishKeylessRefusesPrivatePackage(t *testing.T) {
	isolateTrustHome(t)
	isolateSigningEnv(t)
	t.Setenv(envSigstoreIDToken, "a-token")
	kl := attesttest.NewKeyless(t)
	tokens := kl.InstallSigner(t, "alice@example.com", trustIssuer)
	for _, visibility := range []string{"private", ""} {
		srv, capture := fakeSignedPublishServer(t, visibility)
		newSignedKitProject(t, srv.URL)
		_, err := execPublish(t, "--bump", "patch", "--sign", "--keyless")
		if err == nil || !strings.Contains(err.Error(), "not public") {
			t.Fatalf("visibility %q: err = %v, want a refusal", visibility, err)
		}
		if capture.calls != 1 || !capture.manifest.DryRun {
			t.Errorf("visibility %q: %d call(s), want only the dry run", visibility, capture.calls)
		}
	}
	if len(*tokens) != 0 {
		t.Errorf("Fulcio was asked %d time(s), want none", len(*tokens))
	}
}

func TestPublishSignRefusesDigestMismatch(t *testing.T) {
	isolateTrustHome(t)
	isolateSigningEnv(t)
	srv, capture := fakePublishServer(t, http.StatusOK, func(registry.PublishManifest) string {
		return `{"dryRun":true,"digest":"sha256:` + strings.Repeat("0", 64) + `","version":"1.0.0","visibility":"private","files":[],"warnings":[]}`
	})
	dir := newSignedKitProject(t, srv.URL)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	writeKeyPEM(t, filepath.Join(dir, "signing.pem"), key)

	_, err := execPublish(t, "--bump", "patch", "--sign", "--key", "signing.pem")
	if err == nil || !strings.Contains(err.Error(), "refusing to sign") {
		t.Fatalf("err = %v, want a digest mismatch", err)
	}
	if capture.calls != 1 || capture.attestation != "" {
		t.Errorf("%d call(s), attestation %q; want only the dry run", capture.calls, capture.attestation)
	}
}

func TestPublishSignFlags(t *testing.T) {
	isolateSigningEnv(t)
	srv, capture := fakeSignedPublishServer(t, "public")
	dir := newSignedKitProject(t, srv.URL)
	const secret = "not-a-key-but-a-secret"
	if err := os.WriteFile(filepath.Join(dir, "secret.pem"), []byte("-----BEGIN EC PRIVATE KEY-----\n"+base64.StdEncoding.EncodeToString([]byte(secret))+"\n-----END EC PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--sign"}, "--sign needs a way to sign"},
		{[]string{"--keyless"}, "only apply with --sign"},
		{[]string{"--sign", "--keyless", "--key", "k.pem"}, "exclusive"},
		{[]string{"--sign", "--cert-chain", "c.pem"}, "--cert-chain needs --key"},
		{[]string{"--sign", "--keyless"}, "needs an OIDC identity token"},
		{[]string{"--sign", "--key", "secret.pem"}, "unencrypted PKCS #8"},
	} {
		_, err := execPublish(t, append([]string{"--bump", "patch"}, tc.args...)...)
		var ee *exitError
		if err == nil || !strings.Contains(err.Error()+hintOf(err), tc.want) || !errors.As(err, &ee) {
			t.Errorf("%v: err = %v, want %q", tc.args, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%v: the error quotes the key file", tc.args)
		}
	}
	if capture.calls != 0 {
		t.Errorf("registry contacted %d time(s), want 0", capture.calls)
	}
}

func hintOf(err error) string {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.hint
	}
	return ""
}

// registry_publish passes the signing options to `bino publish`, and the
// result names the signer without any key material.
func TestRegistryPublishPassesSignFlags(t *testing.T) {
	isolateSigningEnv(t)
	srv, capture := fakeSignedPublishServer(t, "private")
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	cs := newPublishClient(t, srv.URL, map[string]string{
		"components/table.yaml": "", "components/kit.yaml": signedKitDoc, "signing.pem": keyPEM,
	})

	res, out := callPublish(t, cs, map[string]any{"bump": "patch", "sign_key": "signing.pem"})
	if res.IsError {
		t.Fatalf("registry_publish failed: %s", toolText(t, res))
	}
	fp, _ := registry.KeyFingerprint(key.Public())
	if out.Result == nil || out.Result.RootKind != "key" || out.Result.Signer != fp {
		t.Errorf("result = %+v, want signed by %s", out.Result, fp)
	}
	if capture.attestation == "" {
		t.Error("the registry got no attestation")
	}
	body := base64.StdEncoding.EncodeToString(der)
	if strings.Contains(out.Output, body[:40]) || strings.Contains(capture.attestation, body[:40]) {
		t.Error("the private key leaked into the output or the bundle")
	}

	// sign_keyless outside CI fails before anything is uploaded.
	before := capture.calls
	res, _ = callPublish(t, cs, map[string]any{"bump": "patch", "sign_keyless": true})
	if !res.IsError || !strings.Contains(toolText(t, res), "OIDC identity token") {
		t.Errorf("sign_keyless outside CI: %s", toolText(t, res))
	}
	if capture.calls != before {
		t.Errorf("sign_keyless outside CI reached the registry")
	}
}
