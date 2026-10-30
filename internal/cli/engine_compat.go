package cli

import (
	"errors"
	"fmt"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
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
	if !used {
		return lint.Finding{}, false
	}
	pinned := ""
	if cfg != nil {
		pinned = cfg.EngineVersion
	}
	err := engine.CheckAnnotationSupport(resolveEngineVersionForCompat(pinned))
	if err == nil {
		return lint.Finding{}, false
	}
	configPath := pathutil.ProjectConfigPath(projectRoot)
	line, col := pathutil.FindEngineVersionLine(configPath)
	return lint.Finding{
		RuleID:  "annotation-engine-version",
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
