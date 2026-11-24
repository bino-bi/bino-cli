package registry_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/pathutil"
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

func checkOf(signers ...registry.Signer) registry.SignatureCheck {
	var c registry.SignatureCheck
	for _, s := range signers {
		c.Verified = append(c.Verified, registry.VerifiedSignature{Signer: s})
	}
	return c
}

func TestJudgeNewPins(t *testing.T) {
	isolateTrust(t)
	alice, bob := keylessSigner("alice@example.com"), keylessSigner("bob@example.com")

	open := policyFor(t, t.TempDir(), "")
	for _, order := range [][]registry.Signer{{alice, bob}, {bob, alice}} {
		d, err := open.Judge(testPkg, testVersion, checkOf(order...), nil, false)
		if err != nil || !d.Repin || d.Pin == nil || d.Pin.Identity != "alice@example.com" {
			t.Errorf("no rule, signers %v: d = %+v, err = %v; want alice pinned whatever the order", order, d, err)
		}
	}

	ruled := policyFor(t, t.TempDir(), keylessRule("@acme/*", "bob@example.com"))
	d, err := ruled.Judge(testPkg, testVersion, checkOf(alice, bob), nil, false)
	if err != nil || d.Pin == nil || d.Pin.Identity != "bob@example.com" {
		t.Errorf("rule for bob: d = %+v, err = %v; want the signer the rule accepts pinned", d, err)
	}
	d, err = ruled.Judge(testPkg, testVersion, checkOf(alice), nil, false)
	if err != nil || !d.Repin || d.Pin != nil {
		t.Errorf("rule rejects the only signer under off: d = %+v, err = %v; want no pin", d, err)
	}
}

func TestJudgePinnedAnchorTheRulesDoNotProvide(t *testing.T) {
	isolateTrust(t)
	policy := policyFor(t, t.TempDir(), "")
	pin := registry.Signer{Kind: registry.SignerX509, Root: "SHA256:abc", Identity: "release@acme.example"}
	_, err := policy.Judge(testPkg, testVersion, registry.SignatureCheck{Unverifiable: 1}, &pin, false)
	if !errors.Is(err, registry.ErrSignerChanged) || !strings.Contains(err.Error(), "key or CA no [trust] rule provides") || strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("err = %v", err)
	}
}

func TestSignerSameComparesForgeIDs(t *testing.T) {
	ci := registry.Signer{Kind: registry.SignerKeyless, Issuer: testIssuer, Repository: "https://github.com/acme/reports",
		Workflow: "https://github.com/acme/reports/.github/workflows/publish.yml", RepositoryID: "1", OwnerID: "2"}
	reborn := ci
	reborn.OwnerID = "3"
	if !ci.Same(ci) || ci.Same(reborn) {
		t.Error("a re-registered owner with the same name counts as the same signer")
	}
}

func TestPrintableStripsControlCharacters(t *testing.T) {
	s := registry.Signer{Kind: registry.SignerKeyless, Issuer: testIssuer, Identity: "alice\x1b[2J\u202e@example.com\n"}
	if got := s.String(); strings.ContainsAny(got, "\x1b\u202e\n") {
		t.Errorf("String() = %q", got)
	}
}

// A misspelled [trust] key would be a policy that silently trusts less, so
// it fails the policy wherever it is written.
func TestTrustUnknownKeysFail(t *testing.T) {
	t.Run("project", func(t *testing.T) {
		isolateTrust(t)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("[future]\nx = 1\n[trust]\nrequires = \"signed\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := pathutil.LoadProjectConfig(dir)
		if err != nil {
			t.Fatalf("an unknown key in [trust] fails the whole bino.toml: %v", err)
		}
		if _, err := registry.LoadTrustPolicy(dir, cfg.Trust); !errors.Is(err, registry.ErrTrustConfig) || !strings.Contains(err.Error(), "trust.requires") {
			t.Fatalf("err = %v, want ErrTrustConfig naming trust.requires", err)
		}
	})
	t.Run("global", func(t *testing.T) {
		isolateTrust(t)
		writeGlobal(t, "[future]\nx = 1\n[[trust.signers]]\nscope = \"@acme/*\"\n")
		if _, err := registry.LoadTrustPolicy(t.TempDir(), pathutil.TrustConfig{}); !errors.Is(err, registry.ErrTrustConfig) || !strings.Contains(err.Error(), "trust.signers") {
			t.Fatalf("err = %v, want ErrTrustConfig naming trust.signers", err)
		}
	})
	t.Run("env file", func(t *testing.T) {
		isolateTrust(t)
		writeEnvPolicy(t, "require = \"signed\"\n")
		if _, err := registry.LoadTrustPolicy(t.TempDir(), pathutil.TrustConfig{}); !errors.Is(err, registry.ErrTrustConfig) || !strings.Contains(err.Error(), "require") {
			t.Fatalf("err = %v, want ErrTrustConfig for a require outside [trust]", err)
		}
	})
}
