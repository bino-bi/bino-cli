package lint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/pathutil"
)

const (
	testProjectRoot = "/proj"
	testPackageName = "@acme/kit"
)

// testIncludeSet builds the default include set the predef rules close over.
func testIncludeSet() *pathutil.IncludeSet {
	return (&pathutil.PackageConfig{Name: testPackageName}).IncludeSet(testProjectRoot)
}

// predefDoc builds a document at a project-relative path under the synthetic
// project root, so include-set membership is decided by the path alone.
func predefDoc(kind, name, relPath string, specData map[string]any) Document {
	return Document{
		File:     filepath.Join(testProjectRoot, filepath.FromSlash(relPath)),
		Position: 1,
		Kind:     kind,
		Name:     name,
		Raw:      rawDoc(kind, name, specData),
	}
}

// runPredefRule runs a rule and asserts the shape every predef finding shares.
func runPredefRule(t *testing.T, rule Rule, docs []Document) []Finding {
	t.Helper()
	findings := rule.Check(context.Background(), docs)
	for i, f := range findings {
		if f.RuleID != rule.ID {
			t.Errorf("finding[%d] RuleID = %q, want %q", i, f.RuleID, rule.ID)
		}
		if f.Severity != "error" {
			t.Errorf("finding[%d] Severity = %q, want %q", i, f.Severity, "error")
		}
	}
	return findings
}

// wantOneFinding asserts either a single finding at wantPath whose message
// contains wantMessage, or no finding at all when wantMessage is empty.
func wantOneFinding(t *testing.T, findings []Finding, wantPath, wantMessage string) {
	t.Helper()
	if wantMessage == "" {
		if len(findings) != 0 {
			t.Fatalf("got %d findings %+v, want none", len(findings), findings)
		}
		return
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings %+v, want 1", len(findings), findings)
	}
	if findings[0].Path != wantPath {
		t.Errorf("path = %q, want %q", findings[0].Path, wantPath)
	}
	if !strings.Contains(findings[0].Message, wantMessage) {
		t.Errorf("message = %q, want it to contain %q", findings[0].Message, wantMessage)
	}
}

func TestPredefNameNamespace(t *testing.T) {
	tests := []struct {
		name        string
		docs        []Document
		wantMessage string
	}{
		{
			name: "namespaced name",
			docs: []Document{predefDoc("Table", "@acme/kit/revenue", "components/revenue.yaml", nil)},
		},
		{
			name: "main definition",
			docs: []Document{predefDoc("Table", "@acme/kit", "components/revenue.yaml", nil)},
		},
		{
			name:        "un-namespaced name",
			docs:        []Document{predefDoc("Table", "revenue", "components/revenue.yaml", nil)},
			wantMessage: `Table "revenue" is inside the package include set but is not namespaced; rename it to "@acme/kit/revenue"`,
		},
		{
			name: "second main definition",
			docs: []Document{
				predefDoc("Table", "@acme/kit", "components/revenue.yaml", nil),
				predefDoc("ComponentStyle", "@acme/kit", "styles/theme.yaml", nil),
			},
			wantMessage: `a package may declare at most one main definition named "@acme/kit"`,
		},
		{
			name:        "un-namespaced DataSource",
			docs:        []Document{predefDoc("DataSource", "revenue_source", "datasources/revenue.yaml", nil)},
			wantMessage: "a DataSource name becomes a DuckDB view name and is limited to two segments",
		},
		{
			name: "DataSource named exactly the package",
			docs: []Document{predefDoc("DataSource", "@acme/kit", "datasources/revenue.yaml", nil)},
		},
		{
			name: "document in mocks",
			docs: []Document{predefDoc("Table", "revenue", "mocks/revenue.yaml", nil)},
		},
		{
			name: "document installed under .bino/registry",
			docs: []Document{predefDoc("Table", "revenue", ".bino/registry/acme/other/revenue.yaml", nil)},
		},
		{
			name: "materialized inline definition",
			docs: []Document{predefDoc("Table", "_inline_page_0", "components/revenue.yaml", nil)},
		},
		{
			name: "empty name",
			docs: []Document{predefDoc("Table", "", "components/revenue.yaml", nil)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := predefNameNamespace(testPackageName, testIncludeSet())
			wantOneFinding(t, runPredefRule(t, rule, tt.docs), "metadata.name", tt.wantMessage)
		})
	}
}

