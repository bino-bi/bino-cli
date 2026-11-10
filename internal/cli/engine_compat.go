package cli

import (
	"errors"
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/schema"
)

// printCompatFinding renders the compat finding to the structured output
// using the same `[ruleID] file:line:col: msg` shape as the main lint
// printer. Used when manifest loading fails so the user still sees the
// actionable engine-version error.
func printCompatFinding(out *Output, projectRoot string, f lint.Finding) {
	out.Warning("Engine compatibility error:")
	rel := pathutil.RelPath(projectRoot, f.File)
	loc := rel
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", rel, f.Line, f.Column)
	}
	out.List(fmt.Sprintf("[%s] %s: %s", f.RuleID, loc, f.Message))
}

// resolveEngineVersionForCompat returns the engine version to validate, see
// engine.ResolveVersion.
func resolveEngineVersionForCompat(pinnedVersion string) string {
	return engine.ResolveVersion(pinnedVersion)
}

// engineCompatFinding runs the compatibility check and, on a
// *engine.CompatibilityError, returns a synthetic lint finding pointing at
// bino.toml's engine-version line. fatal=true signals that lint must exit
// non-zero. Any non-CompatibilityError outcome returns fatal=false so other
// findings can still surface.
func engineCompatFinding(projectRoot, pinnedVersion string) (lint.Finding, bool) {
	resolved := resolveEngineVersionForCompat(pinnedVersion)
	if resolved == "" {
		return lint.Finding{}, false
	}
	cErr, ok := errors.AsType[*engine.CompatibilityError](engine.CheckCompatibility(resolved))
	if !ok {
		return lint.Finding{}, false
	}
	configPath := pathutil.ProjectConfigPath(projectRoot)
	line, col := pathutil.FindEngineVersionLine(configPath)
	return lint.Finding{
		RuleID:  "engine-version-incompatible",
		Message: cErr.Error(),
		File:    configPath,
		Line:    line,
		Column:  col,
	}, true
}

// annotationEngineFinding reports a project with Annotation documents whose
// engine is older than the release with the overlay. It is its own check, as a
// project without annotations keeps working on that engine.
func annotationEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document) (lint.Finding, bool) {
	used := false
	for _, doc := range documents {
		used = used || doc.Kind == schema.KindAnnotation
	}
	return engineGateFinding(projectRoot, cfg, used, engine.CheckAnnotationSupport, "annotation-engine-version")
}

// gridCellLayoutEngineFinding reports a project that uses grid cell size,
// alignment or captions on an engine older than the release with them.
func gridCellLayoutEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document) (lint.Finding, bool) {
	used := config.UsesGridCellLayout(documents)
	check := engine.GridCellLayoutCheck(config.UsesSmallMultipleCard(documents))
	return engineGateFinding(projectRoot, cfg, used, check, "grid-cell-layout-engine-version")
}

// operationSentimentEngineFinding reports a project that uses the variance
// sentiments _op or _opinv on an engine older than the release with them.
func operationSentimentEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document) (lint.Finding, bool) {
	used := config.UsesOperationSentiment(documents)
	return engineGateFinding(projectRoot, cfg, used, engine.CheckOperationSentimentSupport, "operation-sentiment-engine-version")
}

// tableOrderMeasureEngineFinding reports a project that sorts a Table by a
// variance or an interval block on an engine older than the release with it.
func tableOrderMeasureEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document) (lint.Finding, bool) {
	used := config.UsesTableOrderMeasure(documents)
	return engineGateFinding(projectRoot, cfg, used, engine.CheckTableOrderMeasureSupport, "table-order-engine-version")
}

// categoryIntervalPositionEngineFinding reports a project that sets a Table
// categoryIntervalPosition on an engine older than the release with it.
func categoryIntervalPositionEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document) (lint.Finding, bool) {
	used := config.UsesCategoryIntervalPosition(documents)
	return engineGateFinding(projectRoot, cfg, used, engine.CheckCategoryIntervalPositionSupport, "table-category-interval-position-engine-version")
}

// categoryIntervalPositionDefaultEngineFinding is the same gate for a value
// that only a dataset default sets, which shows after the queries ran.
func categoryIntervalPositionDefaultEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document, results []dataset.Result) (lint.Finding, bool) {
	used := !config.UsesCategoryIntervalPosition(documents) && dataset.DefaultsSetCategoryIntervalPosition(results)
	return engineGateFinding(projectRoot, cfg, used, engine.CheckCategoryIntervalPositionSupport, "table-category-interval-position-engine-version")
}

