package registry

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bino-bi/bino-plugin-sdk/registryattest"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
	"google.golang.org/protobuf/encoding/protojson"
)

// Signature errors. ErrInvalidSignature is a bundle that is broken or does not
// describe this release; it fails at every require level. ErrSignerChanged is
// a locked package whose new version is signed by someone else, or not at all.
// ErrUntrusted is a package the policy does not accept under require = "signed".
var (
	ErrInvalidSignature = errors.New("invalid package signature")
	ErrSignerChanged    = errors.New("package signer changed")
	ErrUntrusted        = errors.New("package is not signed by a trusted signer")
)

// Signer identifies who signed a release, in the stable form bino.lock pins.
//
// For a keyless signature from a CI workload (the certificate carries the
// Fulcio source-repository and build-signer extensions), Repository and
// Workflow identify the signer and Identity, the full SAN, is informative
// only: the SAN of a GitHub Actions certificate ends in the ref that ran
// ("...publish.yml@refs/tags/v1.2.0"), so it changes with every release.
// RepositoryID and OwnerID are the numeric IDs the forge assigned, when the
// certificate carries them: a deleted and re-registered owner gets the same
// name but new IDs.
type Signer struct {
	Kind         string `toml:"kind" json:"kind"`
	Issuer       string `toml:"issuer,omitempty" json:"issuer,omitempty"`
	Identity     string `toml:"identity,omitempty" json:"identity,omitempty"`
	Repository   string `toml:"repository,omitempty" json:"repository,omitempty"`
	RepositoryID string `toml:"repository_id,omitempty" json:"repository_id,omitempty"`
	OwnerID      string `toml:"owner_id,omitempty" json:"owner_id,omitempty"`
	Workflow     string `toml:"workflow,omitempty" json:"workflow,omitempty"`
	Root         string `toml:"root,omitempty" json:"root,omitempty"`
	Key          string `toml:"key,omitempty" json:"key,omitempty"`
}

// Same reports whether s and o are the same signer for pinning purposes.
func (s Signer) Same(o Signer) bool {
	if s.Kind != o.Kind {
		return false
	}
	switch s.Kind {
	case SignerKeyless:
		if s.Issuer != o.Issuer {
			return false
		}
		if s.Workflow != "" || o.Workflow != "" {
			return s.Repository == o.Repository && s.Workflow == o.Workflow &&
				s.RepositoryID == o.RepositoryID && s.OwnerID == o.OwnerID
		}
		return s.Identity == o.Identity
	case SignerX509:
		return s.Root == o.Root && s.Identity == o.Identity
	case SignerKey:
		return s.Key == o.Key
	}
	return false
}

// String names the signer for a terminal. Its fields come from a certificate
// someone else made, so control characters are stripped.
func (s Signer) String() string {
	var out string
	switch s.Kind {
	case SignerKeyless:
		if s.Workflow != "" {
			out = fmt.Sprintf("workflow %s of %s (issuer %s)", s.Workflow, s.Repository, s.Issuer)
		} else {
			out = fmt.Sprintf("%s (issuer %s)", s.Identity, s.Issuer)
		}
	case SignerX509:
		out = fmt.Sprintf("%s (CA %s)", s.Identity, s.Root)
	case SignerKey:
		out = "key " + s.Key
	default:
		out = s.Kind
	}
	return Printable(out)
}

// Printable strips control and format characters (including bidi overrides)
// from a string that came from the registry or a bundle, so it cannot move
// the cursor or disguise itself on a terminal.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// Attestation is one signed statement the registry serves for a version. The
// descriptive fields are the registry's claim and are never trusted: only the
// bundle is verified.
type Attestation struct {
	Kind       string          `json:"kind"`
	Signer     string          `json:"signer"`
	Issuer     string          `json:"issuer,omitempty"`
	RootKind   string          `json:"rootKind"`
	VerifiedAt string          `json:"verifiedAt,omitempty"`
	URL        string          `json:"url,omitempty"`
	Bundle     json.RawMessage `json:"bundle,omitempty"`
}

