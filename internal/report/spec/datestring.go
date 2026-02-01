package spec

import (
	"bytes"
	"encoding/json"
	"strings"
)

// DateString captures a date value that may be specified as either a date string
// (YYYY-MM-DD) or a datetime string (YYYY-MM-DDTHH:MM:SSZ) in YAML/JSON.
// YAML parses unquoted dates like 2023-01-05 as time.Time objects, which
// marshal to datetime strings. This type normalizes both formats to date-only.
type DateString string

// UnmarshalJSON supports both date-only and datetime inputs, normalizing to date-only.
func (d *DateString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*d = ""
		return nil
	}

	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		// Fallback to raw string
		*d = DateString(string(data))
		return nil
	}

	// Normalize datetime to date-only (e.g., "2023-01-05T00:00:00Z" -> "2023-01-05")
	*d = DateString(NormalizeDateString(str))
	return nil
}

// MarshalJSON returns the date string as JSON.
func (d DateString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(d))
}

// String returns the underlying string value.
func (d DateString) String() string {
	return string(d)
}

// NormalizeDateString extracts the date portion from a datetime string.
// If the input contains a 'T' (ISO 8601 datetime), only the date part is returned.
// Otherwise, the input is returned as-is.
func NormalizeDateString(s string) string {
	if idx := strings.Index(s, "T"); idx == 10 {
		// Only truncate if T is at position 10 (after YYYY-MM-DD)
		return s[:10]
	}
	return s
}
