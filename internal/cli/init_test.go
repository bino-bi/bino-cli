package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/projectlayout"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/lint"
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
// aligned with projectlayout: every folder it seeds a *manifest* into must be a
// canonical one, so `bino add` co-locates new manifests with the scaffold instead
// of splitting. Non-manifest payloads (docs/*.md, scripts/*.sh) are exempt —
// projectlayout only maps manifest kinds to folders.
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
		DataSetName:    "sample_report_dataset",
	}
	created, _, err := renderBuiltinBundle("standard", data, false)
	if err != nil {
		t.Fatalf("renderBuiltinBundle(standard): %v", err)
	}
	canonical := projectlayout.CanonicalFolders()
	for _, rel := range created {
		if filepath.Ext(rel) != ".yaml" {
			continue // not a manifest — projectlayout has nothing to say about it
		}
		dir, _, nested := strings.Cut(rel, "/")
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
		DataSetName:    "sample_report_dataset",
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

// predefInitData is the render input the three predef scaffold tests share.
func predefInitData(dir string) initTemplateData {
	return initTemplateData{
		Directory:      dir,
		ReportName:     "sample-kit",
		ReportTitle:    "Sample Kit",
		Language:       "en",
		Filename:       "sample-kit.pdf",
		LayoutName:     "sample-kit-page",
		DataSourceName: "sample_kit_data",
		DataSetName:    "sample_kit_dataset",
	}
}

// TestPredefScaffoldLintsClean is the headline acceptance check for the predef
// built-in: the scaffold ships an active [package] table, so it must satisfy the
// rules that table switches on.
func TestPredefScaffoldLintsClean(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := renderBuiltinBundle("predef", predefInitData(tmp), false); err != nil {
		t.Fatalf("renderBuiltinBundle(predef): %v", err)
	}

	ctx := context.Background()
	docs, err := config.LoadDirWithOptions(ctx, tmp, config.LoadOptions{})
	if err != nil {
		t.Fatalf("LoadDirWithOptions: %v", err)
	}
	findings := lint.NewProjectRunner(tmp).Run(ctx, lint.DocumentsFromConfig(docs))

	var offenders []lint.Finding
	for _, f := range findings {
		if strings.HasPrefix(f.RuleID, "predef-") || f.RuleID == "package-config-invalid" {
			offenders = append(offenders, f)
		}
	}
	if len(offenders) > 0 {
		for _, f := range offenders {
			t.Errorf("predef finding: %s: %s (%s %s)", f.RuleID, f.Message, pathutil.RelPath(tmp, f.File), f.Path)
		}
		t.Fatalf("predef scaffold produced %d predef finding(s); all %d finding(s): %v", len(offenders), len(findings), findings)
	}
}

// TestPredefTemplateFolders mirrors TestStandardTemplateUsesCanonicalFolders but
// exempts mocks/: the predef scaffold deliberately seeds a non-canonical mocks/
// folder for the preview harness, because those documents must stay outside the
// package include set.
func TestPredefTemplateFolders(t *testing.T) {
	tmp := t.TempDir()
	created, _, err := renderBuiltinBundle("predef", predefInitData(tmp), false)
	if err != nil {
		t.Fatalf("renderBuiltinBundle(predef): %v", err)
	}
	canonical := projectlayout.CanonicalFolders()
	mockYAML := 0
	for _, rel := range created {
		if filepath.Ext(rel) != ".yaml" {
			continue // not a manifest — projectlayout has nothing to say about it
		}
		dir, _, nested := strings.Cut(rel, "/")
		if !nested {
			continue // top-level file (bino.toml, dotfiles) — not a folder
		}
		if dir == "mocks" {
			mockYAML++
			continue // the deliberate exemption
		}
		if !slices.Contains(canonical, dir) {
			t.Errorf("predef template seeds non-canonical folder %q (file %q); canonical=%v", dir, rel, canonical)
		}
	}
	if mockYAML == 0 {
		t.Fatalf("predef template seeds no YAML under mocks/; created=%v", created)
	}
}

// TestPredefTemplateIsPreviewReady is the default-suite stand-in for `bino
// preview`, which needs Chrome: the scaffold must carry an artefact whose pages
// all resolve.
func TestPredefTemplateIsPreviewReady(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := renderBuiltinBundle("predef", predefInitData(tmp), false); err != nil {
		t.Fatalf("renderBuiltinBundle(predef): %v", err)
	}
	docs, err := config.LoadDirWithOptions(context.Background(), tmp, config.LoadOptions{})
	if err != nil {
		t.Fatalf("LoadDirWithOptions: %v", err)
	}

	pages := map[string]bool{}
	for _, doc := range docs {
		if doc.Kind == "LayoutPage" {
			pages[doc.Name] = true
		}
	}

	artefacts := 0
	for _, doc := range docs {
		if doc.Kind != "ReportArtefact" {
			continue
		}
		artefacts++
		var payload struct {
			Spec struct {
				LayoutPages []struct {
					Page string `json:"page"`
				} `json:"layoutPages"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			t.Fatalf("decode %s: %v", doc.File, err)
		}
		if len(payload.Spec.LayoutPages) == 0 {
			t.Fatalf("ReportArtefact %q declares no layoutPages", doc.Name)
		}
		for _, p := range payload.Spec.LayoutPages {
			if !pages[p.Page] {
				t.Errorf("ReportArtefact %q references LayoutPage %q, which does not exist", doc.Name, p.Page)
			}
		}
	}
	if artefacts == 0 {
		t.Fatalf("predef scaffold has no ReportArtefact; `bino preview` would render nothing")
	}
}
