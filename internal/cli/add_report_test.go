package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReportArtefactDocument(t *testing.T) {
	data := ReportArtefactManifestData{
		Name:        "monthly_report",
		Description: "The monthly board report",
		Filename:    "report_{{date}}.pdf",
		Title:       "Monthly Report",
		Format:      "pdf",
		Orientation: "portrait",
		Language:    "en",
		LayoutPages: []string{"summary_page", "detail_page"},
	}
	got := wizardRoundTrip(t, buildReportArtefactDocument(data), "report.yaml")
	assertContainsAll(t, got, []string{
		"kind: ReportArtefact",
		"name: monthly_report",
		"filename: report_{{date}}.pdf",
		"title: Monthly Report",
		"format: pdf",
		"orientation: portrait",
		"language: en",
		"summary_page",
		"detail_page",
	})
}

func TestBuildReportArtefactDocumentWithParams(t *testing.T) {
	data := ReportArtefactManifestData{
		Name:        "regional_report",
		Filename:    "regional.pdf",
		Title:       "Regional Report",
		LayoutPages: []string{"summary_page"},
		LayoutPageRefs: []LayoutPageRefData{
			{Page: "region_page", Params: map[string]string{"region": "emea"}},
			{Page: "region_page", Params: map[string]string{"region": "apac"}},
		},
	}
	got := wizardRoundTrip(t, buildReportArtefactDocumentWithParams(data), "report_params.yaml")
	assertContainsAll(t, got, []string{
		"kind: ReportArtefact",
		"name: regional_report",
		// the plain string ref and both parameterized object refs coexist
		"summary_page",
		"page: region_page",
		"region: emea",
		"region: apac",
	})
}

func TestBuildLiveReportArtefactDocument(t *testing.T) {
	t.Run("artefact route gets the $ prefix", func(t *testing.T) {
		data := LiveReportArtefactManifestData{
			Name:  "main_app",
			Title: "Report Dashboard",
			Routes: map[string]LiveRoute{
				"/": {Artifact: "monthly_report"},
			},
		}
		got := wizardRoundTrip(t, buildLiveReportArtefactDocument(data), "live.yaml")
		assertContainsAll(t, got, []string{
			"kind: LiveReportArtefact",
			"name: main_app",
			"title: Report Dashboard",
			"artefact: $monthly_report",
		})
	})

	t.Run("layout-pages route survives", func(t *testing.T) {
		data := LiveReportArtefactManifestData{
			Name:  "pages_app",
			Title: "Pages App",
			Routes: map[string]LiveRoute{
				"/": {LayoutPages: []string{"summary_page", "detail_page"}},
			},
		}
		got := wizardRoundTrip(t, buildLiveReportArtefactDocument(data), "live_pages.yaml")
		assertContainsAll(t, got, []string{
			"layoutPages:",
			"summary_page",
			"detail_page",
		})
	})

	// Canary: the schema requires spec.title, but the wizard treats the
	// title as optional — a titleless LiveReportArtefact is rejected by the
	// validating write path instead of landing on disk. If this starts
	// failing, either the schema or the wizard prompt changed.
	t.Run("titleless artefact is rejected at write time", func(t *testing.T) {
		data := LiveReportArtefactManifestData{
			Name:   "untitled_app",
			Routes: map[string]LiveRoute{"/": {}},
		}
		err := WriteSchemaDocument(buildLiveReportArtefactDocument(data), t.TempDir(), "untitled.yaml", false, discardCmd().OutOrStdout())
		if err == nil {
			t.Fatal("expected a schema validation error for the missing title")
		}
	})
}

func TestBuildSigningProfileDocument(t *testing.T) {
	// The wizard must reference certificate and key files by path — never
	// pull key material into the manifest.
	dir := t.TempDir()
	keyPEM := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKc=\n-----END PRIVATE KEY-----\n"
	keyPath := filepath.Join(dir, "company-key.pem")
	if err := os.WriteFile(keyPath, []byte(keyPEM), 0o600); err != nil {
		t.Fatalf("write key fixture: %v", err)
	}

	data := SigningProfileManifestData{
		Name:            "company_signing",
		CertificatePath: "certs/company.pem",
		PrivateKeyPath:  keyPath,
		SignerName:      "Company Inc.",
	}
	got := wizardRoundTrip(t, buildSigningProfileDocument(data), "signing.yaml")
	assertContainsAll(t, got, []string{
		"kind: SigningProfile",
		"name: company_signing",
		"certificate:",
		"path: certs/company.pem",
		"privateKey:",
		"path: " + keyPath,
		"signer:",
		"name: Company Inc.",
	})
	if strings.Contains(got, "inline") || strings.Contains(got, "PRIVATE KEY") || strings.Contains(got, "MIIEvQIBADAN") {
		t.Errorf("key material was inlined into the manifest:\n%s", got)
	}
}
