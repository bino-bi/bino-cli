package spec

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The Design-mode IBCS widgets (edges, attributes, thereof, partof,
// columnthereof) post their value as a JSON array of objects whose intra-object
// key order is meaningful and must survive the edit. Decoding that JSON into a
// map[string]any (as the edit path once did) alphabetizes each object's keys, so
// a no-op flips e.g. label,expression -> expression,label and re-quotes scalars.
// These tests pin the order-preserving JSON edit boundary (DecodeJSONValue) the
// widgets rely on.

// editWidgetField applies a widget's JSON array value to a Table field through
// the same boundary the lsp-helper edit path uses: JSON -> ordered yaml.Node ->
// EditYAMLDocument, returning the edited document.
func editWidgetField(t *testing.T, content, field, valueJSON string) string {
	t.Helper()
	node, err := DecodeJSONValue([]byte(valueJSON))
	if err != nil {
		t.Fatalf("DecodeJSONValue: %v", err)
	}
	_, edited, err := EditYAMLDocument(content, 1, map[string]any{field: node})
	if err != nil {
		t.Fatalf("EditYAMLDocument: %v", err)
	}
	return edited
}

// readFieldAsJSON resolves a dotted spec field in the (single-document) content
// and serializes its yaml.Node value to JSON with object key order preserved,
// mirroring how the designer webview reads a value out of the document. It is the
// inverse of DecodeJSONValue for the round-trip assertions.
func readFieldAsJSON(t *testing.T, content, field string) string {
	t.Helper()
	nodes, err := ParseYAMLNodes(content)
	if err != nil || len(nodes) == 0 {
		t.Fatalf("parse edited doc: %v", err)
	}
	var n *yaml.Node
	cur := nodes[0]
	for _, seg := range strings.Split(field, ".") {
		n = mapValue(cur, seg)
		if n == nil {
			t.Fatalf("field %q not found in:\n%s", field, content)
		}
		cur = n
	}
	var b strings.Builder
	nodeToOrderedJSON(&b, n)
	return b.String()
}

// nodeToOrderedJSON writes a yaml.Node as JSON, emitting mapping keys in node
// order (Go's json.Marshal of a map would sort them, defeating the test).
func nodeToOrderedJSON(b *strings.Builder, n *yaml.Node) {
	switch n.Kind {
	case yaml.MappingNode:
		b.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			key, _ := json.Marshal(n.Content[i].Value)
			b.Write(key)
			b.WriteByte(':')
			nodeToOrderedJSON(b, n.Content[i+1])
		}
		b.WriteByte('}')
	case yaml.SequenceNode:
		b.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				b.WriteByte(',')
			}
			nodeToOrderedJSON(b, c)
		}
		b.WriteByte(']')
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!int":
			b.WriteString(n.Value)
		case "!!float":
			b.WriteString(n.Value)
		case "!!bool":
			b.WriteString(strconv.FormatBool(n.Value == "true"))
		case "!!null":
			b.WriteString("null")
		default:
			s, _ := json.Marshal(n.Value)
			b.Write(s)
		}
	default:
		b.WriteString("null")
	}
}

func TestDecodeJSONValuePreservesObjectKeyOrder(t *testing.T) {
	base := "apiVersion: bino.bi/v1alpha1\nkind: Table\nmetadata:\n  name: t\nspec:\n  dataset: sales\n"

	tests := []struct {
		name      string
		field     string
		valueJSON string
		// wantOrder is the verbatim block the field must render to (sets key order
		// and natural scalar styling). Indented two spaces under spec.
		wantBlock string
	}{
		{
			name:      "attributes keeps label before expression",
			field:     "spec.attributes",
			valueJSON: `[{"label":"Net Sales","expression":"sum(net_sales)"}]`,
			wantBlock: "  attributes:\n    - label: Net Sales\n      expression: sum(net_sales)\n",
		},
		{
			name:      "edges keep from,to,operator,label,style{color,width,dasharray}",
			field:     "spec.edges",
			valueJSON: `[{"from":"a","to":"b","operator":"+","label":"x","style":{"color":"#ff0000","width":2,"dasharray":"4 2"}}]`,
			wantBlock: "  edges:\n    - from: a\n      to: b\n      operator: +\n      label: x\n      style:\n        color: '#ff0000'\n        width: 2\n        dasharray: 4 2\n",
		},
		{
			name:      "thereof keeps rowGroup,category,subCategory",
			field:     "spec.thereof",
			valueJSON: `[{"rowGroup":"g","category":"c","subCategory":"s"}]`,
			wantBlock: "  thereof:\n    - rowGroup: g\n      category: c\n      subCategory: s\n",
		},
		{
			name:      "partof keeps rowGroup,category",
			field:     "spec.partof",
			valueJSON: `[{"rowGroup":"g","category":"c"}]`,
			wantBlock: "  partof:\n    - rowGroup: g\n      category: c\n",
		},
		{
			name:      "columnthereof keeps scenario,name,subGroups",
			field:     "spec.columnthereof",
			valueJSON: `[{"scenario":"ac","name":"n","subGroups":["x","y"]}]`,
			wantBlock: "  columnthereof:\n    - scenario: ac\n      name: n\n      subGroups:\n        - x\n        - y\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edited := editWidgetField(t, base, tt.field, tt.valueJSON)
			if !strings.Contains(edited, tt.wantBlock) {
				t.Errorf("key order/style not preserved:\n--- got ---\n%s\n--- want block ---\n%s", edited, tt.wantBlock)
			}
		})
	}
}

func TestDecodeJSONValueByteEquivalentNoOp(t *testing.T) {
	// A no-op (read the value back, write it unchanged) must round-trip byte-for-
	// byte: the on-disk form is itself produced by this engine, so its natural
	// styling is idempotent. We assert the second write equals the first.
	base := "apiVersion: bino.bi/v1alpha1\nkind: Table\nmetadata:\n  name: t\nspec:\n  dataset: sales\n"

	fields := []struct {
		field     string
		valueJSON string
	}{
		{"spec.attributes", `[{"label":"Net Sales","expression":"sum(net_sales)"},{"label":"Margin","expression":"div(margin,net_sales)"}]`},
		{"spec.edges", `[{"from":"a","to":"b","operator":"+","label":"x","style":{"color":"#ff0000","width":2,"dasharray":"4 2"}},{"from":"c","to":"d","operator":"-"}]`},
		{"spec.thereof", `[{"rowGroup":"g","category":"c","subCategory":"s"}]`},
		{"spec.partof", `[{"rowGroup":"g","category":"c"}]`},
		{"spec.columnthereof", `[{"scenario":"ac","name":"n","subGroups":["x","y"]}]`},
	}

	for _, f := range fields {
		t.Run(f.field, func(t *testing.T) {
			// First write: establishes the on-disk form.
			first := editWidgetField(t, base, f.field, f.valueJSON)
			// Read the field back into JSON exactly as a webview would (order +
			// values preserved), then write it again — must be byte-identical.
			readback := readFieldAsJSON(t, first, f.field)
			second := editWidgetField(t, base, f.field, readback)
			if first != second {
				t.Errorf("no-op not byte-identical:\n--- first ---\n%s\n--- second ---\n%s\n--- readback JSON ---\n%s", first, second, readback)
			}
		})
	}
}
