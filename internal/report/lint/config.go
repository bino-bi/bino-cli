package lint

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"bino.bi/bino/internal/pathutil"
)

// nonRuleFindingIDs are rule IDs that findings carry although no Rule declares
// them: one emitted from inside another rule, and three synthesized by the
// lint command. They are known IDs for [lint] purposes.
var nonRuleFindingIDs = []string{
	"missing-required-reference",  // emitted by page-layout-slots-used
	"schema-validation",           // cli/lint.go schemaFindingsFromErrors
	"manifest-load",               // cli/lint.go schemaFindingsFromErrors
	"engine-version-incompatible", // cli/engine_compat.go
}

// fixedSeverityIDs are known finding IDs whose weight is decided by the state
// of the bundle, not by a severity: a manifest that will not load and an
// incompatible engine pin are fatal because nothing downstream can run. A
// [lint.severity] entry on them could only lie about the exit code, so it is
// rejected instead of silently doing nothing. disable still applies — it
// suppresses the report, never the fatal condition behind it.
var fixedSeverityIDs = []string{
	"schema-validation",
	"manifest-load",
	"engine-version-incompatible",
}

// loadLintConfig reads the project's [lint] table into the runner. A nil cfg
// (missing or unreadable bino.toml) leaves the runner unconfigured, so a
// project without [lint] keeps today's behavior. Entries that name no known
// rule, severity values outside error/warning/info, and severities on the IDs
// that cannot honor one are ignored and reported through ConfigWarnings
// instead of being silently dropped.
func (r *Runner) loadLintConfig(cfg *pathutil.ProjectConfig) {
	if cfg == nil {
		return
	}
	for _, id := range cfg.Lint.Disable {
		if !r.knownRuleID(id) {
			r.warnings = append(r.warnings, fmt.Sprintf("unknown rule id %q in [lint] disable", id))
			continue
		}
		if r.disabled == nil {
			r.disabled = make(map[string]struct{}, len(cfg.Lint.Disable))
		}
		r.disabled[id] = struct{}{}
	}

	ids := make([]string, 0, len(cfg.Lint.Severity))
	for id := range cfg.Lint.Severity {
		ids = append(ids, id)
	}
	sort.Strings(ids) // map order would make the warnings non-deterministic
	for _, id := range ids {
		sev := cfg.Lint.Severity[id]
		switch {
		case !r.knownRuleID(id):
			r.warnings = append(r.warnings, fmt.Sprintf("unknown rule id %q in [lint] severity", id))
		case slices.Contains(fixedSeverityIDs, id):
			r.warnings = append(r.warnings, fmt.Sprintf(
				"severity is not configurable for %q; use [lint] disable to silence it", id))
		case sev != "error" && sev != "warning" && sev != "info":
			r.warnings = append(r.warnings, fmt.Sprintf(
				"invalid severity %q for rule id %q in [lint] severity (want error, warning or info)", sev, id))
		default:
			if r.severity == nil {
				r.severity = make(map[string]string, len(ids))
			}
			r.severity[id] = sev
		}
	}
}

// knownRuleID reports whether findings with this ID can occur in this project.
// A plugin rule ID ("<plugin>/<rule>") is accepted unchecked: the plugin's rule
// set is only knowable after the plugin has run.
func (r *Runner) knownRuleID(id string) bool {
	if strings.Contains(id, "/") {
		return true
	}
	for _, rule := range r.rules {
		if rule.ID == id {
			return true
		}
	}
	for _, extra := range nonRuleFindingIDs {
		if extra == id {
			return true
		}
	}
	return false
}

// Apply enforces the project's [lint] table on a finished set of findings:
// disabled rules are dropped, overridden rules get their new severity. Callers
// apply it once, to the union of rule findings and plugin findings, so every
// surface reports the same set. An unconfigured Runner returns the input.
func (r *Runner) Apply(findings []Finding) []Finding {
	if len(r.disabled) == 0 && len(r.severity) == 0 {
		return findings
	}
	kept := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if _, ok := r.disabled[f.RuleID]; ok {
			continue
		}
		if sev, ok := r.severity[f.RuleID]; ok {
			f.Severity = sev
		}
		kept = append(kept, f)
	}
	return kept
}

// Skip reports whether [lint] disable silences this rule ID. It exists for the
// conditions that are decided before a Finding is built.
func (r *Runner) Skip(ruleID string) bool {
	_, ok := r.disabled[ruleID]
	return ok
}

// SeverityOverride returns the severity the project set for this rule ID in
// [lint] severity, or "" when it set none. Exit-code policy keys on the
// override, never on the finding's own severity, so a rule that has always
// emitted "error" keeps its advisory meaning.
func (r *Runner) SeverityOverride(ruleID string) string {
	return r.severity[ruleID]
}

// ConfigWarnings returns the problems found in the project's [lint] table, in
// a stable order. The lint command prints them; nothing else consults them.
func (r *Runner) ConfigWarnings() []string {
	return r.warnings
}
