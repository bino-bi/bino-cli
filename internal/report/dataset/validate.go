// Package dataset provides execution and caching for DataSet manifests.
package dataset

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// DataValidationMode determines how data validation errors are handled.
type DataValidationMode string

const (
	// DataValidationFail treats data validation errors as fatal (exit 1).
	DataValidationFail DataValidationMode = "fail"
	// DataValidationWarn logs errors and continues (current lint behavior).
	DataValidationWarn DataValidationMode = "warn"
	// DataValidationOff skips data validation entirely.
	DataValidationOff DataValidationMode = "off"
)

// DefaultDataValidationSampleSize is the default number of rows to validate.
const DefaultDataValidationSampleSize = 10

// DataValidationSampleSizeEnvVar is the environment variable to configure sample size.
const DataValidationSampleSizeEnvVar = "BNR_DATA_VALIDATION_SAMPLE_SIZE"

// GetDataValidationSampleSize returns the configured sample size from environment
// or the default value.
func GetDataValidationSampleSize() int {
	if val := os.Getenv(DataValidationSampleSizeEnvVar); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return DefaultDataValidationSampleSize
}

// DataValidationError represents a single validation issue in a dataset row.
type DataValidationError struct {
	RowIndex int    `json:"rowIndex"` // 0-based row index in the data
	Field    string `json:"field"`    // Field name that has the issue
	Code     string `json:"code"`     // Error code: "invalid-type", "invalid-enum", "missing-dependent-required", "invalid-date-format"
	Message  string `json:"message"`  // Human-readable error message
	Expected string `json:"expected"` // Expected value/type
	Actual   any    `json:"actual"`   // Actual value found
}

func (e DataValidationError) String() string {
	return fmt.Sprintf("row %d, field %q: %s", e.RowIndex, e.Field, e.Message)
}

// DataValidationResult captures the outcome of data validation.
type DataValidationResult struct {
	DataSet    string                `json:"dataset"`    // Dataset name
	TotalRows  int                   `json:"totalRows"`  // Total rows in the dataset
	SampleSize int                   `json:"sampleSize"` // Number of rows validated
	Valid      bool                  `json:"valid"`      // True if no errors found
	Errors     []DataValidationError `json:"errors"`     // List of validation errors
}

// dependentRequiredPairs defines field pairs where one requires the other.
var dependentRequiredPairs = map[string]string{
	"rowGroup":            "rowGroupIndex",
	"rowGroupIndex":       "rowGroup",
	"category":            "categoryIndex",
	"categoryIndex":       "category",
	"subCategory":         "subCategoryIndex",
	"subCategoryIndex":    "subCategory",
	"columnGroup":         "columnGroupIndex",
	"columnGroupIndex":    "columnGroup",
	"columnSubGroup":      "columnSubGroupIndex",
	"columnSubGroupIndex": "columnSubGroup",
}

// stringFields are fields that should be strings.
var stringFields = map[string]bool{
	"setname":        true,
	"date":           true,
	"operation":      true,
	"rowGroup":       true,
	"category":       true,
	"subCategory":    true,
	"columnGroup":    true,
	"columnSubGroup": true,
}

// numberFields are fields that should be numbers.
var numberFields = map[string]bool{
	"rowGroupIndex":       true,
	"categoryIndex":       true,
	"subCategoryIndex":    true,
	"columnGroupIndex":    true,
	"columnSubGroupIndex": true,
	"ac1":                 true, "pp1": true, "fc1": true, "pl1": true,
	"ac2": true, "pp2": true, "fc2": true, "pl2": true,
	"ac3": true, "pp3": true, "fc3": true, "pl3": true,
	"ac4": true, "pp4": true, "fc4": true, "pl4": true,
}

