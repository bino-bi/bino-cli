package spec

import (
	"bytes"
	"encoding/json"

	"bino.bi/bino/internal/schema"
)

// ThereofList captures a list of thereof drilldown items that may be specified
// as either a JSON string or an array of objects in YAML/JSON.
type ThereofList []schema.ThereofItem

// UnmarshalJSON supports both string (JSON-encoded) and array inputs.
func (t *ThereofList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*t = nil
		return nil
	}

	// Try to unmarshal as an array of objects first.
	var items []schema.ThereofItem
	if err := json.Unmarshal(data, &items); err == nil {
		*t = items
		return nil
	}

	// Try to unmarshal as a string containing JSON.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if str == "" {
			*t = nil
			return nil
		}
		// Parse the string as JSON array.
		var parsed []schema.ThereofItem
		if err := json.Unmarshal([]byte(str), &parsed); err == nil {
			*t = parsed
			return nil
		}
		// Keep the raw string value as a single-item placeholder if parsing fails.
		*t = nil
		return nil
	}

	*t = nil
	return nil
}

// String returns the JSON string representation of the list.
func (t ThereofList) String() string {
	if len(t) == 0 {
		return ""
	}
	data, err := json.Marshal(t)
	if err != nil {
		return ""
	}
	return string(data)
}

// PartofList captures a list of partof items that may be specified
// as either a JSON string or an array of objects in YAML/JSON.
type PartofList []schema.PartofItem

// UnmarshalJSON supports both string (JSON-encoded) and array inputs.
func (p *PartofList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*p = nil
		return nil
	}

	// Try to unmarshal as an array of objects first.
	var items []schema.PartofItem
	if err := json.Unmarshal(data, &items); err == nil {
		*p = items
		return nil
	}

	// Try to unmarshal as a string containing JSON.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if str == "" {
			*p = nil
			return nil
		}
		// Parse the string as JSON array.
		var parsed []schema.PartofItem
		if err := json.Unmarshal([]byte(str), &parsed); err == nil {
			*p = parsed
			return nil
		}
		*p = nil
		return nil
	}

	*p = nil
	return nil
}

// String returns the JSON string representation of the list.
func (p PartofList) String() string {
	if len(p) == 0 {
		return ""
	}
	data, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(data)
}

// ColumnthereofList captures a list of columnthereof items that may be specified
// as either a JSON string or an array of objects in YAML/JSON.
type ColumnthereofList []schema.ColumnthereofItem

// UnmarshalJSON supports both string (JSON-encoded) and array inputs.
func (c *ColumnthereofList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*c = nil
		return nil
	}

	// Try to unmarshal as an array of objects first.
	var items []schema.ColumnthereofItem
	if err := json.Unmarshal(data, &items); err == nil {
		*c = items
		return nil
	}

	// Try to unmarshal as a string containing JSON.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if str == "" {
			*c = nil
			return nil
		}
		// Parse the string as JSON array.
		var parsed []schema.ColumnthereofItem
		if err := json.Unmarshal([]byte(str), &parsed); err == nil {
			*c = parsed
			return nil
		}
		*c = nil
		return nil
	}

	*c = nil
	return nil
}

// String returns the JSON string representation of the list.
func (c ColumnthereofList) String() string {
	if len(c) == 0 {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(data)
}
