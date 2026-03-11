package schema

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

//go:embed jsonschema/document.schema.json
var documentSchema []byte

var (
	schemaOnce sync.Once
	schemaObj  *gojsonschema.Schema
	schemaErr  error
)

// ValidationError contains structured validation failure information.
type ValidationError struct {
	Errors []ValidationIssue
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "validation failed"
	}

	var b strings.Builder
	b.WriteString("validation failed:\n")

	for i, issue := range e.Errors {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(issue.Format())
	}

	return strings.TrimSpace(b.String())
}

// ValidationIssue represents a single validation error with location.
type ValidationIssue struct {
	Path    string // JSON path like "spec.query"
	Message string // Human-readable error
	Value   any    // The problematic value (if available)
}

// Format returns a formatted string for this issue.
func (v ValidationIssue) Format() string {
	var b strings.Builder

	// Field path with visual indicator
	if v.Path != "" && v.Path != "(root)" {
		b.WriteString(fmt.Sprintf("  - %s: %s", v.Path, v.Message))
	} else {
		b.WriteString(fmt.Sprintf("  - (root): %s", v.Message))
	}

	return b.String()
}

// Validate checks that yamlBytes represents a valid manifest document.
// Returns nil if valid, or a *ValidationError with details.
func Validate(yamlBytes []byte) error {
	// Parse YAML to generic structure
	var doc any
	if err := yaml.Unmarshal(yamlBytes, &doc); err != nil {
		return &ValidationError{
			Errors: []ValidationIssue{
				{Path: "(root)", Message: fmt.Sprintf("invalid YAML: %v", err)},
			},
		}
	}

	// Convert to JSON for schema validation
	jsonBytes, err := json.Marshal(convertYAMLToJSON(doc))
	if err != nil {
		return &ValidationError{
			Errors: []ValidationIssue{
				{Path: "(root)", Message: fmt.Sprintf("failed to convert to JSON: %v", err)},
			},
		}
	}

	return ValidateJSON(jsonBytes)
}

// ValidateJSON validates JSON bytes against the manifest schema.
// This is useful when you already have JSON data.
func ValidateJSON(jsonBytes []byte) error {
	// Initialize schema once
	schemaOnce.Do(func() {
		loader := gojsonschema.NewBytesLoader(documentSchema)
		schemaObj, schemaErr = gojsonschema.NewSchema(loader)
	})

	if schemaErr != nil {
		return fmt.Errorf("load schema: %w", schemaErr)
	}

	// Validate
	result, err := schemaObj.Validate(gojsonschema.NewBytesLoader(jsonBytes))
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if result.Valid() {
		return nil
	}

	// Convert to structured errors, filtering out noisy meta-validator errors
	// from gojsonschema's if/then/else and allOf/anyOf/oneOf composition.
	issues := make([]ValidationIssue, 0, len(result.Errors()))
	for _, desc := range result.Errors() {
		switch desc.Type() {
		case "condition_then", "condition_else",
			"number_all_of", "number_any_of", "number_one_of":
			continue
		}

		field := desc.Field()
		if field == "" {
			field = "(root)"
		}

		issues = append(issues, ValidationIssue{
			Path:    field,
			Message: desc.Description(),
			Value:   desc.Value(),
		})
	}

	// Sort errors by field path for consistent output
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Path < issues[j].Path
	})

	return &ValidationError{Errors: issues}
}

// convertYAMLToJSON converts YAML-parsed data structures to JSON-compatible types.
// This handles the map[string]any vs map[any]any difference between YAML and JSON.
func convertYAMLToJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = convertYAMLToJSON(v)
		}
		return result
	case map[any]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[fmt.Sprintf("%v", k)] = convertYAMLToJSON(v)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = convertYAMLToJSON(v)
		}
		return result
	case time.Time:
		// YAML auto-parses bare dates (e.g. 2025-01-31) as time.Time with midnight UTC.
		// Normalize to date-only string so the schema validator sees "2025-01-31"
		// instead of "2025-01-31T00:00:00Z". Preserve real datetime values.
		if val.Hour() == 0 && val.Minute() == 0 && val.Second() == 0 && val.Nanosecond() == 0 {
			return val.Format("2006-01-02")
		}
		return val.Format(time.RFC3339)
	default:
		return v
	}
}

// IsValidationError checks if an error is a ValidationError.
func IsValidationError(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

// GetValidationIssues extracts ValidationIssues from an error.
// Returns nil if the error is not a ValidationError.
func GetValidationIssues(err error) []ValidationIssue {
	if ve, ok := err.(*ValidationError); ok {
		return ve.Errors
	}
	return nil
}
