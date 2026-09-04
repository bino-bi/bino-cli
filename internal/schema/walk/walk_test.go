package walk

import (
	"encoding/json"
	"slices"
	"sort"
	"testing"

	"bino.bi/bino/internal/schema"
)

// miniSchema mirrors every composition shape the real document schema uses:
// root if/then kind discrimination, $defs with an allOf base, a layoutChild-style
// child-kind conditional with a nested non-kind conditional, oneOf
// string-or-array enums, additionalProperties maps, and a recursive $ref.
const miniSchema = `{
  "properties": {
    "apiVersion": {"const": "bino.bi/v1alpha1"},
    "kind": {"enum": ["Table", "LayoutPage", "I18n"]},
    "metadata": {"properties": {"name": {"description": "Unique name."}}, "required": ["name"]},
    "spec": {}
  },
  "required": ["apiVersion", "kind", "metadata", "spec"],
  "$defs": {
    "tableBase": {
      "type": "object",
      "properties": {
        "title": {"description": "The table title.", "type": "string"},
        "order": {"oneOf": [
          {"enum": ["category", "auto"]},
          {"type": "array", "items": {"enum": ["category", "auto"]}}
        ]},
        "grouped": {"type": "boolean", "default": false},
        "scale": {"type": "string", "default": "auto"}
      }
    },
    "tableSpec": {"allOf": [{"$ref": "#/$defs/tableBase"}], "required": ["dataset"],
      "properties": {"dataset": {"description": "Bound dataset."}}},
    "layoutChild": {
      "type": "object",
      "required": ["kind"],
      "properties": {
        "kind": {"enum": ["Text", "Table"], "description": "The child component type."},
        "ref": {"type": "string"},
        "optional": {"type": "boolean"},
        "spec": {}
      },
      "allOf": [
        {"if": {"properties": {"kind": {"const": "Table"}}},
         "then": {
           "if": {"required": ["ref"]},
           "then": {"properties": {"spec": {"$ref": "#/$defs/tableBase"}}},
           "else": {"properties": {"spec": {"$ref": "#/$defs/tableSpec"}}}
         }}
      ]
    },
    "layoutPageSpec": {
      "type": "object",
      "properties": {
        "pageFormat": {"enum": ["a4", "xga"], "default": "xga"},
        "children": {"type": "array", "items": {"$ref": "#/$defs/layoutChild"}}
      }
    },
    "i18nSpec": {
      "type": "object",
      "properties": {"code": {"type": "string"}},
      "additionalProperties": {"type": "string", "description": "A translation entry."}
    }
  },
  "allOf": [
    {"if": {"properties": {"kind": {"const": "Table"}}},
     "then": {"properties": {"spec": {"$ref": "#/$defs/tableSpec"}}}},
    {"if": {"properties": {"kind": {"const": "LayoutPage"}}},
     "then": {"properties": {"spec": {"$ref": "#/$defs/layoutPageSpec"}}}},
    {"if": {"properties": {"kind": {"const": "I18n"}}},
     "then": {"properties": {"spec": {"$ref": "#/$defs/i18nSpec"}}}}
  ]
}`

func miniModel(t *testing.T) *Model {
	t.Helper()
	m := Parse(json.RawMessage(miniSchema))
	if m.Empty() {
		t.Fatal("mini schema failed to parse")
	}
	return m
}

func propNames(props []PropInfo) []string {
	names := make([]string, len(props))
	for i, p := range props {
		names[i] = p.Name
	}
	sort.Strings(names)
	return names
}

func hasString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestResolveAt_RootProps(t *testing.T) {
	m := miniModel(t)
	node := m.ResolveAt(nil, map[string]string{"": "Table"})
	props := node.Props()
	names := propNames(props)
	for _, want := range []string{"apiVersion", "kind", "metadata", "spec"} {
		if !hasString(names, want) {
			t.Errorf("root props missing %q (got %v)", want, names)
		}
	}
	for _, p := range props {
		if p.Name == "kind" && !p.Required {
			t.Error("kind must be marked required at the root")
		}
		if p.Name == "apiVersion" && !hasString(p.Enum, "bino.bi/v1alpha1") {
			t.Errorf("apiVersion const must surface as a one-value enum, got %v", p.Enum)
		}
	}
}

func TestResolveAt_MetadataProps(t *testing.T) {
	m := miniModel(t)
	props := m.ResolveAt([]string{"metadata"}, map[string]string{"": "Table"}).Props()
	if len(props) != 1 || props[0].Name != "name" || !props[0].Required {
		t.Fatalf("metadata props = %+v, want the required name field", props)
	}
}

