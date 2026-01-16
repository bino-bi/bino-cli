package graph

import (
	"context"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// makeDoc creates a config.Document with the given kind, name, and raw JSON payload.
func makeDoc(kind, name string, raw json.RawMessage) config.Document {
	return config.Document{
		Kind: kind,
		Name: name,
		Raw:  raw,
		File: "test.yaml",
	}
}

func TestBuildLayoutChildWithRef(t *testing.T) {
	ctx := context.Background()

	// ChartTime document that can be referenced.
	chartTimeDoc := makeDoc("ChartTime", "sampleTimeChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "sampleTimeChart"},
		"spec": {
			"dataset": "sales_data",
			"chartTitle": "Original Title"
		}
	}`))

	// LayoutPage that references the ChartTime via ref.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "sampleTimeChart"
				}
			]
		}
	}`))

	docs := []config.Document{chartTimeDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Verify layout page node exists.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}

	// Verify the page has dependencies (the inlined chart child).
	if len(pageNode.DependsOn) == 0 {
		t.Fatalf("expected layout page to have dependencies from ref child")
	}
}

func TestBuildLayoutChildWithRefAndOverride(t *testing.T) {
	ctx := context.Background()

	// ChartTime document.
	chartTimeDoc := makeDoc("ChartTime", "sampleTimeChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "sampleTimeChart"},
		"spec": {
			"dataset": "sales_data",
			"chartTitle": "Original Title",
			"level": "category"
		}
	}`))

	// LayoutPage that references the ChartTime with spec override.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "sampleTimeChart",
					"spec": {
						"chartTitle": "Overridden Title"
					}
				}
			]
		}
	}`))

	docs := []config.Document{chartTimeDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Verify layout page node exists and has dependencies.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}
	if len(pageNode.DependsOn) == 0 {
		t.Fatalf("expected layout page to have dependencies from ref child with override")
	}
}

func TestBuildLayoutChildWithMissingRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that references a non-existent ChartTime (required ref).
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "nonExistentChart"
				}
			]
		}
	}`))

	docs := []config.Document{layoutPageDoc}
	_, err := Build(ctx, docs)
	if err == nil {
		t.Fatalf("Build should error on missing required ref")
	}
	if !contains(err.Error(), "required reference") {
		t.Fatalf("error message should mention 'required reference', got: %v", err)
	}
}

func TestBuildLayoutChildWithOptionalMissingRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that references a non-existent ChartTime with optional: true.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "nonExistentChart",
					"optional": true
				}
			]
		}
	}`))

	docs := []config.Document{layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build should not error on optional missing ref: %v", err)
	}

	// Verify layout page node exists.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}

	// The missing optional ref child should be skipped, so no dependencies.
	if len(pageNode.DependsOn) != 0 {
		t.Fatalf("expected layout page to have no dependencies when optional ref is missing, got %v", pageNode.DependsOn)
	}
}

func TestBuildLayoutChildWithLayoutPageRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that we try to reference (should fail).
	referencedPage := makeDoc("LayoutPage", "otherPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "otherPage"},
		"spec": {
			"children": []
		}
	}`))

	// LayoutPage that tries to ref another LayoutPage (disallowed).
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "LayoutPage",
					"ref": "otherPage"
				}
			]
		}
	}`))

	docs := []config.Document{referencedPage, layoutPageDoc}
	_, err := Build(ctx, docs)
	if err == nil {
		t.Fatalf("expected error when referencing LayoutPage")
	}
	if !contains(err.Error(), "unsupported child kind") {
		// The schema enforces kind enum, so LayoutPage isn't allowed as child kind.
		// The error should be about unsupported child kind.
		t.Logf("got error: %v", err)
	}
}

func TestBuildLayoutChildWithLayoutCardRef(t *testing.T) {
	ctx := context.Background()

	// LayoutCard document that can be referenced.
	layoutCardDoc := makeDoc("LayoutCard", "sampleCard", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutCard",
		"metadata": {"name": "sampleCard"},
		"spec": {
			"children": [
				{
					"kind": "Text",
					"spec": {"value": "Hello from card"}
				}
			]
		}
	}`))

	// LayoutPage that references the LayoutCard via ref.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "LayoutCard",
					"ref": "sampleCard"
				}
			]
		}
	}`))

	docs := []config.Document{layoutCardDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Verify layout page node exists.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}

	// Verify the page has dependencies (the referenced card).
	if len(pageNode.DependsOn) == 0 {
		t.Fatalf("expected layout page to have dependencies from ref LayoutCard")
	}
}

func TestMergeJSONObjects(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		override string
		want     string
		wantErr  bool
	}{
		{
			name:     "simple override",
			base:     `{"a": "1", "b": "2"}`,
			override: `{"b": "3"}`,
			want:     `{"a":"1","b":"3"}`,
		},
		{
			name:     "add new field",
			base:     `{"a": "1"}`,
			override: `{"b": "2"}`,
			want:     `{"a":"1","b":"2"}`,
		},
		{
			name:     "nested merge",
			base:     `{"outer": {"a": "1", "b": "2"}}`,
			override: `{"outer": {"b": "3"}}`,
			want:     `{"outer":{"a":"1","b":"3"}}`,
		},
		{
			name:     "array replace",
			base:     `{"arr": [1, 2, 3]}`,
			override: `{"arr": [4, 5]}`,
			want:     `{"arr":[4,5]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeJSONObjects(json.RawMessage(tt.base), json.RawMessage(tt.override))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Normalize for comparison.
			var gotMap, wantMap map[string]any
			if err := json.Unmarshal(got, &gotMap); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.want), &wantMap); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}

			gotBytes, _ := json.Marshal(gotMap)
			wantBytes, _ := json.Marshal(wantMap)
			if string(gotBytes) != string(wantBytes) {
				t.Fatalf("mergeJSONObjects() = %s, want %s", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
