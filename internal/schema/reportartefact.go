package schema

// ReportArtefactSpec represents the spec section of a ReportArtefact manifest.
// It defines the configuration for generating a PDF or XGA report.
type ReportArtefactSpec struct {
	// Format is the output format: "pdf" or "xga".
	Format string `yaml:"format,omitempty" json:"format,omitempty"`

	// Orientation is the page orientation: "portrait" or "landscape".
	Orientation string `yaml:"orientation,omitempty" json:"orientation,omitempty"`

	// Language is the language code for the report (e.g., "en", "de").
	Language string `yaml:"language,omitempty" json:"language,omitempty"`

	// Filename is the output filename pattern (required).
	// Supports template variables like {{date}}.
	Filename string `yaml:"filename" json:"filename"`

	// Title is the document title for PDF metadata.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`

	// Description is the document description for PDF metadata.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Subject is the document subject for PDF metadata.
	Subject string `yaml:"subject,omitempty" json:"subject,omitempty"`

	// Author is the document author for PDF metadata.
	Author string `yaml:"author,omitempty" json:"author,omitempty"`

	// Keywords are the document keywords for PDF metadata.
	Keywords []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`

	// SigningProfile is the name of a SigningProfile manifest for digital signatures.
	SigningProfile string `yaml:"signingProfile,omitempty" json:"signingProfile,omitempty"`

	// LayoutPages lists the LayoutPage names to include in the report.
	// Each name is rendered with a $ prefix in YAML (e.g., $page_name).
	LayoutPages []string `yaml:"layoutPages,omitempty" json:"layoutPages,omitempty"`
}

// LiveReportArtefactSpec represents the spec section of a LiveReportArtefact manifest.
// It defines routes for serving reports via the bino serve command.
type LiveReportArtefactSpec struct {
	// Title is the application title shown in the browser.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`

	// Description is a description of the live report application.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Routes maps URL paths to report configurations.
	// A root route "/" is required.
	Routes map[string]LiveRouteSpec `yaml:"routes" json:"routes"`
}

// LiveRouteSpec defines a route mapping in a LiveReportArtefact.
// Either Artifact or LayoutPages must be set, but not both.
type LiveRouteSpec struct {
	// Artifact is a reference to a ReportArtefact manifest.
	// Rendered with a $ prefix in YAML (e.g., $report_name).
	Artifact string `yaml:"artefact,omitempty" json:"artefact,omitempty"`

	// LayoutPages lists LayoutPage names to render for this route.
	// Each name is rendered with a $ prefix in YAML.
	LayoutPages []string `yaml:"layoutPages,omitempty" json:"layoutPages,omitempty"`

	// Title is an optional title override for this route.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`

	// QueryParams defines allowed query parameters for this route.
	QueryParams []LiveQueryParamSpec `yaml:"queryParams,omitempty" json:"queryParams,omitempty"`
}

// LiveQueryParamSpec defines an allowed query parameter for live serving.
type LiveQueryParamSpec struct {
	// Name is the query parameter name (required).
	Name string `yaml:"name" json:"name"`

	// Type is the parameter type: string, number, number_range, select, date, date_time.
	// Defaults to "string" if not specified.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Default is the default value if the parameter is not provided.
	// If nil and Optional is false, the parameter is required.
	Default *string `yaml:"default,omitempty" json:"default,omitempty"`

	// Optional indicates the parameter is optional even without a default.
	Optional bool `yaml:"optional,omitempty" json:"optional,omitempty"`

	// Description is a human-readable description of the parameter.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Options defines options for select, number, and number_range types.
	Options *LiveQueryParamOptions `yaml:"options,omitempty" json:"options,omitempty"`
}

// LiveQueryParamOptions defines options for select, number, and number_range type parameters.
type LiveQueryParamOptions struct {
	// Items are static options for select type.
	Items []LiveQueryParamOptionItem `yaml:"items,omitempty" json:"items,omitempty"`

	// Dataset is the dataset name for dynamic options.
	Dataset string `yaml:"dataset,omitempty" json:"dataset,omitempty"`

	// ValueColumn is the column to use as the option value.
	ValueColumn string `yaml:"valueColumn,omitempty" json:"valueColumn,omitempty"`

	// LabelColumn is the column to use as the option label (defaults to ValueColumn).
	LabelColumn string `yaml:"labelColumn,omitempty" json:"labelColumn,omitempty"`

	// Min is the minimum value for number/number_range types.
	Min *float64 `yaml:"min,omitempty" json:"min,omitempty"`

	// Max is the maximum value for number/number_range types.
	Max *float64 `yaml:"max,omitempty" json:"max,omitempty"`

	// Step is the step value for number/number_range types.
	Step *float64 `yaml:"step,omitempty" json:"step,omitempty"`
}

// LiveQueryParamOptionItem defines a single option for select type parameters.
type LiveQueryParamOptionItem struct {
	// Value is the option value.
	Value string `yaml:"value" json:"value"`

	// Label is the display label (defaults to Value if empty).
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
}

// SigningProfileSpec represents the spec section of a SigningProfile manifest.
// It defines the certificate and private key used for digital signatures.
type SigningProfileSpec struct {
	// Certificate is a reference to the certificate file.
	Certificate *FileRef `yaml:"certificate,omitempty" json:"certificate,omitempty"`

	// PrivateKey is a reference to the private key file.
	PrivateKey *FileRef `yaml:"privateKey,omitempty" json:"privateKey,omitempty"`

	// SignerName is the name of the signer shown in PDF signature properties.
	SignerName string `yaml:"signerName,omitempty" json:"signerName,omitempty"`
}

// FileRef represents a reference to a local file.
type FileRef struct {
	// LocalPath is the path to the file relative to the manifest directory.
	LocalPath string `yaml:"localPath" json:"localPath"`
}
