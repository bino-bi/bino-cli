package registry

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"bino.bi/bino/internal/pathutil"
)

// EnvTrustPolicy names a TOML file whose [trust] table is layered on top of
// the project's and the global one, for example a policy CI provides.
const EnvTrustPolicy = "BINO_TRUST_POLICY"

// Requirement levels of [trust] require, weakest first.
const (
	RequireOff    = "off"
	RequireWarn   = "warn"
	RequireSigned = "signed"
)

// Signer kinds.
const (
	SignerKeyless = "keyless"
	SignerX509    = "x509"
	SignerKey     = "key"
)

// ErrTrustConfig reports a [trust] table that cannot be used. A broken policy
// fails the command instead of silently trusting less than the user wrote.
var ErrTrustConfig = errors.New("invalid [trust] configuration")

// TrustPolicy is the effective [trust] policy of a project: the project's own
// table with the global and the BINO_TRUST_POLICY tables layered on top.
//
// Upper layers can only tighten. Require is the strictest level any layer
// sets. Signer rules are not merged: each layer keeps its own list, and for a
// package every layer that has a rule for it contributes its first matching
// rule. The package is trusted only when its signatures satisfy all of those
// rules. So an upper layer can add a rule for a scope the project leaves open,
// or add a second requirement for a scope the project covers, but it cannot
// remove or replace a project rule.
type TrustPolicy struct {
	Require string
	rank    int
	layers  []trustLayer
}

type trustLayer struct {
	source string
	rules  []trustRule
}

type trustRule struct {
	scope string // "@scope/*" or "@scope/name"
	kind  string

	issuer     string // keyless
	identity   string // keyless SAN, or the x509 leaf identity
	repository string // keyless CI source repository
	workflow   string // keyless CI workflow, without the ref

	root   *x509.Certificate // x509
	rootFP string

	key     crypto.PublicKey // key
	keyFP   string
	keyHint string // the key hint sigstore-go writes by default: base64 of the DER's SHA-256
}

// LoadTrustPolicy compiles the project's [trust] table and layers the global
// config and the BINO_TRUST_POLICY file on top. Relative file paths in a rule
// resolve against the directory of the file that declares it.
func LoadTrustPolicy(projectRoot string, project pathutil.TrustConfig) (*TrustPolicy, error) {
	p := &TrustPolicy{Require: RequireOff}
	if err := p.addLayer(pathutil.ProjectConfigPath(projectRoot), project); err != nil {
		return nil, err
	}

	gc, err := LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	globalPath, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	if err := p.addLayer(globalPath, gc.Trust); err != nil {
		return nil, err
	}

	if envPath := strings.TrimSpace(os.Getenv(EnvTrustPolicy)); envPath != "" {
		// A named policy file that is missing is an error, not an empty layer:
		// CI that points at a policy expects it to apply.
		data, err := os.ReadFile(envPath) //nolint:gosec // G703: the user names this file on purpose
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrTrustConfig, EnvTrustPolicy, err)
		}
		var file struct {
			Trust pathutil.TrustConfig `toml:"trust"`
		}
		if err := toml.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("%w: parse %s: %w", ErrTrustConfig, envPath, err)
		}
		if err := p.addLayer(envPath, file.Trust); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *TrustPolicy) addLayer(source string, cfg pathutil.TrustConfig) error {
	level, err := requireRank(cfg.Require)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrTrustConfig, source, err)
	}
	if level > p.rank {
		p.Require, p.rank = cfg.Require, level
	}
	layer := trustLayer{source: source}
	for i, sc := range cfg.Signers {
		r, err := compileRule(filepath.Dir(source), sc)
		if err != nil {
			return fmt.Errorf("%w: %s: [[trust.signer]] #%d: %w", ErrTrustConfig, source, i+1, err)
		}
		layer.rules = append(layer.rules, r)
	}
	if len(layer.rules) > 0 {
		p.layers = append(p.layers, layer)
	}
	return nil
}

func requireRank(level string) (int, error) {
	switch level {
	case "", RequireOff:
		return 0, nil
	case RequireWarn:
		return 1, nil
	case RequireSigned:
		return 2, nil
	}
	return 0, fmt.Errorf("require = %q: want %q, %q or %q", level, RequireOff, RequireWarn, RequireSigned)
}

