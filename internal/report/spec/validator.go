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
	b.WriteString("schema validation failed:\n")

	// Group errors by field path prefix for better readability
	for i, err := range e.Errors {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(formatSchemaError(err))
	}

	return strings.TrimSpace(b.String())
}

// formatSchemaError formats a single schema error with context and suggestions.
func formatSchemaError(err SchemaError) string {
	var b strings.Builder

	// Field path with visual indicator
	if err.Field != "" && err.Field != "(root)" {
		fmt.Fprintf(&b, "  ✗ %s\n", err.Field)
	} else {
		b.WriteString("  ✗ (document root)\n")
	}

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

// getSuggestion returns a helpful hint for common schema errors.
func getSuggestion(err SchemaError) string {
	desc := strings.ToLower(err.Description)
	field := strings.ToLower(err.Field)

	// Missing required field
	if strings.Contains(desc, "is required") {
		if strings.Contains(field, "kind") {
			return "Add 'kind: <DocumentType>' to specify the document type"
		}
		if strings.Contains(field, "metadata") {
			return "Add 'metadata:' section with required fields"
		}
		if strings.Contains(field, "name") {
			return "Add 'name: <unique-identifier>' under metadata"
		}
	}

	// Invalid enum value
	if strings.Contains(desc, "must be one of") {
		return "Check the allowed values in the schema documentation"
	}

	// Type mismatch
	if strings.Contains(desc, "invalid type") {
		if strings.Contains(desc, "expected string") {
			return "Wrap the value in quotes to make it a string"
		}
		if strings.Contains(desc, "expected array") {
			return "Use YAML list syntax with '- ' prefix for each item"
		}
		if strings.Contains(desc, "expected object") {
			return "Use YAML mapping syntax with 'key: value' pairs"
		}
	}

	// Additional properties
	if strings.Contains(desc, "additional property") {
		// Extract property name from description
		re := regexp.MustCompile(`"([^"]+)"`)
		if matches := re.FindStringSubmatch(err.Description); len(matches) > 1 {
			return fmt.Sprintf("'%s' is not a valid field; check for typos", matches[1])
		}
		return "Remove unknown fields or check for typos in field names"
	}

	return ""
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
			Description: issue.Message,
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
