package registry_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/registry/attesttest"
)

func keylessSigner(identity string) registry.Signer {
	return registry.Signer{Kind: registry.SignerKeyless, Issuer: testIssuer, Identity: identity}
}

func keylessRule(scope, identity string) string {
	return "[[trust.signer]]\nscope = \"" + scope + "\"\noidc-issuer = \"" + testIssuer + "\"\nidentity = \"" + identity + "\"\n"
}

func writeGlobal(t *testing.T, src string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".bino")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeEnvPolicy(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(registry.EnvTrustPolicy, p)
	return p
}

func TestTrustLayersOnlyTighten(t *testing.T) {
	isolateTrust(t)
	writeGlobal(t, "[registry]\nurl = \"https://r.example\"\n[trust]\nrequire = \"warn\"\n"+
		keylessRule("@acme/*", "bob@example.com")+ // tries to replace the project's signer
		keylessRule("@other/*", "carol@example.com")) // a scope the project leaves open
	writeEnvPolicy(t, "[trust]\nrequire = \"off\"\n") // tries to lower require
	policy := policyFor(t, t.TempDir(), "[trust]\nrequire = \"off\"\n"+keylessRule("@acme/*", "alice@example.com"))

	if policy.Level() != registry.RequireWarn {
		t.Errorf("level = %q, want warn: a lower layer's off must not win over a raised level", policy.Level())
	}
	alice, bob := keylessSigner("alice@example.com"), keylessSigner("bob@example.com")
	for _, tc := range []struct {
		name    string
		pkg     string
		signers []registry.Signer
		covered bool
		trusted bool
	}{
		{"project signer alone misses the global requirement", "@acme/kit", []registry.Signer{alice}, true, false},
		{"global signer cannot replace the project one", "@acme/kit", []registry.Signer{bob}, true, false},
		{"both requirements met", "@acme/kit", []registry.Signer{alice, bob}, true, true},
		{"global adds an uncovered scope", "@other/x", []registry.Signer{keylessSigner("carol@example.com")}, true, true},
		{"global scope rejects another signer", "@other/x", []registry.Signer{alice}, true, false},
		{"no layer covers the package", "@free/x", []registry.Signer{alice}, false, false},
	} {
		covered, trusted, _ := policy.Evaluate(tc.pkg, tc.signers)
		if covered != tc.covered || trusted != tc.trusted {
			t.Errorf("%s: covered, trusted = %v, %v; want %v, %v", tc.name, covered, trusted, tc.covered, tc.trusted)
		}
	}
}

func TestTrustFirstMatchWinsWithinALayer(t *testing.T) {
	isolateTrust(t)
	policy := policyFor(t, t.TempDir(), keylessRule("@acme/kit", "alice@example.com")+keylessRule("@acme/*", "bob@example.com"))
	if _, ok, _ := policy.Evaluate("@acme/kit", []registry.Signer{keylessSigner("bob@example.com")}); ok {
		t.Error("the later @acme/* rule applied to @acme/kit, which an earlier rule covers")
	}
	if _, ok, _ := policy.Evaluate("@acme/other", []registry.Signer{keylessSigner("bob@example.com")}); !ok {
		t.Error("@acme/* did not apply to @acme/other")
	}
}

func TestTrustRequireIsTheStrictestLayer(t *testing.T) {
	for _, tc := range []struct{ project, global, env, want string }{
		{"", "", "", registry.RequireOff},
		{"signed", "off", "", registry.RequireSigned},
		{"off", "", "signed", registry.RequireSigned},
		{"warn", "signed", "off", registry.RequireSigned},
		{"signed", "warn", "warn", registry.RequireSigned},
	} {
		isolateTrust(t)
		writeGlobal(t, "[trust]\nrequire = \""+tc.global+"\"\n")
		if tc.env != "" {
			writeEnvPolicy(t, "[trust]\nrequire = \""+tc.env+"\"\n")
		}
		policy := policyFor(t, t.TempDir(), "[trust]\nrequire = \""+tc.project+"\"\n")
		if policy.Level() != tc.want {
			t.Errorf("%+v: level = %q", tc, policy.Level())
		}
	}
}