// tableCalculationSchemeEngineFinding reports a project whose Tables turn on
// runningSum, thereofPosition or varianceWaterfall on an engine older than the
// release with them.
func tableCalculationSchemeEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document) (lint.Finding, bool) {
	props := config.TableCalculationSchemeProps(documents)
	return engineGateFinding(projectRoot, cfg, len(props) > 0, engine.TableCalculationSchemeCheck(props), "table-calculation-scheme-engine-version")
}

// tableCalculationSchemeDefaultEngineFinding is the same gate for fields that
// only a dataset default turns on, which shows after the queries ran.
func tableCalculationSchemeDefaultEngineFinding(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document, results []dataset.Result) (lint.Finding, bool) {
	props := dataset.DefaultsTableCalculationSchemeProps(results)
	used := len(config.TableCalculationSchemeProps(documents)) == 0 && len(props) > 0
	return engineGateFinding(projectRoot, cfg, used, engine.TableCalculationSchemeCheck(props), "table-calculation-scheme-engine-version")
}

// tableEngineFindings returns the engine gate findings of the Table features.
func tableEngineFindings(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document) []lint.Finding {
	var findings []lint.Finding
	for _, gate := range []func(string, *pathutil.ProjectConfig, []config.Document) (lint.Finding, bool){
		categoryIntervalPositionEngineFinding,
		tableOrderMeasureEngineFinding,
		tableCalculationSchemeEngineFinding,
	} {
		if finding, ok := gate(projectRoot, cfg, documents); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

// tableDefaultEngineFindings returns the engine gate findings of the Table
// features that only a dataset default turns on.
func tableDefaultEngineFindings(projectRoot string, cfg *pathutil.ProjectConfig, documents []config.Document, results []dataset.Result) []lint.Finding {
	var findings []lint.Finding
	for _, gate := range []func(string, *pathutil.ProjectConfig, []config.Document, []dataset.Result) (lint.Finding, bool){
		categoryIntervalPositionDefaultEngineFinding,
		tableCalculationSchemeDefaultEngineFinding,
	} {
		if finding, ok := gate(projectRoot, cfg, documents, results); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

// engineGateFinding runs a feature gate of the engine when the project uses
// the feature, and reports a failure on the engine-version line of bino.toml.
func engineGateFinding(projectRoot string, cfg *pathutil.ProjectConfig, used bool, check func(string) error, ruleID string) (lint.Finding, bool) {
	if !used {
		return lint.Finding{}, false
	}
	pinned := ""
	if cfg != nil {
		pinned = cfg.EngineVersion
	}
	err := check(resolveEngineVersionForCompat(pinned))
	if err == nil {
		return lint.Finding{}, false
	}
	configPath := pathutil.ProjectConfigPath(projectRoot)
	line, col := pathutil.FindEngineVersionLine(configPath)
	return lint.Finding{
		RuleID:  ruleID,
		Message: err.Error(),
		File:    configPath,
		Line:    line,
		Column:  col,
	}, true
}

// engineCompatDiagnostic is the LSP-shaped equivalent of engineCompatFinding.
// On a *engine.CompatibilityError it returns an error-severity diagnostic on
// bino.toml. ok=false means there is no compat issue (or no engine to check).
func engineCompatDiagnostic(projectRoot string) (LSPDiagnostic, bool) {
	cfg, err := pathutil.LoadProjectConfig(projectRoot)
	if err != nil {
		cfg = &pathutil.ProjectConfig{}
	}
	resolved := resolveEngineVersionForCompat(cfg.EngineVersion)
	if resolved == "" {
		return LSPDiagnostic{}, false
	}
	cErr, ok := errors.AsType[*engine.CompatibilityError](engine.CheckCompatibility(resolved))
	if !ok {
		return LSPDiagnostic{}, false
	}
	configPath := pathutil.ProjectConfigPath(projectRoot)
	line, col := pathutil.FindEngineVersionLine(configPath)
	return LSPDiagnostic{
		File:     configPath,
		Line:     line,
		Column:   col,
		Severity: "error",
		Message:  cErr.Error(),
		Code:     "engine-version-incompatible",
	}, true
}
