package dataset

// ColumnKind classifies a standard dataset column as numeric or string.
type ColumnKind string

const (
	ColumnNumber ColumnKind = "number"
	ColumnString ColumnKind = "string"
)

// StandardColumn describes one column of the canonical dataset schema — the
// structure charts and tables understand (scenarios, dimensions, metadata).
type StandardColumn struct {
	Name string `json:"name"`
	// Kind is "number" or "string".
	Kind ColumnKind `json:"kind"`
	// Group buckets the column for presentation: Measures, Dimensions, Metadata.
	Group string `json:"group"`
	// Pair, when set, names the partner column that must accompany this one (and
	// vice versa) — e.g. category <-> categoryIndex.
	Pair string `json:"pair,omitempty"`
}

// standardColumns is the single source of truth for the dataset schema. The
// validation field maps (stringFields/numberFields/dependentRequiredPairs) are
// derived from it, and it is exposed verbatim to the VS Code wizard's mapper via
// `bino lsp-helper dataset-schema` / the daemon's GET /dataset-schema, so the CLI
// and the editor can never drift apart.
var standardColumns = func() []StandardColumn {
	cols := make([]StandardColumn, 0, 29)
	for _, n := range []string{
		"ac1", "ac2", "ac3", "ac4",
		"pp1", "pp2", "pp3", "pp4",
		"fc1", "fc2", "fc3", "fc4",
		"pl1", "pl2", "pl3", "pl4",
	} {
		cols = append(cols, StandardColumn{Name: n, Kind: ColumnNumber, Group: "Measures"})
	}
	for _, d := range []struct{ str, idx string }{
		{"rowGroup", "rowGroupIndex"},
		{"category", "categoryIndex"},
		{"subCategory", "subCategoryIndex"},
		{"columnGroup", "columnGroupIndex"},
		{"columnSubGroup", "columnSubGroupIndex"},
	} {
		cols = append(cols,
			StandardColumn{Name: d.str, Kind: ColumnString, Group: "Dimensions", Pair: d.idx},
			StandardColumn{Name: d.idx, Kind: ColumnNumber, Group: "Dimensions", Pair: d.str},
		)
	}
	for _, n := range []string{"date", "operation", "setname"} {
		cols = append(cols, StandardColumn{Name: n, Kind: ColumnString, Group: "Metadata"})
	}
	return cols
}()

// StandardColumns returns a copy of the canonical, ordered dataset schema.
func StandardColumns() []StandardColumn {
	out := make([]StandardColumn, len(standardColumns))
	copy(out, standardColumns)
	return out
}

// fieldsOfKind builds the set of standard column names of a given kind.
func fieldsOfKind(k ColumnKind) map[string]bool {
	m := make(map[string]bool)
	for _, c := range standardColumns {
		if c.Kind == k {
			m[c.Name] = true
		}
	}
	return m
}

// standardPairs builds the bidirectional dependent-column map (e.g. category
// requires categoryIndex and vice versa).
func standardPairs() map[string]string {
	m := make(map[string]string)
	for _, c := range standardColumns {
		if c.Pair != "" {
			m[c.Name] = c.Pair
		}
	}
	return m
}
