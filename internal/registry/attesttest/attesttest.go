// Package attesttest signs registry release statements for tests: keyless
// through a virtual Sigstore, x509 with a throwaway CA, and with a bare key.
// It is imported only by tests, so the virtual Sigstore never reaches the
// binary.
package attesttest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/bino-bi/bino-plugin-sdk/registryattest"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/sign"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"google.golang.org/protobuf/encoding/protojson"

	"bino.bi/bino/internal/registry"
)

// Statement returns the in-toto statement for pkg ("@scope/name") at version
// with the given manifest digest.
func Statement(t testing.TB, pkg, version, digest string) []byte {
	t.Helper()
	scope, name, err := registry.ParseName(pkg)
	if err != nil {
		t.Fatal(err)
	}
	pred, err := registryattest.NewPredicate(registryattest.PublishChannel{Channel: registryattest.ChannelOIDC}, "https://bino.bi/test")
	if err != nil {
		t.Fatal(err)
	}
	st, err := registryattest.NewStatement(scope, name, version, digest, pred)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Attestation wraps a bundle in the registry's wire shape.
func Attestation(kind, rootKind string, bundle json.RawMessage) registry.Attestation {
	return registry.Attestation{Kind: kind, Signer: "test", RootKind: rootKind, Bundle: bundle}
}

// Keyless is a virtual Sigstore: Fulcio, Rekor and a TSA with throwaway keys.
type Keyless struct{ vs *ca.VirtualSigstore }

// NewKeyless creates a virtual Sigstore and installs it as the keyless trust
// root for the rest of the test.
func NewKeyless(t testing.TB) *Keyless {
	t.Helper()
	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.SetKeylessTrustForTesting(vs, false))
	return &Keyless{vs: vs}
}

// Sign returns a v0.3 bundle over payload for an email identity, carrying a
// Rekor entry with inclusion proof and signed entry timestamp.
func (k *Keyless) Sign(t testing.TB, identity, issuer string, payload []byte) json.RawMessage {
	t.Helper()
	ent, err := k.vs.AttestAtTime(identity, issuer, payload, time.Now().Add(time.Minute), true)
	if err != nil {
		t.Fatal(err)
	}
	vc, err := ent.VerificationContent()
	must(t, err)
	sc, err := ent.SignatureContent()
	must(t, err)
	env := sc.EnvelopeContent().RawEnvelope()
	entries, err := ent.TlogEntries()
	must(t, err)
	tle := entries[0].TransparencyLogEntry()
	tle.KindVersion = &protorekor.KindVersion{Kind: "dsse", Version: "0.0.1"}
	logID, err := k.vs.RekorLogID()
	if err != nil {
		t.Fatal(err)
	}
	set, err := k.vs.RekorSignPayload(tlog.RekorPayload{
		Body:           base64.StdEncoding.EncodeToString(tle.CanonicalizedBody),
		IntegratedTime: tle.IntegratedTime,
		LogIndex:       tle.LogIndex,
		LogID:          logID,
	})
	if err != nil {
		t.Fatal(err)
	}
	tle.InclusionPromise = &protorekor.InclusionPromise{SignedEntryTimestamp: set}
	sig, err := base64.StdEncoding.DecodeString(env.Signatures[0].Sig)
	must(t, err)
	body, err := base64.StdEncoding.DecodeString(env.Payload)
	must(t, err)
	return marshal(t, &protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle.v0.3+json",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content:     &protobundle.VerificationMaterial_Certificate{Certificate: &protocommon.X509Certificate{RawBytes: vc.Certificate().Raw}},
			TlogEntries: []*protorekor.TransparencyLogEntry{tle},
		},
		Content: &protobundle.Bundle_DsseEnvelope{DsseEnvelope: &protodsse.Envelope{
			Payload:     body,
			PayloadType: env.PayloadType,
			Signatures:  []*protodsse.Signature{{Sig: sig, Keyid: env.Signatures[0].KeyID}},
		}},
	})
}

// Key is a long-lived signing key.
type Key struct{ kp *sign.EphemeralKeypair }

// NewKey creates an ed25519 key.
func NewKey(t testing.TB) *Key {
	t.Helper()
	kp, err := sign.NewEphemeralKeypair(&sign.EphemeralKeypairOptions{Algorithm: protocommon.PublicKeyDetails_PKIX_ED25519})
	if err != nil {
		t.Fatal(err)
	}
	return &Key{kp: kp}
}

// WritePublicKey writes the PEM public key to path.
func (k *Key) WritePublicKey(t testing.TB, path string) {
	t.Helper()
	p, err := k.kp.GetPublicKeyPem()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(p), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Fingerprint is the key's fingerprint as [trust] pins it.
func (k *Key) Fingerprint(t testing.TB) string {
	t.Helper()
	fp, err := registry.KeyFingerprint(k.kp.GetPublicKey())
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// Sign returns a bundle over payload with a public key hint.
func (k *Key) Sign(t testing.TB, payload []byte) json.RawMessage {
	t.Helper()
	return signDSSE(t, payload, k.kp, sign.BundleOptions{})
}

// CA is a company certificate authority.
type CA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewCA creates a self-signed root.
func NewCA(t testing.TB) *CA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Company CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &CA{cert: cert, key: key}
}

// WriteRoot writes the PEM root certificate to path.
func (c *CA) WriteRoot(t testing.TB, path string) {
	t.Helper()
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.cert.Raw})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Fingerprint is the root's fingerprint as bino.lock records it.
func (c *CA) Fingerprint() string { return registry.CertFingerprint(c.cert) }

// Sign returns a bundle over payload, signed by a fresh leaf for an email
// identity that this CA issues.
func (c *CA) Sign(t testing.TB, identity string, payload []byte) json.RawMessage {
	t.Helper()
	kp, err := sign.NewEphemeralKeypair(nil)
	if err != nil {
		t.Fatal(err)
	}
	return signDSSE(t, payload, kp, sign.BundleOptions{CertificateProvider: leafProvider{ca: c, identity: identity}})
}

type leafProvider struct {
	ca       *CA
	identity string
}

func (p leafProvider) GetCertificate(_ context.Context, kp sign.Keypair, _ *sign.CertificateProviderOptions) ([]byte, error) {
	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(time.Now().UnixNano()),
		EmailAddresses: []string{p.identity},
		NotBefore:      time.Now().Add(-time.Minute),
		NotAfter:       time.Now().Add(time.Hour),
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}
	return x509.CreateCertificate(rand.Reader, tmpl, p.ca.cert, kp.GetPublicKey(), p.ca.key)
}

func signDSSE(t testing.TB, payload []byte, kp sign.Keypair, opts sign.BundleOptions) json.RawMessage {
	t.Helper()
	pb, err := sign.Bundle(&sign.DSSEData{Data: payload, PayloadType: registryattest.PayloadType}, kp, opts)
	if err != nil {
		t.Fatal(err)
	}
	return marshal(t, pb)
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func marshal(t testing.TB, pb *protobundle.Bundle) json.RawMessage {
	t.Helper()
	b, err := protojson.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
