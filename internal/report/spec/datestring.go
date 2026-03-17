package spec

import (
	"bytes"
	"encoding/json"
	"strings"
)

// DateString captures a date value that may be specified as either a date string
// (YYYY-MM-DD) or a datetime string (YYYY-MM-DDTHH:MM:SSZ) in YAML/JSON.
// YAML parses unquoted dates like 2023-01-05 as time.Time objects, which
// marshal to datetime strings with midnight UTC (T00:00:00Z). This type strips
// the midnight artifact but preserves real datetime values (e.g. T14:30:00Z).
type DateString string

// UnmarshalJSON supports both date-only and datetime inputs.
// Midnight UTC datetimes (from YAML date parsing) are normalized to date-only.
// Non-midnight datetimes are preserved as-is.
func (d *DateString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*d = ""
		return nil
	}

	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		*d = DateString(string(data))
		return nil //nolint:nilerr // fallback to raw string on parse failure
	}

	// Normalize only midnight UTC datetimes (YAML artifact) to date-only.
	// Real datetime values are preserved.
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

// NormalizeDateString normalizes a date or datetime string.
// Midnight UTC datetimes (T00:00:00Z) are stripped to date-only since these
// are artifacts of YAML parsing unquoted dates. Non-midnight datetimes are
// preserved to support real datetime values.
func NormalizeDateString(s string) string {
	if idx := strings.Index(s, "T"); idx == 10 {
		// Only strip midnight UTC (YAML artifact: 2023-01-05 → 2023-01-05T00:00:00Z)
		if s[idx:] == "T00:00:00Z" {
			return s[:10]
		}
		// Preserve real datetime values (e.g. 2023-01-05T14:30:00Z)
	}
	return s
}
