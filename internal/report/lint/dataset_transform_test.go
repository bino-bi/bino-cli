package lint

import (
	"context"
	"strings"
	"testing"
)

func TestDatasetTransformInvalid_BadFilterScalarForIn(t *testing.T) {
	// op "in" with a scalar value is a semantic error the JSON schema cannot
	// express; dataset.ValidateSpec returns "requires an array value".
	docs := []Document{
		{
			File:     "/test/data.yaml",
			Position: 1,
			Kind:     "DataSet",
			Name:     "sales",
			Raw: rawDoc("DataSet", "sales", map[string]any{
				"query": "SELECT region, sales FROM raw",
				"filter": map[string]any{
					"conditions": []any{
						map[string]any{"column": "region", "op": "in", "value": "EMEA"},
					},
				},
			}),
		},
	}

	findings := datasetTransformInvalid.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %#v", len(findings), findings)
	}
	if findings[0].RuleID != "dataset-transform-invalid" {
		t.Errorf("expected rule ID 'dataset-transform-invalid', got %q", findings[0].RuleID)
	}
	if findings[0].File != "/test/data.yaml" {
		t.Errorf("expected file '/test/data.yaml', got %q", findings[0].File)
	}
	if findings[0].DocIdx != 1 {
		t.Errorf("expected DocIdx 1, got %d", findings[0].DocIdx)
	}
	if findings[0].Path != "spec" {
		t.Errorf("expected Path 'spec', got %q", findings[0].Path)
	}
	if !strings.Contains(findings[0].Message, "requires an array value") {
		t.Errorf("message %q does not describe the array mismatch", findings[0].Message)
	}
}

func TestDatasetTransformInvalid_FirstWithoutOrderByWarns(t *testing.T) {
	// A first/last aggregate without orderBy is non-fatal but surfaces as a
	// warning finding under the same rule ID.
	docs := []Document{
		{
			File:     "/test/data.yaml",
			Position: 1,
			Kind:     "DataSet",
			Name:     "sales",
			Raw: rawDoc("DataSet", "sales", map[string]any{
				"query": "SELECT region, sales FROM raw",
				"groupBy": map[string]any{
					"columns": []any{"region"},
					"aggregates": []any{
						map[string]any{"column": "sales", "fn": "first", "as": "f"},
					},
				},
			}),
		},
	}

	findings := datasetTransformInvalid.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 warning finding, got %d: %#v", len(findings), findings)
	}
	if findings[0].RuleID != "dataset-transform-invalid" {
		t.Errorf("expected rule ID 'dataset-transform-invalid', got %q", findings[0].RuleID)
	}
	if !strings.Contains(findings[0].Message, "nondeterministic") {
		t.Errorf("message %q is not the expected nondeterminism warning", findings[0].Message)
	}
}

func TestDatasetTransformInvalid_QueryOnlyNoFindings(t *testing.T) {
	// A plain query-only DataSet (no filter/groupBy/indexColumns) must not be
	// flagged, so existing datasets never produce false positives.
	docs := []Document{
		{
			File:     "/test/data.yaml",
			Position: 1,
			Kind:     "DataSet",
			Name:     "sales",
			Raw: rawDoc("DataSet", "sales", map[string]any{
				"query": "SELECT region, sales FROM raw",
			}),
		},
	}

	findings := datasetTransformInvalid.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for query-only DataSet, got %d: %#v", len(findings), findings)
	}
}

func TestDatasetTransformInvalid_IgnoresNonDataSet(t *testing.T) {
	// Non-DataSet documents are skipped even if they carry a transform-shaped
	// spec, so the rule only governs DataSet kinds.
	docs := []Document{
		{
			File:     "/test/text.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "intro",
			Raw: rawDoc("Text", "intro", map[string]any{
				"filter": map[string]any{
					"conditions": []any{
						map[string]any{"column": "region", "op": "in", "value": "EMEA"},
					},
				},
			}),
		},
	}

	findings := datasetTransformInvalid.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for non-DataSet, got %d: %#v", len(findings), findings)
	}
}
