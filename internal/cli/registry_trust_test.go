package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/registry/attesttest"
	"bino.bi/bino/internal/report/lint"
)

const trustIssuer = "https://issuer.example"

// isolateTrustHome keeps the developer's ~/.bino/config.toml and any
// BINO_TRUST_POLICY out of a test.
func isolateTrustHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(registry.EnvTrustPolicy, "")
}

// signedKit is a tree package @acme/kit whose attestations a test sets.
func signedKit(version string) *fakePackage {
	return &fakePackage{tag: "latest", version: version, kind: "Text", kinds: []string{"Text"}, tree: fakeTree()}
}

func appendTrust(t *testing.T, dir, src string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, "bino.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\n" + src); err != nil {
		t.Fatal(err)
	}
}

func publish(t *testing.T, pkg *fakePackage, rootKind string, sign func([]byte) json.RawMessage) {
	t.Helper()
	st := attesttest.Statement(t, "@acme/kit", pkg.version, pkg.manifestDigest(t))
	pkg.attestations = []registry.Attestation{attesttest.Attestation("publish", rootKind, sign(st))}
}

func lockedKit(t *testing.T, dir string) *registry.Entry {
	t.Helper()
	lock, err := registry.LoadLockfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Get("@acme/kit")
	if e == nil {
		t.Fatal("@acme/kit is not locked")
	}
	return e
}

// Each root kind: add verifies and pins the signer, the lock moves to v3, and
// verify re-checks the stored signature with the registry gone.
func TestRegistrySignedAddPinsSignerAndVerifiesOffline(t *testing.T) {
	isolateTrustHome(t)
	kl := attesttest.NewKeyless(t)
	company := attesttest.NewCA(t)
	key := attesttest.NewKey(t)

	for _, tc := range []struct {
		kind string
		rule string
		sign func([]byte) json.RawMessage
		pre  func(dir string)
	}{
		{"keyless", "oidc-issuer = \"" + trustIssuer + "\"\nidentity = \"alice@example.com\"",
			func(p []byte) json.RawMessage { return kl.Sign(t, "alice@example.com", trustIssuer, p) }, nil},
		{"x509", "x509-root = \"trust/ca.pem\"",
			func(p []byte) json.RawMessage { return company.Sign(t, "release@acme.example", p) },
			func(dir string) { company.WriteRoot(t, filepath.Join(dir, "trust", "ca.pem")) }},
		{"key", "key-file = \"trust/acme.pub\"",
			func(p []byte) json.RawMessage { return key.Sign(t, p) },
			func(dir string) { key.WritePublicKey(t, filepath.Join(dir, "trust", "acme.pub")) }},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			kit := signedKit("1.0.0")
			publish(t, kit, tc.kind, tc.sign)
			srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{"@acme/kit": kit})
			dir := newRegistryTestProject(t, srv.URL)
			if err := os.MkdirAll(filepath.Join(dir, "trust"), 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.pre != nil {
				tc.pre(dir)
			}
			appendTrust(t, dir, "[trust]\nrequire = \"signed\"\n[[trust.signer]]\nscope = \"@acme/*\"\n"+tc.rule+"\n")

			if err := runRegistry(t, "add", "@acme/kit"); err != nil {
				t.Fatalf("add: %v", err)
			}
			e := lockedKit(t, dir)
			if e.Signer == nil || e.Signer.Kind != tc.kind {
				t.Fatalf("locked signer = %+v, want a %s signer", e.Signer, tc.kind)
			}
			data, _ := os.ReadFile(filepath.Join(dir, registry.LockfileName))
			if !strings.Contains(string(data), "lockfile_version = 3") || !strings.Contains(string(data), "signer = {") {
				t.Errorf("lock does not record the signer next to the digest:\n%s", data)
			}

			srv.Close() // verify must not need the registry, or anything else
			if err := runRegistry(t, "verify"); err != nil {
				t.Fatalf("offline verify: %v", err)
			}
		})
	}
}

