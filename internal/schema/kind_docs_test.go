package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestKindSpecDescriptions guards the per-kind prose the editor surfaces on
// kind completion/hover and MCP describe_kind: every built-in kind's spec
// definition must carry a top-level description. A future kind added without
// one fails here, not silently in the editor.
func TestKindSpecDescriptions(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(DocumentSchemaBytes(), &doc); err != nil {
		t.Fatal(err)
	}
	defs, _ := doc["$defs"].(map[string]any)
	blocks, _ := doc["allOf"].([]any)
	checked := 0
	for _, b := range blocks {
		block, _ := b.(map[string]any)
		kind := testKindConst(block)
		if kind == "" {
			continue
		}
		then, _ := block["then"].(map[string]any)
		props, _ := then["properties"].(map[string]any)
		spec, _ := props["spec"].(map[string]any)
		ref, _ := spec["$ref"].(string)
		if ref == "" {
			continue // inline conditional variants are covered by their $ref sibling block
		}
		name := strings.TrimPrefix(ref, "#/$defs/")
		def, _ := defs[name].(map[string]any)
		if def == nil {
			t.Errorf("kind %s: $defs/%s missing", kind, name)
			continue
		}
		if d, _ := def["description"].(string); strings.TrimSpace(d) == "" {
			t.Errorf("kind %s: $defs/%s lacks a description (kind completion, hover, and describe_kind surface it)", kind, name)
		}
		checked++
	}
	if checked < 20 {
		t.Fatalf("only %d kind blocks checked — did the schema shape change?", checked)
	}
}

func testKindConst(block map[string]any) string {
	ifClause, _ := block["if"].(map[string]any)
	props, _ := ifClause["properties"].(map[string]any)
	kind, _ := props["kind"].(map[string]any)
	c, _ := kind["const"].(string)
	return c
}