// Attestations fetches the attestations of one version. A registry without
// the route, or a version without attestations, yields none: both mean
// "unsigned", which the trust policy then judges.
func (c *Client) Attestations(ctx context.Context, scope, name, version string) ([]Attestation, error) {
	u := fmt.Sprintf("%s/api/registry/v2/attestations/%s/%s/%s", c.baseURL,
		url.PathEscape(scope), url.PathEscape(name), url.PathEscape(version))
	body, _, err := c.get(ctx, u)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	var out struct {
		Attestations []Attestation `json:"attestations"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("registry: decode attestations response: %w", err)
	}
	return out.Attestations, nil
}

// SignatureCheck is the result of verifying a version's attestations.
// Unverifiable counts bundles that are well formed and describe this release
// but cannot be checked: they name a key or CA no [trust] rule for the package
// provides, their certificate was not valid at the time bino can establish, or
// their log or CA is unknown to the built-in Sigstore root. bino can neither
// trust nor reject them. Times are the verification times of the verified
// bundles that carry no signed time, for the lock to record.
type SignatureCheck struct {
	Verified     []VerifiedSignature
	Unverifiable int
	Times        []BundleTime
}

// VerifiedSignature is one attestation whose signature checked out.
type VerifiedSignature struct {
	Signer  Signer
	Channel registryattest.PublishChannel
}

// Signers returns the verified signers in attestation order.
func (c SignatureCheck) Signers() []Signer {
	out := make([]Signer, len(c.Verified))
	for i, v := range c.Verified {
		out[i] = v.Signer
	}
	return out
}

// maxAttestations bounds the work a registry can make one version cost; a
// release carries a publish signature and at most a few approvals.
const maxAttestations = 16

//go:embed sigstore_trusted_root.json
var embeddedTrustedRoot []byte

type keylessTrust struct {
	material   root.TrustedMaterial
	requireSCT bool
}

var (
	keylessMu      sync.Mutex
	keylessCurrent *keylessTrust
)

// keyless returns the Sigstore public-good trust root. It is embedded, so
// verification never needs the network: the Rekor inclusion proof and the
// signed entry timestamp travel in the bundle.
func keyless() (*keylessTrust, error) {
	keylessMu.Lock()
	defer keylessMu.Unlock()
	if keylessCurrent != nil {
		return keylessCurrent, nil
	}
	tr, err := root.NewTrustedRootFromJSON(embeddedTrustedRoot)
	if err != nil {
		return nil, fmt.Errorf("load embedded Sigstore trusted root: %w", err)
	}
	keylessCurrent = &keylessTrust{material: tr, requireSCT: true}
	return keylessCurrent, nil
}

// SetKeylessTrustForTesting replaces the keyless trust root, for tests that
// sign with a virtual Sigstore whose certificates carry no SCTs. It returns a
// function that restores the embedded root.
func SetKeylessTrustForTesting(material root.TrustedMaterial, requireSCT bool) (restore func()) {
	keylessMu.Lock()
	prev := keylessCurrent
	keylessCurrent = &keylessTrust{material: material, requireSCT: requireSCT}
	keylessMu.Unlock()
	return func() {
		keylessMu.Lock()
		keylessCurrent = prev
		keylessMu.Unlock()
	}
}

// VerifyAttestations verifies every attestation of pkg@version offline and
// returns the signers that checked out. manifestDigest must be computed
// locally from the verified file list, never taken from the registry.
// recorded holds the times earlier runs first verified bundles that carry no
// signed time; such a bundle found there is checked at that time, any other
// at the current time.
//
// A bundle that does not parse, is older than v0.2, is not a DSSE in-toto
// statement for exactly this release, or fails its signature check against a
// trust anchor it belongs to is ErrInvalidSignature. A bundle bino cannot
// check (see SignatureCheck) is counted as unverifiable instead.
func VerifyAttestations(pkg, version, manifestDigest string, atts []Attestation, policy *TrustPolicy, recorded []BundleTime) (SignatureCheck, error) {
	var out SignatureCheck
	if len(atts) == 0 {
		return out, nil
	}
	if len(atts) > maxAttestations {
		return out, fmt.Errorf("%w: %s@%s: %d attestations, more than the %d bino verifies", ErrInvalidSignature, pkg, version, len(atts), maxAttestations)
	}
	scope, base, err := ParseName(pkg)
	if err != nil {
		return out, err
	}
	digestHex, ok := strings.CutPrefix(manifestDigest, "sha256:")
	if !ok {
		return out, fmt.Errorf("%w: %s@%s: manifest digest %q is not sha256", ErrInvalidSignature, pkg, version, manifestDigest)
	}
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return out, fmt.Errorf("%w: %s@%s: manifest digest %q: %w", ErrInvalidSignature, pkg, version, manifestDigest, err)
	}
	kt, err := keyless()
	if err != nil {
		return out, err
	}
	roots, keys := policy.anchors(pkg)
	now := time.Now().UTC().Truncate(time.Second)
	for i, a := range atts {
		fail := func(format string, args ...any) error {
			return fmt.Errorf("%w: %s@%s: attestation %d (%s): %s", ErrInvalidSignature, pkg, version, i+1, Printable(a.Kind), fmt.Sprintf(format, args...))
		}
		b, payload, err := parseAttestationBundle(a.Bundle)
		if err != nil {
			return SignatureCheck{}, fail("%v", err)
		}
		if err := registryattest.CheckStatement(payload, scope, base, version, manifestDigest); err != nil {
			return SignatureCheck{}, fail("%v", err)
		}
		channel, err := registryattest.ReadPublishChannel(payload)
		if err != nil {
			return SignatureCheck{}, fail("%v", err)
		}
		id := BundleDigest(a.Bundle)
		at := now
		for _, r := range recorded {
			if r.Bundle == id {
				at = r.At
				break
			}
		}
		signer, known, clockless, err := verifyBundle(b, digest, kt, roots, keys, at)
		if err != nil {
			return SignatureCheck{}, fail("%v", err)
		}
		if !known {
			out.Unverifiable++
			continue
		}
		out.Verified = append(out.Verified, VerifiedSignature{Signer: signer, Channel: channel})
		if clockless {
			out.Times = append(out.Times, BundleTime{Bundle: id, At: at})
		}
	}
	return out, nil
}

// BundleDigest names a bundle by the sha256 of its compact JSON, so the same
// bundle served with other whitespace keeps its name.
func BundleDigest(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		buf.Reset()
		buf.Write(raw)
	}
	sum := sha256.Sum256(buf.Bytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func parseAttestationBundle(raw json.RawMessage) (*bundle.Bundle, []byte, error) {
	if len(raw) == 0 {
		return nil, nil, errors.New("no bundle")
	}
	pb := new(protobundle.Bundle)
	if err := protojson.Unmarshal(raw, pb); err != nil {
		return nil, nil, fmt.Errorf("decode bundle: %w", err)
	}
	// The version decides the options: sigstore-go refuses a v0.2 bundle
	// when certificate chains are allowed, which v0.3 needs for a company CA.
	probe := &bundle.Bundle{Bundle: pb}
	if _, err := probe.Version(); err != nil {
		return nil, nil, fmt.Errorf("decode bundle: %w", err)
	}
	if !probe.MinVersion("v0.2") {
		return nil, nil, errors.New("bundle is older than v0.2")
	}
	var opts []bundle.Option
	if probe.MinVersion("v0.3") {
		opts = append(opts, bundle.AllowCertificateChain())
	}
	b, err := bundle.NewBundle(pb, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("decode bundle: %w", err)
	}
	env, err := b.Envelope()
	if err != nil {
		return nil, nil, fmt.Errorf("bundle carries no DSSE envelope: %w", err)
	}
	raw2 := env.RawEnvelope()
	if raw2.PayloadType != registryattest.PayloadType {
		return nil, nil, fmt.Errorf("DSSE payload type %q, want %q", raw2.PayloadType, registryattest.PayloadType)
	}
	payload, err := base64.StdEncoding.DecodeString(raw2.Payload)
	if err != nil {
		return nil, nil, fmt.Errorf("decode DSSE payload: %w", err)
	}
	return b, payload, nil
}

// verifyBundle picks the trust anchor a bundle belongs to and verifies it
// against that anchor alone. known is false when bino cannot check the bundle:
// no anchor applies, its transparency log is not one the built-in root knows,
// or its x509 certificate was not valid at the time it can be checked at.
// clockless reports an x509 bundle with neither a log entry nor a signed
// timestamp, which was checked at "at".
func verifyBundle(b *bundle.Bundle, digest []byte, kt *keylessTrust, roots, keys []trustRule, at time.Time) (s Signer, known, clockless bool, err error) {
	vc, err := b.VerificationContent()
	if err != nil {
		return Signer{}, false, false, err
	}
	policy := func(opt verify.PolicyOption) verify.PolicyBuilder {
		return verify.NewPolicy(verify.WithArtifactDigest("sha256", digest), opt)
	}
	// A log entry is what gives the signing time. One from a log the built-in
	// root does not know means the root is older than the bundle, which is
	// not a forgery: bino cannot check it.
	if len(b.VerificationMaterial.GetTlogEntries()) > 0 && !knownLog(b, kt.material.RekorLogs()) {
		return Signer{}, false, false, nil
	}

	if pk := vc.PublicKey(); pk != nil {
		verifyWith := func(k trustRule) error {
			v, err := verify.NewVerifier(anchorMaterial(kt.material, nil, k.key), timeOptions(b, false)...)
			if err != nil {
				return err
			}
			_, err = v.Verify(b, policy(verify.WithKey()))
			return err
		}
		// A hint naming a trusted key says which key must verify, so its
		// failure is a bad signature. Otherwise every key covering the
		// package is tried, and none verifying means an unknown key.
		if hint := pk.Hint(); hint != "" {
			for _, k := range keys {
				if hint == k.keyFP || hint == k.keyHint {
					if err := verifyWith(k); err != nil {
						return Signer{}, false, false, err
					}
					return Signer{Kind: SignerKey, Key: k.keyFP}, true, false, nil
				}
			}
		}
		for _, k := range keys {
			if verifyWith(k) == nil {
				return Signer{Kind: SignerKey, Key: k.keyFP}, true, false, nil
			}
		}
		return Signer{}, false, false, nil
	}

	leaf := vc.Certificate()
	if leaf == nil {
		return Signer{}, false, false, errors.New("bundle has neither a certificate nor a public key")
	}
	intermediates := vc.Intermediates()
	if chainsToAny(leaf, intermediates, kt.material.FulcioCertificateAuthorities(), leaf.NotBefore) {
		if !knownLog(b, kt.material.RekorLogs()) {
			return Signer{}, false, false, nil
		}
		opts := []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)}
		if kt.requireSCT {
			opts = append(opts, verify.WithSignedCertificateTimestamps(1))
		}
		v, err := verify.NewVerifier(kt.material, opts...)
		if err != nil {
			return Signer{}, false, false, err
		}
		res, err := v.Verify(b, policy(verify.WithoutIdentitiesUnsafe()))
		if err != nil {
			return Signer{}, false, false, err
		}
		s, err := keylessSigner(*res.Signature.Certificate)
		return s, err == nil, false, err
	}
	for _, r := range roots {
		ca := &root.FulcioCertificateAuthority{Root: r.root}
		if !chainsToAny(leaf, intermediates, []root.CertificateAuthority{ca}, leaf.NotBefore) {
			continue
		}
		opts := timeOptions(b, true)
		var anchor root.CertificateAuthority = ca
		if clockless = opts == nil; clockless {
			// No signed time: check at the time this bundle was first
			// verified. A certificate not valid then has expired, which is
			// not a forgery.
			if !chainsToAny(leaf, intermediates, []root.CertificateAuthority{ca}, at) {
				return Signer{}, false, false, nil
			}
			anchor = fixedTimeCA{ca: &root.FulcioCertificateAuthority{Root: r.root, Intermediates: intermediates}, at: at}
			opts = []verify.VerifierOption{verify.WithCurrentTime()}
		}
		v, err := verify.NewVerifier(anchorMaterial(kt.material, anchor, nil), opts...)
		if err != nil {
			return Signer{}, false, false, err
		}
		res, err := v.Verify(b, policy(verify.WithoutIdentitiesUnsafe()))
		if err != nil {
			return Signer{}, false, false, err
		}
		identity := res.Signature.Certificate.SubjectAlternativeName
		if identity == "" {
			identity = leaf.Subject.String()
		}
		return Signer{Kind: SignerX509, Root: r.rootFP, Identity: identity}, true, clockless, nil
	}
	return Signer{}, false, false, nil
}

func knownLog(b *bundle.Bundle, logs map[string]*root.TransparencyLog) bool {
	for _, e := range b.VerificationMaterial.GetTlogEntries() {
		if _, ok := logs[hex.EncodeToString(e.GetLogId().GetKeyId())]; ok {
			return true
		}
	}
	return false
}

// fixedTimeCA checks a certificate chain at a fixed time instead of the one
// sigstore-go passes, which for a bundle without a signed time is now.
type fixedTimeCA struct {
	ca *root.FulcioCertificateAuthority
	at time.Time
}

func (c fixedTimeCA) Verify(cert *x509.Certificate, _ time.Time) ([][]*x509.Certificate, error) {
	return c.ca.Verify(cert, c.at)
}

func keylessSigner(sum certificate.Summary) (Signer, error) {
	if sum.Issuer == "" || sum.SubjectAlternativeName == "" {
		return Signer{}, errors.New("keyless certificate names no issuer or identity")
	}
	s := Signer{Kind: SignerKeyless, Issuer: sum.Issuer, Identity: sum.SubjectAlternativeName}
	if sum.SourceRepositoryURI != "" && sum.BuildSignerURI != "" {
		s.Repository = sum.SourceRepositoryURI
		s.RepositoryID = sum.SourceRepositoryIdentifier
		s.OwnerID = sum.SourceRepositoryOwnerIdentifier
		s.Workflow, _, _ = strings.Cut(sum.BuildSignerURI, "@")
	}
	return s, nil
}

// timeOptions decides which clock a non-keyless signature is checked at. A
// transparency log entry or a signed timestamp, when present, is verified
// against the public-good root and gives the signing time. Without one, a key
// needs no time at all, and an x509 leaf gets nil: the caller checks it at
// the time the bundle was first verified.
func timeOptions(b *bundle.Bundle, cert bool) []verify.VerifierOption {
	vm := b.VerificationMaterial
	switch {
	case len(vm.GetTlogEntries()) > 0:
		return []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)}
	case len(vm.GetTimestampVerificationData().GetRfc3161Timestamps()) > 0:
		return []verify.VerifierOption{verify.WithSignedTimestamps(1)}
	case cert:
		return nil
	}
	return []verify.VerifierOption{verify.WithNoObserverTimestamps()}
}

// chainsToAny reports whether leaf chains to one of the CAs at time t, for
// any key usage. It only classifies a bundle; the real check is sigstore-go's.
func chainsToAny(leaf *x509.Certificate, intermediates []*x509.Certificate, cas []root.CertificateAuthority, t time.Time) bool {
	c := *leaf
	c.UnhandledCriticalExtensions = nil // Fulcio's otherName SAN; see verify.Verify
	for _, ca := range cas {
		fca, ok := ca.(*root.FulcioCertificateAuthority)
		if !ok || fca.Root == nil {
			continue
		}
		roots := x509.NewCertPool()
		roots.AddCert(fca.Root)
		pool := x509.NewCertPool()
		for _, ic := range append(append([]*x509.Certificate{}, fca.Intermediates...), intermediates...) {
			pool.AddCert(ic)
		}
		_, err := c.Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: pool,
			CurrentTime:   t,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		})
		if err == nil {
			return true
		}
	}
	return false
}

// trustAnchor is the trust material for one x509 CA or one public key: that
// anchor alone, plus the public-good logs and timestamp authorities so a
// transparency log entry or a signed timestamp in the bundle can be checked.
type trustAnchor struct {
	root.BaseTrustedMaterial
	logs root.TrustedMaterial
	ca   root.CertificateAuthority
	key  root.TimeConstrainedVerifier
}

func anchorMaterial(logs root.TrustedMaterial, ca root.CertificateAuthority, key crypto.PublicKey) *trustAnchor {
	m := &trustAnchor{logs: logs, ca: ca}
	if key != nil {
		if v, err := signature.LoadDefaultVerifier(key); err == nil {
			m.key = root.NewExpiringKey(v, time.Time{}, time.Time{})
		}
	}
	return m
}

func (m *trustAnchor) TimestampingAuthorities() []root.TimestampingAuthority {
	return m.logs.TimestampingAuthorities()
}

func (m *trustAnchor) RekorLogs() map[string]*root.TransparencyLog { return m.logs.RekorLogs() }

func (m *trustAnchor) FulcioCertificateAuthorities() []root.CertificateAuthority {
	if m.ca == nil {
		return nil
	}
	return []root.CertificateAuthority{m.ca}
}

// PublicKeyVerifier ignores the hint: the bundle's hint is the signer's own
// claim, and the only key on offer is the one the policy named.
func (m *trustAnchor) PublicKeyVerifier(string) (root.TimeConstrainedVerifier, error) {
	if m.key == nil {
		return nil, errors.New("no trusted key")
	}
	return m.key, nil
}

func (m *trustAnchor) PublicKeyVerifiers() []root.TimeConstrainedVerifier {
	if m.key == nil {
		return nil
	}
	return []root.TimeConstrainedVerifier{m.key}
}
