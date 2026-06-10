package spec

import (
	"strings"
	"testing"
)

func TestSchemaValidationError_Error_Enriched(t *testing.T) {
	source := `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: revenue
spec:
  query: SELECT 1
  level: warning
`
	err := &SchemaValidationError{
		File:        "/tmp/project/datasets.yaml",
		DocPosition: 1,
		Source:      source,
		Errors: []SchemaError{
			{
				Field:       "spec.level",
				Description: "additional property 'level' not allowed",
				Value:       "warning",
				Line:        7,
				Column:      3,
			},
		},
	}

	msg := err.Error()

	for _, want := range []string{
		"in /tmp/project/datasets.yaml (document #1)",
		"spec.level (line 7, col 3)",
		"additional property 'level' not allowed",
		"7 │   level: warning", // source snippet around the offending line
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() missing %q, got:\n%s", want, msg)
		}
	}
}

func TestSchemaValidationError_Error_Unenriched(t *testing.T) {
	err := &SchemaValidationError{
		Errors: []SchemaError{
			{Field: "spec", Description: "missing property 'query'"},
		},
	}

	msg := err.Error()
	if !strings.HasPrefix(msg, "schema validation failed:") {
		t.Errorf("expected plain header without file info, got:\n%s", msg)
	}
	if strings.Contains(msg, "line") {
		t.Errorf("expected no line info when position is unknown, got:\n%s", msg)
	}
}
