package registry

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/bino-bi/bino-plugin-sdk/registryattest"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/sign"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"google.golang.org/protobuf/encoding/protojson"
)

// The Sigstore public-good instance keyless publishing signs with.
const (
	publicFulcioURL = "https://fulcio.sigstore.dev"
	publicRekorURL  = "https://rekor.sigstore.dev"
)

// ReleaseSigner signs a release statement into a Sigstore bundle. RootKind is
// the kind of root the bundle chains to ("keyless", "x509" or "key"); Signer
// names who signed once Sign has run.
type ReleaseSigner struct {
	RootKind string
	Signer   string

	keypair sign.Keypair
	opts    sign.BundleOptions
}

// Sign returns the bundle JSON for a DSSE envelope over statement.
func (s *ReleaseSigner) Sign(ctx context.Context, statement []byte) (json.RawMessage, error) {
	opts := s.opts
	opts.Context = ctx
	pb, err := sign.Bundle(&sign.DSSEData{Data: statement, PayloadType: registryattest.PayloadType}, s.keypair, opts)
	if err != nil {
		return nil, fmt.Errorf("sign release: %w", err)
	}
	if raw := pb.GetVerificationMaterial().GetCertificate().GetRawBytes(); raw != nil {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return nil, fmt.Errorf("sign release: %w", err)
		}
		if sum, err := certificate.SummarizeCertificate(cert); err == nil && sum.SubjectAlternativeName != "" {
			s.Signer = sum.SubjectAlternativeName
		}
	}
	return protojson.Marshal(pb)
}

// KeylessServices are the Fulcio and Rekor a keyless signature goes through,
// and the key it signs with. Tests replace them with a virtual Sigstore.
type KeylessServices struct {
	Key   crypto.Signer
	Certs sign.CertificateProvider
	Log   sign.Transparency
}

var keylessServices = func() (KeylessServices, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return KeylessServices{}, err
	}
	return KeylessServices{
		Key:   key,
		Certs: sign.NewFulcio(&sign.FulcioOptions{BaseURL: publicFulcioURL, Retries: 1}),
		Log:   sign.NewRekor(&sign.RekorOptions{BaseURL: publicRekorURL, Retries: 1, Version: 1}),
	}, nil
}

// SetKeylessServicesForTesting replaces the public Sigstore keyless signing
// goes through. It returns a function that restores it.
func SetKeylessServicesForTesting(f func() (KeylessServices, error)) (restore func()) {
	prev := keylessServices
	keylessServices = f
	return func() { keylessServices = prev }
}

// NewKeylessSigner signs with a short-lived key whose certificate Fulcio
// issues for the OIDC idToken, and logs the signature in Rekor. The bundle
// goes to a public log, so it must only be used for public packages.
func NewKeylessSigner(idToken string) (*ReleaseSigner, error) {
	svc, err := keylessServices()
	if err != nil {
		return nil, err
	}
	kp, err := newKeypair(svc.Key)
	if err != nil {
		return nil, err
	}
	return &ReleaseSigner{
		RootKind: SignerKeyless,
		keypair:  kp,
		opts: sign.BundleOptions{
			CertificateProvider:        svc.Certs,
			CertificateProviderOptions: &sign.CertificateProviderOptions{IDToken: idToken},
			TransparencyLogs:           []sign.Transparency{svc.Log},
		},
	}, nil
}

// NewKeySigner signs with the unencrypted PKCS #8 PEM private key in keyFile.
// Without chainFile the bundle names the key by its fingerprint. With it, the
// bundle carries the certificate chain in chainFile instead: the leaf for the
// key first, then intermediates. A self-signed root in the file is dropped,
// since verifiers bring their own.
func NewKeySigner(keyFile, chainFile string) (*ReleaseSigner, error) {
	key, err := loadPrivateKey(keyFile)
	if err != nil {
		return nil, err
	}
	kp, err := newKeypair(key)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keyFile, err)
	}
	if chainFile == "" {
		return &ReleaseSigner{RootKind: SignerKey, Signer: string(kp.hint), keypair: kp}, nil
	}
	chain, err := loadCertChain(chainFile)
	if err != nil {
		return nil, err
	}
	leaf := chain[0]
	if !publicKeysEqual(leaf.PublicKey, key.Public()) {
		return nil, fmt.Errorf("%s: the first certificate is not for the key in %s", chainFile, keyFile)
	}
	identity := leaf.Subject.String()
	if sum, err := certificate.SummarizeCertificate(leaf); err == nil && sum.SubjectAlternativeName != "" {
		identity = sum.SubjectAlternativeName
	}
	s := &ReleaseSigner{RootKind: SignerX509, Signer: identity, keypair: kp}
	if len(chain) == 1 {
		s.opts.CertificateProvider = fixedCerts{leaf.Raw}
	} else {
		der := make([][]byte, len(chain))
		for i, c := range chain {
			der[i] = c.Raw
		}
		s.opts.CertificateChainProvider = fixedCerts(der)
	}
	return s, nil
}

