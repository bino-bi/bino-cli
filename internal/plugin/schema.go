package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/schema"
)

// pluginSpecIDBase prefixes the $id given to a plugin spec schema without one.
const pluginSpecIDBase = "https://bino.bi/schemas/plugins/"

// ensureSchemaID gives an embedded plugin schema its own id, so its local
// "#/..." refs resolve inside it and not against the bino schema. A
// fragment-only id names no resource, and an id already used by another kind
// fails compilation, so both are replaced. seen collects the ids in use.
func ensureSchemaID(obj map[string]any, kindName string, seen map[string]bool) {
	key := "$id"
	if s, _ := obj["$schema"].(string); strings.Contains(s, "draft-04") {
		key = "id"
	}
	id, _ := obj[key].(string)
	if id == "" || strings.HasPrefix(id, "#") || seen[id] {
		id = pluginSpecIDBase + kindName + ".json"
		obj[key] = id
	}
	seen[id] = true
	// Draft-04 ignores "id" next to a "$ref"; the compiler then falls back to
	// the root draft, which reads "$id".
	if key == "id" {
		obj["$id"] = id
	}
}

// SchemaAggregator merges built-in and plugin schemas into a unified JSON Schema.
type SchemaAggregator struct {
	registry    *PluginRegistry
	merged      json.RawMessage
	kindSchemas map[string]json.RawMessage // kind → plugin-provided schema
	specPtrs    map[string]string          // kind → JSON pointer of the embedded spec schema
	fetchErr    error                      // GetSchemas failures, see FetchErrors
}

// NewSchemaAggregator creates an aggregator backed by the given registry.
func NewSchemaAggregator(registry *PluginRegistry) *SchemaAggregator {
	return &SchemaAggregator{
		registry:    registry,
		kindSchemas: make(map[string]json.RawMessage),
		specPtrs:    make(map[string]string),
	}
}

// Build fetches schemas from all plugins and merges with the built-in schema.
func (a *SchemaAggregator) Build(ctx context.Context) error {
	log := logx.FromContext(ctx)
	// Collect schemas from all plugins that provide kinds.
	for _, p := range a.registry.AllPlugins() {
		schemas, err := p.GetSchemas(ctx)
		if err != nil {
			// Skipping silently would make the user's manifests fail
			// validation pointing at their YAML instead of the plugin.
			log.Warnf("plugin %s: get schemas failed, its kinds are unavailable: %v", p.Manifest().Name, err)
			a.fetchErr = errors.Join(a.fetchErr, fmt.Errorf("plugin %s: get schemas: %w", p.Manifest().Name, err))
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

	if len(a.kindSchemas) == 0 && len(a.registry.AllKinds()) == 0 {
		a.merged = builtinBytes
		return nil
	}

	// Extend the kind enum with plugin kinds.
	if err := a.extendKindEnum(schemaObj); err != nil {
		return err
	}

	// Add if-then blocks for plugin kinds in the allOf array.
	a.addPluginIfThenBlocks(schemaObj)

	// Allow plugin component kinds as layout children, grid cells and tree nodes.
	a.extendChildDefs(schemaObj)

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

	// Sorted so the merged schema, and which kind keeps a shared id, is stable.
	names := make([]string, 0, len(a.kindSchemas))
	for kindName := range a.kindSchemas {
		names = append(names, kindName)
	}
	sort.Strings(names)
	seen := make(map[string]bool, len(names))
	for _, kindName := range names {
		kindSchema := a.kindSchemas[kindName]
		var specSchema any
		if err := json.Unmarshal(kindSchema, &specSchema); err != nil {
			continue
		}
		if obj, ok := specSchema.(map[string]any); ok {
			ensureSchemaID(obj, kindName, seen)
			a.specPtrs[kindName] = fmt.Sprintf("#/allOf/%d/then/properties/spec", len(allOf))
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

// childDefs are the $defs that place a component inside a container. They
// share one shape: a kind enum and an allOf of if/then blocks.
var childDefs = []string{"layoutChild", "gridChild", "treeNode"}

// extendChildDefs adds every plugin component kind to each of childDefs: the
// kind enum and an if/then that validates an inline spec with the plugin's
// schema. With a ref, spec is only an override object.
func (a *SchemaAggregator) extendChildDefs(schemaObj map[string]any) {
	var names []string
	for _, k := range a.registry.AllKinds() {
		if k.Category == KindCategoryComponent {
			names = append(names, k.KindName)
		}
	}
	sort.Strings(names)

	blocks := make([]any, len(names))
	for i, name := range names {
		var specSchema any = map[string]any{"type": "object"}
		// Point at the copy in the top-level allOf: a second copy would
		// duplicate its id, and a local pointer is what the LSP can follow.
		if ptr, ok := a.specPtrs[name]; ok {
			specSchema = map[string]any{"$ref": ptr}
		} else if raw, ok := a.kindSchemas[name]; ok {
			var s any
			if json.Unmarshal(raw, &s) == nil {
				specSchema = s
			}
		}
		blocks[i] = map[string]any{
			"if": map[string]any{
				"properties": map[string]any{"kind": map[string]any{"const": name}},
				"required":   []any{"kind"},
			},
			"then": map[string]any{
				"if":   map[string]any{"required": []any{"ref"}},
				"then": map[string]any{"properties": map[string]any{"spec": map[string]any{"type": "object"}}},
				"else": map[string]any{
					"required":   []any{"spec"},
					"properties": map[string]any{"spec": specSchema},
				},
			},
		}
	}

	defs, _ := schemaObj["$defs"].(map[string]any)
	for _, def := range childDefs {
		child, _ := defs[def].(map[string]any)
		props, _ := child["properties"].(map[string]any)
		kindProp, _ := props["kind"].(map[string]any)
		enumSlice, ok := kindProp["enum"].([]any)
		if !ok {
			continue
		}
		allOf, _ := child["allOf"].([]any)
		for i, name := range names {
			enumSlice = append(enumSlice, name)
			allOf = append(allOf, blocks[i])
		}
		kindProp["enum"] = enumSlice
		child["allOf"] = allOf
	}
}

// FetchErrors returns the GetSchemas failures of the last Build. Those plugins'
// kinds are in the merged schema without spec validation.
func (a *SchemaAggregator) FetchErrors() error {
	return a.fetchErr
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

// KindNames returns every manifest kind (built-in + plugin) from the merged
// schema's kind enum.
func (a *SchemaAggregator) KindNames() []string {
	var doc struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
		} `json:"properties"`
	}
	if json.Unmarshal(a.MergedSchema(), &doc) != nil {
		return nil
	}
	return doc.Properties.Kind.Enum
}
