package registry_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/registry/attesttest"
)

const (
	testPkg     = "@acme/kit"
	testVersion = "1.0.0"
	testIssuer  = "https://issuer.example"
)

var (
	testDigest  = "sha256:" + strings.Repeat("ab", 32)
	otherDigest = "sha256:" + strings.Repeat("cd", 32)
)

// isolateTrust points HOME at an empty directory and clears
// BINO_TRUST_POLICY, so only the layers a test writes apply.
func isolateTrust(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv(registry.EnvTrustPolicy, "")
}

func parseTrust(t *testing.T, src string) pathutil.TrustConfig {
	t.Helper()
	var f struct {
		Trust pathutil.TrustConfig `toml:"trust"`
	}
	if err := toml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	return f.Trust
}

func policyFor(t *testing.T, dir, src string) *registry.TrustPolicy {
	t.Helper()
	p, err := registry.LoadTrustPolicy(dir, parseTrust(t, src))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	return p
}

// signerFixture signs statements for one root kind and writes the [trust]
// rule that names its good signer.
type signerFixture struct {
	rootKind string
	rule     string
	good     func(payload []byte) json.RawMessage
	other    func(payload []byte) json.RawMessage // another signer
}

func fixtures(t *testing.T, dir string) []signerFixture {
	kl := attesttest.NewKeyless(t)
	company := attesttest.NewCA(t)
	company.WriteRoot(t, filepath.Join(dir, "ca.pem"))
	key := attesttest.NewKey(t)
	key.WritePublicKey(t, filepath.Join(dir, "key.pub"))
	otherKey := attesttest.NewKey(t)
	return []signerFixture{
		{
			rootKind: "keyless",
			rule:     `oidc-issuer = "` + testIssuer + `"` + "\nidentity = \"alice@example.com\"",
			good:     func(p []byte) json.RawMessage { return kl.Sign(t, "alice@example.com", testIssuer, p) },
			other:    func(p []byte) json.RawMessage { return kl.Sign(t, "mallory@example.com", testIssuer, p) },
		},
		{
			rootKind: "x509",
			rule:     `x509-root = "ca.pem"` + "\nidentity = \"release@acme.example\"",
			good:     func(p []byte) json.RawMessage { return company.Sign(t, "release@acme.example", p) },
			other:    func(p []byte) json.RawMessage { return company.Sign(t, "intern@acme.example", p) },
		},
		{
			rootKind: "key",
			rule:     `key-file = "key.pub"` + "\nkey = \"" + key.Fingerprint(t) + "\"",
			good:     func(p []byte) json.RawMessage { return key.Sign(t, p) },
			other:    func(p []byte) json.RawMessage { return otherKey.Sign(t, p) },
		},
	}
}

func TestSignatureMatrix(t *testing.T) {
	isolateTrust(t)
	dir := t.TempDir()
	good := attesttest.Statement(t, testPkg, testVersion, testDigest)
	wrongDigest := attesttest.Statement(t, testPkg, testVersion, otherDigest)
	wrongVersion := attesttest.Statement(t, testPkg, "9.9.9", testDigest)

	for _, fx := range fixtures(t, dir) {
		for _, level := range []string{registry.RequireOff, registry.RequireWarn, registry.RequireSigned} {
			policy := policyFor(t, dir, "[trust]\nrequire = \""+level+"\"\n[[trust.signer]]\nscope = \"@acme/*\"\n"+fx.rule+"\n")
			judge := func(atts []registry.Attestation) (registry.TrustDecision, error) {
				check, err := registry.VerifyAttestations(testPkg, testVersion, testDigest, atts, policy)
				if err != nil {
					return registry.TrustDecision{}, err
				}
				return policy.Judge(testPkg, testVersion, check, nil, false)
			}
			att := func(b json.RawMessage) []registry.Attestation {
				return []registry.Attestation{attesttest.Attestation("publish", fx.rootKind, b)}
			}
			name := fx.rootKind + "/" + level

			t.Run(name+"/ok", func(t *testing.T) {
				d, err := judge(att(fx.good(good)))
				if err != nil || d.Warning != "" || d.Signer == nil || d.Signer.Kind != fx.rootKind {
					t.Fatalf("d = %+v, err = %v", d, err)
				}
			})
			for label, st := range map[string][]byte{"wrong-digest": wrongDigest, "wrong-version": wrongVersion} {
				t.Run(name+"/"+label, func(t *testing.T) {
					if _, err := judge(att(fx.good(st))); !errors.Is(err, registry.ErrInvalidSignature) {
						t.Fatalf("err = %v, want ErrInvalidSignature", err)
					}
				})
			}
			t.Run(name+"/bad-signature", func(t *testing.T) {
				if _, err := judge(att(swapPayload(t, fx.good(good), good))); !errors.Is(err, registry.ErrInvalidSignature) {
					t.Fatalf("err = %v, want ErrInvalidSignature", err)
				}
			})
			wantRejected := func(t *testing.T, d registry.TrustDecision, err error) {
				t.Helper()
				switch level {
				case registry.RequireOff:
					if err != nil || d.Warning != "" {
						t.Fatalf("off: d = %+v, err = %v", d, err)
					}
				case registry.RequireWarn:
					if err != nil || d.Warning == "" {
						t.Fatalf("warn: d = %+v, err = %v, want a warning", d, err)
					}
				case registry.RequireSigned:
					if !errors.Is(err, registry.ErrUntrusted) {
						t.Fatalf("signed: err = %v, want ErrUntrusted", err)
					}
				}
			}
			t.Run(name+"/wrong-signer", func(t *testing.T) {
				d, err := judge(att(fx.other(good)))
				wantRejected(t, d, err)
			})
			t.Run(name+"/unsigned", func(t *testing.T) {
				d, err := judge(nil)
				wantRejected(t, d, err)
				if err == nil && d.Signer != nil {
					t.Errorf("unsigned package got signer %+v", d.Signer)
				}
			})
		}
	}
}

