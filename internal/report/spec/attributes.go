package spec

import (
	"bytes"
	"encoding/json"
	"strings"

	"bino.bi/bino/internal/schema"
)

// AttributesList captures the table `attributes` prop. Authors write either
// an ordered YAML/JSON array of {label, expression} objects, or a raw JSON
// object string. The string form is kept verbatim (only checked to parse as
// a JSON object) because decoding into a Go map would destroy key order,
// which the template engine uses as column order.
type AttributesList struct {
	Items []schema.AttributeItem
	Raw   string
}

// UnmarshalJSON supports both array and string (JSON-object) inputs.
func (a *AttributesList) UnmarshalJSON(data []byte) error {
	*a = AttributesList{}

	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	// Try to unmarshal as an array of objects first.
	var items []schema.AttributeItem
	if err := json.Unmarshal(data, &items); err == nil {
		a.Items = items
		return nil
	}

	// Try to unmarshal as a string containing a JSON object.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		trimmed := strings.TrimSpace(str)
		if !strings.HasPrefix(trimmed, "{") {
			return nil
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(str), &probe); err == nil {
			// Keep the original string untouched to preserve key order.
			a.Raw = str
		}
		return nil
	}

	return nil
}

// MarshalJSON mirrors the accepted input forms instead of the struct shape.
func (a AttributesList) MarshalJSON() ([]byte, error) {
	if a.Raw != "" {
		return json.Marshal(a.Raw)
	}
	if a.Items != nil {
		return json.Marshal(a.Items)
	}
	return []byte("null"), nil
}

// String returns the JSON object string representation in item order.
func (a AttributesList) String() string {
	if a.Raw != "" {
		return a.Raw
	}
	if len(a.Items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, item := range a.Items {
		if i > 0 {
			b.WriteByte(',')
		}
		label, err := json.Marshal(item.Label)
		if err != nil {
			return ""
		}
		expression, err := json.Marshal(item.Expression)
		if err != nil {
			return ""
		}
		b.Write(label)
		b.WriteByte(':')
		b.Write(expression)
	}
	b.WriteByte('}')
	return b.String()
}
