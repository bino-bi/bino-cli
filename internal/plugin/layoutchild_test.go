package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// newLayoutChildRegistry registers one component kind and one datasource kind,
// both with a spec schema.
func newLayoutChildRegistry() *PluginRegistry {
	reg := NewRegistry()
	reg.Register(&schemaMockPlugin{
		mockPlugin: mockPlugin{manifest: PluginManifest{
			Name: "gauges",
			Kinds: []KindRegistration{
				{KindName: "Gauge", Category: KindCategoryComponent},
				{KindName: "GaugeDataSource", Category: KindCategoryDataSource},
			},
		}},
		schemas: map[string][]byte{
			"Gauge":           []byte(`{"type":"object","required":["title"],"additionalProperties":false,"properties":{"title":{"type":"string"},"dataset":{"type":"string"}}}`),
			"GaugeDataSource": []byte(`{"type":"object","properties":{"url":{"type":"string"}}}`),
		},
	})
	return reg
}

func defEnum(t *testing.T, merged []byte, def string) []string {
	t.Helper()
	var doc struct {
		Defs map[string]struct {
			Properties struct {
				Kind struct {
					Enum []string `json:"enum"`
				} `json:"kind"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Defs[def].Properties.Kind.Enum
}

func TestSchemaAggregator_LayoutChildKinds(t *testing.T) {
	agg := NewSchemaAggregator(newLayoutChildRegistry())
	if err := agg.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	merged := agg.MergedSchema()

	layoutChild := defEnum(t, merged, "layoutChild")
	if !slices.Contains(layoutChild, "Gauge") {
		t.Errorf("layoutChild enum misses component kind Gauge: %v", layoutChild)
	}
	if slices.Contains(layoutChild, "GaugeDataSource") {
		t.Errorf("layoutChild enum lists datasource kind GaugeDataSource: %v", layoutChild)
	}
	if grid := defEnum(t, merged, "gridChild"); slices.Contains(grid, "Gauge") {
		t.Errorf("gridChild enum must stay closed, got %v", grid)
	}
}

func layoutPage(children string) []byte {
	return []byte(`{"apiVersion":"bino.bi/v1alpha1","kind":"LayoutPage","metadata":{"name":"page"},"spec":{"children":` + children + `}}`)
}

func TestDocumentValidator_PluginLayoutChild(t *testing.T) {
	v, err := newLayoutChildRegistry().DocumentValidator(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("expected a validator with plugin kinds loaded")
	}

	tests := []struct {
		name    string
		doc     []byte
		wantErr bool
	}{
		{"inline valid", layoutPage(`[{"kind":"Gauge","spec":{"title":"Revenue"}}]`), false},
		{"inline spec violates plugin schema", layoutPage(`[{"kind":"Gauge","spec":{"color":"red"}}]`), true},
		{"inline without spec", layoutPage(`[{"kind":"Gauge"}]`), true},
		{"ref with override", layoutPage(`[{"kind":"Gauge","ref":"g1","spec":{"anything":1}}]`), false},
		{"in LayoutCard", layoutPage(`[{"kind":"LayoutCard","spec":{"children":[{"kind":"Gauge","spec":{"title":"x"}}]}}]`), false},
		{"datasource kind as child", layoutPage(`[{"kind":"GaugeDataSource","spec":{"url":"x"}}]`), true},
		{"inside Grid", layoutPage(`[{"kind":"Grid","spec":{"children":[{"kind":"Gauge","spec":{"title":"x"}}]}}]`), true},
		{"top-level plugin doc valid", []byte(`{"apiVersion":"bino.bi/v1alpha1","kind":"Gauge","metadata":{"name":"g1"},"spec":{"title":"x"}}`), false},
		{"top-level plugin doc invalid", []byte(`{"apiVersion":"bino.bi/v1alpha1","kind":"Gauge","metadata":{"name":"g1"},"spec":{}}`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateJSON(tt.doc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateJSON() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDocumentValidator_NoPluginKinds(t *testing.T) {
	v, err := NewRegistry().DocumentValidator(context.Background())
	if err != nil || v != nil {
		t.Fatalf("want nil validator without plugin kinds, got %v, %v", v, err)
	}
	var nilReg *PluginRegistry
	if v, err := nilReg.DocumentValidator(context.Background()); err != nil || v != nil {
		t.Fatalf("want nil validator for nil registry, got %v, %v", v, err)
	}
}

func writeBundle(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const pageWithGauge = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Gauge
      spec:
        title: Revenue
    - kind: Gauge
      ref: g1
`

const gaugeDoc = `apiVersion: bino.bi/v1alpha1
kind: Gauge
metadata:
  name: g1
spec:
  title: Costs
`

func TestLoadDir_PluginLayoutChild(t *testing.T) {
	ctx := context.Background()
	root := writeBundle(t, map[string]string{"page.yaml": pageWithGauge, "gauge.yaml": gaugeDoc})

	docs, err := config.LoadDirWithOptions(ctx, root, config.LoadOptions{KindProvider: newLayoutChildRegistry()})
	if err != nil {
		t.Fatalf("load with plugin registry: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 documents, got %d", len(docs))
	}

	// Without plugins the child kind is still unknown to the schema.
	pageOnly := writeBundle(t, map[string]string{"page.yaml": pageWithGauge})
	_, err = config.LoadDirWithOptions(ctx, pageOnly, config.LoadOptions{})
	if err == nil {
		t.Fatal("want an error without a kind provider")
	}
	issue := findIssue(err, "spec.children.0.kind")
	if issue == "" {
		t.Fatalf("want an error on spec.children.0.kind, got %v", err)
	}
	if want := "value must be one of"; !strings.Contains(issue, want) {
		t.Errorf("want %q in %q", want, issue)
	}
}

func TestLoadDir_InvalidTopLevelPluginDoc(t *testing.T) {
	root := writeBundle(t, map[string]string{"gauge.yaml": strings.Replace(gaugeDoc, "title: Costs", "color: red", 1)})

	_, err := config.LoadDirWithOptions(context.Background(), root, config.LoadOptions{KindProvider: newLayoutChildRegistry()})
	if err == nil {
		t.Fatal("want a schema error for a plugin document that violates the plugin schema")
	}
	if findIssue(err, "spec") == "" && findIssue(err, "spec.color") == "" {
		t.Fatalf("want an error on the Gauge spec, got %v", err)
	}
}

// findIssue returns the message of the first schema issue at field, or "".
func findIssue(err error, field string) string {
	var se *spec.SchemaValidationError
	if !errors.As(err, &se) {
		return ""
	}
	for _, e := range se.Errors {
		if e.Field == field {
			return e.Description
		}
	}
	return ""
}
