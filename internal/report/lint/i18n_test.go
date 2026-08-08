package lint

import (
	"context"
	"testing"
)

// i18nDoc builds an Internationalization document.
func i18nDoc(name, code, namespace string) Document {
	spec := map[string]any{
		"code":    code,
		"content": map[string]any{"global.ac1": "Ist"},
	}
	if namespace != "" {
		spec["namespace"] = namespace
	}
	return Document{
		File:     "/project/i18n.yaml",
		Position: 1,
		Kind:     "Internationalization",
		Name:     name,
		Raw:      rawDoc("Internationalization", name, spec),
	}
}

// artefactDoc builds a ReportArtefact document with the given language.
func artefactDoc(language string) Document {
	spec := map[string]any{"layoutPages": []any{"page"}}
	if language != "" {
		spec["language"] = language
	}
	return Document{
		File:     "/project/report.yaml",
		Position: 1,
		Kind:     "ReportArtefact",
		Name:     "report",
		Raw:      rawDoc("ReportArtefact", "report", spec),
	}
}

func runRule(t *testing.T, rule Rule, docs []Document) []Finding {
	t.Helper()
	findings := rule.Check(context.Background(), docs)
	for _, f := range findings {
		if f.RuleID != rule.ID {
			t.Errorf("RuleID = %q, want %q", f.RuleID, rule.ID)
		}
	}
	return findings
}

// liveArtefactDoc builds a LiveReportArtefact document. Its spec has no
// language property (the schema forbids one), so it must not count as a
// language-carrying artefact for i18n-code-unused.
func liveArtefactDoc() Document {
	spec := map[string]any{"title": "Live"}
	return Document{
		File:     "/project/live.yaml",
		Position: 1,
		Kind:     "LiveReportArtefact",
		Name:     "live",
		Raw:      rawDoc("LiveReportArtefact", "live", spec),
	}
}

func TestI18nCodeUnusedIgnoresLiveReportArtefact(t *testing.T) {
	// A bundle with only a LiveReportArtefact has no artefact language to
	// compare against, so the rule must stay silent instead of matching the
	// i18n code against a phantom default locale.
	docs := []Document{liveArtefactDoc(), i18nDoc("fr", "fr", "")}
	findings := runRule(t, i18nCodeUnused, docs)
	if len(findings) != 0 {
		t.Errorf("expected no findings for LiveReportArtefact-only bundle, got %d: %v", len(findings), findings)
	}
}

func TestI18nCodeUnused(t *testing.T) {
	tests := []struct {
		name     string
		docs     []Document
		expected int
	}{
		{
			name:     "matching code passes",
			docs:     []Document{artefactDoc("de"), i18nDoc("de", "de", "")},
			expected: 0,
		},
		{
			name:     "BCP 47 code never matches",
			docs:     []Document{artefactDoc("de"), i18nDoc("de", "de-DE", "")},
			expected: 1,
		},
		{
			name:     "unset artefact language defaults to de",
			docs:     []Document{artefactDoc(""), i18nDoc("de", "de", "")},
			expected: 0,
		},
		{
			name:     "code for a language no artefact uses",
			docs:     []Document{artefactDoc("de"), i18nDoc("en", "en", "")},
			expected: 1,
		},
		{
			name:     "no artefacts at all skips the rule",
			docs:     []Document{i18nDoc("fr", "fr", "")},
			expected: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runRule(t, i18nCodeUnused, tt.docs)
			if len(findings) != tt.expected {
				t.Fatalf("got %d findings %v, want %d", len(findings), findings, tt.expected)
			}
		})
	}
}

func TestI18nNamespaceUnreferenced(t *testing.T) {
	pageWithNamespace := func(field, value string) Document {
		return componentDoc("LayoutPage", "page", map[string]any{
			field:      value,
			"children": []any{},
		})
	}

	tests := []struct {
		name     string
		docs     []Document
		expected int
	}{
		{
			name:     "default namespace passes",
			docs:     []Document{i18nDoc("de", "de", "")},
			expected: 0,
		},
		{
			name:     "_system passes",
			docs:     []Document{i18nDoc("de", "de", "_system")},
			expected: 0,
		},
		{
			name:     "named namespace nobody references",
			docs:     []Document{i18nDoc("de", "de", "global")},
			expected: 1,
		},
		{
			name:     "named namespace referenced via i18nNamespace",
			docs:     []Document{i18nDoc("de", "de", "global"), pageWithNamespace("i18nNamespace", "global")},
			expected: 0,
		},
		{
			name:     "named namespace referenced via titleNamespace",
			docs:     []Document{i18nDoc("de", "de", "global"), pageWithNamespace("titleNamespace", "global")},
			expected: 0,
		},
		{
			name: "reference on a nested child counts",
			docs: []Document{
				i18nDoc("de", "de", "external"),
				componentDoc("LayoutPage", "page", map[string]any{
					"children": []any{
						map[string]any{"kind": "Table", "spec": map[string]any{"i18nNamespace": "external"}},
					},
				}),
			},
			expected: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runRule(t, i18nNamespaceUnreferenced, tt.docs)
			if len(findings) != tt.expected {
				t.Fatalf("got %d findings %v, want %d", len(findings), findings, tt.expected)
			}
		})
	}
}

func TestI18nTitleNamespaceDeprecated(t *testing.T) {
	page := componentDoc("LayoutPage", "page", map[string]any{
		"titleNamespace": "global",
		"children": []any{
			map[string]any{"kind": "LayoutCard", "spec": map[string]any{"titleNamespace": "other"}},
		},
	})
	findings := runRule(t, i18nTitleNamespaceDeprecated, []Document{page})
	if len(findings) != 2 {
		t.Fatalf("got %d findings %v, want 2", len(findings), findings)
	}
	if findings[0].Path != "spec.titleNamespace" {
		t.Errorf("finding[0].Path = %q, want spec.titleNamespace", findings[0].Path)
	}
	if findings[1].Path != "spec.children.0.spec.titleNamespace" {
		t.Errorf("finding[1].Path = %q, want spec.children.0.spec.titleNamespace", findings[1].Path)
	}

	clean := componentDoc("LayoutPage", "page", map[string]any{"i18nNamespace": "global", "children": []any{}})
	if findings := runRule(t, i18nTitleNamespaceDeprecated, []Document{clean}); len(findings) != 0 {
		t.Fatalf("got %d findings %v, want 0", len(findings), findings)
	}
}
