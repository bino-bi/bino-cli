package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractSpecSchemaDropsAllOfRefs(t *testing.T) {
	merged := json.RawMessage(`{
		"$defs": {"layoutChild": {"allOf": [{"then": {"else": {"properties": {"spec": {"$ref": "#/allOf/1/then/properties/spec"}}}}}]}},
		"allOf": [
			{"if": {"properties": {"kind": {"const": "LayoutPage"}}}, "then": {"properties": {"spec": {"properties": {"children": {"items": {"$ref": "#/$defs/layoutChild"}}}}}}},
			{"if": {"properties": {"kind": {"const": "Gauge"}}}, "then": {"properties": {"spec": {"$id": "https://bino.bi/schemas/plugins/Gauge.json"}}}}
		]
	}`)
	raw, ok := extractSpecSchema(merged, "LayoutPage")
	if !ok {
		t.Fatal("LayoutPage spec not found")
	}
	if strings.Contains(string(raw), "#/allOf/") {
		t.Errorf("extracted schema keeps a pointer into the root allOf: %s", raw)
	}
	if !strings.Contains(string(raw), "#/$defs/layoutChild") {
		t.Errorf("extracted schema lost its $defs refs: %s", raw)
	}
}
