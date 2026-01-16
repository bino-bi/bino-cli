package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// DatasetList captures one or more dataset references declared in component specs.
// Each entry can be either a string reference to a named DataSet document or
// an inline DataSet definition. This enables inline definitions in ChartStructure,
// Table, and other component specs while maintaining backward compatibility.
type DatasetList struct {
	entries []DatasetRef
}

// UnmarshalJSON supports multiple input formats:
//   - Single string: "myDataset"
//   - Array of strings: ["ds1", "ds2"]
//   - Single inline object: { "query": "SELECT ...", "dependencies": [...] }
//   - Array of mixed: ["ds1", { "query": "..." }]
func (d *DatasetList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		d.entries = nil
		return nil
	}

	// Try single string first (most common case for backward compatibility)
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			d.entries = nil
			return nil
		}
		d.entries = []DatasetRef{{Ref: single}}
		return nil
	}

	// Try single inline object (for inline DataSet at top level)
	var singleRef DatasetRef
	if err := json.Unmarshal(data, &singleRef); err == nil {
		if singleRef.IsInline() {
			d.entries = []DatasetRef{singleRef}
			return nil
		}
	}

	// Try array (can be strings, inline objects, or mixed)
	var rawArray []json.RawMessage
	if err := json.Unmarshal(data, &rawArray); err != nil {
		return fmt.Errorf("dataset must be a string, inline object, or array: %w", err)
	}

	if len(rawArray) == 0 {
		d.entries = nil
		return nil
	}

	entries := make([]DatasetRef, 0, len(rawArray))
	for i, raw := range rawArray {
		var entry DatasetRef
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("dataset[%d]: %w", i, err)
		}

		// Skip empty string refs
		if entry.IsRef() && strings.TrimSpace(entry.Ref) == "" {
			continue
		}

		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		d.entries = nil
		return nil
	}

	d.entries = entries
	return nil
}

// MarshalJSON serializes the DatasetList back to JSON.
func (d DatasetList) MarshalJSON() ([]byte, error) {
	if len(d.entries) == 0 {
		return []byte("null"), nil
	}
	if len(d.entries) == 1 {
		return json.Marshal(d.entries[0])
	}
	return json.Marshal(d.entries)
}

// Strings returns a list of string references only.
// Inline definitions are not included; use Entries() to access those.
// This method provides backward compatibility with existing code.
func (d DatasetList) Strings() []string {
	if len(d.entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(d.entries))
	for _, entry := range d.entries {
		if entry.IsRef() {
			trimmed := strings.TrimSpace(entry.Ref)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Entries returns all entries including both string refs and inline definitions.
func (d DatasetList) Entries() []DatasetRef {
	if len(d.entries) == 0 {
		return nil
	}
	// Return a copy to prevent external modification
	out := make([]DatasetRef, len(d.entries))
	copy(out, d.entries)
	return out
}

// Join concatenates the string references using the provided separator.
// Inline definitions are not included in the output.
func (d DatasetList) Join(sep string) string {
	values := d.Strings()
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, sep)
}

// Empty reports whether the list contains any dataset entries.
func (d DatasetList) Empty() bool {
	return len(d.entries) == 0
}

// HasInline returns true if any entry is an inline definition.
func (d DatasetList) HasInline() bool {
	for _, entry := range d.entries {
		if entry.IsInline() {
			return true
		}
	}
	return false
}

// InlineCount returns the number of inline definitions in the list.
func (d DatasetList) InlineCount() int {
	count := 0
	for _, entry := range d.entries {
		if entry.IsInline() {
			count++
		}
	}
	return count
}

// SetResolvedNames replaces inline entries with string references to their
// generated names. This is called during materialization to convert inline
// definitions to references after synthetic documents are created.
func (d *DatasetList) SetResolvedNames(names []string) {
	if len(names) == 0 {
		return
	}
	d.entries = make([]DatasetRef, len(names))
	for i, name := range names {
		d.entries[i] = DatasetRef{Ref: name}
	}
}
