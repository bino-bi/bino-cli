package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datacheck"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/diagnostics"
	"bino.bi/bino/internal/report/lint"
)

// dataRun is the input of the --with-data checks.
type dataRun struct {
	projectRoot string
	projectCfg  *pathutil.ProjectConfig
	documents   []config.Document
	kinds       config.KindProvider
	runner      *lint.Runner
	timeout     time.Duration
}

// runDataChecks runs the --with-data checks within the time limit and
// returns findings with the data findings added, plus the data validation
// warnings. The checks and the execution run once per document set of
// datacheck.Variants, so constraint variants never meet; without
// constraints that is one run on all documents. A run that hits the limit
// keeps what it found and adds one info finding that names what was not
// checked.
func runDataChecks(ctx context.Context, out *Output, run dataRun, findings []lint.Finding) ([]lint.Finding, []dataset.Warning) {
	dataCtx := ctx
	if run.timeout > 0 {
		var cancel context.CancelFunc
		dataCtx, cancel = context.WithTimeout(ctx, run.timeout)
		defer cancel()
	}

	sets, uncovered := datacheck.Variants(run.documents)
	var (
		prepend, appended []lint.Finding
		warnings          []dataset.Warning
		unchecked         []string
	)
	for i, set := range sets {
		label := ""
		if len(sets) > 1 {
			label = fmt.Sprintf(" (document set %d of %d)", i+1, len(sets))
		}
		v := checkVariant(ctx, dataCtx, out, run, set, label)
		prepend = append(prepend, v.prepend...)
		appended = append(appended, v.appended...)
		warnings = append(warnings, v.warnings...)
		for _, n := range v.unchecked {
			if !slices.Contains(unchecked, n) {
				unchecked = append(unchecked, n)
			}
		}
	}
	if len(sets) > 1 {
		prepend = datacheck.Dedupe(prepend)
		appended = datacheck.Dedupe(appended)
		warnings = dedupeWarnings(warnings)
	}
	findings = append(append(prepend, findings...), appended...)

	toml := filepath.Join(run.projectRoot, "bino.toml")
	if len(uncovered) > 0 {
		findings = append(findings, run.runner.Apply([]lint.Finding{{
			RuleID:   lint.RuleDataUnchecked,
			Severity: "info",
			Message:  fmt.Sprintf("not checked: %s (no artefact selects them)", strings.Join(uncovered, ", ")),
			File:     toml, Line: 1, Column: 1,
		}})...)
	}
	if dataCtx.Err() != nil && ctx.Err() == nil {
		notChecked := "the checks that read the full rows"
		if len(unchecked) > 0 {
			notChecked = strings.Join(unchecked, ", ")
		}
		findings = append(findings, run.runner.Apply([]lint.Finding{{
			RuleID:   lint.RuleDataTimeout,
			Severity: "info",
			Message:  fmt.Sprintf("data checks stopped after %s; not checked: %s", run.timeout, notChecked),
			File:     toml, Line: 1, Column: 1,
		}})...)
	}
	return findings, warnings
}

// variantResult is what the data checks found on one document set.
type variantResult struct {
	prepend, appended []lint.Finding
	warnings          []dataset.Warning
	unchecked         []string
}

