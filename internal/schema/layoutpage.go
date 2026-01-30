package schema

// LayoutPageParamSpec defines a parameter for a LayoutPage manifest.
// Parameters allow the same page to be instantiated multiple times with different values.
type LayoutPageParamSpec struct {
	// Name is the parameter name, referenced as ${NAME} in the spec.
	Name string `yaml:"name" json:"name"`

	// Type is the parameter type: string, number, boolean, select, or date.
	// Defaults to "string" if not specified.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Description is a human-readable description of the parameter.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Default is the default value if the parameter is not provided.
	// If empty and Required is true, the parameter must be provided.
	Default string `yaml:"default,omitempty" json:"default,omitempty"`

	// Required indicates the parameter must be provided when using the page.
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`

	// Options defines type-specific constraints for select and number types.
	Options *LayoutPageParamOptions `yaml:"options,omitempty" json:"options,omitempty"`
}

// LayoutPageParamOptions defines type-specific options for parameters.
type LayoutPageParamOptions struct {
	// Items are the available options for select type parameters.
	Items []LayoutPageParamOptionItem `yaml:"items,omitempty" json:"items,omitempty"`

	// Min is the minimum value for number type parameters.
	Min *float64 `yaml:"min,omitempty" json:"min,omitempty"`

	// Max is the maximum value for number type parameters.
	Max *float64 `yaml:"max,omitempty" json:"max,omitempty"`
}

// LayoutPageParamOptionItem defines a single option for select type parameters.
type LayoutPageParamOptionItem struct {
	// Value is the option value used in the parameter.
	Value string `yaml:"value" json:"value"`

	// Label is the display label for the option. Defaults to Value if empty.
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
}

// LayoutPageRef references a LayoutPage with optional parameter values.
// This is used in ReportArtefact.spec.layoutPages to instantiate parameterized pages.
type LayoutPageRef struct {
	// Page is the LayoutPage name (metadata.name).
	Page string `yaml:"page" json:"page"`

	// Params are the parameter values to pass to the page.
	Params map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
}

// LayoutPageSpecWithParams extends LayoutPageSpec with parameter support.
// Used when generating LayoutPage manifests with parameters.
type LayoutPageSpecWithParams struct {
	LayoutPageSpec `yaml:",inline"`

	// Params are defined in metadata, but we track them here for generation convenience.
}

// ParamType constants for LayoutPageParamSpec.Type
const (
	ParamTypeString  = "string"
	ParamTypeNumber  = "number"
	ParamTypeBoolean = "boolean"
	ParamTypeSelect  = "select"
	ParamTypeDate    = "date"
)

// ValidParamTypes returns all valid parameter types.
func ValidParamTypes() []string {
	return []string{
		ParamTypeString,
		ParamTypeNumber,
		ParamTypeBoolean,
		ParamTypeSelect,
		ParamTypeDate,
	}
}
