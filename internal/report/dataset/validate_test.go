package dataset

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestValidateRows_ValidData(t *testing.T) {
	data := json.RawMessage(`[
		{"category": "Revenue", "categoryIndex": 1, "ac1": 100.5},
		{"category": "Costs", "categoryIndex": 2, "operation": "-", "ac1": 50}
	]`)

	result := ValidateRows("test-dataset", data, 10)

	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
	if result.TotalRows != 2 {
		t.Errorf("expected TotalRows=2, got %d", result.TotalRows)
	}
	if result.SampleSize != 2 {
		t.Errorf("expected SampleSize=2, got %d", result.SampleSize)
	}
}

func TestValidateRows_InvalidType_String(t *testing.T) {
	data := json.RawMessage(`[
		{"category": 123, "categoryIndex": 1}
	]`)

	result := ValidateRows("test-dataset", data, 10)

	if result.Valid {
		t.Error("expected invalid result for wrong type")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "invalid-type" {
		t.Errorf("expected code 'invalid-type', got %q", result.Errors[0].Code)
	}
	if result.Errors[0].Field != "category" {
		t.Errorf("expected field 'category', got %q", result.Errors[0].Field)
	}
}

func TestValidateRows_InvalidType_Number(t *testing.T) {
	data := json.RawMessage(`[
		{"category": "Revenue", "categoryIndex": "one", "ac1": "not-a-number"}
	]`)

	result := ValidateRows("test-dataset", data, 10)

	if result.Valid {
		t.Error("expected invalid result for wrong type")
	}
	// Expect 2 errors: categoryIndex and ac1 both wrong type
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(result.Errors), result.Errors)
	}
	for _, err := range result.Errors {
		if err.Code != "invalid-type" {
			t.Errorf("expected code 'invalid-type', got %q", err.Code)
		}
	}
}

