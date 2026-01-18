package schema

// Measure represents a single measure with a name and unit.
// Used in chart components for specifying which measures to display.
type Measure struct {
	Name string `json:"name" yaml:"name"`
	Unit string `json:"unit" yaml:"unit"`
}

// ThereofItem represents a single drilldown object with rowGroup, category, and subCategory.
// Used in table/chart components for hierarchical data display.
type ThereofItem struct {
	RowGroup    string `json:"rowGroup,omitempty" yaml:"rowGroup,omitempty"`
	Category    string `json:"category,omitempty" yaml:"category,omitempty"`
	SubCategory string `json:"subCategory,omitempty" yaml:"subCategory,omitempty"`
}

// PartofItem represents a single part-of object with rowGroup and category.
// Used in table/chart components for hierarchical grouping.
type PartofItem struct {
	RowGroup string `json:"rowGroup,omitempty" yaml:"rowGroup,omitempty"`
	Category string `json:"category,omitempty" yaml:"category,omitempty"`
}

// ColumnthereofItem represents a single column-thereof object.
// Used in table/chart components for column-level drilldowns.
type ColumnthereofItem struct {
	Scenario  string   `json:"scenario,omitempty" yaml:"scenario,omitempty"`
	Name      string   `json:"name,omitempty" yaml:"name,omitempty"`
	SubGroups []string `json:"subGroups,omitempty" yaml:"subGroups,omitempty"`
}

// InlineLocation tracks the original source location of an inline definition
// for error messages and debugging. When inline DataSources or DataSets are
// materialized into synthetic documents, this information allows error messages
// to point back to the original YAML location rather than the generated name.
type InlineLocation struct {
	// File is the absolute path to the source YAML file.
	File string

	// Position is the 1-based document index within a multi-document YAML file.
	Position int

	// ParentKind is the Kind of the parent document (e.g., "DataSet", "ChartStructure").
	ParentKind string

	// ParentName is the metadata.name of the parent document.
	ParentName string

	// Path is the JSON path within the parent document where the inline definition
	// was found (e.g., "spec.dependencies[0]" or "spec.dataset").
	Path string
}

// String returns a human-readable representation of the location for error messages.
func (loc InlineLocation) String() string {
	if loc.File == "" {
		return "<unknown location>"
	}
	if loc.ParentKind != "" && loc.ParentName != "" {
		return loc.File + " (" + loc.ParentKind + "/" + loc.ParentName + ", " + loc.Path + ")"
	}
	return loc.File
}