// checkVariant runs the data checks and the full execution on one document
// set, so the warnings it drops and the results it reads all belong to it.
func checkVariant(ctx, dataCtx context.Context, out *Output, run dataRun, docs []config.Document, label string) variantResult {
	var v variantResult
	out.Step("Checking dataset queries and columns" + label + "...")
	checkStart := time.Now()
	res, err := datacheck.RunSet(dataCtx, docs, run.kinds)
	if err != nil {
		out.StepDone("Data checks failed", time.Since(checkStart))
		out.Warning(fmt.Sprintf("Data check error: %v", err))
	} else {
		out.StepDone(fmt.Sprintf("Found %d data finding(s)", len(res.Findings)), time.Since(checkStart))
		v.appended = append(v.appended, run.runner.Apply(res.Findings)...)
	}
	v.unchecked = res.Unchecked

	if dataCtx.Err() != nil {
		if ctx.Err() == nil {
			v.unchecked = appendNotRun(v.unchecked, docs, nil, res.FailedDataSets)
		}
		return v
	}

	out.Step("Executing dataset queries" + label + "...")
	queryStart := time.Now()
	execOpts := &dataset.ExecuteOptions{
		DataValidation:           dataset.DataValidationWarn,
		DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
		ContinueOnQueryError:     true,
	}
	results, execWarnings, err := dataset.Execute(dataCtx, run.projectRoot, docs, execOpts)
	switch {
	// With ContinueOnQueryError a query the deadline cancels is only a
	// warning, and err stays nil when it was the last one.
	case dataCtx.Err() != nil && ctx.Err() == nil:
		out.StepDone("Query execution stopped by --data-timeout", time.Since(queryStart))
		v.warnings = dropInterrupted(dropReportedQueryErrors(execWarnings, res), results)
		v.unchecked = appendNotRun(v.unchecked, docs, results, res.FailedDataSets)
	case err != nil:
		out.StepDone("Query execution failed", time.Since(queryStart))
		out.Warning(fmt.Sprintf("Query execution error: %v", err))
	default:
		out.StepDone(fmt.Sprintf("Executed %d dataset(s)", len(results)), time.Since(queryStart))
		v.warnings = dropReportedQueryErrors(execWarnings, res)
		v.appended = append(v.appended, run.runner.Apply(diagnostics.AnnotationPointFindings(ctx, docs, results))...)
		v.appended = append(v.appended, run.runner.Apply(diagnostics.CategoryIntervalPositionFindings(docs, results))...)
		v.appended = append(v.appended, run.runner.Apply(diagnostics.SmallMultipleOverrideFindings(docs, results))...)
		v.prepend = tableDefaultEngineFindings(run.projectRoot, run.projectCfg, docs, results)
	}
	return v
}

func dedupeWarnings(warnings []dataset.Warning) []dataset.Warning {
	seen := map[dataset.Warning]bool{}
	out := warnings[:0]
	for _, w := range warnings {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// appendNotRun adds the DataSets the full execution did not reach.
func appendNotRun(unchecked []string, docs []config.Document, results []dataset.Result, failed []string) []string {
	ran := map[string]bool{}
	for _, r := range results {
		ran[r.Name] = true
	}
	for _, d := range docs {
		if d.Kind != "DataSet" || ran[d.Name] || slices.Contains(failed, d.Name) || slices.Contains(unchecked, d.Name) {
			continue
		}
		unchecked = append(unchecked, d.Name)
	}
	return unchecked
}

// dropReportedQueryErrors removes the execution warnings that a data
// finding already reports: the query error of a DataSet with a dataset-sql
// finding, and the view error of a DataSource that such a finding names.
func dropReportedQueryErrors(warnings []dataset.Warning, res datacheck.Result) []dataset.Warning {
	out := warnings[:0]
	for _, w := range warnings {
		if slices.Contains(res.FailedDataSets, w.DataSet) && strings.HasPrefix(w.Message, "execute:") {
			continue
		}
		if slices.Contains(res.FailedSources, w.DataSet) && strings.HasPrefix(w.Message, "datasource:") {
			continue
		}
		out = append(out, w)
	}
	return out
}

// dropInterrupted removes the execution warnings of DataSets that have no
// result: after the deadline their error is the cancellation, not the data.
// appendNotRun lists them as not checked instead.
func dropInterrupted(warnings []dataset.Warning, results []dataset.Result) []dataset.Warning {
	ran := map[string]bool{}
	for _, r := range results {
		ran[r.Name] = true
	}
	out := warnings[:0]
	for _, w := range warnings {
		if !ran[w.DataSet] && strings.HasPrefix(w.Message, "execute:") {
			continue
		}
		out = append(out, w)
	}
	return out
}
