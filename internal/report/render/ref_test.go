package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// makeTestDoc creates a config.Document for testing.
func makeTestDoc(kind, name string, raw json.RawMessage) config.Document {
	return config.Document{
		Kind: kind,
		Name: name,
		Raw:  raw,
		File: "test.yaml",
	}
}

func TestRenderLayoutChildWithRef(t *testing.T) {
	ctx := context.Background()

	// ChartTime document that can be referenced.
	chartTimeDoc := makeTestDoc("ChartTime", "sampleTimeChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "sampleTimeChart"},
		"spec": {
			"dataset": "sales_data",
			"chartTitle": "Original Title"
		}
	}`))

	// LayoutPage that references the ChartTime via ref.
	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
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
	result, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	html := string(result.HTML)

	// Verify the chart is rendered with original title.
	if !strings.Contains(html, `chart-title='Original Title'`) {
		t.Fatalf("expected chart with original title in HTML, got:\n%s", html)
	}
	if !strings.Contains(html, `<bn-chart-time`) {
		t.Fatalf("expected bn-chart-time element in HTML, got:\n%s", html)
	}
}

func TestRenderLayoutChildWithRefAndOverride(t *testing.T) {
	ctx := context.Background()

	// ChartTime document.
	chartTimeDoc := makeTestDoc("ChartTime", "sampleTimeChart", json.RawMessage(`{
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
	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
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
	result, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	html := string(result.HTML)

	// Verify the chart is rendered with overridden title.
	if !strings.Contains(html, `chart-title='Overridden Title'`) {
		t.Fatalf("expected chart with overridden title in HTML, got:\n%s", html)
	}
	// Verify the original level is preserved.
	if !strings.Contains(html, `level='category'`) {
		t.Fatalf("expected level=category to be preserved from base spec, got:\n%s", html)
	}
}

func TestRenderLayoutChildWithMissingRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that references a non-existent ChartTime (required ref).
	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
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
	_, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err == nil {
		t.Fatalf("GenerateHTMLFromDocuments should error on missing required ref")
	}
	if !strings.Contains(err.Error(), "required reference") {
		t.Fatalf("error message should mention 'required reference', got: %v", err)
	}
}

func TestRenderLayoutChildWithOptionalMissingRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that references a non-existent ChartTime with optional: true.
	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
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
	result, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments should not error on optional missing ref: %v", err)
	}

	html := string(result.HTML)

	// The missing optional ref child should be skipped, so no chart element.
	if strings.Contains(html, `<bn-chart-time`) {
		t.Fatalf("expected no bn-chart-time element when optional ref is missing, got:\n%s", html)
	}
}

func TestRenderLayoutChildWithConstraintFilteredRef(t *testing.T) {
	ctx := context.Background()

	// ChartTime document that exists but will be "filtered out" by simulating allDocs vs docs difference.
	chartTimeDoc := makeTestDoc("ChartTime", "constraintFilteredChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "constraintFilteredChart"},
		"spec": {
			"dataset": "sales_data",
			"chartTitle": "Filtered Chart"
		}
	}`))

	// LayoutPage that references the ChartTime.
	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "constraintFilteredChart"
				}
			]
		}
	}`))

	// Simulate constraint filtering: allDocs contains the chart, but docs (filtered) does not.
	allDocs := []config.Document{chartTimeDoc, layoutPageDoc}
	docs := []config.Document{layoutPageDoc} // Chart is filtered out

	// Use the full function to pass allDocs
	result, _, err := GenerateHTMLFromDocumentsWithDatasets(ctx, docs, nil, "de", "", "", ModePreview, nil, nil, "v1.0.0", allDocs)
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocumentsWithDatasets should not error on constraint-filtered ref: %v", err)
	}

	html := string(result.HTML)

	// The constraint-filtered ref child should be skipped gracefully, so no chart element.
	if strings.Contains(html, `<bn-chart-time`) {
		t.Fatalf("expected no bn-chart-time element when ref is constraint-filtered, got:\n%s", html)
	}
}

func TestRenderLayoutChildWithLayoutCardRef(t *testing.T) {
	ctx := context.Background()

	// LayoutCard document that can be referenced.
	layoutCardDoc := makeTestDoc("LayoutCard", "sampleCard", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutCard",
		"metadata": {"name": "sampleCard"},
		"spec": {
			"cardLayout": "single",
			"children": [
				{
					"kind": "Text",
					"spec": {"value": "Hello from card"}
				}
			]
		}
	}`))

	// LayoutPage that references the LayoutCard via ref.
	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
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
	result, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	html := string(result.HTML)

	// Verify the card is rendered.
	if !strings.Contains(html, `<bn-layout-card`) {
		t.Fatalf("expected bn-layout-card element in HTML, got:\n%s", html)
	}
	// Verify the text inside the card is rendered.
	if !strings.Contains(html, `value='&lt;p&gt;Hello from card&lt;/p&gt;'`) {
		t.Fatalf("expected text content from card in HTML, got:\n%s", html)
	}
}

func TestRenderLayoutChildWithLayoutCardRefAndOverride(t *testing.T) {
	ctx := context.Background()

	// LayoutCard document.
	layoutCardDoc := makeTestDoc("LayoutCard", "sampleCard", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutCard",
		"metadata": {"name": "sampleCard"},
		"spec": {
			"cardLayout": "single",
			"titleImage": "original.png",
			"children": [
				{
					"kind": "Text",
					"spec": {"value": "Hello from card"}
				}
			]
		}
	}`))

	// LayoutPage that references the LayoutCard with override.
	layoutPageDoc := makeTestDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "LayoutCard",
					"ref": "sampleCard",
					"spec": {
						"titleImage": "overridden.png"
					}
				}
			]
		}
	}`))

	docs := []config.Document{layoutCardDoc, layoutPageDoc}
	result, _, err := GenerateHTMLFromDocuments(ctx, docs, "de", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}

	html := string(result.HTML)

	// Verify the card has overridden title-image.
	if !strings.Contains(html, `title-image='overridden.png'`) {
		t.Fatalf("expected overridden title-image in HTML, got:\n%s", html)
	}
	// Verify the original layout is preserved.
	if !strings.Contains(html, `card-layout='single'`) {
		t.Fatalf("expected card-layout to be preserved from base spec, got:\n%s", html)
	}
}

func TestRenderMergeJSONObjects(t *testing.T) {
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
