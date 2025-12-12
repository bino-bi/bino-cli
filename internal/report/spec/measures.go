package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Measure represents a single measure with a name and unit.
type Measure struct {
	Name string `json:"name"`
	Unit string `json:"unit"`
}

// MeasureList captures one or more measures declared in component specs.
// It supports unmarshaling from either a JSON string or a YAML/JSON array.
type MeasureList []Measure

// UnmarshalJSON supports both string and array inputs for measure bindings.
func (m *MeasureList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*m = nil
		return nil
	}

	// Try parsing as a JSON string first (the string contains a JSON array).
	var jsonStr string
	if err := json.Unmarshal(data, &jsonStr); err == nil {
		jsonStr = string(bytes.TrimSpace([]byte(jsonStr)))
		if jsonStr == "" {
			*m = nil
			return nil
		}
		// Parse the string content as an array of measures.
		var measures []Measure
		if err := json.Unmarshal([]byte(jsonStr), &measures); err != nil {
			return fmt.Errorf("invalid measure JSON string: %w", err)
		}
		*m = measures
		return nil
	}

	// Try parsing as a direct array of measure objects.
	var measures []Measure
	if err := json.Unmarshal(data, &measures); err == nil {
		*m = measures
		return nil
	}

	return fmt.Errorf("measures must be a JSON string or an array of measure objects")
}

// String returns the JSON string representation of the measure list.
func (m MeasureList) String() string {
	if len(m) == 0 {
		return ""
	}
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}

// Empty reports whether the list contains any measures.
func (m MeasureList) Empty() bool {
	return len(m) == 0
}
