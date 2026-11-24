package registry

import (
	"testing"

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
	v1, _, err := keylessSigner(summary("refs/tags/v1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if v1.Workflow != "https://github.com/acme/reports/.github/workflows/publish.yml" || v1.Repository != "https://github.com/acme/reports" {
		t.Fatalf("signer = %+v", v1)
	}
	v2, _, _ := keylessSigner(summary("refs/tags/v2.0.0"))
	if !v1.Same(v2) {
		t.Error("two releases of the same workflow are not the same signer")
	}
	email, _, _ := keylessSigner(certificate.Summary{SubjectAlternativeName: "alice@example.com", Extensions: certificate.Extensions{Issuer: "https://accounts.example"}})
	if email.Workflow != "" || email.Identity != "alice@example.com" || email.Same(v1) {
		t.Errorf("email signer = %+v", email)
	}
	if _, _, err := keylessSigner(certificate.Summary{SubjectAlternativeName: "x"}); err == nil {
		t.Error("a certificate without an issuer was accepted")
	}
}
