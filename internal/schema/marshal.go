package schema

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// MarshalYAML implements yaml.Marshaler for QueryField.
// If File is set, marshals as {"$file": path}; otherwise marshals as scalar string.
func (q QueryField) MarshalYAML() (any, error) {
	if q.File != "" {
		return map[string]string{"$file": q.File}, nil
	}
	return q.Inline, nil
}

// UnmarshalYAML implements yaml.Unmarshaler for QueryField.
// Handles both scalar strings (inline query) and maps with $file key.
func (q *QueryField) UnmarshalYAML(node *yaml.Node) error {
	// Handle null
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}

	// Try scalar string first (inline query)
	if node.Kind == yaml.ScalarNode {
		q.Inline = node.Value
		return nil
	}

	// Try map with $file key
	if node.Kind == yaml.MappingNode {
		var m map[string]string
		if err := node.Decode(&m); err != nil {
			return fmt.Errorf("invalid query field: %w", err)
		}
		if f, ok := m["$file"]; ok {
			q.File = f
			return nil
		}
		return fmt.Errorf("query map must have $file key")
	}

	return fmt.Errorf("query must be a string or {$file: path} map, got %v", node.Kind)
}

// MarshalYAML implements yaml.Marshaler for DataSourceRef.
// If Ref is set, marshals as scalar string; if Inline is set, marshals the spec.
func (d DataSourceRef) MarshalYAML() (any, error) {
	if d.Ref != "" {
		return d.Ref, nil
	}
	if d.Inline != nil {
		return d.Inline, nil
	}
	return nil, nil
}

// UnmarshalYAML implements yaml.Unmarshaler for DataSourceRef.
// Handles both scalar strings (reference) and maps (inline definition).
func (d *DataSourceRef) UnmarshalYAML(node *yaml.Node) error {
	// Handle null
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}

	// Try scalar string first (reference)
	if node.Kind == yaml.ScalarNode {
		d.Ref = node.Value
		return nil
	}

	// Try map (inline DataSource definition)
	if node.Kind == yaml.MappingNode {
		var spec DataSourceSpec
		if err := node.Decode(&spec); err != nil {
			return fmt.Errorf("invalid inline DataSource: %w", err)
		}
		d.Inline = &spec
		return nil
	}

	return fmt.Errorf("DataSource reference must be a string or inline definition, got %v", node.Kind)
}

// MarshalYAML implements yaml.Marshaler for DatasetRef.
// If Ref is set, marshals as scalar string; if Inline is set, marshals the spec.
func (d DatasetRef) MarshalYAML() (any, error) {
	if d.Ref != "" {
		return d.Ref, nil
	}
	if d.Inline != nil {
		return d.Inline, nil
	}
	return nil, nil
}

// UnmarshalYAML implements yaml.Unmarshaler for DatasetRef.
// Handles both scalar strings (reference) and maps (inline definition).
func (d *DatasetRef) UnmarshalYAML(node *yaml.Node) error {
	// Handle null
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}

	// Try scalar string first (reference)
	if node.Kind == yaml.ScalarNode {
		d.Ref = node.Value
		return nil
	}

	// Try map (inline DataSet definition)
	if node.Kind == yaml.MappingNode {
		var spec DataSetSpec
		if err := node.Decode(&spec); err != nil {
			return fmt.Errorf("invalid inline DataSet: %w", err)
		}
		d.Inline = &spec
		return nil
	}

	return fmt.Errorf("DataSet reference must be a string or inline definition, got %v", node.Kind)
}

// MarshalYAML implements yaml.Marshaler for FilterNode.
// It emits whichever member is set (group or leaf condition).
func (n FilterNode) MarshalYAML() (any, error) {
	if n.Group != nil {
		return n.Group, nil
	}
	if n.Leaf != nil {
		return n.Leaf, nil
	}
	return nil, nil
}

// UnmarshalYAML implements yaml.Unmarshaler for FilterNode.
// A mapping with a "conditions" key is a nested group; otherwise a leaf condition.
// The custom unmarshal lives only on FilterNode: decoding into FilterGroup
// recurses back into child FilterNodes and terminates at leaves.
func (n *FilterNode) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("filter node must be a mapping, got %v", node.Kind)
	}
	if yamlMappingHasKey(node, "conditions") {
		var g FilterGroup
		if err := node.Decode(&g); err != nil {
			return fmt.Errorf("invalid filter group: %w", err)
		}
		n.Group = &g
		return nil
	}
	var c FilterCondition
	if err := node.Decode(&c); err != nil {
		return fmt.Errorf("invalid filter condition: %w", err)
	}
	n.Leaf = &c
	return nil
}

// yamlMappingHasKey reports whether a YAML mapping node has the given key.
func yamlMappingHasKey(node *yaml.Node, key string) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// MarshalJSON implements json.Marshaler for FilterNode.
// It emits whichever member is set (group or leaf condition).
func (n FilterNode) MarshalJSON() ([]byte, error) {
	if n.Group != nil {
		return json.Marshal(n.Group)
	}
	if n.Leaf != nil {
		return json.Marshal(n.Leaf)
	}
	return []byte("null"), nil
}

// UnmarshalJSON implements json.Unmarshaler for FilterNode.
// An object with a "conditions" key is a nested group; otherwise a leaf condition.
func (n *FilterNode) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("filter node must be an object: %w", err)
	}
	if _, ok := probe["conditions"]; ok {
		var g FilterGroup
		if err := json.Unmarshal(data, &g); err != nil {
			return fmt.Errorf("invalid filter group: %w", err)
		}
		n.Group = &g
		return nil
	}
	var c FilterCondition
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("invalid filter condition: %w", err)
	}
	n.Leaf = &c
	return nil
}
