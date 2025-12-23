package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSanitizeManifestName(t *testing.T) {
	tests := map[string]string{
		"Demo Report":      "demo-report",
		"   ":              "rainbow-report",
		"UPPER_case--demo": "upper_case-demo",
		"@@@demo***":       "demo",
		"ends-with-":       "ends-with",
	}
	for input, want := range tests {
		got := sanitizeManifestName(input, "rainbow-report")
		if got != want {
			t.Fatalf("sanitizeManifestName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSanitizeSQLIdentifier(t *testing.T) {
	tests := map[string]string{
		"Sample":       "sample",
		"123abc":       "ds_123abc",
		"--foo--":      "foo",
		"Already_good": "already_good",
	}
	for input, want := range tests {
		if got := sanitizeSQLIdentifier(input); got != want {
			t.Fatalf("sanitizeSQLIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildInitTemplateData(t *testing.T) {
	data, err := buildInitTemplateData(initAnswers{
		Directory:   "./tmp-report",
		ReportName:  "Pretty Report",
		ReportTitle: "Quarterly Coffee",
		Language:    "de-DE",
	})
	if err != nil {
		t.Fatalf("buildInitTemplateData returned error: %v", err)
	}
	if !strings.HasSuffix(data.Directory, filepath.FromSlash("tmp-report")) {
		t.Fatalf("expected directory suffix tmp-report, got %s", data.Directory)
	}
	if data.ReportName != "pretty-report" {
		t.Fatalf("ReportName = %s", data.ReportName)
	}
	if data.Language != "de" || data.Locale != "de-DE" {
		t.Fatalf("unexpected language/locale: %s %s", data.Language, data.Locale)
	}
	if data.DataSourceName == "" || data.LayoutName == "" {
		t.Fatalf("expected derived names to be non-empty")
	}
}

func TestWriteInitBundleCreatesFiles(t *testing.T) {
	tmp := t.TempDir()
	data := initTemplateData{
		Directory:      tmp,
		ReportName:     "sample-report",
		ReportTitle:    "Sample",
		Language:       "en",
		Locale:         "en-US",
		Filename:       "sample-report.pdf",
		LayoutName:     "sample-report-page",
		DataSourceName: "sample_report_data",
		StyleName:      "sample-report-style",
		I18nName:       "sample-report-copy",
	}
	created, _, err := writeInitBundle(data, false)
	if err != nil {
		t.Fatalf("writeInitBundle error: %v", err)
	}
	want := []string{".bnignore", ".gitignore", "bino.toml", "data.yaml", "pages.yaml", "report.yaml"}
	slices.Sort(created)
	if !slices.Equal(created, want) {
		t.Fatalf("created files %v, want %v", created, want)
	}

	// Verify bino.toml was created with report-id
	binoTomlPath := filepath.Join(tmp, "bino.toml")
	binoContent, err := os.ReadFile(binoTomlPath)
	if err != nil {
		t.Fatalf("read bino.toml: %v", err)
	}
	if !strings.Contains(string(binoContent), "report-id") {
		t.Fatalf("bino.toml missing report-id: %s", string(binoContent))
	}

	ignorePath := filepath.Join(tmp, ".bnignore")
	content, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .bnignore: %v", err)
	}
	if !strings.Contains(string(content), "dist/") {
		t.Fatalf(".bnignore missing dist/ entry: %s", string(content))
	}
	if _, _, err := writeInitBundle(data, false); err == nil {
		t.Fatalf("expected error when re-running without force")
	}
	if _, _, err := writeInitBundle(data, true); err != nil {
		t.Fatalf("force write failed: %v", err)
	}
}
