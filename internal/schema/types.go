// Package schema provides canonical types for all YAML manifest structures.
// This package is the single source of truth for manifest schemas, used by both
// the CLI generator and the parser.
package schema

// APIVersion is the current API version for all manifests.
const APIVersion = "bino.bi/v1alpha1"

// Document is the envelope for all manifest kinds.
// It contains the standard fields present in every manifest document.
type Document struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       any      `yaml:"spec" json:"spec"`
}

// Metadata contains standard metadata fields for all manifests.
type Metadata struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Constraints ConstraintList    `yaml:"constraints,omitempty" json:"constraints,omitempty"`
}

// Kind constants for all supported manifest types.
const (
	KindDataSet              = "DataSet"
	KindDataSource           = "DataSource"
	KindConnectionSecret     = "ConnectionSecret"
	KindLayoutPage           = "LayoutPage"
	KindLayoutCard           = "LayoutCard"
	KindText                 = "Text"
	KindTable                = "Table"
	KindChartStructure       = "ChartStructure"
	KindChartTime            = "ChartTime"
	KindAsset                = "Asset"
	KindComponentStyle       = "ComponentStyle"
	KindInternationalization = "Internationalization"
	KindReportArtefact       = "ReportArtefact"
	KindLiveReportArtefact   = "LiveReportArtefact"
	KindSigningProfile       = "SigningProfile"
)

// NewDocument creates a new Document with the given kind and name.
func NewDocument(kind, name string) *Document {
	return &Document{
		APIVersion: APIVersion,
		Kind:       kind,
		Metadata: Metadata{
			Name: name,
		},
	}
}