// swapPayload replaces a bundle's DSSE payload after signing, so the
// statement still names the release but the signature no longer covers it.
func swapPayload(t *testing.T, bundle json.RawMessage, statement []byte) json.RawMessage {
	t.Helper()
	var st map[string]any
	if err := json.Unmarshal(statement, &st); err != nil {
		t.Fatal(err)
	}
	st["predicate"].(map[string]any)["runDetails"] = map[string]any{"builder": map[string]any{"id": "https://evil.example"}}
	forged, _ := json.Marshal(st)
	var b map[string]any
	if err := json.Unmarshal(bundle, &b); err != nil {
		t.Fatal(err)
	}
	b["dsseEnvelope"].(map[string]any)["payload"] = base64.StdEncoding.EncodeToString(forged)
	out, _ := json.Marshal(b)
	return out
}

func TestSignatureMalformedBundleAlwaysFails(t *testing.T) {
	isolateTrust(t)
	policy := policyFor(t, t.TempDir(), "")
	for name, raw := range map[string]string{
		"not json":  `{`,
		"no bundle": ``,
		"v0.1":      `{"mediaType":"application/vnd.dev.sigstore.bundle+json;version=0.1"}`,
	} {
		atts := []registry.Attestation{{Kind: "publish", Bundle: json.RawMessage(raw)}}
		if _, err := registry.VerifyAttestations(testPkg, testVersion, testDigest, atts, policy); !errors.Is(err, registry.ErrInvalidSignature) {
			t.Errorf("%s: err = %v, want ErrInvalidSignature", name, err)
		}
	}
}

func TestSignatureAnchorsAreScoped(t *testing.T) {
	isolateTrust(t)
	dir := t.TempDir()
	key := attesttest.NewKey(t)
	key.WritePublicKey(t, filepath.Join(dir, "key.pub"))
	// The key is trusted for @other only, so it cannot vouch for @acme/kit.
	policy := policyFor(t, dir, "[[trust.signer]]\nscope = \"@other/*\"\nkey-file = \"key.pub\"\n")
	atts := []registry.Attestation{attesttest.Attestation("publish", "key", key.Sign(t, attesttest.Statement(t, testPkg, testVersion, testDigest)))}
	check, err := registry.VerifyAttestations(testPkg, testVersion, testDigest, atts, policy)
	if err != nil || len(check.Verified) != 0 || check.Unverifiable != 1 {
		t.Fatalf("check = %+v, err = %v; want one unverifiable bundle", check, err)
	}
}

func TestJudgePin(t *testing.T) {
	isolateTrust(t)
	kl := attesttest.NewKeyless(t)
	policy := policyFor(t, t.TempDir(), "")
	st := attesttest.Statement(t, testPkg, testVersion, testDigest)
	verify := func(identity string) registry.SignatureCheck {
		atts := []registry.Attestation{attesttest.Attestation("publish", "keyless", kl.Sign(t, identity, testIssuer, st))}
		check, err := registry.VerifyAttestations(testPkg, testVersion, testDigest, atts, policy)
		if err != nil {
			t.Fatal(err)
		}
		return check
	}
	alice := verify("alice@example.com")
	pin := alice.Verified[0].Signer

	if d, err := policy.Judge(testPkg, testVersion, alice, &pin, false); err != nil || d.Signer == nil || !d.Signer.Same(pin) {
		t.Fatalf("same signer: d = %+v, err = %v", d, err)
	}
	bob := verify("bob@example.com")
	if _, err := policy.Judge(testPkg, testVersion, bob, &pin, false); !errors.Is(err, registry.ErrSignerChanged) || !strings.Contains(err.Error(), "trust accept "+testPkg) {
		t.Fatalf("other signer under off: err = %v, want ErrSignerChanged with a trust accept hint", err)
	}
	if _, err := policy.Judge(testPkg, testVersion, registry.SignatureCheck{}, &pin, false); !errors.Is(err, registry.ErrSignerChanged) {
		t.Fatalf("now unsigned under off: err = %v, want ErrSignerChanged", err)
	}
	d, err := policy.Judge(testPkg, testVersion, bob, &pin, true)
	if err != nil || d.Signer == nil || d.Signer.Identity != "bob@example.com" {
		t.Fatalf("accepted change: d = %+v, err = %v", d, err)
	}
}
