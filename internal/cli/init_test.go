package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/projectlayout"
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
	if data.Language != "de" {
		t.Fatalf("unexpected language: %s", data.Language)
	}
	if data.DataSourceName == "" || data.LayoutName == "" {
		t.Fatalf("expected derived names to be non-empty")
	}
}

// TestStandardTemplateUsesCanonicalFolders keeps the built-in standard scaffold
// aligned with projectlayout: every folder it seeds must be a canonical one, so
// `bino add` co-locates new manifests with the scaffold instead of splitting.
func TestStandardTemplateUsesCanonicalFolders(t *testing.T) {
	tmp := t.TempDir()
	data := initTemplateData{
		Directory:      tmp,
		ReportName:     "sample-report",
		ReportTitle:    "Sample",
		Language:       "en",
		Filename:       "sample-report.pdf",
		LayoutName:     "sample-report-page",
		DataSourceName: "sample_report_data",
	}
	created, _, err := renderBuiltinBundle("standard", data, false)
	if err != nil {
		t.Fatalf("renderBuiltinBundle(standard): %v", err)
	}
	canonical := projectlayout.CanonicalFolders()
	for _, rel := range created {
		dir, _, nested := strings.Cut(rel, string(filepath.Separator))
		if !nested {
			continue // top-level file (bino.toml, dotfiles) — not a folder
		}
		if !slices.Contains(canonical, dir) {
			t.Errorf("standard template seeds non-canonical folder %q (file %q); canonical=%v", dir, rel, canonical)
		}
	}
}

func TestRenderBuiltinMinimalCreatesFiles(t *testing.T) {
	tmp := t.TempDir()
	data := initTemplateData{
		Directory:      tmp,
		ReportName:     "sample-report",
		ReportTitle:    "Sample",
		Language:       "en",
		Filename:       "sample-report.pdf",
		LayoutName:     "sample-report-page",
		DataSourceName: "sample_report_data",
	}
	created, _, err := renderBuiltinBundle("minimal", data, false)
	if err != nil {
		t.Fatalf("renderBuiltinBundle error: %v", err)
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
	if _, _, err := renderBuiltinBundle("minimal", data, false); err == nil {
		t.Fatalf("expected error when re-running without force")
	}
	if _, _, err := renderBuiltinBundle("minimal", data, true); err != nil {
		t.Fatalf("force write failed: %v", err)
	}
}
