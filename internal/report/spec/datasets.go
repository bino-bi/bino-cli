package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// DatasetList captures one or more dataset references declared in component specs.
type DatasetList []string

// UnmarshalJSON supports both string and string array inputs for dataset bindings.
func (d *DatasetList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*d = nil
		return nil
	}

	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			*d = nil
			return nil
		}
		*d = DatasetList{single}
		return nil
	}

	var multi []string
	if err := json.Unmarshal(data, &multi); err == nil {
		cleaned := normalizeDatasetValues(multi)
		if len(cleaned) == 0 {
			*d = nil
			return nil
		}
		*d = DatasetList(cleaned)
		return nil
	}

	return fmt.Errorf("dataset must be a string or an array of strings")
}

// Strings returns a defensive copy of the stored dataset references.
func (d DatasetList) Strings() []string {
	if len(d) == 0 {
		return nil
	}
	out := make([]string, 0, len(d))
	for _, value := range d {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Join concatenates the dataset references using the provided separator.
func (d DatasetList) Join(sep string) string {
	values := d.Strings()
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, sep)
}

// Empty reports whether the list contains any dataset references.
func (d DatasetList) Empty() bool {
	return len(d.Strings()) == 0
}

func normalizeDatasetValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}
