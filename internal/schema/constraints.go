package schema

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ConstraintItem represents a single constraint that can be either:
// - A string expression: "mode == preview", "labels.env in [dev,staging]"
// - A structured object: {field: "mode", operator: "==", value: "preview"}
type ConstraintItem struct {
	// String holds the constraint as a string expression (mutually exclusive with Structured).
	String string

	// Structured holds the constraint as a structured object (mutually exclusive with String).
	Structured *StructuredConstraint
}

// StructuredConstraint is the Go representation of a structured constraint object.
type StructuredConstraint struct {
	Field    string `yaml:"field" json:"field"`
	Operator string `yaml:"operator" json:"operator"`
	Value    any    `yaml:"value" json:"value"` // string, bool, or []string
}

// IsString returns true if this constraint is a string expression.
func (c ConstraintItem) IsString() bool {
	return c.Structured == nil
}

// IsStructured returns true if this constraint is a structured object.
func (c ConstraintItem) IsStructured() bool {
	return c.Structured != nil
}

// MarshalYAML implements yaml.Marshaler.
func (c ConstraintItem) MarshalYAML() (any, error) {
	if c.Structured != nil {
		return c.Structured, nil
	}
	return c.String, nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *ConstraintItem) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		c.String = node.Value
		c.Structured = nil
		return nil
	case yaml.MappingNode:
		var sc StructuredConstraint
		if err := node.Decode(&sc); err != nil {
			return fmt.Errorf("invalid structured constraint: %w", err)
		}
		c.Structured = &sc
		c.String = ""
		return nil
	default:
		return fmt.Errorf("constraint must be a string or object, got %v", node.Kind)
	}
}

// MarshalJSON implements json.Marshaler.
func (c ConstraintItem) MarshalJSON() ([]byte, error) {
	if c.Structured != nil {
		return json.Marshal(c.Structured)
	}
	return json.Marshal(c.String)
}

// UnmarshalJSON implements json.Unmarshaler.
func (c *ConstraintItem) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.String = s
		c.Structured = nil
		return nil
	}

	// Try structured constraint
	var sc StructuredConstraint
	if err := json.Unmarshal(data, &sc); err == nil {
		c.Structured = &sc
		c.String = ""
		return nil
	}

	return fmt.Errorf("constraint must be a string or object")
}

// ConstraintList is a list of constraints that can contain both string and structured formats.
type ConstraintList []ConstraintItem

// Strings returns all string constraints in the list.
// Structured constraints are skipped.
func (cl ConstraintList) Strings() []string {
	var result []string
	for _, c := range cl {
		if c.IsString() {
			result = append(result, c.String)
		}
	}
	return result
}

// ToAny converts the constraint list to []any for use with spec.ParseMixedConstraints.
func (cl ConstraintList) ToAny() []any {
	if len(cl) == 0 {
		return nil
	}
	result := make([]any, len(cl))
	for i, c := range cl {
		if c.IsStructured() {
			result[i] = map[string]any{
				"field":    c.Structured.Field,
				"operator": c.Structured.Operator,
				"value":    c.Structured.Value,
			}
		} else {
			result[i] = c.String
		}
	}
	return result
}

// FromStrings creates a ConstraintList from string constraints.
func ConstraintListFromStrings(strs []string) ConstraintList {
	if len(strs) == 0 {
		return nil
	}
	result := make(ConstraintList, len(strs))
	for i, s := range strs {
		result[i] = ConstraintItem{String: s}
	}
	return result
}
