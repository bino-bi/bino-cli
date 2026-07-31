package spec

import (
	"strings"
	"testing"
)

func TestFriendlyDescription(t *testing.T) {
	cases := []struct{ in, want string }{
		{"got null, want string", "no value provided (expected string)"},
		{"got null, want array", "no value provided (expected array)"},
		{"got null, want object", "no value provided (expected object)"},
		// Everything else passes through verbatim — downstream parsers depend on it.
		{"missing property 'spec'", "missing property 'spec'"},
		{"got number, want string", "got number, want string"},
		{"additional properties 'foo' not allowed", "additional properties 'foo' not allowed"},
	}
	for _, tc := range cases {
		if got := friendlyDescription(tc.in); got != tc.want {
			t.Errorf("friendlyDescription(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHint(t *testing.T) {
	cases := []struct {
		name     string
		err      SchemaError
		contains string
	}{
		{
			name:     "null string value",
			err:      SchemaError{Field: "kind", Description: "no value provided (expected string)"},
			contains: "after the colon",
		},
		{
			name:     "null array value",
			err:      SchemaError{Field: "spec.scenarios", Description: "no value provided (expected array)"},
			contains: "- item",
		},
		{
			name:     "null object value",
			err:      SchemaError{Field: "spec", Description: "no value provided (expected object)"},
			contains: "key: value",
		},
		{
			name:     "missing kind",
			err:      SchemaError{Field: "kind", Description: "missing property 'kind'"},
			contains: "kind: <DocumentType>",
		},
		{
			name:     "missing other property",
			err:      SchemaError{Field: "spec.dataset", Description: "missing property 'dataset'"},
			contains: "required by the schema",
		},
		{
			name:     "non-null type mismatch",
			err:      SchemaError{Field: "spec.limit", Description: "got string, want number"},
			contains: "",
		},
		{
			name:     "want string mismatch",
			err:      SchemaError{Field: "spec.title", Description: "got number, want string"},
			contains: "quotes",
		},
		{
			name:     "additional property",
			err:      SchemaError{Field: "spec.bogus", Description: "additional properties 'bogus' not allowed"},
			contains: "check for typos",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Hint(tc.err)
			if tc.contains == "" {
				return // no assertion on wording, only that Hint doesn't panic
			}
			if !strings.Contains(got, tc.contains) {
				t.Errorf("Hint(%q) = %q, want it to mention %q", tc.err.Description, got, tc.contains)
			}
		})
	}
}