func TestResolveAt_SpecPropsMergeAllOfBaseAndRequired(t *testing.T) {
	m := miniModel(t)
	props := m.ResolveAt([]string{"spec"}, map[string]string{"": "Table"}).Props()
	names := propNames(props)
	for _, want := range []string{"dataset", "title", "order", "grouped", "scale"} {
		if !hasString(names, want) {
			t.Errorf("Table spec props missing %q (got %v)", want, names)
		}
	}
	for _, p := range props {
		switch p.Name {
		case "dataset":
			if !p.Required {
				t.Error("dataset must be required (declared on tableSpec, not the base)")
			}
		case "grouped":
			if p.Type != "boolean" || p.Default != false {
				t.Errorf("grouped metadata lost: type=%q default=%v", p.Type, p.Default)
			}
		case "scale":
			if p.Default != "auto" {
				t.Errorf("scale default lost: %v", p.Default)
			}
		}
	}
}

func TestResolveAt_OneOfEnumUnion(t *testing.T) {
	m := miniModel(t)
	vals := m.ResolveAt([]string{"spec", "order"}, map[string]string{"": "Table"}).EnumValues()
	if !hasString(vals, "category") || !hasString(vals, "auto") {
		t.Fatalf("oneOf-wrapped enum (string or array-of) must union, got %v", vals)
	}
}

func TestResolveAt_LayoutChildKindEnum(t *testing.T) {
	m := miniModel(t)
	kinds := map[string]string{"": "LayoutPage"}
	vals := m.ResolveAt([]string{"spec", "children", "0", "kind"}, kinds).EnumValues()
	if !hasString(vals, "Text") || !hasString(vals, "Table") {
		t.Fatalf("layout child kind enum unreachable, got %v", vals)
	}
	// The same resolution with a partially-typed child kind ("Ta") must still
	// reach the enum — the conditional simply matches no branch.
	kinds["spec.children.0"] = "Ta"
	vals = m.ResolveAt([]string{"spec", "children", "0", "kind"}, kinds).EnumValues()
	if !hasString(vals, "Table") {
		t.Fatalf("mid-word child kind broke enum resolution, got %v", vals)
	}
}

func TestResolveAt_LayoutChildKeys(t *testing.T) {
	m := miniModel(t)
	kinds := map[string]string{"": "LayoutPage", "spec.children.0": "Table"}
	node := m.ResolveAt([]string{"spec", "children", "0"}, kinds)
	if !node.IsObject() {
		t.Fatal("layout child position must be object-shaped")
	}
	names := propNames(node.Props())
	for _, want := range []string{"kind", "ref", "optional", "spec"} {
		if !hasString(names, want) {
			t.Errorf("child keys missing %q (got %v)", want, names)
		}
	}
	if hasString(names, "dataset") {
		t.Errorf("top-level spec fields leaked into the child key position: %v", names)
	}
}

func TestResolveAt_NestedChildSpecUnionsConditional(t *testing.T) {
	// The child's spec resolves through the nested non-kind conditional (ref
	// present vs not) — both branches union, so tableBase fields appear.
	m := miniModel(t)
	kinds := map[string]string{"": "LayoutPage", "spec.children.0": "Table"}
	props := m.ResolveAt([]string{"spec", "children", "0", "spec"}, kinds).Props()
	names := propNames(props)
	if !hasString(names, "title") || !hasString(names, "dataset") {
		t.Fatalf("nested child spec props incomplete, got %v", names)
	}
}

func TestResolveAt_AdditionalProperties(t *testing.T) {
	m := miniModel(t)
	node := m.ResolveAt([]string{"spec", "de_DE"}, map[string]string{"": "I18n"})
	if node.Doc() != "A translation entry." {
		t.Fatalf("additionalProperties fallback failed, doc=%q", node.Doc())
	}
}

func TestResolveAt_UnknownKindOffersNoSpecFields(t *testing.T) {
	m := miniModel(t)
	props := m.ResolveAt([]string{"spec"}, map[string]string{}).Props()
	if len(props) != 0 {
		t.Fatalf("spec with unknown kind must offer nothing (not every kind's fields), got %v", propNames(props))
	}
}

func TestResolveAt_BoolAndCycleSafety(t *testing.T) {
	m := miniModel(t)
	kinds := map[string]string{"": "LayoutPage"}
	if !m.ResolveAt([]string{"spec", "children", "0", "optional"}, kinds).IsBool() {
		t.Error("optional must resolve as boolean")
	}
	// Degenerate schemas must not hang or panic.
	cyclic := Parse(json.RawMessage(`{"$defs": {"a": {"$ref": "#/$defs/b"}, "b": {"$ref": "#/$defs/a"}}, "properties": {"x": {"$ref": "#/$defs/a"}}}`))
	_ = cyclic.ResolveAt([]string{"x", "y"}, nil)
}

