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
// warnings. A run that hits the limit keeps what it found and adds one info
// finding that names what was not checked.
func runDataChecks(ctx context.Context, out *Output, run dataRun, findings []lint.Finding) ([]lint.Finding, []dataset.Warning) {
	dataCtx := ctx
	if run.timeout > 0 {
		var cancel context.CancelFunc
		dataCtx, cancel = context.WithTimeout(ctx, run.timeout)
		defer cancel()
	}

	out.Step("Checking dataset queries and columns...")
	checkStart := time.Now()
	res, err := datacheck.Run(dataCtx, run.documents, run.kinds)
	if err != nil {
		out.StepDone("Data checks failed", time.Since(checkStart))
		out.Warning(fmt.Sprintf("Data check error: %v", err))
	} else {
		out.StepDone(fmt.Sprintf("Found %d data finding(s)", len(res.Findings)), time.Since(checkStart))
		findings = append(findings, run.runner.Apply(res.Findings)...)
	}
	unchecked := res.Unchecked

	var warnings []dataset.Warning
	if dataCtx.Err() == nil {
		out.Step("Executing dataset queries...")
		queryStart := time.Now()
		execOpts := &dataset.ExecuteOptions{
			DataValidation:           dataset.DataValidationWarn,
			DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
			ContinueOnQueryError:     true,
		}
		results, execWarnings, err := dataset.Execute(dataCtx, run.projectRoot, run.documents, execOpts)
		switch {
		// With ContinueOnQueryError a query the deadline cancels is only a
		// warning, and err stays nil when it was the last one.
		case dataCtx.Err() != nil && ctx.Err() == nil:
			out.StepDone("Query execution stopped by --data-timeout", time.Since(queryStart))
			warnings = dropReportedQueryErrors(execWarnings, res)
			warnings = dropInterrupted(warnings, results)
			unchecked = appendNotRun(unchecked, run.documents, results, res.FailedDataSets)
		case err != nil:
			out.StepDone("Query execution failed", time.Since(queryStart))
			out.Warning(fmt.Sprintf("Query execution error: %v", err))
		default:
			out.StepDone(fmt.Sprintf("Executed %d dataset(s)", len(results)), time.Since(queryStart))
			warnings = dropReportedQueryErrors(execWarnings, res)
			findings = append(findings, run.runner.Apply(diagnostics.AnnotationPointFindings(ctx, run.documents, results))...)
			findings = append(findings, run.runner.Apply(diagnostics.CategoryIntervalPositionFindings(run.documents, results))...)
			findings = append(findings, run.runner.Apply(diagnostics.SmallMultipleOverrideFindings(run.documents, results))...)
			findings = append(tableDefaultEngineFindings(run.projectRoot, run.projectCfg, run.documents, results), findings...)
		}
	} else if ctx.Err() == nil {
		unchecked = appendNotRun(unchecked, run.documents, nil, res.FailedDataSets)
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
			File:     filepath.Join(run.projectRoot, "bino.toml"),
			Line:     1,
			Column:   1,
		}})...)
	}
	return findings, warnings
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
