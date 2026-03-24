package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"bino.bi/bino/internal/schema"
)

// SchemaAggregator merges built-in and plugin schemas into a unified JSON Schema.
type SchemaAggregator struct {
	registry    *PluginRegistry
	merged      json.RawMessage
	kindSchemas map[string]json.RawMessage // kind → plugin-provided schema
}

// NewSchemaAggregator creates an aggregator backed by the given registry.
func NewSchemaAggregator(registry *PluginRegistry) *SchemaAggregator {
	return &SchemaAggregator{
		registry:    registry,
		kindSchemas: make(map[string]json.RawMessage),
	}
}

// Build fetches schemas from all plugins and merges with the built-in schema.
func (a *SchemaAggregator) Build(ctx context.Context) error {
	// Collect schemas from all plugins that provide kinds.
	for _, p := range a.registry.AllPlugins() {
		schemas, err := p.GetSchemas(ctx)
		if err != nil {
			continue
		}
		for kindName, schemaBytes := range schemas {
			if _, exists := a.kindSchemas[kindName]; exists {
				return fmt.Errorf("duplicate schema for kind %q", kindName)
			}
			a.kindSchemas[kindName] = json.RawMessage(schemaBytes)
		}
	}

	// Load the built-in schema as a mutable JSON object.
	builtinBytes := schema.DocumentSchemaBytes()
	var schemaObj map[string]any
	if err := json.Unmarshal(builtinBytes, &schemaObj); err != nil {
		return fmt.Errorf("parsing built-in schema: %w", err)
	}

	if len(a.kindSchemas) == 0 {
		a.merged = builtinBytes
		return nil
	}

	// Extend the kind enum with plugin kinds.
	if err := a.extendKindEnum(schemaObj); err != nil {
		return err
	}

	// Add if-then blocks for plugin kinds in the allOf array.
	a.addPluginIfThenBlocks(schemaObj)

	// Marshal back.
	merged, err := json.Marshal(schemaObj)
	if err != nil {
		return fmt.Errorf("marshaling merged schema: %w", err)
	}
	a.merged = merged
	return nil
}

// extendKindEnum appends plugin kind names to the kind enum array.
func (a *SchemaAggregator) extendKindEnum(schemaObj map[string]any) error {
	props, ok := schemaObj["properties"].(map[string]any)
	if !ok {
		return nil
	}
	kindProp, ok := props["kind"].(map[string]any)
	if !ok {
		return nil
	}
	enumSlice, ok := kindProp["enum"].([]any)
	if !ok {
		return nil
	}

	for kindName := range a.kindSchemas {
		enumSlice = append(enumSlice, kindName)
	}
	// Also add kinds that have no schema but are registered.
	for _, k := range a.registry.AllKinds() {
		if _, hasSchema := a.kindSchemas[k.KindName]; hasSchema {
			continue
		}
		enumSlice = append(enumSlice, k.KindName)
	}

	kindProp["enum"] = enumSlice
	return nil
}

// addPluginIfThenBlocks adds if-then conditional validation for plugin kinds.
func (a *SchemaAggregator) addPluginIfThenBlocks(schemaObj map[string]any) {
	allOf, ok := schemaObj["allOf"].([]any)
	if !ok {
		allOf = []any{}
	}

	for kindName, kindSchema := range a.kindSchemas {
		var specSchema any
		if err := json.Unmarshal(kindSchema, &specSchema); err != nil {
			continue
		}

		block := map[string]any{
			"if": map[string]any{
				"properties": map[string]any{
					"kind": map[string]any{"const": kindName},
				},
			},
			"then": map[string]any{
				"properties": map[string]any{
					"spec": specSchema,
				},
			},
		}
		allOf = append(allOf, block)
	}

	schemaObj["allOf"] = allOf
}

// MergedSchema returns the complete merged JSON Schema.
func (a *SchemaAggregator) MergedSchema() json.RawMessage {
	if a.merged == nil {
		return schema.DocumentSchemaBytes()
	}
	return a.merged
}

// SchemaForKind returns the JSON Schema for a specific kind.
func (a *SchemaAggregator) SchemaForKind(kind string) (json.RawMessage, bool) {
	s, ok := a.kindSchemas[kind]
	return s, ok
}
