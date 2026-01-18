package schema

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidate_ValidDocument(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "valid DataSet with inline query",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
spec:
  query: SELECT 1
`,
		},
		{
			name: "valid DataSource CSV",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: test_source
spec:
  type: csv
  path: data/test.csv
`,
		},
		{
			name: "valid ReportArtefact",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: test_report
spec:
  filename: report.pdf
  title: Test Report
  format: pdf
  orientation: portrait
`,
		},
		{
			name: "valid DataSet with constraints",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
  constraints:
    - labels.env == production
spec:
  query: SELECT 1
`,
		},
		{
			name: "valid DataSet with description",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test_dataset
  description: A test dataset
spec:
  query: SELECT * FROM users
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestValidate_MissingRequiredField(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantPath    string
		wantMessage string
	}{
		{
			name: "missing kind",
			yaml: `
apiVersion: bino.bi/v1alpha1
metadata:
  name: test
spec:
  query: SELECT 1
`,
			wantPath:    "(root)",
			wantMessage: "kind is required",
		},
		{
			name: "missing metadata.name",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  description: test
spec:
  query: SELECT 1
`,
			wantPath:    "metadata",
			wantMessage: "name is required",
		},
		{
			name: "missing spec",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test
`,
			wantPath:    "(root)",
			wantMessage: "spec is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			ve, ok := err.(*ValidationError)
			if !ok {
				t.Fatalf("expected *ValidationError, got %T", err)
			}

			found := false
			for _, issue := range ve.Errors {
				if strings.Contains(issue.Path, tt.wantPath) &&
					strings.Contains(issue.Message, tt.wantMessage) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected issue with path containing %q and message containing %q, got: %v",
					tt.wantPath, tt.wantMessage, ve.Errors)
			}
		})
	}
}

func TestValidate_WrongType(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantPath    string
		wantMessage string
	}{
		{
			name: "constraints not array",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test
  constraints: "not an array"
spec:
  query: SELECT 1
`,
			wantPath:    "metadata.constraints",
			wantMessage: "Invalid type",
		},
		{
			name: "labels not object",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: test
  labels: "not an object"
spec:
  query: SELECT 1
`,
			wantPath:    "metadata.labels",
			wantMessage: "Invalid type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			ve, ok := err.(*ValidationError)
			if !ok {
				t.Fatalf("expected *ValidationError, got %T", err)
			}

			found := false
			for _, issue := range ve.Errors {
				if strings.Contains(issue.Path, tt.wantPath) &&
					strings.Contains(issue.Message, tt.wantMessage) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected issue with path containing %q and message containing %q, got: %v",
					tt.wantPath, tt.wantMessage, ve.Errors)
			}
		})
	}
}

func TestValidate_UnknownKind(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: UnknownKind
metadata:
  name: test
spec:
  foo: bar
`
	err := Validate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	// Should have an error about kind not being valid
	found := false
	for _, issue := range ve.Errors {
		if strings.Contains(issue.Path, "kind") ||
			strings.Contains(issue.Message, "enum") ||
			strings.Contains(issue.Message, "must be one of") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected issue about invalid kind, got: %v", ve.Errors)
	}
}

func TestValidate_InvalidYAML(t *testing.T) {
	yaml := `
this is not: valid: yaml: syntax
  - broken
`
	err := Validate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	if len(ve.Errors) == 0 {
		t.Error("expected at least one error")
	}

	if !strings.Contains(ve.Errors[0].Message, "invalid YAML") {
		t.Errorf("expected 'invalid YAML' message, got: %s", ve.Errors[0].Message)
	}
}

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{
		Errors: []ValidationIssue{
			{Path: "metadata.name", Message: "name is required"},
			{Path: "spec.query", Message: "query must be a string"},
		},
	}

	errStr := ve.Error()
	if !strings.Contains(errStr, "metadata.name") {
		t.Errorf("expected error to contain 'metadata.name', got: %s", errStr)
	}
	if !strings.Contains(errStr, "spec.query") {
		t.Errorf("expected error to contain 'spec.query', got: %s", errStr)
	}
}

func TestIsValidationError(t *testing.T) {
	ve := &ValidationError{Errors: []ValidationIssue{{Path: "test", Message: "test"}}}

	if !IsValidationError(ve) {
		t.Error("expected IsValidationError to return true for *ValidationError")
	}

	if IsValidationError(nil) {
		t.Error("expected IsValidationError to return false for nil")
	}

	if IsValidationError(fmt.Errorf("not a validation error")) {
		t.Error("expected IsValidationError to return false for non-ValidationError")
	}
}

func TestGetValidationIssues(t *testing.T) {
	issues := []ValidationIssue{
		{Path: "test1", Message: "message1"},
		{Path: "test2", Message: "message2"},
	}
	ve := &ValidationError{Errors: issues}

	got := GetValidationIssues(ve)
	if len(got) != len(issues) {
		t.Errorf("expected %d issues, got %d", len(issues), len(got))
	}

	if GetValidationIssues(nil) != nil {
		t.Error("expected nil for nil error")
	}
}
