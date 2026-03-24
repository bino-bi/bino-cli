package lint

import (
	"context"
	"errors"
	"testing"
)

type mockLinter struct {
	name     string
	findings []PluginLintFinding
	err      error
}

func (m *mockLinter) Manifest() PluginManifestInfo {
	return PluginManifestInfo{Name: m.name, ProvidesLinter: true}
}

func (m *mockLinter) Lint(_ context.Context, _ []PluginDocumentPayload, _ *PluginLintOptions) ([]PluginLintFinding, error) {
	return m.findings, m.err
}

type mockLinterRegistry struct {
	linters []PluginLinter
}

func (r *mockLinterRegistry) PluginsWithLinter() []PluginLinter { return r.linters }

func TestRunPluginLinters_NilRegistry(t *testing.T) {
	findings := RunPluginLinters(context.Background(), nil, nil)
	if len(findings) != 0 {
		t.Fatal("expected no findings for nil registry")
	}
}

func TestRunPluginLinters_SinglePlugin(t *testing.T) {
	reg := &mockLinterRegistry{
		linters: []PluginLinter{
			&mockLinter{
				name: "sf",
				findings: []PluginLintFinding{
					{RuleID: "deprecated-api", Message: "old API", File: "a.yaml"},
					{RuleID: "sf/prefixed", Message: "already prefixed", File: "b.yaml"},
				},
			},
		},
	}

	findings := RunPluginLinters(context.Background(), []Document{{File: "a.yaml", Kind: "DataSource"}}, reg)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].RuleID != "sf/deprecated-api" {
		t.Fatalf("expected auto-prefixed rule ID, got %q", findings[0].RuleID)
	}
	if findings[1].RuleID != "sf/prefixed" {
		t.Fatalf("expected already-prefixed rule ID preserved, got %q", findings[1].RuleID)
	}
}

func TestRunPluginLinters_PluginError_NonFatal(t *testing.T) {
	reg := &mockLinterRegistry{
		linters: []PluginLinter{
			&mockLinter{name: "broken", err: errors.New("crash")},
			&mockLinter{name: "working", findings: []PluginLintFinding{{RuleID: "ok", Message: "fine"}}},
		},
	}

	findings := RunPluginLinters(context.Background(), []Document{{}}, reg)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (from working plugin), got %d", len(findings))
	}
	if findings[0].RuleID != "working/ok" {
		t.Fatalf("expected finding from working plugin, got %q", findings[0].RuleID)
	}
}

func TestFilterFindings_DisableRule(t *testing.T) {
	findings := []Finding{
		{RuleID: "sf/deprecated-api", Message: "old"},
		{RuleID: "sf/missing-flag", Message: "missing"},
		{RuleID: "builtin/unused-ds", Message: "unused"},
	}

	cfg := LintConfig{Disable: []string{"sf/deprecated-api"}}
	filtered := FilterFindings(findings, cfg)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 findings after filter, got %d", len(filtered))
	}
	if filtered[0].RuleID != "sf/missing-flag" {
		t.Fatal("wrong finding remained")
	}
}

func TestFilterFindings_EmptyConfig(t *testing.T) {
	findings := []Finding{{RuleID: "a"}, {RuleID: "b"}}
	filtered := FilterFindings(findings, LintConfig{})
	if len(filtered) != 2 {
		t.Fatal("empty config should pass all findings through")
	}
}
