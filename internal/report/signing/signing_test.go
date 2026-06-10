package signing

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitorus/pdfsign/sign"

	"bino.bi/bino/internal/report/config"
)

// writeMinimalPDF writes a minimal but valid single-page PDF to path.
func writeMinimalPDF(t *testing.T, path string) {
	t.Helper()

	var buf bytes.Buffer
	var offsets []int

	addObj := func(body string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(body)
	}

	buf.WriteString("%PDF-1.4\n")
	addObj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	addObj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	addObj("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << >> >>\nendobj\n")

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(offsets)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(offsets)+1, xrefOffset)

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write minimal pdf: %v", err)
	}
}

// generateKeyPair creates a throwaway self-signed certificate and matching
// private key, both PEM-encoded.
func generateKeyPair(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "bino test signer"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM
}

// testProfile builds a SigningProfile with the given PEM sources.
func testProfile(cert, key config.PEMSource, manifestPath string) config.SigningProfile {
	return config.SigningProfile{
		Document: config.Document{Name: "test-profile", File: manifestPath},
		Spec: config.SigningProfileSpec{
			Certificate: cert,
			PrivateKey:  key,
			Signer: config.SigningProfileSigner{
				Name:     "Test Signer",
				Location: "Test Suite",
				Reason:   "Unit test",
			},
		},
	}
}

func TestApply(t *testing.T) {
	certPEM, keyPEM := generateKeyPair(t)

	t.Run("signs pdf in place", func(t *testing.T) {
		tmp := t.TempDir()
		pdfPath := filepath.Join(tmp, "report.pdf")
		writeMinimalPDF(t, pdfPath)
		original, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatalf("read original: %v", err)
		}

		profile := testProfile(
			config.PEMSource{Inline: certPEM},
			config.PEMSource{Inline: keyPEM},
			filepath.Join(tmp, "manifest.yaml"),
		)
		if err := Apply(context.Background(), profile, pdfPath); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}

		signed, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatalf("read signed: %v", err)
		}
		if len(signed) <= len(original) {
			t.Errorf("signed file size = %d, want larger than original %d", len(signed), len(original))
		}
		if !bytes.Contains(signed, []byte("/ByteRange")) {
			t.Error("signed pdf is missing /ByteRange signature entry")
		}
		if !bytes.Contains(signed, []byte("adbe.pkcs7.detached")) {
			t.Error("signed pdf is missing PKCS7 signature subfilter")
		}

		// No leftover temp files next to the output.
		entries, err := os.ReadDir(tmp)
		if err != nil {
			t.Fatalf("read dir: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".bnr-signed-") {
				t.Errorf("leftover temp file: %s", e.Name())
			}
		}
	})

	t.Run("loads pem from path relative to manifest", func(t *testing.T) {
		tmp := t.TempDir()
		pdfPath := filepath.Join(tmp, "report.pdf")
		writeMinimalPDF(t, pdfPath)

		if err := os.WriteFile(filepath.Join(tmp, "cert.pem"), []byte(certPEM), 0o600); err != nil {
			t.Fatalf("write cert: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "key.pem"), []byte(keyPEM), 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}

		profile := testProfile(
			config.PEMSource{Path: "cert.pem"},
			config.PEMSource{Path: "key.pem"},
			filepath.Join(tmp, "manifest.yaml"),
		)
		if err := Apply(context.Background(), profile, pdfPath); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}

		signed, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatalf("read signed: %v", err)
		}
		if !bytes.Contains(signed, []byte("/ByteRange")) {
			t.Error("signed pdf is missing /ByteRange signature entry")
		}
	})

	t.Run("canceled context returns early", func(t *testing.T) {
		tmp := t.TempDir()
		pdfPath := filepath.Join(tmp, "report.pdf")
		writeMinimalPDF(t, pdfPath)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		profile := testProfile(
			config.PEMSource{Inline: certPEM},
			config.PEMSource{Inline: keyPEM},
			filepath.Join(tmp, "manifest.yaml"),
		)
		if err := Apply(ctx, profile, pdfPath); err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("Apply() error = %v, want context canceled", err)
		}
	})
}

