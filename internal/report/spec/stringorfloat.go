package spec

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// StringOrFloat captures a value that may be specified as either a string or a
// floating-point number in YAML/JSON. Internally it stores the value as a string.
type StringOrFloat string

// UnmarshalJSON supports both string and numeric (float) inputs.
func (s *StringOrFloat) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}

	// Try to unmarshal as a string first.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = StringOrFloat(str)
		return nil
	}

	// Try to unmarshal as a number.
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*s = StringOrFloat(strconv.FormatFloat(num, 'f', -1, 64))
		return nil
	}

	// Return the string representation of the raw value as a fallback.
	*s = StringOrFloat(string(data))
	return nil
}

// String returns the underlying string value.
func (s StringOrFloat) String() string {
	return string(s)
}
