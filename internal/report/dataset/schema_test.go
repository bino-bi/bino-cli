package dataset

import "testing"

func TestStandardColumnsCanonical(t *testing.T) {
	cols := StandardColumns()
	if len(cols) != 29 {
		t.Fatalf("StandardColumns() = %d columns, want 29", len(cols))
	}

	byName := map[string]StandardColumn{}
	for _, c := range cols {
		byName[c.Name] = c
	}

	// Spot-check classification and grouping.
	checks := []struct {
		name  string
		kind  ColumnKind
		group string
		pair  string
	}{
		{"ac1", ColumnNumber, "Measures", ""},
		{"pl4", ColumnNumber, "Measures", ""},
		{"category", ColumnString, "Dimensions", "categoryIndex"},
		{"categoryIndex", ColumnNumber, "Dimensions", "category"},
		{"columnSubGroup", ColumnString, "Dimensions", "columnSubGroupIndex"},
		{"operation", ColumnString, "Metadata", ""},
		{"setname", ColumnString, "Metadata", ""},
	}
	for _, c := range checks {
		got, ok := byName[c.name]
		if !ok {
			t.Errorf("missing column %q", c.name)
			continue
		}
		if got.Kind != c.kind || got.Group != c.group || got.Pair != c.pair {
			t.Errorf("%s = {%s %s pair=%q}, want {%s %s pair=%q}", c.name, got.Kind, got.Group, got.Pair, c.kind, c.group, c.pair)
		}
	}

	// Pairs must be symmetric.
	for _, c := range cols {
		if c.Pair == "" {
			continue
		}
		if byName[c.Pair].Pair != c.Name {
			t.Errorf("pair not symmetric: %s -> %s -> %s", c.Name, c.Pair, byName[c.Pair].Pair)
		}
	}

	// The derived validation maps must agree with the schema.
	if len(numberFields)+len(stringFields) != len(cols) {
		t.Errorf("derived field maps cover %d names, want %d", len(numberFields)+len(stringFields), len(cols))
	}
	if !numberFields["ac1"] || !numberFields["categoryIndex"] || !stringFields["operation"] {
		t.Error("derived field maps disagree with the schema")
	}
	if dependentRequiredPairs["category"] != "categoryIndex" {
		t.Errorf("dependentRequiredPairs[category] = %q, want categoryIndex", dependentRequiredPairs["category"])
	}
}
