package refresh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/lint"
)

// captureLogger records every line the refresh loop logs, per level.
type captureLogger struct {
	infos  []string
	warns  []string
	errors []string
}

func (c *captureLogger) Infof(format string, args ...any) {
	c.infos = append(c.infos, fmt.Sprintf(format, args...))
}
func (c *captureLogger) Successf(string, ...any) {}
func (c *captureLogger) Warnf(format string, args ...any) {
	c.warns = append(c.warns, fmt.Sprintf(format, args...))
}
func (c *captureLogger) Errorf(format string, args ...any) {
	c.errors = append(c.errors, fmt.Sprintf(format, args...))
}
func (c *captureLogger) Debugf(string, ...any)      {}
func (c *captureLogger) Channel(string) logx.Logger { return c }

// lintFixtureProject writes a bino.toml plus a bundle that raises exactly one
// artefact-layoutpage-required finding (a4 artefact, xga page).
func lintFixtureProject(t *testing.T, toml string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"bino.toml": toml,
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: r
spec:
  format: a4
  orientation: portrait
  language: en
  filename: out.pdf
  title: Sample
  layoutPages:
    - p
`,
		"page.yaml": `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: p
spec:
  children:
    - kind: Text
      spec:
        value: hi
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// TestLogLintFindings pins the preview server's lint report against the
// project's [lint] table: disable removes the line, and a severity the project
// set decides the level it is logged at.
func TestLogLintFindings(t *testing.T) {
	tests := []struct {
		name       string
		toml       string
		wantInfo   int
		wantWarn   int
		wantErrors int
	}{
		{
			name:     "no lint table",
			toml:     "report-id = \"t\"\n",
			wantWarn: 1,
		},
		{
			name: "rule disabled",
			toml: "report-id = \"t\"\n\n[lint]\ndisable = [\"artefact-layoutpage-required\"]\n",
		},
		{
			name:       "rule raised to error",
			toml:       "report-id = \"t\"\n\n[lint.severity]\nartefact-layoutpage-required = \"error\"\n",
			wantErrors: 1,
		},
		{
			name:     "rule lowered to info",
			toml:     "report-id = \"t\"\n\n[lint.severity]\nartefact-layoutpage-required = \"info\"\n",
			wantInfo: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := lintFixtureProject(t, tc.toml)
			docs, err := config.LoadDirWithOptions(ctx, dir, config.LoadOptions{})
			if err != nil {
				t.Fatalf("load manifests: %v", err)
			}

			logger := &captureLogger{}
			logLintFindings(ctx, logger, dir, lint.DocumentsFromConfig(docs), nil)

			if len(logger.infos) != tc.wantInfo || len(logger.warns) != tc.wantWarn || len(logger.errors) != tc.wantErrors {
				t.Fatalf("logged infos=%v warns=%v errors=%v, want %d/%d/%d",
					logger.infos, logger.warns, logger.errors, tc.wantInfo, tc.wantWarn, tc.wantErrors)
			}
			for _, line := range append(append(append([]string{}, logger.infos...), logger.warns...), logger.errors...) {
				if !strings.Contains(line, "[artefact-layoutpage-required] report.yaml #1:") {
					t.Errorf("line = %q, want the rule id and relative location", line)
				}
			}
		})
	}
}
