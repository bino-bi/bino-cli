package spec

import (
	"encoding/json"
	"strings"
)

// StringOrSlice is a type that can unmarshal from either a string or an array of strings.
type StringOrSlice []string

// UnmarshalJSON implements json.Unmarshaler for StringOrSlice.
func (s *StringOrSlice) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a single string first
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = []string{single}
		return nil
	}

	// Try to unmarshal as an array of strings
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*s = arr
	return nil
}

// MarshalJSON implements json.Marshaler for StringOrSlice.
// Returns a single string if only one element, otherwise an array.
func (s StringOrSlice) MarshalJSON() ([]byte, error) {
	if len(s) == 1 {
		return json.Marshal(s[0])
	}
	return json.Marshal([]string(s))
}

// String returns the elements joined with commas.
func (s StringOrSlice) String() string {
	return strings.Join(s, ",")
}
