package registry

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
)

func TestKeylessSignerFromCIWorkload(t *testing.T) {
	summary := func(ref string) certificate.Summary {
		return certificate.Summary{
			SubjectAlternativeName: "https://github.com/acme/reports/.github/workflows/publish.yml@" + ref,
			Extensions: certificate.Extensions{
				Issuer:              "https://token.actions.githubusercontent.com",
				SourceRepositoryURI: "https://github.com/acme/reports",
				BuildSignerURI:      "https://github.com/acme/reports/.github/workflows/publish.yml@" + ref,
			},
		}
	}
	v1, err := keylessSigner(summary("refs/tags/v1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if v1.Workflow != "https://github.com/acme/reports/.github/workflows/publish.yml" || v1.Repository != "https://github.com/acme/reports" {
		t.Fatalf("signer = %+v", v1)
	}
	v2, _ := keylessSigner(summary("refs/tags/v2.0.0"))
	if !v1.Same(v2) {
		t.Error("two releases of the same workflow are not the same signer")
	}
	email, _ := keylessSigner(certificate.Summary{SubjectAlternativeName: "alice@example.com", Extensions: certificate.Extensions{Issuer: "https://accounts.example"}})
	if email.Workflow != "" || email.Identity != "alice@example.com" || email.Same(v1) {
		t.Errorf("email signer = %+v", email)
	}
	if _, err := keylessSigner(certificate.Summary{SubjectAlternativeName: "x"}); err == nil {
		t.Error("a certificate without an issuer was accepted")
	}
}

// The GitHub build provenance of bino-plugin-sdk v0.0.7's SBOM, a real
// public-good keyless bundle. Its statement is SLSA provenance, not a
// registry statement, so it is checked below the statement check: signature,
// Fulcio chain, Rekor entry and SCT against the embedded root, and the signer
// read from the certificate.
func TestKeylessPublicGoodBundle(t *testing.T) {
	const subject = "4185522550d458c6d0be015737cb25d85e012d61b185db2925714873c1c00388" // sha256 of bino-plugin-sdk-sbom.spdx.json
	raw, err := os.ReadFile(filepath.Join("testdata", "bino-plugin-sdk-v0.0.7-sbom.sigstore.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := parseAttestationBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	kt, err := keyless()
	if err != nil {
		t.Fatal(err)
	}
	if !kt.requireSCT {
		t.Fatal("the embedded root does not require SCTs")
	}
	digest, _ := hex.DecodeString(subject)
	s, known, clockless, err := verifyBundle(b, digest, kt, nil, nil, time.Now())
	if err != nil || !known || clockless {
		t.Fatalf("verify: known %v, clockless %v, err %v", known, clockless, err)
	}
	if s.Kind != SignerKeyless || s.Issuer != "https://token.actions.githubusercontent.com" ||
		s.Repository != "https://github.com/bino-bi/bino-plugin-sdk" || s.RepositoryID == "" || s.OwnerID == "" ||
		!strings.HasPrefix(s.Workflow, "https://github.com/bino-bi/bino-plugin-sdk/.github/workflows/") {
		t.Errorf("signer = %+v", s)
	}

	other := make([]byte, len(digest))
	if _, _, _, err := verifyBundle(b, other, kt, nil, nil, time.Now()); err == nil {
		t.Error("the bundle verified for another subject")
	}
}