func TestRegistrySignerChangeNeedsTrustAccept(t *testing.T) {
	isolateTrustHome(t)
	kl := attesttest.NewKeyless(t)
	kit := signedKit("1.0.0")
	publish(t, kit, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "alice@example.com", trustIssuer, p) })
	packages := map[string]*fakePackage{"@acme/kit": kit}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)

	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	lockBefore, _ := os.ReadFile(filepath.Join(dir, registry.LockfileName))

	// 2.0.0 is signed by someone else: refused under require = "off".
	next := signedKit("2.0.0")
	publish(t, next, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "mallory@example.com", trustIssuer, p) })
	packages["@acme/kit"] = next
	err := runRegistry(t, "update")
	if !errors.Is(err, registry.ErrSignerChanged) || !strings.Contains(err.Error(), "bino registry trust accept @acme/kit") {
		t.Fatalf("update to another signer: err = %v, want ErrSignerChanged with the accept command", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, registry.LockfileName)); string(got) != string(lockBefore) {
		t.Errorf("refused update changed bino.lock:\n%s", got)
	}

	// A version with no signature is refused the same way.
	next.attestations = nil
	if err := runRegistry(t, "update"); !errors.Is(err, registry.ErrSignerChanged) {
		t.Fatalf("update to an unsigned version: err = %v, want ErrSignerChanged", err)
	}

	publish(t, next, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "bob@example.com", trustIssuer, p) })
	p, err := registryProjectFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	name, before, after, _, err := registryTrustAccept(context.Background(), p, "@acme/kit")
	if err != nil {
		t.Fatalf("trust accept: %v", err)
	}
	if name != "@acme/kit" || before == nil || before.Identity != "alice@example.com" || after == nil || after.Identity != "bob@example.com" {
		t.Fatalf("trust accept: %s before %+v after %+v", name, before, after)
	}
	if e := lockedKit(t, dir); e.Version != "2.0.0" || e.Signer.Identity != "bob@example.com" {
		t.Errorf("locked after accept: %+v", e)
	}
	// The new pin holds from now on.
	if err := runRegistry(t, "install"); err != nil {
		t.Fatalf("install after accept: %v", err)
	}
}

// The registry swapping the signature of an already locked version is caught
// by install too, which never re-resolves.
func TestRegistryInstallRefusesASwappedSigner(t *testing.T) {
	isolateTrustHome(t)
	kl := attesttest.NewKeyless(t)
	kit := signedKit("1.0.0")
	publish(t, kit, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "alice@example.com", trustIssuer, p) })
	srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{"@acme/kit": kit})
	newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	publish(t, kit, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "mallory@example.com", trustIssuer, p) })
	if err := runRegistry(t, "install"); !errors.Is(err, registry.ErrSignerChanged) {
		t.Fatalf("install: err = %v, want ErrSignerChanged", err)
	}
}

func TestRegistryRequireLevels(t *testing.T) {
	isolateTrustHome(t)
	for _, tc := range []struct {
		level   string
		legacy  bool // a registry without the attestations route
		wantErr bool
		warns   bool
	}{
		{level: "off"},
		{level: "warn", warns: true},
		{level: "signed", wantErr: true},
		{level: "off", legacy: true},
		{level: "signed", legacy: true, wantErr: true},
	} {
		name := tc.level
		if tc.legacy {
			name += "/legacy-registry"
		}
		t.Run(name, func(t *testing.T) {
			kit := signedKit("1.0.0")
			kit.noAttestRoute = tc.legacy
			srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{"@acme/kit": kit})
			dir := newRegistryTestProject(t, srv.URL)
			appendTrust(t, dir, "[trust]\nrequire = \""+tc.level+"\"\n")
			p, err := registryProjectFor(dir)
			if err != nil {
				t.Fatal(err)
			}
			res, err := registryAdd(context.Background(), p, []string{"@acme/kit"}, nil)
			if tc.wantErr {
				if !errors.Is(err, registry.ErrUntrusted) {
					t.Fatalf("err = %v, want ErrUntrusted", err)
				}
				if _, statErr := os.Stat(filepath.Join(dir, registry.LockfileName)); !os.IsNotExist(statErr) {
					t.Error("a refused add wrote bino.lock")
				}
				if _, statErr := os.Stat(filepath.Join(dir, ".bino", "registry", "acme", "kit")); !os.IsNotExist(statErr) {
					t.Error("a refused add materialized the package")
				}
				return
			}
			if err != nil {
				t.Fatalf("add: %v", err)
			}
			warned := false
			for _, w := range res.Warnings {
				warned = warned || strings.Contains(w, "is not signed")
			}
			if warned != tc.warns {
				t.Errorf("warnings = %q, want an unsigned warning: %v", res.Warnings, tc.warns)
			}
		})
	}
}

func TestRegistryInvalidSignatureFailsUnderOff(t *testing.T) {
	isolateTrustHome(t)
	kl := attesttest.NewKeyless(t)
	kit := signedKit("1.0.0")
	st := attesttest.Statement(t, "@acme/kit", "1.0.0", "sha256:"+strings.Repeat("0", 64))
	kit.attestations = []registry.Attestation{attesttest.Attestation("publish", "keyless", kl.Sign(t, "alice@example.com", trustIssuer, st))}
	srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{"@acme/kit": kit})
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit"); !errors.Is(err, registry.ErrInvalidSignature) {
		t.Fatalf("add: err = %v, want ErrInvalidSignature", err)
	}
	if _, err := os.Stat(filepath.Join(dir, registry.LockfileName)); !os.IsNotExist(err) {
		t.Error("an invalid signature still wrote bino.lock")
	}
}

