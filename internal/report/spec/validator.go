package spec

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed schema/document.schema.json
var documentSchema []byte

var (
	schemaOnce sync.Once
	schemaObj  *gojsonschema.Schema
	schemaErr  error
)

// SchemaError represents a structured schema validation error with improved formatting.
type SchemaError struct {
	Field       string
	Description string
	Value       interface{}
	Context     string
}

// SchemaValidationError holds multiple schema errors with helpful formatting.
type SchemaValidationError struct {
	Errors []SchemaError
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
		b.WriteString(fmt.Sprintf("  ✗ %s\n", err.Field))
	} else {
		b.WriteString("  ✗ (document root)\n")
	}

	// Description
	b.WriteString(fmt.Sprintf("    %s\n", err.Description))

	// Show problematic value if available and small
	if err.Value != nil {
		valStr := fmt.Sprintf("%v", err.Value)
		if len(valStr) <= 50 {
			b.WriteString(fmt.Sprintf("    got: %s\n", valStr))
		}
	}

	// Add suggestions for common issues
	if suggestion := getSuggestion(err); suggestion != "" {
		b.WriteString(fmt.Sprintf("    hint: %s\n", suggestion))
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
// bundle schema. The schema is compiled once and cached for subsequent calls.
func ValidateDocument(doc []byte) error {
	schemaOnce.Do(func() {
		loader := gojsonschema.NewBytesLoader(documentSchema)
		schemaObj, schemaErr = gojsonschema.NewSchema(loader)
	})

	if schemaErr != nil {
		return fmt.Errorf("load schema: %w", schemaErr)
	}

	result, err := schemaObj.Validate(gojsonschema.NewBytesLoader(doc))
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if result.Valid() {
		return nil
	}

	// Convert to structured errors
	errors := make([]SchemaError, 0, len(result.Errors()))
	for _, desc := range result.Errors() {
		field := desc.Field()
		if field == "" {
			field = "(root)"
		}

		errors = append(errors, SchemaError{
			Field:       field,
			Description: desc.Description(),
			Value:       desc.Value(),
			Context:     desc.Context().String(),
		})
	}

	// Sort errors by field path for consistent output
	sort.Slice(errors, func(i, j int) bool {
		return errors[i].Field < errors[j].Field
	})

	return &SchemaValidationError{Errors: errors}
}