func TestPredefForbiddenKind(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		relPath     string
		wantMessage string
	}{
		{name: "ReportArtefact", kind: "ReportArtefact", relPath: "manifests/report.yaml", wantMessage: "artefacts render a report"},
		{name: "LiveReportArtefact", kind: "LiveReportArtefact", relPath: "manifests/live.yaml", wantMessage: "artefacts render a report"},
		{name: "ScreenshotArtefact", kind: "ScreenshotArtefact", relPath: "manifests/shot.yaml", wantMessage: "artefacts render a report"},
		{name: "DocumentArtefact", kind: "DocumentArtefact", relPath: "manifests/doc.yaml", wantMessage: "artefacts render a report"},
		{name: "ConnectionSecret", kind: "ConnectionSecret", relPath: "secrets/db.yaml", wantMessage: "credentials must never be published"},
		{name: "SigningProfile", kind: "SigningProfile", relPath: "signing/profile.yaml", wantMessage: "credentials must never be published"},
		{name: "publishable kind", kind: "Table", relPath: "components/revenue.yaml"},
		{name: "artefact in mocks", kind: "ReportArtefact", relPath: "mocks/report.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{predefDoc(tt.kind, "@acme/kit/x", tt.relPath, nil)}
			rule := predefForbiddenKind(testIncludeSet())
			wantOneFinding(t, runPredefRule(t, rule, docs), "kind", tt.wantMessage)
		})
	}
}

func TestPredefAssetAbsolutePath(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		relPath     string
		localPath   string
		wantMessage string
	}{
		{
			name: "posix absolute path", kind: "Asset", relPath: "resources/logo.yaml", localPath: "/etc/logo.png",
			wantMessage: `asset source "/etc/logo.png" is an absolute path`,
		},
		{
			name: "windows absolute path", kind: "Asset", relPath: "resources/logo.yaml", localPath: `C:\logo.png`,
			wantMessage: `is an absolute path`,
		},
		{name: "relative path", kind: "Asset", relPath: "resources/logo.yaml", localPath: "logo.svg"},
		{name: "no local source", kind: "Asset", relPath: "resources/logo.yaml", localPath: ""},
		{name: "env placeholder", kind: "Asset", relPath: "resources/logo.yaml", localPath: "${LOGO}"},
		{name: "non-Asset kind", kind: "Table", relPath: "components/revenue.yaml", localPath: "/etc/logo.png"},
		{name: "asset in mocks", kind: "Asset", relPath: "mocks/logo.yaml", localPath: "/etc/logo.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specData := map[string]any{"source": map[string]any{"localPath": tt.localPath}}
			docs := []Document{predefDoc(tt.kind, "@acme/kit/logo", tt.relPath, specData)}
			rule := predefAssetAbsolutePath(testIncludeSet())
			wantOneFinding(t, runPredefRule(t, rule, docs), "spec.source.localPath", tt.wantMessage)
		})
	}
}

func TestPredefExternalRef(t *testing.T) {
	inSetStyle := predefDoc("ComponentStyle", "@acme/kit/theme", "styles/theme.yaml", nil)
	mockStyle := predefDoc("ComponentStyle", "mock_theme", "mocks/theme.yaml", nil)
	mockDataset := predefDoc("DataSet", "mock_revenue", "mocks/data.yaml", nil)

	tests := []struct {
		name        string
		specData    map[string]any
		extra       []Document
		deps        map[string]string
		wantPath    string
		wantMessage string
	}{
		{
			name:     "child ref inside the package",
			specData: map[string]any{"children": []any{map[string]any{"kind": "Table", "ref": "@acme/kit/revenue"}}},
			extra:    []Document{predefDoc("Table", "@acme/kit/revenue", "components/revenue.yaml", nil)},
		},
		{
			name:        "child ref to an undeclared package",
			specData:    map[string]any{"children": []any{map[string]any{"kind": "Table", "ref": "@other/kit/revenue"}}},
			wantPath:    "spec.children.0.ref",
			wantMessage: `references "@other/kit/revenue", which belongs to package "@other/kit"; declare it under [dependencies] in bino.toml`,
		},
		{
			name:     "child ref to a declared dependency",
			specData: map[string]any{"children": []any{map[string]any{"kind": "Table", "ref": "@other/kit/revenue"}}},
			deps:     map[string]string{"@other/kit": "1.2.3"},
		},
		{
			name:     "selectedStyle inside the package",
			specData: map[string]any{"selectedStyle": "@acme/kit/theme"},
			extra:    []Document{inSetStyle},
		},
		{
			name:        "selectedStyle outside the include set",
			specData:    map[string]any{"selectedStyle": "mock_theme"},
			extra:       []Document{mockStyle},
			wantPath:    "spec.selectedStyle",
			wantMessage: `references "mock_theme", which is outside the package include set`,
		},
		{
			name:     "selectedStyle placeholder",
			specData: map[string]any{"selectedStyle": "${THEME}"},
			extra:    []Document{mockStyle},
		},
		{
			name:     "ruleset glob",
			specData: map[string]any{"ruleset": "ibcs-*"},
		},
		{
			name:     "unknown bare name is owned by missing-required-reference",
			specData: map[string]any{"selectedStyle": "nowhere"},
		},
		{
			// The binding seam: a packaged Table exists so the consumer can
			// point it at their own dataset.
			name:     "spec.dataset outside the include set is exempt",
			specData: map[string]any{"dataset": "mock_revenue"},
			extra:    []Document{mockDataset},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := append([]Document{predefDoc("Table", "@acme/kit", "components/main.yaml", tt.specData)}, tt.extra...)
			rule := predefExternalRef(testPackageName, testIncludeSet(), tt.deps)
			wantOneFinding(t, runPredefRule(t, rule, docs), tt.wantPath, tt.wantMessage)
		})
	}
}

