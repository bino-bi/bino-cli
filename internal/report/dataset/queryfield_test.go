package dataset

import (
	"encoding/json"
	"testing"
)

// Regression: the executor redeclared the query-field union type without the
// null/empty guards spec.QueryField has, so `query: {}` unmarshalled to an
// empty field and read as "absent" instead of "malformed".
func TestParseDataSetSpecRejectsMalformedQuery(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"empty object", `{"spec": {"query": {}}}`},
		{"empty $file", `{"spec": {"query": {"$file": ""}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseDataSetSpec(json.RawMessage(tc.raw)); err == nil {
				t.Errorf("parseDataSetSpec accepted a malformed query field: %s", tc.raw)
			}
		})
	}
}