// TestResolveAt_RealSchema locks the resolver to the shipped document schema —
// the shapes the fixtures mirror must actually hold in the real file.
func TestResolveAt_RealSchema(t *testing.T) {
	m := Parse(schema.DocumentSchemaBytes())
	if m.Empty() {
		t.Fatal("real schema failed to parse")
	}

	t.Run("layout child kind enum", func(t *testing.T) {
		kinds := map[string]string{"": "LayoutPage"}
		vals := m.ResolveAt([]string{"spec", "children", "0", "kind"}, kinds).EnumValues()
		for _, want := range []string{"Text", "Table", "ChartTime", "Tree", "Grid", "LayoutCard", "Image"} {
			if !hasString(vals, want) {
				t.Errorf("child kind enum missing %q (got %v)", want, vals)
			}
		}
	})

	t.Run("child key completion", func(t *testing.T) {
		kinds := map[string]string{"": "LayoutPage", "spec.children.0": "Table"}
		names := propNames(m.ResolveAt([]string{"spec", "children", "0"}, kinds).Props())
		for _, want := range []string{"kind", "ref", "optional", "params", "spec"} {
			if !hasString(names, want) {
				t.Errorf("child keys missing %q (got %v)", want, names)
			}
		}
	})

	t.Run("table spec fields with required dataset", func(t *testing.T) {
		props := m.ResolveAt([]string{"spec"}, map[string]string{"": "Table"}).Props()
		var hasDataset, hasSumTitle bool
		for _, p := range props {
			if p.Name == "dataset" {
				hasDataset = true
				if !p.Required {
					t.Error("Table spec dataset must be required")
				}
			}
			if p.Name == "sumTitle" {
				hasSumTitle = true
			}
		}
		if !hasDataset || !hasSumTitle {
			t.Errorf("Table spec fields incomplete (dataset=%v sumTitle=%v)", hasDataset, hasSumTitle)
		}
	})

	t.Run("root and metadata props", func(t *testing.T) {
		rootNames := propNames(m.ResolveAt(nil, map[string]string{"": "Table"}).Props())
		for _, want := range []string{"apiVersion", "kind", "metadata", "spec"} {
			if !hasString(rootNames, want) {
				t.Errorf("root props missing %q (got %v)", want, rootNames)
			}
		}
		metaNames := propNames(m.ResolveAt([]string{"metadata"}, map[string]string{"": "Table"}).Props())
		if !hasString(metaNames, "name") {
			t.Errorf("metadata props missing name (got %v)", metaNames)
		}
		if hasString(metaNames, "dataset") {
			t.Errorf("spec fields leaked into metadata (got %v)", metaNames)
		}
	})

	t.Run("oneOf enum union on table scale", func(t *testing.T) {
		props := m.ResolveAt([]string{"spec"}, map[string]string{"": "Table"}).Props()
		for _, p := range props {
			if p.Name != "scale" {
				continue
			}
			if len(p.Enum) == 0 {
				t.Errorf("scale enum lost through oneOf composition: %+v", p)
			}
			return
		}
		t.Error("scale prop not found on Table spec")
	})
}

// TestParse_EmbeddedSchemaKinds locks the walker to the embedded schema: the
// MCP server parses exactly these bytes to project a kind's spec.
func TestParse_EmbeddedSchemaKinds(t *testing.T) {
	if kinds := Parse(schema.DocumentSchemaBytes()).Kinds(); len(kinds) == 0 {
		t.Fatal("embedded schema yields no kinds")
	}
}

func TestProps_TypesUnionAcrossVariants(t *testing.T) {
	m := miniModel(t)
	for _, p := range m.ResolveAt([]string{"spec"}, map[string]string{"": "Table"}).Props() {
		if p.Name == "grouped" && !slices.Equal(p.Types, []string{"boolean"}) {
			t.Errorf("grouped Types = %v, want [boolean]", p.Types)
		}
	}
	full := Parse(schema.DocumentSchemaBytes())
	for _, p := range full.ResolveAt([]string{"spec"}, map[string]string{"": "Table"}).Props() {
		if p.Name != "dataset" {
			continue
		}
		if len(p.Types) < 2 || p.Type != p.Types[0] {
			t.Errorf("dataset Types = %v (Type %q): want the union of the oneOf branches, first one matching Type", p.Types, p.Type)
		}
	}
}

func TestIsMap(t *testing.T) {
	m := miniModel(t)
	if !m.ResolveAt([]string{"spec"}, map[string]string{"": "I18n"}).IsMap() {
		t.Error("i18n spec (additionalProperties) must be map-shaped")
	}
	if m.ResolveAt([]string{"spec"}, map[string]string{"": "Table"}).IsMap() {
		t.Error("Table spec must not be map-shaped")
	}
}