func TestRegistryUpdateWarnsOnRollback(t *testing.T) {
	isolateTrustHome(t)
	packages := map[string]*fakePackage{"@acme/kit": signedKit("2.0.0")}
	srv, _, _ := fakeRegistryServer(t, packages)
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	packages["@acme/kit"] = signedKit("1.5.0") // the tag moved backwards
	p, err := registryProjectFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := registryUpdate(context.Background(), p, nil, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "older than the locked 2.0.0") {
		t.Errorf("warnings = %q, want a rollback warning", res.Warnings)
	}
}

func TestRegistryV2LockUpgradesToV3(t *testing.T) {
	isolateTrustHome(t)
	kl := attesttest.NewKeyless(t)
	kit := signedKit("1.0.0")
	publish(t, kit, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "alice@example.com", trustIssuer, p) })
	srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{"@acme/kit": kit})
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// What a previous bino wrote: version 2, no signer.
	lockPath := filepath.Join(dir, registry.LockfileName)
	data, _ := os.ReadFile(lockPath)
	var v2 []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "signer = ") {
			continue
		}
		v2 = append(v2, strings.Replace(line, "lockfile_version = 3", "lockfile_version = 2", 1))
	}
	if err := os.WriteFile(lockPath, []byte(strings.Join(v2, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "install"); err != nil {
		t.Fatalf("install from a v2 lock: %v", err)
	}
	if got, _ := os.ReadFile(lockPath); !strings.Contains(string(got), "lockfile_version = 2") {
		t.Error("install rewrote the v2 lock")
	}
	if err := runRegistry(t, "update"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if e := lockedKit(t, dir); e.Signer == nil {
		t.Error("update did not pin the signer")
	}
	if got, _ := os.ReadFile(lockPath); !strings.Contains(string(got), "lockfile_version = 3") {
		t.Errorf("update did not upgrade the lock:\n%s", got)
	}
}

func TestRegistryVerifyCatchesASwappedStoredSignature(t *testing.T) {
	isolateTrustHome(t)
	kl := attesttest.NewKeyless(t)
	kit := signedKit("1.0.0")
	publish(t, kit, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "alice@example.com", trustIssuer, p) })
	srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{"@acme/kit": kit})
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit"); err != nil {
		t.Fatalf("add: %v", err)
	}
	st := attesttest.Statement(t, "@acme/kit", "1.0.0", kit.manifestDigest(t))
	swapped := []registry.Attestation{attesttest.Attestation("publish", "keyless", kl.Sign(t, "mallory@example.com", trustIssuer, st))}
	if err := registry.WriteAttestations(dir, "@acme/kit", "1.0.0", swapped); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "verify"); err == nil {
		t.Fatal("verify passed with a stored signature by another signer")
	}
	if err := registry.RemoveAttestations(dir, "@acme/kit"); err != nil {
		t.Fatal(err)
	}
	if err := runRegistry(t, "verify"); err == nil {
		t.Fatal("verify passed with the pinned signature missing")
	}
}

func TestRegistryTrustLintRules(t *testing.T) {
	isolateTrustHome(t)
	kl := attesttest.NewKeyless(t)
	signed := signedKit("1.0.0")
	publish(t, signed, "keyless", func(p []byte) json.RawMessage { return kl.Sign(t, "alice@example.com", trustIssuer, p) })
	unsignedBody, unsignedDigest := fakeDoc(t, "@bino/plain", "Text")
	srv, _, _ := fakeRegistryServer(t, map[string]*fakePackage{
		"@acme/kit":   signed,
		"@bino/plain": {tag: "latest", version: "1.0.0", kind: "Text", body: unsignedBody, digest: unsignedDigest},
	})
	dir := newRegistryTestProject(t, srv.URL)
	if err := runRegistry(t, "add", "@acme/kit", "@bino/plain"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// The policy tightens after the lock was written.
	appendTrust(t, dir, "[trust]\nrequire = \"signed\"\n[[trust.signer]]\nscope = \"@acme/*\"\noidc-issuer = \""+trustIssuer+"\"\nidentity = \"bob@example.com\"\n")

	found := map[string]string{}
	for _, f := range lint.NewProjectRunner(dir).Run(context.Background(), nil) {
		if strings.HasPrefix(f.RuleID, "registry-") {
			found[f.RuleID] = f.Message
			if f.File != filepath.Join(dir, registry.LockfileName) || f.Line <= 1 {
				t.Errorf("%s anchored at %s:%d", f.RuleID, f.File, f.Line)
			}
		}
	}
	if !strings.Contains(found["registry-unsigned-dependency"], "@bino/plain") {
		t.Errorf("registry-unsigned-dependency = %q", found["registry-unsigned-dependency"])
	}
	if !strings.Contains(found["registry-signer-mismatch"], "@acme/kit") {
		t.Errorf("registry-signer-mismatch = %q", found["registry-signer-mismatch"])
	}
}
