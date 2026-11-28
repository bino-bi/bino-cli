package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// lintDataTOML silences the findings of a project without a report.
	lintDataTOML   = "report-id = \"t\"\n\n[lint]\ndisable = [\"report-artefact-required\", \"dataset-dependency-undeclared\"]\n"
	lintDataSource = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_src
spec:
  type: inline
  content:
    - category: EU
      categoryIndex: 1
      ac1: 10
`
	// The typo "amout" is on line 8.
	lintDataSetTypo = `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: |
    SELECT category,
           amout
    FROM sales_src
`
	lintDataSetOK = `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: SELECT category, categoryIndex, ac1 FROM sales_src
`
)

func TestLintWithoutDataRunsNoQuery(t *testing.T) {
	dir := writeLintProject(t, lintDataTOML, map[string]string{"src.yaml": lintDataSource, "set.yaml": lintDataSetTypo})
	out, err := runLintCommand(t, dir)
	if err != nil {
		t.Fatalf("lint without --with-data must not see the query error, got %v\n%s", err, out)
	}
	for _, s := range []string{"dataset-sql", "Executing dataset queries", "Checking dataset queries"} {
		if strings.Contains(out, s) {
			t.Errorf("lint without --with-data ran queries (%q):\n%s", s, out)
		}
	}
}

func TestLintWithDataReportsSQLError(t *testing.T) {
	for _, flag := range []string{"--with-data", "--execute-queries"} {
		t.Run(flag, func(t *testing.T) {
			dir := writeLintProject(t, lintDataTOML, map[string]string{"src.yaml": lintDataSource, "set.yaml": lintDataSetTypo})
			out, err := runLintCommand(t, dir, flag, "--lint-log-format", "json")
			if err == nil {
				t.Fatalf("a failing query must fail lint:\n%s", out)
			}
			if !strings.Contains(out, "[error] [dataset-sql] set.yaml:8:12 (spec.query): DataSet \"sales\": Binder Error") {
				t.Errorf("missing dataset-sql finding on line 8:\n%s", out)
			}
			if strings.Contains(out, "data validation warning") {
				t.Errorf("the query error is reported twice:\n%s", out)
			}

			logs, _ := filepath.Glob(filepath.Join(dir, "out", "bino-lint-*.json"))
			if len(logs) != 1 {
				t.Fatalf("json logs = %v", logs)
			}
			b, err := os.ReadFile(logs[0])
			if err != nil {
				t.Fatal(err)
			}
			var log struct {
				Lint []struct {
					RuleID   string `json:"rule_id"`
					Severity string `json:"severity"`
					Line     int    `json:"line"`
				} `json:"lint"`
			}
			if err := json.Unmarshal(b, &log); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range log.Lint {
				if e.RuleID == "dataset-sql" && e.Severity == "error" && e.Line == 8 {
					found = true
				}
			}
			if !found {
				t.Errorf("json log lacks the dataset-sql finding:\n%s", b)
			}
		})
	}
}

func TestLintWithDataClean(t *testing.T) {
	dir := writeLintProject(t, lintDataTOML, map[string]string{"src.yaml": lintDataSource, "set.yaml": lintDataSetOK})
	out, err := runLintCommand(t, dir, "--with-data", "--fail-on-warnings")
	if err != nil || !strings.Contains(out, "No lint warnings found") {
		t.Fatalf("clean project: %v\n%s", err, out)
	}
}

func TestLintWithDataTimeout(t *testing.T) {
	dir := writeLintProject(t, lintDataTOML, map[string]string{"src.yaml": lintDataSource, "set.yaml": lintDataSetTypo})
	out, err := runLintCommand(t, dir, "--with-data", "--data-timeout", "1ns", "--fail-on-warnings")
	if err != nil {
		t.Fatalf("the time limit must not fail lint, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "[info] [data-timeout] bino.toml:1:1: data checks stopped after 1ns; not checked: sales") {
		t.Errorf("missing data-timeout finding:\n%s", out)
	}
	if strings.Contains(out, "dataset-sql") {
		t.Errorf("a stopped check reported findings:\n%s", out)
	}
}