func compileRule(baseDir string, sc pathutil.TrustSignerConfig) (trustRule, error) {
	r := trustRule{scope: sc.Scope}
	if err := validateTrustScope(sc.Scope); err != nil {
		return trustRule{}, err
	}
	kinds := 0
	for _, set := range []bool{sc.OIDCIssuer != "", sc.X509Root != "", sc.KeyFile != "" || sc.Key != ""} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return trustRule{}, errors.New("set exactly one of oidc-issuer, x509-root or key-file")
	}
	switch {
	case sc.OIDCIssuer != "":
		r.kind = SignerKeyless
		r.issuer = sc.OIDCIssuer
		byIdentity := sc.Identity != ""
		byWorkflow := sc.Repository != "" || sc.Workflow != ""
		switch {
		case byIdentity && byWorkflow:
			return trustRule{}, errors.New("identity cannot be combined with repository and workflow")
		case byWorkflow && (sc.Repository == "" || sc.Workflow == ""):
			return trustRule{}, errors.New("repository and workflow must be set together")
		case !byIdentity && !byWorkflow:
			return trustRule{}, errors.New("oidc-issuer needs identity, or repository and workflow")
		case byWorkflow && strings.Contains(sc.Workflow, "@"):
			return trustRule{}, fmt.Errorf("workflow %q must not carry a ref (\"@...\")", sc.Workflow)
		}
		r.identity, r.repository, r.workflow = sc.Identity, sc.Repository, sc.Workflow
	case sc.X509Root != "":
		if sc.Repository != "" || sc.Workflow != "" {
			return trustRule{}, errors.New("repository and workflow only apply to oidc-issuer rules")
		}
		cert, err := readCertificate(resolveTrustPath(baseDir, sc.X509Root))
		if err != nil {
			return trustRule{}, fmt.Errorf("x509-root: %w", err)
		}
		r.kind = SignerX509
		r.root = cert
		r.rootFP = CertFingerprint(cert)
		r.identity = sc.Identity
	default:
		if sc.Identity != "" || sc.Repository != "" || sc.Workflow != "" {
			return trustRule{}, errors.New("identity, repository and workflow do not apply to key rules")
		}
		if sc.KeyFile == "" {
			return trustRule{}, errors.New("key needs key-file: the fingerprint pins the key, the file provides it")
		}
		pub, err := readPublicKey(resolveTrustPath(baseDir, sc.KeyFile))
		if err != nil {
			return trustRule{}, fmt.Errorf("key-file: %w", err)
		}
		fp, err := KeyFingerprint(pub)
		if err != nil {
			return trustRule{}, fmt.Errorf("key-file: %w", err)
		}
		if sc.Key != "" && sc.Key != fp {
			return trustRule{}, fmt.Errorf("key-file %s has fingerprint %s, not the pinned %s", sc.KeyFile, fp, sc.Key)
		}
		r.kind = SignerKey
		r.key = pub
		r.keyFP = fp
		r.keyHint = keyHint(pub)
	}
	return r, nil
}

func validateTrustScope(scope string) error {
	if s, ok := strings.CutSuffix(scope, "/*"); ok {
		if !strings.HasPrefix(s, "@") || !tokenRe.MatchString(s[1:]) {
			return fmt.Errorf("scope %q: want \"@scope/*\" or \"@scope/name\"", scope)
		}
		return nil
	}
	if _, _, err := ParseName(scope); err != nil {
		return fmt.Errorf("scope %q: want \"@scope/*\" or \"@scope/name\"", scope)
	}
	return nil
}

func resolveTrustPath(baseDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, filepath.FromSlash(p))
}

func readCertificate(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s: no PEM CERTIFICATE block", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

func readPublicKey(path string) (crypto.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("%s: no PEM PUBLIC KEY block", path)
	}
	return x509.ParsePKIXPublicKey(block.Bytes)
}