// loadPrivateKey reads a PKCS #8 private key. Errors never quote the file,
// which may hold some other secret.
func loadPrivateKey(path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the publisher names their own key file
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s is not a PEM file", path)
	}
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s holds a PEM %q block; bino needs an unencrypted PKCS #8 \"PRIVATE KEY\" (ed25519 or ECDSA P-256)", path, block.Type)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid PKCS #8 private key", path)
	}
	switch k := parsed.(type) {
	case ed25519.PrivateKey:
		return k, nil
	case *ecdsa.PrivateKey:
		if k.Curve == elliptic.P256() {
			return k, nil
		}
	}
	return nil, fmt.Errorf("%s: unsupported key type; bino signs with ed25519 or ECDSA P-256", path)
}

func loadCertChain(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the publisher names their own chain file
	if err != nil {
		return nil, fmt.Errorf("read certificate chain: %w", err)
	}
	certs, err := cryptoutils.UnmarshalCertificatesFromPEM(data)
	if err != nil || len(certs) == 0 {
		return nil, fmt.Errorf("%s holds no PEM certificates", path)
	}
	out := certs[:1]
	for _, c := range certs[1:] {
		if c.CheckSignatureFrom(c) != nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func publicKeysEqual(a, b crypto.PublicKey) bool {
	k, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	return ok && k.Equal(b)
}

// fixedCerts hands sign.Bundle a certificate, or a chain, the publisher
// already holds.
type fixedCerts [][]byte

func (f fixedCerts) GetCertificate(context.Context, sign.Keypair, *sign.CertificateProviderOptions) ([]byte, error) {
	return f[0], nil
}

func (f fixedCerts) GetCertificateChain(context.Context, sign.Keypair, *sign.CertificateProviderOptions) ([][]byte, error) {
	return f, nil
}

// keypair is a sign.Keypair over a key loaded from disk or generated for one
// keyless signature. Its hint is the fingerprint [trust] pins.
type keypair struct {
	key  crypto.Signer
	alg  signature.AlgorithmDetails
	hint []byte
}

func newKeypair(key crypto.Signer) (*keypair, error) {
	var id protocommon.PublicKeyDetails
	switch k := key.Public().(type) {
	case ed25519.PublicKey:
		id = protocommon.PublicKeyDetails_PKIX_ED25519
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() {
			return nil, errors.New("unsupported ECDSA curve")
		}
		id = protocommon.PublicKeyDetails_PKIX_ECDSA_P256_SHA_256
	default:
		return nil, fmt.Errorf("unsupported key type %T", k)
	}
	alg, err := signature.GetAlgorithmDetails(id)
	if err != nil {
		return nil, err
	}
	fp, err := KeyFingerprint(key.Public())
	if err != nil {
		return nil, err
	}
	return &keypair{key: key, alg: alg, hint: []byte(fp)}, nil
}

func (k *keypair) GetHashAlgorithm() protocommon.HashAlgorithm { return k.alg.GetProtoHashType() }

func (k *keypair) GetSigningAlgorithm() protocommon.PublicKeyDetails {
	return k.alg.GetSignatureAlgorithm()
}

func (k *keypair) GetHint() []byte { return k.hint }

func (k *keypair) GetKeyAlgorithm() string {
	if k.alg.GetKeyType() == signature.ED25519 {
		return "ED25519"
	}
	return "ECDSA"
}

func (k *keypair) GetPublicKey() crypto.PublicKey { return k.key.Public() }

func (k *keypair) GetPublicKeyPem() (string, error) {
	b, err := cryptoutils.MarshalPublicKeyToPEM(k.key.Public())
	return string(b), err
}

// SignData signs data, or its digest for ECDSA; pure ed25519 hashes itself.
func (k *keypair) SignData(_ context.Context, data []byte) (sig, signed []byte, err error) {
	hf := k.alg.GetHashType()
	signed = data
	if hf != crypto.Hash(0) {
		h := hf.New()
		h.Write(data)
		signed = h.Sum(nil)
	}
	sig, err = k.key.Sign(rand.Reader, signed, hf)
	return sig, signed, err
}