func TestValidateRows_InvalidEnum_Operation(t *testing.T) {
	data := json.RawMessage(`[
		{"category": "Revenue", "categoryIndex": 1, "operation": "x"}
	]`)

	result := ValidateRows("test-dataset", data, 10)

	if result.Valid {
		t.Error("expected invalid result for wrong enum")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "invalid-enum" {
		t.Errorf("expected code 'invalid-enum', got %q", result.Errors[0].Code)
	}
	if result.Errors[0].Field != "operation" {
		t.Errorf("expected field 'operation', got %q", result.Errors[0].Field)
	}
}

func TestValidateRows_ValidEnum_Operation(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
	}{
		{"plus", json.RawMessage(`[{"operation": "+"}]`)},
		{"minus", json.RawMessage(`[{"operation": "-"}]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateRows("test-dataset", tt.data, 10)
			if !result.Valid {
				t.Errorf("expected valid result, got errors: %v", result.Errors)
			}
		})
	}
}

func TestValidateRows_InvalidDateFormat(t *testing.T) {
	tests := []struct {
		name string
		date string
	}{
		{"wrong-format", "01/15/2024"},
		{"missing-dashes", "20240115"},
		{"short-year", "24-01-15"},
		{"text", "January 15, 2024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := json.RawMessage(`[{"date": "` + tt.date + `"}]`)
			result := ValidateRows("test-dataset", data, 10)

			if result.Valid {
				t.Error("expected invalid result for wrong date format")
			}
			if len(result.Errors) != 1 {
				t.Fatalf("expected 1 error, got %d", len(result.Errors))
			}
			if result.Errors[0].Code != "invalid-date-format" {
				t.Errorf("expected code 'invalid-date-format', got %q", result.Errors[0].Code)
			}
		})
	}
}

func TestValidateRows_ValidDateFormat(t *testing.T) {
	data := json.RawMessage(`[{"date": "2024-01-15"}]`)

	result := ValidateRows("test-dataset", data, 10)

	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
}

func TestValidateRows_MissingDependentRequired(t *testing.T) {
	tests := []struct {
		name        string
		data        json.RawMessage
		missingField string
	}{
		{"rowGroup-without-index", json.RawMessage(`[{"rowGroup": "Revenue"}]`), "rowGroupIndex"},
		{"rowGroupIndex-without-rowGroup", json.RawMessage(`[{"rowGroupIndex": 1}]`), "rowGroup"},
		{"category-without-index", json.RawMessage(`[{"category": "Sales"}]`), "categoryIndex"},
		{"categoryIndex-without-category", json.RawMessage(`[{"categoryIndex": 1}]`), "category"},
		{"subCategory-without-index", json.RawMessage(`[{"subCategory": "Product A"}]`), "subCategoryIndex"},
		{"columnGroup-without-index", json.RawMessage(`[{"columnGroup": "Region"}]`), "columnGroupIndex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateRows("test-dataset", tt.data, 10)

			if result.Valid {
				t.Error("expected invalid result for missing dependent required")
			}
			if len(result.Errors) != 1 {
				t.Fatalf("expected 1 error, got %d: %v", len(result.Errors), result.Errors)
			}
			if result.Errors[0].Code != "missing-dependent-required" {
				t.Errorf("expected code 'missing-dependent-required', got %q", result.Errors[0].Code)
			}
			if result.Errors[0].Field != tt.missingField {
				t.Errorf("expected field %q, got %q", tt.missingField, result.Errors[0].Field)
			}
		})
	}
}

func TestValidateRows_DependentRequiredBothPresent(t *testing.T) {
	data := json.RawMessage(`[
		{"rowGroup": "Revenue", "rowGroupIndex": 1},
		{"category": "Sales", "categoryIndex": 1},
		{"subCategory": "Product A", "subCategoryIndex": 1},
		{"columnGroup": "Region", "columnGroupIndex": 1}
	]`)

	result := ValidateRows("test-dataset", data, 10)

	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
}

func TestValidateRows_NullValues(t *testing.T) {
	data := json.RawMessage(`[
		{"category": null, "categoryIndex": null, "ac1": null}
	]`)

	result := ValidateRows("test-dataset", data, 10)

	if !result.Valid {
		t.Errorf("expected valid result (nulls allowed), got errors: %v", result.Errors)
	}
}

func TestValidateRows_SampleSize(t *testing.T) {
	// Create data with 20 rows, only sample first 5
	rows := make([]map[string]any, 20)
	for i := 0; i < 20; i++ {
		rows[i] = map[string]any{"ac1": float64(i)}
	}
	data, _ := json.Marshal(rows)

	result := ValidateRows("test-dataset", data, 5)

	if result.TotalRows != 20 {
		t.Errorf("expected TotalRows=20, got %d", result.TotalRows)
	}
	if result.SampleSize != 5 {
		t.Errorf("expected SampleSize=5, got %d", result.SampleSize)
	}
}

func TestValidateRows_SampleSizeLargerThanData(t *testing.T) {
	data := json.RawMessage(`[{"ac1": 100}]`)

	result := ValidateRows("test-dataset", data, 100)

	if result.TotalRows != 1 {
		t.Errorf("expected TotalRows=1, got %d", result.TotalRows)
	}
	if result.SampleSize != 1 {
		t.Errorf("expected SampleSize=1 (limited to actual rows), got %d", result.SampleSize)
	}
}

func TestValidateRows_InvalidJSON(t *testing.T) {
	data := json.RawMessage(`not valid json`)

	result := ValidateRows("test-dataset", data, 10)

	if result.Valid {
		t.Error("expected invalid result for invalid JSON")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "invalid-json" {
		t.Errorf("expected code 'invalid-json', got %q", result.Errors[0].Code)
	}
}

func TestValidateRows_EmptyArray(t *testing.T) {
	data := json.RawMessage(`[]`)

	result := ValidateRows("test-dataset", data, 10)

	if !result.Valid {
		t.Errorf("expected valid result for empty array, got errors: %v", result.Errors)
	}
	if result.TotalRows != 0 {
		t.Errorf("expected TotalRows=0, got %d", result.TotalRows)
	}
}

func TestValidateRows_ComplexRow(t *testing.T) {
	data := json.RawMessage(`[{
		"setname": "quarterly",
		"date": "2024-01-15",
		"operation": "+",
		"rowGroup": "Revenue",
		"rowGroupIndex": 1,
		"category": "Product Sales",
		"categoryIndex": 1,
		"subCategory": "Widget A",
		"subCategoryIndex": 1,
		"columnGroup": "Region",
		"columnGroupIndex": 1,
		"ac1": 10000.50,
		"pp1": 9500.25,
		"fc1": 10500.00,
		"pl1": 10200.00
	}]`)

	result := ValidateRows("test-dataset", data, 10)

	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
}

func TestDataValidationResultToWarnings(t *testing.T) {
	result := DataValidationResult{
		DataSet:    "test-dataset",
		TotalRows:  100,
		SampleSize: 10,
		Valid:      false,
		Errors: []DataValidationError{
			{RowIndex: 0, Field: "operation", Code: "invalid-enum", Message: "Invalid value"},
			{RowIndex: 1, Field: "date", Code: "invalid-date-format", Message: "Invalid format"},
		},
	}

	warnings := DataValidationResultToWarnings(result)

	if len(warnings) == 0 {
		t.Fatal("expected warnings, got none")
	}
	// Should have summary + 2 error details
	if len(warnings) != 3 {
		t.Errorf("expected 3 warnings (1 summary + 2 errors), got %d", len(warnings))
	}
}

func TestDataValidationResultToWarnings_Valid(t *testing.T) {
	result := DataValidationResult{
		DataSet:    "test-dataset",
		TotalRows:  10,
		SampleSize: 10,
		Valid:      true,
		Errors:     []DataValidationError{},
	}

	warnings := DataValidationResultToWarnings(result)

	if warnings != nil {
		t.Errorf("expected no warnings for valid result, got %v", warnings)
	}
}

func TestGetDataValidationSampleSize(t *testing.T) {
	// Test default value
	os.Unsetenv(DataValidationSampleSizeEnvVar)
	if got := GetDataValidationSampleSize(); got != DefaultDataValidationSampleSize {
		t.Errorf("expected default %d, got %d", DefaultDataValidationSampleSize, got)
	}

	// Test custom value
	os.Setenv(DataValidationSampleSizeEnvVar, "50")
	if got := GetDataValidationSampleSize(); got != 50 {
		t.Errorf("expected 50, got %d", got)
	}

	// Test invalid value falls back to default
	os.Setenv(DataValidationSampleSizeEnvVar, "invalid")
	if got := GetDataValidationSampleSize(); got != DefaultDataValidationSampleSize {
		t.Errorf("expected default %d for invalid env, got %d", DefaultDataValidationSampleSize, got)
	}

	// Cleanup
	os.Unsetenv(DataValidationSampleSizeEnvVar)
}

func TestFormatValidationErrors(t *testing.T) {
	result := DataValidationResult{
		DataSet:    "test-dataset",
		TotalRows:  100,
		SampleSize: 10,
		Valid:      false,
		Errors: []DataValidationError{
			{RowIndex: 0, Field: "operation", Code: "invalid-enum", Message: "Invalid value 'x'. Expected '+' or '-'"},
		},
	}

	output := FormatValidationErrors(result)

	if output == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(output, "test-dataset") {
		t.Error("expected output to contain dataset name")
	}
	if !strings.Contains(output, "operation") {
		t.Error("expected output to contain field name")
	}
}

func TestFormatValidationErrors_Valid(t *testing.T) {
	result := DataValidationResult{
		DataSet: "test-dataset",
		Valid:   true,
	}

	output := FormatValidationErrors(result)

	if output != "" {
		t.Errorf("expected empty output for valid result, got %q", output)
	}
}
