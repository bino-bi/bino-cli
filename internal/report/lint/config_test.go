package lint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/pathutil"
)

// lintProject writes a bino.toml carrying the given body and returns its root.
func lintProject(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(pathutil.ProjectConfigPath(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("write bino.toml: %v", err)
	}
	return dir
}

// findingIDs reduces findings to "<rule id>:<severity>" pairs for comparison.
func findingIDs(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.RuleID+":"+f.Severity)
	}
	return out
}

func TestRunnerApply(t *testing.T) {
	input := []Finding{
		{RuleID: "text-content-required", Message: "empty text"},
		{RuleID: "dataset-required", Message: "no dataset"},
		{RuleID: "missing-required-reference", Message: "dangling", Severity: "error"},
	}

	tests := []struct {
		name string
		toml string
		want []string
	}{
		{
			name: "no lint table",
			toml: "report-id = \"r\"\n",
			want: []string{"text-content-required:", "dataset-required:", "missing-required-reference:error"},
		},
		{
			name: "disable only",
			toml: "[lint]\ndisable = [\"dataset-required\"]\n",
			want: []string{"text-content-required:", "missing-required-reference:error"},
		},
		{
			name: "severity only",
			toml: "[lint.severity]\ntext-content-required = \"error\"\n",
			want: []string{"text-content-required:error", "dataset-required:", "missing-required-reference:error"},
		},
		{
			name: "severity downgrade",
			toml: "[lint.severity]\nmissing-required-reference = \"info\"\n",
			want: []string{"text-content-required:", "dataset-required:", "missing-required-reference:info"},
		},
		{
			name: "disable wins over severity",
			toml: "[lint]\ndisable = [\"text-content-required\"]\n\n[lint.severity]\ntext-content-required = \"error\"\n",
			want: []string{"dataset-required:", "missing-required-reference:error"},
		},
		{
			name: "empty lint table",
			toml: "report-id = \"r\"\n\n[lint]\n",
			want: []string{"text-content-required:", "dataset-required:", "missing-required-reference:error"},
		},
		{
			name: "empty disable list",
			toml: "[lint]\ndisable = []\n",
			want: []string{"text-content-required:", "dataset-required:", "missing-required-reference:error"},
		},
		{
			name: "configured rule without findings",
			toml: "[lint]\ndisable = [\"asset-source-missing\"]\n\n[lint.severity]\nref-params = \"error\"\n",
			want: []string{"text-content-required:", "dataset-required:", "missing-required-reference:error"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := NewProjectRunner(lintProject(t, tc.toml))
			if warnings := runner.ConfigWarnings(); len(warnings) != 0 {
				t.Fatalf("unexpected config warnings: %v", warnings)
			}

			got := findingIDs(runner.Apply(input))
			if len(got) != len(tc.want) {
				t.Fatalf("Apply returned %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("finding[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRunnerApplyPluginRuleID: a plugin-prefixed id is not merely accepted by
// ConfigWarnings — Apply really drops it and really re-grades it.
func TestRunnerApplyPluginRuleID(t *testing.T) {
	runner := NewProjectRunner(lintProject(t,
		"[lint]\ndisable = [\"myplugin/dropped\"]\n\n[lint.severity]\n\"myplugin/regraded\" = \"error\"\n"))
	if warnings := runner.ConfigWarnings(); len(warnings) != 0 {
		t.Fatalf("unexpected config warnings: %v", warnings)
	}

	input := []Finding{
		{RuleID: "myplugin/dropped", Message: "gone"},
		{RuleID: "myplugin/regraded", Message: "louder"},
		{RuleID: "otherplugin/kept", Message: "untouched"},
	}
	got := findingIDs(runner.Apply(input))
	want := []string{"myplugin/regraded:error", "otherplugin/kept:"}
	if len(got) != len(want) {
		t.Fatalf("Apply returned %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("finding[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewProjectRunnerLintConfig(t *testing.T) {
	root := lintProject(t, "[lint]\ndisable = [\"report-artefact-required\"]\n\n[lint.severity]\ntext-content-required = \"error\"\n")
	runner := NewProjectRunner(root)

	if !runner.Skip("report-artefact-required") {
		t.Error("Skip(report-artefact-required) = false, want true")
	}
	if runner.Skip("text-content-required") {
		t.Error("Skip(text-content-required) = true, want false")
	}
	if got := runner.SeverityOverride("text-content-required"); got != "error" {
		t.Errorf("SeverityOverride(text-content-required) = %q, want %q", got, "error")
	}
	if got := runner.SeverityOverride("report-artefact-required"); got != "" {
		t.Errorf("SeverityOverride(report-artefact-required) = %q, want %q", got, "")
	}

	docs := []Document{{
		File:     filepath.Join(root, "text.yaml"),
		Position: 1,
		Kind:     "Text",
		Name:     "empty",
		Raw:      []byte(`{"kind":"Text","metadata":{"name":"empty"},"spec":{"value":""}}`),
	}}

	raw := runner.Run(context.Background(), docs)
	if len(raw) != 2 {
		t.Fatalf("Run returned %v, want report-artefact-required and text-content-required", findingIDs(raw))
	}

	got := findingIDs(runner.Apply(raw))
	want := []string{"text-content-required:error"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Apply returned %v, want %v", got, want)
	}
}

func TestRunnerConfigWarnings(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want []string
	}{
		{
			name: "unknown id in disable",
			toml: "[lint]\ndisable = [\"no-such-rule\"]\n",
			want: []string{`unknown rule id "no-such-rule" in [lint] disable`},
		},
		{
			name: "unknown id in severity",
			toml: "[lint.severity]\nno-such-rule = \"error\"\n",
			want: []string{`unknown rule id "no-such-rule" in [lint] severity`},
		},
		{
			name: "invalid severity value",
			toml: "[lint.severity]\ntext-content-required = \"fatal\"\n",
			want: []string{`invalid severity "fatal" for rule id "text-content-required" in [lint] severity (want error, warning or info)`},
		},
		{
			name: "two problems sorted by rule id",
			toml: "[lint.severity]\ntext-content-required = \"fatal\"\ndataset-required = \"loud\"\n",
			want: []string{
				`invalid severity "loud" for rule id "dataset-required" in [lint] severity (want error, warning or info)`,
				`invalid severity "fatal" for rule id "text-content-required" in [lint] severity (want error, warning or info)`,
			},
		},
		{
			name: "severity on the ids whose exit meaning is fixed",
			toml: "[lint.severity]\nschema-validation = \"warning\"\nmanifest-load = \"info\"\n" +
				"engine-version-incompatible = \"error\"\n",
			want: []string{
				`severity is not configurable for "engine-version-incompatible"; use [lint] disable to silence it`,
				`severity is not configurable for "manifest-load"; use [lint] disable to silence it`,
				`severity is not configurable for "schema-validation"; use [lint] disable to silence it`,
			},
		},
		{
			name: "non-rule finding ids and plugin ids are known",
			toml: "[lint]\ndisable = [\"missing-required-reference\", \"schema-validation\", \"manifest-load\", " +
				"\"engine-version-incompatible\", \"myplugin/whatever\"]\n",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := NewProjectRunner(lintProject(t, tc.toml))

			got := runner.ConfigWarnings()
			if len(got) != len(tc.want) {
				t.Fatalf("ConfigWarnings = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("warning[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRunnerConfigWarningsDoNotFilter(t *testing.T) {
	runner := NewProjectRunner(lintProject(t,
		"[lint]\ndisable = [\"no-such-rule\"]\n\n[lint.severity]\ntext-content-required = \"fatal\"\n"))

	if runner.Skip("no-such-rule") {
		t.Error("Skip(no-such-rule) = true, want false for an unknown id")
	}
	if got := runner.SeverityOverride("text-content-required"); got != "" {
		t.Errorf("SeverityOverride(text-content-required) = %q, want %q for an invalid value", got, "")
	}

	input := []Finding{{RuleID: "no-such-rule"}, {RuleID: "text-content-required"}}
	got := findingIDs(runner.Apply(input))
	want := []string{"no-such-rule:", "text-content-required:"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Apply returned %v, want %v", got, want)
	}
}

// TestRunnerFixedSeverityIDsStayInert: rejecting a severity on these ids must
// leave both the override and the finding untouched, while disable keeps
// working on them.
func TestRunnerFixedSeverityIDsStayInert(t *testing.T) {
	runner := NewProjectRunner(lintProject(t,
		"[lint]\ndisable = [\"manifest-load\"]\n\n[lint.severity]\nschema-validation = \"info\"\n"))

	if got := runner.SeverityOverride("schema-validation"); got != "" {
		t.Errorf("SeverityOverride(schema-validation) = %q, want %q", got, "")
	}
	if !runner.Skip("manifest-load") {
		t.Error("Skip(manifest-load) = false, want true; disable must still work on these ids")
	}

	got := findingIDs(runner.Apply([]Finding{{RuleID: "schema-validation"}, {RuleID: "manifest-load"}}))
	want := []string{"schema-validation:"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Apply returned %v, want %v", got, want)
	}
}

func TestRunnerNoLintConfig(t *testing.T) {
	input := []Finding{{RuleID: "text-content-required"}, {RuleID: "missing-required-reference", Severity: "error"}}

	runners := map[string]*Runner{
		"NewRunner":        NewRunner(DefaultRules()),
		"NewDefaultRunner": NewDefaultRunner(),
		"NewProjectRunner": NewProjectRunner(t.TempDir()), // no bino.toml at all
	}

	for name, runner := range runners {
		t.Run(name, func(t *testing.T) {
			if runner.Skip("text-content-required") {
				t.Error("Skip = true, want false without a [lint] table")
			}
			if got := runner.SeverityOverride("text-content-required"); got != "" {
				t.Errorf("SeverityOverride = %q, want %q", got, "")
			}
			if warnings := runner.ConfigWarnings(); len(warnings) != 0 {
				t.Errorf("ConfigWarnings = %v, want none", warnings)
			}

			got := runner.Apply(input)
			if len(got) != len(input) {
				t.Fatalf("Apply returned %d findings, want %d", len(got), len(input))
			}
			for i := range got {
				if got[i] != input[i] {
					t.Errorf("finding[%d] = %+v, want %+v", i, got[i], input[i])
				}
			}
		})
	}
}
