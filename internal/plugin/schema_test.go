package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type schemaMockPlugin struct {
	mockPlugin
	schemas map[string][]byte
}

func (m *schemaMockPlugin) GetSchemas(context.Context) (map[string][]byte, error) {
	return m.schemas, nil
}

func TestSchemaAggregator_NoPlugins(t *testing.T) {
	reg := NewRegistry()
	agg := NewSchemaAggregator(reg)
	if err := agg.Build(context.Background()); err != nil {
		t.Fatal(err)
	}

	merged := agg.MergedSchema()
	if len(merged) == 0 {
		t.Fatal("expected non-empty schema")
	}

	// Should contain the built-in kind enum.
	if !strings.Contains(string(merged), `"DataSource"`) {
		t.Fatal("expected DataSource in schema")
	}
}

func TestSchemaAggregator_AddsPluginKinds(t *testing.T) {
	reg := NewRegistry()
	p := &schemaMockPlugin{
		mockPlugin: mockPlugin{manifest: PluginManifest{
			Name: "sf",
			Kinds: []KindRegistration{
				{KindName: "SalesforceDataSource", Category: KindCategoryDataSource},
			},
		}},
		schemas: map[string][]byte{
			"SalesforceDataSource": []byte(`{"type":"object","properties":{"connection":{"type":"string"}}}`),
		},
	}
	reg.Register(p)

	agg := NewSchemaAggregator(reg)
	if err := agg.Build(context.Background()); err != nil {
		t.Fatal(err)
	}

	merged := agg.MergedSchema()

	// Kind enum should include plugin kind.
	if !strings.Contains(string(merged), `"SalesforceDataSource"`) {
		t.Fatal("expected SalesforceDataSource in kind enum")
	}

	// Should have an if-then block for the plugin kind.
	if !strings.Contains(string(merged), `"const":"SalesforceDataSource"`) {
		t.Fatal("expected if-then block for SalesforceDataSource")
	}
}

func TestSchemaAggregator_SchemaForKind(t *testing.T) {
	reg := NewRegistry()
	schema := []byte(`{"type":"object"}`)
	p := &schemaMockPlugin{
		mockPlugin: mockPlugin{manifest: PluginManifest{
			Name:  "sf",
			Kinds: []KindRegistration{{KindName: "SfDS"}},
		}},
		schemas: map[string][]byte{"SfDS": schema},
	}
	reg.Register(p)

	agg := NewSchemaAggregator(reg)
	if err := agg.Build(context.Background()); err != nil {
		t.Fatal(err)
	}

	s, ok := agg.SchemaForKind("SfDS")
	if !ok {
		t.Fatal("expected to find schema for SfDS")
	}
	if string(s) != string(schema) {
		t.Fatalf("got %q, want %q", string(s), string(schema))
	}

	_, ok = agg.SchemaForKind("Unknown")
	if ok {
		t.Fatal("should not find schema for Unknown")
	}
}

func TestSchemaAggregator_MergedSchemaIsValidJSON(t *testing.T) {
	reg := NewRegistry()
	p := &schemaMockPlugin{
		mockPlugin: mockPlugin{manifest: PluginManifest{
			Name:  "test",
			Kinds: []KindRegistration{{KindName: "TestKind"}},
		}},
		schemas: map[string][]byte{
			"TestKind": []byte(`{"type":"object","properties":{"foo":{"type":"string"}}}`),
		},
	}
	reg.Register(p)

	agg := NewSchemaAggregator(reg)
	if err := agg.Build(context.Background()); err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(agg.MergedSchema(), &parsed); err != nil {
		t.Fatalf("merged schema is not valid JSON: %v", err)
	}
}
