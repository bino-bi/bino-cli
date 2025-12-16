// Package lint provides a rule-based content linter for bino report documents.
//
// The linter runs after schema validation (which checks YAML structure) and
// validates semantic/content aspects of report configurations. All lint findings
// are currently treated as warnings.
//
// # Adding New Rules
//
// To add a new lint rule:
//  1. Create a Rule with a unique ID, name, and description.
//  2. Implement the Check function that inspects documents and returns findings.
//  3. Add the rule to DefaultRules() in rules.go.
//
// Example:
//
//	var myRule = Rule{
//	    ID:          "my-rule-id",
//	    Name:        "My Rule Name",
//	    Description: "Explains what this rule checks.",
//	    Check: func(ctx context.Context, docs []Document) []Finding {
//	        var findings []Finding
//	        // inspect docs and append findings...
//	        return findings
//	    },
//	}
package lint

import (
	"context"
	"encoding/json"
)

// Document represents a loaded bino document for linting.
// It mirrors the essential fields from config.Document.
type Document struct {
	File        string            // Absolute path to the YAML file.
	Position    int               // 1-based index within multi-doc YAML.
	Kind        string            // Document kind (e.g., "ReportArtefact", "Dataset").
	Name        string            // metadata.name value.
	Labels      map[string]string // metadata.labels for constraint evaluation.
	Constraints []string          // metadata.constraints for conditional inclusion.
	Raw         json.RawMessage   // Validated JSON payload.
}

// Finding represents a single lint warning.
type Finding struct {
	RuleID  string // ID of the rule that produced this finding.
	Message string // Human-readable description of the issue.
	File    string // Absolute path to the file (always set).
	DocIdx  int    // 1-based document index within the file (0 if unknown).
	Path    string // Optional JSON path within the document (e.g., "datasets[0].name").
	Line    int    // 1-based line number (0 if unknown).
	Column  int    // 1-based column number (0 if unknown).
}

// Rule defines a single lint check.
type Rule struct {
	ID          string                                               // Unique identifier (e.g., "missing-description").
	Name        string                                               // Short human-readable name.
	Description string                                               // Longer explanation of what the rule checks.
	Check       func(ctx context.Context, docs []Document) []Finding // The check function.
}

// Runner executes lint rules against a set of documents.
type Runner struct {
	rules []Rule
}

// NewRunner creates a Runner with the given rules.
func NewRunner(rules []Rule) *Runner {
	return &Runner{rules: rules}
}

// NewDefaultRunner creates a Runner with the default set of rules.
func NewDefaultRunner() *Runner {
	return NewRunner(DefaultRules())
}

// Run executes all rules against the provided documents and returns all findings.
// Findings are returned in rule order, then in the order each rule produces them.
func (r *Runner) Run(ctx context.Context, docs []Document) []Finding {
	var all []Finding
	for _, rule := range r.rules {
		if rule.Check == nil {
			continue
		}
		findings := rule.Check(ctx, docs)
		all = append(all, findings...)
	}
	return all
}

// Rules returns the list of rules configured in this runner.
func (r *Runner) Rules() []Rule {
	return r.rules
}
