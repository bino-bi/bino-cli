package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

// The deadline falls inside the full execution of the last DataSet: its
// LIMIT 0 check is fast, its full query is not.
func TestLintWithDataTimeoutDuringExecution(t *testing.T) {
	slow := `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: slow
spec:
  query: SELECT count(*) AS n FROM range(100000000) a, range(1000) b
`
	dir := writeLintProject(t, lintDataTOML, map[string]string{"src.yaml": lintDataSource, "set.yaml": lintDataSetOK, "slow.yaml": slow})
	out, err := runLintCommand(t, dir, "--with-data", "--data-timeout", "1500ms", "--fail-on-warnings")
	if err != nil {
		t.Fatalf("the time limit must not fail lint, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "Query execution stopped by --data-timeout") {
		t.Fatalf("the deadline did not fall inside the execution:\n%s", out)
	}
	// Under -race the deadline can also catch sales before it ran.
	if !regexp.MustCompile(`\[info\] \[data-timeout\] bino\.toml:1:1: data checks stopped after 1\.5s; not checked: (sales, )?slow\n`).MatchString(out) {
		t.Errorf("missing data-timeout finding naming slow:\n%s", out)
	}
	if strings.Contains(out, "data validation warning") || strings.Contains(out, "BNR_MAX_QUERY_DURATION_MS") {
		t.Errorf("the cancelled query is reported as a warning:\n%s", out)
	}
}

func TestLintWithDataMissingFileReportedOnce(t *testing.T) {
	src := `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_src
spec:
  type: csv
  path: missing.csv
`
	dir := writeLintProject(t, lintDataTOML, map[string]string{"src.yaml": src, "set.yaml": lintDataSetOK})
	out, _ := runLintCommand(t, dir, "--with-data")
	if !strings.Contains(out, "[dataset-sql]") || !strings.Contains(out, `DataSource "sales_src"`) {
		t.Fatalf("missing dataset-sql finding naming the DataSource:\n%s", out)
	}
	if strings.Contains(out, "data validation warning") {
		t.Errorf("the missing file is reported twice:\n%s", out)
	}
}

// lintVariantTOML silences the page findings the minimal artefacts raise.
const lintVariantTOML = "report-id = \"t\"\n\n[lint]\ndisable = [\"artefact-layoutpage-required\"]\n"

// lintVariantFiles are two language variants of one DataSource and DataSet;
// each artefact builds with its own pair.
func lintVariantFiles() map[string]string {
	artefact := func(name, lang string) string {
		return `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: ` + name + `
spec:
  format: a4
  orientation: portrait
  language: ` + lang + `
  filename: ` + name + `.pdf
  title: T
  layoutPages: [p]
`
	}
	variant := func(lang, col string) string {
		return `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: src
  constraints: [spec.language==` + lang + `]
spec:
  type: inline
  content:
    - category: EU
      categoryIndex: 1
      ` + col + `: 1
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
  constraints: [spec.language==` + lang + `]
spec:
  dependencies: [src]
  query: SELECT category, categoryIndex, ` + col + ` AS ac1 FROM src
`
	}
	return map[string]string{
		"de.yaml": artefact("r_de", "de"), "en.yaml": artefact("r_en", "en"),
		"v_de.yaml": variant("de", "umsatz"), "v_en.yaml": variant("en", "revenue"),
		"page.yaml": lintCfgPage,
	}
}

func TestLintWithDataConstraintVariants(t *testing.T) {
	dir := writeLintProject(t, lintVariantTOML, lintVariantFiles())
	out, err := runLintCommand(t, dir, "--with-data", "--fail-on-warnings")
	if err != nil {
		t.Fatalf("each artefact builds, so lint must pass, got %v\n%s", err, out)
	}
	if strings.Contains(out, "Binder Error") || strings.Contains(out, "data validation warning") {
		t.Errorf("a variant was checked against the other one:\n%s", out)
	}
}

func TestLintWithDataUnselectedDataSet(t *testing.T) {
	files := lintVariantFiles()
	files["orphan.yaml"] = `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: orphan
  constraints: [mode==preview]
spec:
  query: SELECT 1 AS ac1
`
	dir := writeLintProject(t, lintVariantTOML, files)
	out, err := runLintCommand(t, dir, "--with-data", "--fail-on-warnings")
	if err != nil {
		t.Fatalf("an unchecked DataSet is info, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "[info] [data-unchecked] bino.toml:1:1: not checked: orphan (no artefact selects them)") {
		t.Errorf("missing data-unchecked finding:\n%s", out)
	}
}