func TestTrustEnvPolicyResolvesPathsBesideItself(t *testing.T) {
	isolateTrust(t)
	key := attesttest.NewKey(t)
	policyPath := writeEnvPolicy(t, "[trust]\nrequire = \"signed\"\n[[trust.signer]]\nscope = \"@acme/*\"\nkey-file = \"acme.pub\"\n")
	key.WritePublicKey(t, filepath.Join(filepath.Dir(policyPath), "acme.pub"))
	policy := policyFor(t, t.TempDir(), "")
	if _, ok, _ := policy.Evaluate(testPkg, []registry.Signer{{Kind: registry.SignerKey, Key: key.Fingerprint(t)}}); !ok {
		t.Error("the env policy's key rule did not apply")
	}

	t.Setenv(registry.EnvTrustPolicy, filepath.Join(t.TempDir(), "missing.toml"))
	if _, err := registry.LoadTrustPolicy(t.TempDir(), parseTrust(t, "")); !errors.Is(err, registry.ErrTrustConfig) {
		t.Errorf("missing env policy: err = %v, want ErrTrustConfig", err)
	}
}

func TestTrustConfigErrors(t *testing.T) {
	isolateTrust(t)
	dir := t.TempDir()
	key := attesttest.NewKey(t)
	key.WritePublicKey(t, filepath.Join(dir, "key.pub"))
	for name, src := range map[string]string{
		"unknown level":           "[trust]\nrequire = \"maybe\"\n",
		"bad scope":               "[[trust.signer]]\nscope = \"acme\"\nkey-file = \"key.pub\"\n",
		"no kind":                 "[[trust.signer]]\nscope = \"@acme/*\"\nidentity = \"x\"\n",
		"two kinds":               "[[trust.signer]]\nscope = \"@acme/*\"\nkey-file = \"key.pub\"\noidc-issuer = \"i\"\nidentity = \"x\"\n",
		"keyless without subject": "[[trust.signer]]\nscope = \"@acme/*\"\noidc-issuer = \"i\"\n",
		"identity and workflow":   "[[trust.signer]]\nscope = \"@acme/*\"\noidc-issuer = \"i\"\nidentity = \"x\"\nrepository = \"r\"\nworkflow = \"w\"\n",
		"workflow alone":          "[[trust.signer]]\nscope = \"@acme/*\"\noidc-issuer = \"i\"\nworkflow = \"w\"\n",
		"workflow with ref":       "[[trust.signer]]\nscope = \"@acme/*\"\noidc-issuer = \"i\"\nrepository = \"r\"\nworkflow = \"w@refs/tags/v1\"\n",
		"key without file":        "[[trust.signer]]\nscope = \"@acme/*\"\nkey = \"" + key.Fingerprint(t) + "\"\n",
		"key fingerprint differs": "[[trust.signer]]\nscope = \"@acme/*\"\nkey-file = \"key.pub\"\nkey = \"ed25519:SHA256:AAAA\"\n",
		"missing ca":              "[[trust.signer]]\nscope = \"@acme/*\"\nx509-root = \"nope.pem\"\n",
	} {
		if _, err := registry.LoadTrustPolicy(dir, parseTrust(t, src)); !errors.Is(err, registry.ErrTrustConfig) {
			t.Errorf("%s: err = %v, want ErrTrustConfig", name, err)
		}
	}
	writeGlobal(t, "[trust]\nrequire = \"nope\"\n")
	if _, err := registry.LoadTrustPolicy(dir, parseTrust(t, "")); !errors.Is(err, registry.ErrTrustConfig) {
		t.Errorf("broken global layer: err = %v, want ErrTrustConfig", err)
	}
}

func TestTrustWorkflowRule(t *testing.T) {
	isolateTrust(t)
	const (
		iss  = "https://token.actions.githubusercontent.com"
		repo = "https://github.com/acme/reports"
		wf   = "https://github.com/acme/reports/.github/workflows/publish.yml"
	)
	policy := policyFor(t, t.TempDir(), "[[trust.signer]]\nscope = \"@acme/*\"\noidc-issuer = \""+iss+"\"\nrepository = \""+repo+"\"\nworkflow = \""+wf+"\"\n")
	ci := func(repository, workflow string) registry.Signer {
		return registry.Signer{Kind: registry.SignerKeyless, Issuer: iss, Identity: workflow + "@refs/tags/v1.2.0", Repository: repository, Workflow: workflow}
	}
	if _, ok, _ := policy.Evaluate(testPkg, []registry.Signer{ci(repo, wf)}); !ok {
		t.Error("the release workflow was not accepted")
	}
	if _, ok, _ := policy.Evaluate(testPkg, []registry.Signer{ci("https://github.com/evil/fork", wf)}); ok {
		t.Error("the same workflow run from another repository was accepted")
	}
	if _, ok, _ := policy.Evaluate(testPkg, []registry.Signer{ci(repo, repo+"/.github/workflows/other.yml")}); ok {
		t.Error("another workflow of the repository was accepted")
	}
}
