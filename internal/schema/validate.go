package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"gopkg.in/yaml.v3"
)

//go:embed jsonschema/document.schema.json
var documentSchema []byte

// DocumentSchemaBytes returns the raw embedded JSON Schema bytes.
func DocumentSchemaBytes() []byte {
	return documentSchema
}

var (
	builtinKindsOnce sync.Once
	builtinKinds     map[string]struct{}
)

// IsBuiltinKind reports whether the kind name is in the embedded schema's kind enum.
// Any other kind of a loaded document comes from a plugin.
func IsBuiltinKind(name string) bool {
	builtinKindsOnce.Do(func() {
		var doc struct {
			Properties struct {
				Kind struct {
					Enum []string `json:"enum"`
				} `json:"kind"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(documentSchema, &doc); err != nil {
			return // the embedded schema is tested to parse
		}
		builtinKinds = make(map[string]struct{}, len(doc.Properties.Kind.Enum))
		for _, k := range doc.Properties.Kind.Enum {
			builtinKinds[k] = struct{}{}
		}
	})
	_, ok := builtinKinds[name]
	return ok
}

var (
	schemaOnce sync.Once
	schemaObj  *jsonschema.Schema
	errSchema  error
)

// msgPrinter renders the validation library's localized (English) error messages.
var msgPrinter = message.NewPrinter(language.English)

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
		fmt.Fprintf(&b, "  - %s: %s", v.Path, v.Message)
	} else {
		fmt.Fprintf(&b, "  - (root): %s", v.Message)
	}

	return b.String()
}

// Validate checks that yamlBytes represents a valid manifest document.
// Returns nil if valid, or a *ValidationError with details.
func Validate(yamlBytes []byte) error {
	jsonBytes, err := yamlToJSON(yamlBytes)
	if err != nil {
		return err
	}
	return ValidateJSON(jsonBytes)
}

// yamlToJSON converts a YAML manifest to JSON for schema validation.
func yamlToJSON(yamlBytes []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(yamlBytes, &doc); err != nil {
		return nil, &ValidationError{
			Errors: []ValidationIssue{
				{Path: "(root)", Message: fmt.Sprintf("invalid YAML: %v", err)},
			},
		}
	}
	jsonBytes, err := json.Marshal(convertYAMLToJSON(doc))
	if err != nil {
		return nil, &ValidationError{
			Errors: []ValidationIssue{
				{Path: "(root)", Message: fmt.Sprintf("failed to convert to JSON: %v", err)},
			},
		}
	}
	return jsonBytes, nil
}

// ValidateJSON validates JSON bytes against the manifest schema.
// This is useful when you already have JSON data.
func ValidateJSON(jsonBytes []byte) error {
	// Initialize schema once
	schemaOnce.Do(func() {
		schemaObj, errSchema = compile(documentSchema)
	})

	if errSchema != nil {
		return fmt.Errorf("load schema: %w", errSchema)
	}

	return validateWith(schemaObj, jsonBytes)
}

// Validator validates documents against a compiled schema other than the
// embedded one, e.g. the schema merged with plugin kinds.
type Validator struct {
	s *jsonschema.Schema
}

// CompileValidator compiles schemaBytes into a Validator.
func CompileValidator(schemaBytes []byte) (*Validator, error) {
	s, err := compile(schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("load schema: %w", err)
	}
	return &Validator{s: s}, nil
}

// ValidateJSON validates JSON bytes against the compiled schema.
func (v *Validator) ValidateJSON(jsonBytes []byte) error {
	return validateWith(v.s, jsonBytes)
}

// Validate is the package-level Validate against the compiled schema.
func (v *Validator) Validate(yamlBytes []byte) error {
	jsonBytes, err := yamlToJSON(yamlBytes)
	if err != nil {
		return err
	}
	return v.ValidateJSON(jsonBytes)
}

func compile(schemaBytes []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat() // assert string formats (e.g. uri), matching prior behavior
	const schemaURL = "https://bino.bi/schemas/report-bundle.json"
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, err
	}
	return c.Compile(schemaURL)
}

func validateWith(s *jsonschema.Schema, jsonBytes []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	verr := s.Validate(inst)
	if verr == nil {
		return nil
	}

	var ve *jsonschema.ValidationError
	if !errors.As(verr, &ve) {
		return fmt.Errorf("validate: %w", verr)
	}

	issues := flattenIssues(ve, inst)

	// Sort errors by field path for consistent output
	sort.SliceStable(issues, func(i, j int) bool {
		return issues[i].Path < issues[j].Path
	})

	return &ValidationError{Errors: issues}
}

// flattenIssues converts a jsonschema validation error tree into a flat list of
// ValidationIssues. Only leaf errors (nodes without nested causes) describe actual
// constraint failures; the structural nodes for if/then/else and allOf/anyOf/oneOf
// composition carry causes and are skipped.
func flattenIssues(ve *jsonschema.ValidationError, inst any) []ValidationIssue {
	var issues []ValidationIssue
	var walk func(n *jsonschema.ValidationError)
	walk = func(n *jsonschema.ValidationError) {
		if len(n.Causes) > 0 {
			for _, c := range n.Causes {
				walk(c)
			}
			return
		}

		path := "(root)"
		if len(n.InstanceLocation) > 0 {
			path = strings.Join(n.InstanceLocation, ".")
		}

		// additionalProperties failures are reported on the parent object and
		// list every disallowed key. Emit one issue per key, pathed at the key
		// itself, so line/column resolution points at the offending property.
		if ap, ok := n.ErrorKind.(*kind.AdditionalProperties); ok {
			for _, prop := range ap.Properties {
				loc := append(append([]string{}, n.InstanceLocation...), prop)
				issues = append(issues, ValidationIssue{
					Path:    strings.Join(loc, "."),
					Message: fmt.Sprintf("additional property '%s' not allowed", prop),
					Value:   valueAt(inst, loc),
				})
			}
			return
		}

		issues = append(issues, ValidationIssue{
			Path:    path,
			Message: n.ErrorKind.LocalizedString(msgPrinter),
			Value:   valueAt(inst, n.InstanceLocation),
		})
	}
	walk(ve)
	return issues
}

// valueAt returns the instance value at the given location, or nil if the path
// cannot be resolved (e.g. a missing required property).
func valueAt(root any, loc []string) any {
	cur := root
	for _, seg := range loc {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[seg]
			if !ok {
				return nil
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(c) {
				return nil
			}
			cur = c[i]
		default:
			return nil
		}
	}
	return cur
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
	validationError := &ValidationError{}
	ok := errors.As(err, &validationError)
	return ok
}

// GetValidationIssues extracts ValidationIssues from an error.
// Returns nil if the error is not a ValidationError.
func GetValidationIssues(err error) []ValidationIssue {
	ve := &ValidationError{}
	if errors.As(err, &ve) {
		return ve.Errors
	}
	return nil
}