// datePattern matches ISO 8601 dates (YYYY-MM-DD) and datetimes
// (YYYY-MM-DDThh:mm:ss with optional timezone offset or Z).
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})?)?$`)

// ValidateRows validates dataset rows against the dataset schema.
// It samples up to sampleSize rows for efficiency on large datasets.
func ValidateRows(datasetName string, data json.RawMessage, sampleSize int) DataValidationResult {
	result := DataValidationResult{
		DataSet:    datasetName,
		SampleSize: sampleSize,
		Valid:      true,
		Errors:     []DataValidationError{},
	}

	// Parse JSON data as array of objects
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		// If we can't parse as array, treat as single validation error
		result.Valid = false
		result.Errors = append(result.Errors, DataValidationError{
			RowIndex: -1,
			Field:    "",
			Code:     "invalid-json",
			Message:  fmt.Sprintf("Failed to parse data as JSON array: %v", err),
			Expected: "array of objects",
			Actual:   nil,
		})
		return result
	}

	result.TotalRows = len(rows)

	// Determine how many rows to validate
	rowsToValidate := len(rows)
	if sampleSize > 0 && sampleSize < rowsToValidate {
		rowsToValidate = sampleSize
	}
	result.SampleSize = rowsToValidate

	// Validate each row
	for i := 0; i < rowsToValidate; i++ {
		rowErrors := validateRow(i, rows[i])
		if len(rowErrors) > 0 {
			result.Valid = false
			result.Errors = append(result.Errors, rowErrors...)
		}
	}

	return result
}

// validateRow validates a single row against the dataset schema.
func validateRow(rowIndex int, row map[string]any) []DataValidationError {
	var errors []DataValidationError

	for field, value := range row {
		// Skip null values - they're always allowed.
		// CSV sources may represent null as the literal string "null".
		if value == nil {
			continue
		}
		if s, ok := value.(string); ok && s == "null" {
			continue
		}

		// Type validation for string fields
		if stringFields[field] {
			if _, ok := value.(string); !ok {
				errors = append(errors, DataValidationError{
					RowIndex: rowIndex,
					Field:    field,
					Code:     "invalid-type",
					Message:  fmt.Sprintf("Expected string, got %T", value),
					Expected: "string",
					Actual:   value,
				})
				continue
			}
		}

		// Type validation for number fields
		if numberFields[field] {
			if !isNumber(value) {
				errors = append(errors, DataValidationError{
					RowIndex: rowIndex,
					Field:    field,
					Code:     "invalid-type",
					Message:  fmt.Sprintf("Expected number, got %T", value),
					Expected: "number",
					Actual:   value,
				})
				continue
			}
		}

		// Enum validation for operation field
		if field == "operation" {
			if str, ok := value.(string); ok {
				if str != "+" && str != "-" {
					errors = append(errors, DataValidationError{
						RowIndex: rowIndex,
						Field:    field,
						Code:     "invalid-enum",
						Message:  fmt.Sprintf("Invalid value %q. Expected '+' or '-'", str),
						Expected: "'+' or '-'",
						Actual:   str,
					})
				}
			}
		}

		// Date format validation
		if field == "date" {
			if str, ok := value.(string); ok {
				if !datePattern.MatchString(str) {
					errors = append(errors, DataValidationError{
						RowIndex: rowIndex,
						Field:    field,
						Code:     "invalid-date-format",
						Message:  fmt.Sprintf("Invalid date format %q. Expected ISO 8601 date or datetime", str),
						Expected: "YYYY-MM-DD or YYYY-MM-DDThh:mm:ss with optional timezone",
						Actual:   str,
					})
				}
			}
		}
	}

	// Check dependent required fields
	for field, required := range dependentRequiredPairs {
		if hasField(row, field) && !hasField(row, required) {
			errors = append(errors, DataValidationError{
				RowIndex: rowIndex,
				Field:    required,
				Code:     "missing-dependent-required",
				Message:  fmt.Sprintf("Field %q requires %q to also be present", field, required),
				Expected: fmt.Sprintf("%q when %q is set", required, field),
				Actual:   nil,
			})
		}
	}

	return errors
}

// isNumber checks if a value is a numeric type or a string that represents a number.
// Strings with comma decimal separators (e.g., "936,6667") are accepted since
// CSV data loaded through DuckDB may preserve locale-specific number formatting.
func isNumber(v any) bool {
	switch v := v.(type) {
	case float64, float32, int, int32, int64, uint, uint32, uint64, json.Number:
		return true
	case string:
		s := v
		// Try parsing directly (handles "123", "1.5", "-3.14")
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			return true
		}
		// Try with comma decimal separator replaced (handles "936,6667")
		if strings.ContainsRune(s, ',') {
			normalized := strings.Replace(s, ",", ".", 1)
			if _, err := strconv.ParseFloat(normalized, 64); err == nil {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// hasField checks if a row has a non-nil field.
// The string "null" (from CSV sources) is treated as absent.
func hasField(row map[string]any, field string) bool {
	val, ok := row[field]
	if !ok || val == nil {
		return false
	}
	if s, ok := val.(string); ok && s == "null" {
		return false
	}
	return true
}

// DataValidationResultToWarnings converts validation results to Warning structs.
func DataValidationResultToWarnings(result DataValidationResult) []Warning {
	if result.Valid {
		return nil
	}

	var warnings []Warning

	// Create a summary warning
	summary := fmt.Sprintf("data validation: %d error(s) in %d/%d rows sampled",
		len(result.Errors), result.SampleSize, result.TotalRows)
	warnings = append(warnings, Warning{
		DataSet: result.DataSet,
		Message: summary,
	})

	// Add individual error details (limit to first 5 for readability)
	limit := 5
	for i, err := range result.Errors {
		if i >= limit {
			warnings = append(warnings, Warning{
				DataSet: result.DataSet,
				Message: fmt.Sprintf("... and %d more validation error(s)", len(result.Errors)-limit),
			})
			break
		}
		warnings = append(warnings, Warning{
			DataSet: result.DataSet,
			Message: fmt.Sprintf("  %s", err.String()),
		})
	}

	return warnings
}

// FormatValidationErrors formats validation errors for human-readable output.
func FormatValidationErrors(result DataValidationResult) string {
	if result.Valid {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Dataset %q: %d validation error(s) in %d/%d rows sampled:\n",
		result.DataSet, len(result.Errors), result.SampleSize, result.TotalRows)

	for _, err := range result.Errors {
		fmt.Fprintf(&sb, "  - %s\n", err.String())
	}

	return sb.String()
}
