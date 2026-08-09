package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/schema"
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

	// The wizard now requires a title and a root route target; the write
	// gate backstops both. If these start failing, liveReportArtefactSpec
	// stopped requiring title, or liveRouteSpec its artefact/layoutPages.
	t.Run("titleless artefact is rejected at write time", func(t *testing.T) {
		data := LiveReportArtefactManifestData{
			Name:   "untitled_app",
			Routes: map[string]LiveRoute{"/": {Artifact: "monthly_report"}},
		}
		err := WriteSchemaDocument(buildLiveReportArtefactDocument(data), t.TempDir(), "untitled.yaml", false, discardCmd().OutOrStdout())
		if err == nil {
			t.Fatal("expected a schema validation error for the missing title")
		}
	})

	t.Run("empty root route is rejected at write time", func(t *testing.T) {
		data := LiveReportArtefactManifestData{
			Name:   "empty_route_app",
			Title:  "Empty Route App",
			Routes: map[string]LiveRoute{"/": {}},
		}
		err := WriteSchemaDocument(buildLiveReportArtefactDocument(data), t.TempDir(), "empty_route.yaml", false, discardCmd().OutOrStdout())
		if err == nil {
			t.Fatal("expected a schema validation error for the route without artefact or layoutPages")
		}
	})
}

// TestAddLiveReportArtefactNoPrompt pins the non-interactive contract: the
// flags must collect everything the schema requires, and what they collect
// must validate on disk.
func TestAddLiveReportArtefactNoPrompt(t *testing.T) {
	t.Run("missing title and route content are reported up front", func(t *testing.T) {
		t.Chdir(t.TempDir())
		cmd := newAddLiveReportArtefactCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"main_app", "--output", "live.yaml", "--no-prompt"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected a missing-flags error")
		}
		for _, want := range []string{"--title", "--artefact or --layout-pages"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %s", err, want)
			}
		}
	})

	t.Run("artefact route round-trips through the write gate", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		cmd := newAddLiveReportArtefactCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{
			"main_app",
			"--title", "Report Dashboard",
			"--artefact", "monthly_report",
			"--output", "live.yaml",
			"--no-prompt",
		})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(dir, "live.yaml"))
		if err != nil {
			t.Fatalf("read written manifest: %v", err)
		}
		if err := schema.Validate(content); err != nil {
			t.Fatalf("written manifest failed schema.Validate:\n%s\nerror: %v", content, err)
		}
		assertContainsAll(t, string(content), []string{
			"title: Report Dashboard",
			"artefact: $monthly_report",
		})
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

// TestAddSigningProfileNoPromptRequiresAllInputs pins the non-interactive
// contract: certificate, private key, and signer are required by the schema,
// so the flags must be too.
func TestAddSigningProfileNoPromptRequiresAllInputs(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newAddSigningProfileCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"company_signing", "--output", "signing.yaml", "--no-prompt"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a missing-flags error")
	}
	for _, want := range []string{"--certificate", "--private-key", "--signer-name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