// KeyFingerprint names a public key as "<alg>:SHA256:<base64>", the SHA-256
// of its DER (PKIX) encoding without padding.
func KeyFingerprint(pub crypto.PublicKey) (string, error) {
	var alg string
	switch pub.(type) {
	case ed25519.PublicKey:
		alg = "ed25519"
	case *ecdsa.PublicKey:
		alg = "ecdsa"
	case *rsa.PublicKey:
		alg = "rsa"
	default:
		return "", fmt.Errorf("unsupported public key type %T", pub)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return alg + ":SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

func keyHint(pub crypto.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// CertFingerprint names a CA certificate as "SHA256:<base64>" of its DER.
func CertFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func (r trustRule) covers(pkg string) bool {
	if s, ok := strings.CutSuffix(r.scope, "/*"); ok {
		return strings.HasPrefix(pkg, s+"/")
	}
	return r.scope == pkg
}

func (r trustRule) matches(s Signer) bool {
	if s.Kind != r.kind {
		return false
	}
	switch r.kind {
	case SignerKeyless:
		if s.Issuer != r.issuer {
			return false
		}
		if r.identity != "" {
			return s.Identity == r.identity
		}
		return s.Repository == r.repository && s.Workflow == r.workflow
	case SignerX509:
		return s.Root == r.rootFP && (r.identity == "" || s.Identity == r.identity)
	case SignerKey:
		return s.Key == r.keyFP
	}
	return false
}

// Level returns the effective requirement level.
func (p *TrustPolicy) Level() string { return p.Require }

// Evaluate checks a package's verified signers against the policy. covered
// reports whether any layer has a rule for the package; trusted, that every
// such layer's first matching rule is satisfied by one of the signers. matched
// is the signer that satisfied the lowest covering layer.
func (p *TrustPolicy) Evaluate(pkg string, signers []Signer) (covered, trusted bool, matched *Signer) {
	trusted = true
	for _, layer := range p.layers {
		r, ok := layer.first(pkg)
		if !ok {
			continue
		}
		covered = true
		var hit *Signer
		for i := range signers {
			if r.matches(signers[i]) {
				hit = &signers[i]
				break
			}
		}
		if hit == nil {
			trusted = false
			continue
		}
		if matched == nil {
			matched = hit
		}
	}
	if !covered {
		return false, false, nil
	}
	if !trusted {
		matched = nil
	}
	return covered, trusted, matched
}

// MatchesAny reports whether s satisfies at least one layer's rule for pkg.
// Lint uses it on the one signer the lock records, which cannot show every
// layer's requirement at once.
func (p *TrustPolicy) MatchesAny(pkg string, s Signer) (covered, ok bool) {
	for _, layer := range p.layers {
		r, has := layer.first(pkg)
		if !has {
			continue
		}
		covered = true
		if r.matches(s) {
			return true, true
		}
	}
	return covered, false
}

func (l trustLayer) first(pkg string) (trustRule, bool) {
	for _, r := range l.rules {
		if r.covers(pkg) {
			return r, true
		}
	}
	return trustRule{}, false
}

// anchors returns the CA roots and public keys of every rule that covers pkg,
// in any layer. Only these can verify an x509 or key signature on it: a CA a
// user trusts for one scope says nothing about another.
func (p *TrustPolicy) anchors(pkg string) (roots []trustRule, keys []trustRule) {
	seen := map[string]bool{}
	for _, layer := range p.layers {
		for _, r := range layer.rules {
			if !r.covers(pkg) {
				continue
			}
			switch r.kind {
			case SignerX509:
				if !seen[r.rootFP] {
					seen[r.rootFP] = true
					roots = append(roots, r)
				}
			case SignerKey:
				if !seen[r.keyFP] {
					seen[r.keyFP] = true
					keys = append(keys, r)
				}
			}
		}
	}
	return roots, keys
}

// TrustDecision is the outcome of judging one package version's signatures.
type TrustDecision struct {
	Signer  *Signer // the signer bino.lock pins; nil when none verified
	Warning string  // set when require = "warn" does not accept the package
}

// Judge applies the lock pin and the policy to a version's verified
// signatures. pinned is the signer the lock records for the package, nil when
// it records none. acceptChange waives the pin, which is what 'bino registry
// trust accept' does; the policy still applies.
//
// A pinned package whose new version lacks the pinned signer fails at every
// require level: the lock is the user's record of who released it before.
func (p *TrustPolicy) Judge(pkg, version string, check SignatureCheck, pinned *Signer, acceptChange bool) (TrustDecision, error) {
	signers := check.Signers()
	var kept *Signer
	if pinned != nil {
		for i := range signers {
			if signers[i].Same(*pinned) {
				kept = &signers[i]
				break
			}
		}
		if kept == nil && !acceptChange {
			now := "is unsigned"
			if len(signers) > 0 {
				now = "is signed by " + signers[0].String()
			}
			return TrustDecision{}, fmt.Errorf("%w: %s was signed by %s, but %s %s; run 'bino registry trust accept %s' if the change is expected",
				ErrSignerChanged, pkg, pinned.String(), version, now, pkg)
		}
	}

	covered, trusted, matched := p.Evaluate(pkg, signers)
	d := TrustDecision{Signer: kept}
	if d.Signer == nil {
		d.Signer = matched
	}
	if d.Signer == nil && len(signers) > 0 {
		d.Signer = &signers[0]
		for i, v := range check.Verified {
			if v.Kind == "publish" {
				d.Signer = &signers[i]
				break
			}
		}
	}
	if d.Signer != nil {
		s := *d.Signer
		d.Signer = &s
	}
	if trusted {
		return d, nil
	}

	var why string
	switch {
	case len(signers) == 0 && check.Unverifiable == 0:
		why = "is not signed"
	case len(signers) == 0:
		why = "is signed, but with a key or CA no [trust] signer rule for it provides"
	case !covered:
		why = "is signed by " + signers[0].String() + ", but no [trust] signer rule covers it"
	default:
		why = "is signed by " + signers[0].String() + ", which does not satisfy the [trust] signer rules for it"
	}
	msg := fmt.Sprintf("%s@%s %s", pkg, version, why)
	switch p.Require {
	case RequireSigned:
		return TrustDecision{}, fmt.Errorf("%w: %s (require = \"signed\")", ErrUntrusted, msg)
	case RequireWarn:
		d.Warning = msg
	}
	return d, nil
}
