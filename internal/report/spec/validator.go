package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"bino.bi/bino/internal/schema"
)

// SchemaError represents a structured schema validation error with improved formatting.
type SchemaError struct {
	Field       string
	Description string
	Value       interface{}
	Context     string
	Line        int // 1-based line number in source YAML (0 = unknown)
	Column      int // 1-based column number in source YAML (0 = unknown)
}

// SchemaValidationError holds multiple schema errors with helpful formatting.
type SchemaValidationError struct {
	Errors      []SchemaError
	File        string // source file path
	DocPosition int    // 1-based document index in multi-doc file
	Source      string // original YAML content (for snippet display)
}

func (e *SchemaValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "schema validation failed"
	}

	var b strings.Builder
	b.WriteString("schema validation failed")
	if e.File != "" {
		fmt.Fprintf(&b, " in %s", e.File)
		if e.DocPosition > 0 {
			fmt.Fprintf(&b, " (document #%d)", e.DocPosition)
		}
	}
	b.WriteString(":\n")

	// Group errors by field path prefix for better readability
	for i, err := range e.Errors {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(formatSchemaError(err))
		if err.Line > 0 && e.Source != "" {
			if snippet := ExtractSourceSnippet(e.Source, err.Line, 2); snippet != "" {
				b.WriteString("\n")
				b.WriteString(snippet)
			}
		}
	}

	return strings.TrimSpace(b.String())
}

// formatSchemaError formats a single schema error with context and suggestions.
func formatSchemaError(err SchemaError) string {
	var b strings.Builder

	// Field path with visual indicator
	if err.Field != "" && err.Field != "(root)" {
		fmt.Fprintf(&b, "  ✗ %s", err.Field)
	} else {
		b.WriteString("  ✗ (document root)")
	}
	if err.Line > 0 {
		fmt.Fprintf(&b, " (line %d, col %d)", err.Line, err.Column)
	}
	b.WriteString("\n")

	// Description
	fmt.Fprintf(&b, "    %s\n", err.Description)

	// Show problematic value if available and small
	if err.Value != nil {
		valStr := fmt.Sprintf("%v", err.Value)
		if len(valStr) <= 50 {
			fmt.Fprintf(&b, "    got: %s\n", valStr)
		}
	}

	// Add suggestions for common issues
	if suggestion := getSuggestion(err); suggestion != "" {
		fmt.Fprintf(&b, "    hint: %s\n", suggestion)
	}

	return b.String()
}

// Hint returns the guidance line for a schema error. It is the same text the
// CLI formatter appends as "hint:" — exported so the structured diagnostic
// path (daemon → LSP/MCP) can carry it as a field.
func Hint(err SchemaError) string { return getSuggestion(err) }

// getSuggestion returns a helpful hint for common schema errors.
func getSuggestion(err SchemaError) string {
	desc := strings.ToLower(err.Description)
	field := strings.ToLower(err.Field)

	// A key typed with no value yet ("kind: " → null; see friendlyDescription).
	if strings.Contains(desc, "no value provided") {
		switch {
		case strings.Contains(desc, "expected string"):
			return "Add a value after the colon (quote it to force a string)"
		case strings.Contains(desc, "expected array"):
			return "Add a YAML list under this key: one '- item' per line"
		case strings.Contains(desc, "expected object"):
			return "Add an indented block of 'key: value' pairs under this key"
		case strings.Contains(desc, "expected number"), strings.Contains(desc, "expected integer"):
			return "Add a numeric value after the colon"
		}
		return "Add a value after the colon"
	}

	// Missing required field
	if strings.Contains(desc, "missing propert") {
		if strings.Contains(field, "kind") {
			return "Add 'kind: <DocumentType>' to specify the document type"
		}
		if strings.Contains(field, "metadata") {
			return "Add 'metadata:' section with required fields"
		}
		if strings.Contains(field, "name") {
			return "Add 'name: <unique-identifier>' under metadata"
		}
		return "This field is required by the schema"
	}

	// Invalid enum value
	if strings.Contains(desc, "must be one of") {
		return "Check the allowed values in the schema documentation"
	}

	// Type mismatch
	if strings.Contains(desc, ", want ") {
		if strings.Contains(desc, "want string") {
			return "Wrap the value in quotes to make it a string"
		}
		if strings.Contains(desc, "want array") {
			return "Use YAML list syntax with '- ' prefix for each item"
		}
		if strings.Contains(desc, "want object") {
			return "Use YAML mapping syntax with 'key: value' pairs"
		}
	}

	// Additional properties
	if strings.Contains(desc, "additional propert") {
		// Extract property name from description
		re := regexp.MustCompile(`'([^']+)'`)
		if matches := re.FindStringSubmatch(err.Description); len(matches) > 1 {
			return fmt.Sprintf("'%s' is not a valid field; check for typos", matches[1])
		}
		return "Remove unknown fields or check for typos in field names"
	}

	return ""
}

// friendlyDescription rewrites the one raw jsonschema message that reads as
// gibberish while typing: a key with no value yet unmarshals to null, and
// "got null, want string" says nothing about the fix. Every other message
// passes through verbatim — "missing property 'x'" in particular is parsed
// downstream by the editor's quick-fix pipeline.
func friendlyDescription(msg string) string {
	if rest, ok := strings.CutPrefix(msg, "got null, want "); ok {
		return "no value provided (expected " + rest + ")"
	}
	return msg
}

// ValidateDocument verifies that the provided JSON manifest matches the report
// bundle schema. Delegates to schema.ValidateJSON for actual validation.
func ValidateDocument(doc []byte) error {
	err := schema.ValidateJSON(doc)
	if err == nil {
		return nil
	}

	// Convert schema.ValidationError to spec.SchemaValidationError for
	// backwards compatibility and enhanced error formatting with suggestions.
	issues := schema.GetValidationIssues(err)
	if issues == nil {
		// Not a validation error, return as-is
		return err
	}

	errors := make([]SchemaError, 0, len(issues))
	for _, issue := range issues {
		errors = append(errors, SchemaError{
			Field:       issue.Path,
			Description: friendlyDescription(issue.Message),
			Value:       issue.Value,
			Context:     issue.Path,
		})
	}

	// Sort errors by field path for consistent output
	sort.Slice(errors, func(i, j int) bool {
		return errors[i].Field < errors[j].Field
	})

	return &SchemaValidationError{Errors: errors}
}