func TestPredefExternalRefSkipsExcludedDocuments(t *testing.T) {
	docs := []Document{
		predefDoc("LayoutPage", "preview", "mocks/preview.yaml",
			map[string]any{"children": []any{map[string]any{"kind": "Table", "ref": "@other/kit/revenue"}}}),
		predefDoc("LayoutPage", "installed", ".bino/registry/acme/other/page.yaml",
			map[string]any{"children": []any{map[string]any{"kind": "Table", "ref": "@other/kit/revenue"}}}),
	}
	rule := predefExternalRef(testPackageName, testIncludeSet(), nil)
	wantOneFinding(t, runPredefRule(t, rule, docs), "", "")
}

func TestPredefRulesTolerateMalformedDocuments(t *testing.T) {
	raws := []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`[]`),
		json.RawMessage(`{"kind":"Asset","metadata":{"name":"@acme/kit/logo"},"spec":"nope"}`),
	}

	for _, raw := range raws {
		t.Run(string(raw), func(t *testing.T) {
			doc := predefDoc("Asset", "@acme/kit/logo", "resources/logo.yaml", nil)
			doc.Raw = raw
			docs := []Document{doc}
			inc := testIncludeSet()
			rules := []Rule{
				predefNameNamespace(testPackageName, inc),
				predefForbiddenKind(inc),
				predefAssetAbsolutePath(inc),
				predefExternalRef(testPackageName, inc, nil),
			}
			for _, rule := range rules {
				if findings := rule.Check(context.Background(), docs); len(findings) != 0 {
					t.Fatalf("%s: got %d findings %+v, want none", rule.ID, len(findings), findings)
				}
			}
		})
	}
}

// writeProject writes a bino.toml with the given body and returns the project root.
func writeProject(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write bino.toml: %v", err)
	}
	return dir
}

func TestPredefRulesWithoutPackage(t *testing.T) {
	tests := []struct {
		name string
		root func(t *testing.T) string
	}{
		{
			name: "no package table",
			root: func(t *testing.T) string { return writeProject(t, "report-id = \"r1\"\n") },
		},
		{
			name: "no bino.toml",
			root: func(t *testing.T) string { return t.TempDir() },
		},
		{
			name: "unparseable bino.toml",
			root: func(t *testing.T) string { return writeProject(t, "not = = toml\n") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rules := predefRulesFor(tt.root(t)); rules != nil {
				t.Fatalf("got %d rules, want nil", len(rules))
			}
		})
	}
}

// predefRulesFor is the production path NewProjectRunner takes, minus the
// default rules: load the project config (nil when unreadable), then derive
// the predef rules from it.
func predefRulesFor(projectRoot string) []Rule {
	return predefRules(projectRoot, projectConfigOrNil(projectRoot))
}

func TestPredefRulesWithPackage(t *testing.T) {
	root := writeProject(t, "report-id = \"r1\"\n\n[package]\nname = \"@acme/kit\"\n")
	rules := predefRulesFor(root)

	want := []string{"predef-name-namespace", "predef-forbidden-kind", "predef-asset-absolute-path", "predef-external-ref"}
	if len(rules) != len(want) {
		t.Fatalf("got %d rules, want %d", len(rules), len(want))
	}
	for i, id := range want {
		if rules[i].ID != id {
			t.Errorf("rules[%d].ID = %q, want %q", i, rules[i].ID, id)
		}
	}
}

func TestPredefRulesInvalidPackage(t *testing.T) {
	root := writeProject(t, "[package]\nname = \"kit\"\n")
	rules := predefRulesFor(root)

	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	if rules[0].ID != "package-config-invalid" {
		t.Fatalf("rules[0].ID = %q, want package-config-invalid", rules[0].ID)
	}

	findings := rules[0].Check(context.Background(), nil)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	f := findings[0]
	if f.File != filepath.Join(root, "bino.toml") {
		t.Errorf("file = %q, want %q", f.File, filepath.Join(root, "bino.toml"))
	}
	if f.Line != 1 || f.Column != 1 {
		t.Errorf("position = %d:%d, want 1:1", f.Line, f.Column)
	}
	if f.Severity != "error" {
		t.Errorf("severity = %q, want error", f.Severity)
	}
	if !strings.Contains(f.Message, `invalid [package] name "kit"`) {
		t.Errorf("message = %q, want it to name the invalid package name", f.Message)
	}
}
