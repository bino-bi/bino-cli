package signing

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digitorus/pdfsign/sign"

	"bino.bi/bino/internal/report/config"
)

// Apply signs the provided PDF using the supplied SigningProfile definition.
func Apply(ctx context.Context, profile config.SigningProfile, pdfPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	certPEM, err := resolvePEM(profile.Spec.Certificate, profile.Document.File)
	if err != nil {
		return fmt.Errorf("load certificate for SigningProfile %s: %w", profile.Document.Name, err)
	}

	keyPEM, err := resolvePEM(profile.Spec.PrivateKey, profile.Document.File)
	if err != nil {
		return fmt.Errorf("load private key for SigningProfile %s: %w", profile.Document.Name, err)
	}

	keyPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("parse key pair for SigningProfile %s: %w", profile.Document.Name, err)
	}

	cert, err := loadCertificate(keyPair)
	if err != nil {
		return fmt.Errorf("parse certificate for SigningProfile %s: %w", profile.Document.Name, err)
	}

	signer, err := extractSigner(keyPair)
	if err != nil {
		return fmt.Errorf("SigningProfile %s: %w", profile.Document.Name, err)
	}

	chains, err := buildCertificateChains(keyPair)
	if err != nil {
		return fmt.Errorf("SigningProfile %s: %w", profile.Document.Name, err)
	}

	signData := sign.SignData{
		Signature: sign.SignDataSignature{
			CertType:   mapCertType(profile.Spec.CertType),
			DocMDPPerm: mapDocMDPPerm(profile.Spec.DocMDPPerm),
			Info: sign.SignDataSignatureInfo{
				Name:        profile.Spec.Signer.Name,
				Location:    profile.Spec.Signer.Location,
				Reason:      profile.Spec.Signer.Reason,
				ContactInfo: profile.Spec.Signer.Contact,
				Date:        time.Now().Local(),
			},
		},
		Signer:            signer,
		DigestAlgorithm:   mapDigest(profile.Spec.DigestAlgorithm),
		Certificate:       cert,
		CertificateChains: chains,
		TSA:               sign.TSA{URL: profile.Spec.TSAURL},
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(pdfPath), ".bnr-signed-*.pdf")
	if err != nil {
		return fmt.Errorf("create temp signed PDF: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp signed PDF: %w", err)
	}

	if err := sign.SignFile(pdfPath, tmpPath, signData); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sign %s using profile %s: %w", pdfPath, profile.Document.Name, err)
	}

	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, pdfPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace %s with signed output: %w", pdfPath, err)
	}
	return nil
}

func resolvePEM(src config.PEMSource, manifestPath string) ([]byte, error) {
	if trimmed := strings.TrimSpace(src.Inline); trimmed != "" {
		return []byte(src.Inline), nil
	}
	if src.Path == "" {
		return nil, fmt.Errorf("neither inline nor path provided")
	}
	path := src.Path
	if !filepath.IsAbs(path) {
		base := filepath.Dir(manifestPath)
		path = filepath.Join(base, path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return content, nil
}

func loadCertificate(pair tls.Certificate) (*x509.Certificate, error) {
	if len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("certificate chain is empty")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse leaf certificate: %w", err)
	}
	return cert, nil
}

func extractSigner(pair tls.Certificate) (crypto.Signer, error) {
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok || signer == nil {
		return nil, fmt.Errorf("private key does not implement crypto.Signer (got %T)", pair.PrivateKey)
	}
	return signer, nil
}

func buildCertificateChains(pair tls.Certificate) ([][]*x509.Certificate, error) {
	if len(pair.Certificate) <= 1 {
		return nil, nil
	}
	chain := make([]*x509.Certificate, 0, len(pair.Certificate))
	for _, raw := range pair.Certificate {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return nil, fmt.Errorf("parse certificate in chain: %w", err)
		}
		chain = append(chain, cert)
	}
	return [][]*x509.Certificate{chain}, nil
}

func mapCertType(raw string) sign.CertType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "certification":
		return sign.CertificationSignature
	case "usage-rights":
		return sign.UsageRightsSignature
	case "timestamp":
		return sign.TimeStampSignature
	default:
		return sign.ApprovalSignature
	}
}

func mapDocMDPPerm(raw string) sign.DocMDPPerm {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "no-changes":
		return sign.DoNotAllowAnyChangesPerms
	case "annotate":
		return sign.AllowFillingExistingFormFieldsAndSignaturesAndCRUDAnnotationsPerms
	default:
		return sign.AllowFillingExistingFormFieldsAndSignaturesPerms
	}
}

func mapDigest(raw string) crypto.Hash {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sha384":
		return crypto.SHA384
	case "sha512":
		return crypto.SHA512
	default:
		return crypto.SHA256
	}
}
