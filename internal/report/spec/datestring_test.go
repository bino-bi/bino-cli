package spec

import (
	"encoding/json"
	"testing"
)

func TestDateString_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "date only",
			input:    `"2023-01-05"`,
			expected: "2023-01-05",
		},
		{
			name:     "datetime midnight UTC (YAML artifact)",
			input:    `"2023-01-05T00:00:00Z"`,
			expected: "2023-01-05",
		},
		{
			name:     "datetime with non-midnight time preserved",
			input:    `"2023-12-31T23:59:59Z"`,
			expected: "2023-12-31T23:59:59Z",
		},
		{
			name:     "datetime with afternoon time preserved",
			input:    `"2024-06-15T14:30:00Z"`,
			expected: "2024-06-15T14:30:00Z",
		},
		{
			name:     "null",
			input:    `null`,
			expected: "",
		},
		{
			name:     "empty string",
			input:    `""`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d DateString
			if err := json.Unmarshal([]byte(tt.input), &d); err != nil {
				t.Fatalf("UnmarshalJSON() error = %v", err)
			}
			if string(d) != tt.expected {
				t.Errorf("UnmarshalJSON() = %q, want %q", string(d), tt.expected)
			}
		})
	}
}

func TestDateString_InStruct(t *testing.T) {
	type TestSpec struct {
		DateStart DateString `json:"dateStart"`
		DateEnd   DateString `json:"dateEnd"`
	}

	t.Run("midnight UTC normalized to date-only", func(t *testing.T) {
		// Simulates YAML parsing unquoted date → time.Time → JSON datetime
		input := `{"dateStart": "2023-01-05T00:00:00Z", "dateEnd": "2023-12-31"}`

		var spec TestSpec
		if err := json.Unmarshal([]byte(input), &spec); err != nil {
			t.Fatalf("Unmarshal error = %v", err)
		}

		if spec.DateStart.String() != "2023-01-05" {
			t.Errorf("DateStart = %q, want %q", spec.DateStart.String(), "2023-01-05")
		}
		if spec.DateEnd.String() != "2023-12-31" {
			t.Errorf("DateEnd = %q, want %q", spec.DateEnd.String(), "2023-12-31")
		}
	})

	t.Run("non-midnight datetime preserved", func(t *testing.T) {
		input := `{"dateStart": "2024-01-01T08:00:00Z", "dateEnd": "2024-03-31T17:30:00Z"}`

		var spec TestSpec
		if err := json.Unmarshal([]byte(input), &spec); err != nil {
			t.Fatalf("Unmarshal error = %v", err)
		}

		if spec.DateStart.String() != "2024-01-01T08:00:00Z" {
			t.Errorf("DateStart = %q, want %q", spec.DateStart.String(), "2024-01-01T08:00:00Z")
		}
		if spec.DateEnd.String() != "2024-03-31T17:30:00Z" {
			t.Errorf("DateEnd = %q, want %q", spec.DateEnd.String(), "2024-03-31T17:30:00Z")
		}
	})
}

func TestNormalizeDateString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2023-01-05", "2023-01-05"},
		{"2023-01-05T00:00:00Z", "2023-01-05"},              // midnight UTC → date-only
		{"2023-12-31T23:59:59Z", "2023-12-31T23:59:59Z"},    // non-midnight preserved
		{"2024-06-15T14:30:00Z", "2024-06-15T14:30:00Z"},    // non-midnight preserved
		{"2024-01-01T08:00:00+01:00", "2024-01-01T08:00:00+01:00"}, // with offset preserved
		{"", ""},
		{"invalid", "invalid"},
		{"2023-01-05T", "2023-01-05T"},                       // T at position 10, not midnight → preserved
		{"2023-01-0T00:00:00Z", "2023-01-0T00:00:00Z"},      // Invalid date, T not at position 10
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeDateString(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeDateString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