func TestApplyErrors(t *testing.T) {
	certPEM, keyPEM := generateKeyPair(t)
	otherCertPEM, _ := generateKeyPair(t)

	tests := []struct {
		name    string
		cert    config.PEMSource
		key     config.PEMSource
		wantErr string
	}{
		{
			name:    "missing certificate source",
			cert:    config.PEMSource{},
			key:     config.PEMSource{Inline: keyPEM},
			wantErr: "load certificate",
		},
		{
			name:    "missing private key source",
			cert:    config.PEMSource{Inline: certPEM},
			key:     config.PEMSource{},
			wantErr: "load private key",
		},
		{
			name:    "certificate path does not exist",
			cert:    config.PEMSource{Path: "missing-cert.pem"},
			key:     config.PEMSource{Inline: keyPEM},
			wantErr: "load certificate",
		},
		{
			name:    "garbage pem",
			cert:    config.PEMSource{Inline: "not a certificate"},
			key:     config.PEMSource{Inline: keyPEM},
			wantErr: "parse key pair",
		},
		{
			name:    "mismatched certificate and key",
			cert:    config.PEMSource{Inline: otherCertPEM},
			key:     config.PEMSource{Inline: keyPEM},
			wantErr: "parse key pair",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			pdfPath := filepath.Join(tmp, "report.pdf")
			writeMinimalPDF(t, pdfPath)
			original, err := os.ReadFile(pdfPath)
			if err != nil {
				t.Fatalf("read original: %v", err)
			}

			profile := testProfile(tt.cert, tt.key, filepath.Join(tmp, "manifest.yaml"))
			err = Apply(context.Background(), profile, pdfPath)
			if err == nil {
				t.Fatal("Apply() should error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want %s", err, tt.wantErr)
			}

			// The input PDF must be left untouched on failure.
			after, err := os.ReadFile(pdfPath)
			if err != nil {
				t.Fatalf("read after failure: %v", err)
			}
			if !bytes.Equal(original, after) {
				t.Error("input pdf was modified despite signing failure")
			}
		})
	}

	t.Run("invalid pdf input", func(t *testing.T) {
		tmp := t.TempDir()
		pdfPath := filepath.Join(tmp, "broken.pdf")
		if err := os.WriteFile(pdfPath, []byte("not a pdf"), 0o644); err != nil {
			t.Fatalf("write broken pdf: %v", err)
		}

		profile := testProfile(
			config.PEMSource{Inline: certPEM},
			config.PEMSource{Inline: keyPEM},
			filepath.Join(tmp, "manifest.yaml"),
		)
		err := Apply(context.Background(), profile, pdfPath)
		if err == nil {
			t.Fatal("Apply() should error on invalid pdf")
		}
		if !strings.Contains(err.Error(), "sign") {
			t.Errorf("error = %v, want sign failure", err)
		}

		// No leftover temp files next to the input.
		entries, readErr := os.ReadDir(tmp)
		if readErr != nil {
			t.Fatalf("read dir: %v", readErr)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".bnr-signed-") {
				t.Errorf("leftover temp file: %s", e.Name())
			}
		}
	})
}

func TestMapCertType(t *testing.T) {
	tests := []struct {
		raw  string
		want sign.CertType
	}{
		{"certification", sign.CertificationSignature},
		{" Certification ", sign.CertificationSignature},
		{"usage-rights", sign.UsageRightsSignature},
		{"timestamp", sign.TimeStampSignature},
		{"approval", sign.ApprovalSignature},
		{"", sign.ApprovalSignature},
		{"unknown", sign.ApprovalSignature},
	}

	for _, tt := range tests {
		t.Run("raw="+tt.raw, func(t *testing.T) {
			if got := mapCertType(tt.raw); got != tt.want {
				t.Errorf("mapCertType(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMapDocMDPPerm(t *testing.T) {
	tests := []struct {
		raw  string
		want sign.DocMDPPerm
	}{
		{"no-changes", sign.DoNotAllowAnyChangesPerms},
		{"annotate", sign.AllowFillingExistingFormFieldsAndSignaturesAndCRUDAnnotationsPerms},
		{"", sign.AllowFillingExistingFormFieldsAndSignaturesPerms},
		{"unknown", sign.AllowFillingExistingFormFieldsAndSignaturesPerms},
	}

	for _, tt := range tests {
		t.Run("raw="+tt.raw, func(t *testing.T) {
			if got := mapDocMDPPerm(tt.raw); got != tt.want {
				t.Errorf("mapDocMDPPerm(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMapDigest(t *testing.T) {
	tests := []struct {
		raw  string
		want crypto.Hash
	}{
		{"sha384", crypto.SHA384},
		{"SHA512 ", crypto.SHA512},
		{"sha256", crypto.SHA256},
		{"", crypto.SHA256},
		{"unknown", crypto.SHA256},
	}

	for _, tt := range tests {
		t.Run("raw="+tt.raw, func(t *testing.T) {
			if got := mapDigest(tt.raw); got != tt.want {
				t.Errorf("mapDigest(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
