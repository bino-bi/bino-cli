package spec

import (
	"strings"
	"testing"
)

func TestEditYAMLDocumentPreservesFidelity(t *testing.T) {
	content := `# leading comment
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales # the table name
spec:
  title: Old Title
  dataset: revenue # source dataset
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: note
spec:
  value: untouched
`

	full, edited, err := EditYAMLDocument(content, 1, map[string]any{"spec.title": "New Title"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	// The edit was applied.
	if !strings.Contains(full, "New Title") || strings.Contains(full, "Old Title") {
		t.Errorf("title not updated:\n%s", full)
	}
	if !strings.Contains(edited, "New Title") {
		t.Errorf("edited doc missing new title:\n%s", edited)
	}

	// Comments on untouched keys are preserved.
	for _, want := range []string{"# leading comment", "# the table name", "# source dataset"} {
		if !strings.Contains(full, want) {
			t.Errorf("lost comment %q:\n%s", want, full)
		}
	}

	// The second document is untouched (still two documents, value intact).
	if !strings.Contains(full, "untouched") {
		t.Errorf("second document changed:\n%s", full)
	}
	if strings.Count(full, "apiVersion: bino.bi/v1alpha1") != 2 {
		t.Errorf("expected 2 documents:\n%s", full)
	}
}

func TestEditYAMLDocumentCreatesAndIndexes(t *testing.T) {
	content := "kind: Table\nspec:\n  columns:\n    - a\n    - b\n"

	// Create a missing nested key.
	full, _, err := EditYAMLDocument(content, 1, map[string]any{"spec.title": "T"})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if !strings.Contains(full, "title: T") {
		t.Errorf("nested key not created:\n%s", full)
	}

	// Replace a sequence element by index.
	full2, _, err := EditYAMLDocument(content, 1, map[string]any{"spec.columns[1]": "B"})
	if err != nil {
		t.Fatalf("index edit: %v", err)
	}
	if !strings.Contains(full2, "B") || strings.Contains(full2, "- b\n") {
		t.Errorf("index element not replaced:\n%s", full2)
	}
}

func TestEditYAMLDocumentOutOfRange(t *testing.T) {
	if _, _, err := EditYAMLDocument("kind: Table\n", 2, map[string]any{"x": 1}); err == nil {
		t.Error("expected out-of-range error for position 2")
	}
}

func TestEditYAMLDocumentIntegerStaysInteger(t *testing.T) {
	// JSON numbers arrive as float64; integral values must re-encode as integers.
	full, _, err := EditYAMLDocument("kind: Table\nspec:\n  width: 1\n", 1, map[string]any{"spec.width": float64(42)})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !strings.Contains(full, "width: 42") {
		t.Errorf("integer not preserved:\n%s", full)
	}
}
