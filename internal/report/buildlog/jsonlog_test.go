package buildlog_test

import (
	"testing"

	"bino.bi/bino/internal/report/buildlog"
)

// TestBuildLintEntrySeverity pins that the lint log carries the severity the
// project graded a rule with. Hardcoding "warning" here made the CI-parseable
// artifact contradict the terminal output and the exit code of the same run.
func TestBuildLintEntrySeverity(t *testing.T) {
	cases := []struct{ in, want string }{
		{in: "", want: "warning"},
		{in: "warning", want: "warning"},
		{in: "error", want: "error"},
		{in: "info", want: "info"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := buildlog.BuildLintEntry("some-rule", tc.in, "msg", "f.yaml", 1, "spec", 2, 3)
			if got.Severity != tc.want {
				t.Fatalf("BuildLintEntry(severity=%q).Severity = %q, want %q", tc.in, got.Severity, tc.want)
			}
		})
	}
}
