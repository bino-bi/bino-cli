package schema

// ConnectionSecretType is the type of connection secret.
type ConnectionSecretType string

// ConnectionSecretType constants for all supported connection secret types.
const (
	ConnectionSecretTypePostgres ConnectionSecretType = "postgres"
	ConnectionSecretTypeMySQL    ConnectionSecretType = "mysql"
	ConnectionSecretTypeS3       ConnectionSecretType = "s3"
	ConnectionSecretTypeGCS      ConnectionSecretType = "gcs"
	ConnectionSecretTypeR2       ConnectionSecretType = "r2"
	ConnectionSecretTypeHTTP     ConnectionSecretType = "http"
	ConnectionSecretTypeAzure    ConnectionSecretType = "azure"
	ConnectionSecretTypeWebDAV   ConnectionSecretType = "webdav"
)

// ConnectionSecretSpec represents the spec section of a ConnectionSecret manifest.
type ConnectionSecretSpec struct {
	// Type is the connection secret type (required).
	Type ConnectionSecretType `yaml:"type" json:"type"`

	// PasswordFromEnv is the environment variable containing the password (postgres, mysql, webdav).
	PasswordFromEnv string `yaml:"passwordFromEnv,omitempty" json:"passwordFromEnv,omitempty"`

	// KeyID is the access key ID (s3, gcs, r2).
	KeyID string `yaml:"keyId,omitempty" json:"keyId,omitempty"`

	// SecretFromEnv is the environment variable containing the secret key (s3, gcs, r2).
	SecretFromEnv string `yaml:"secretFromEnv,omitempty" json:"secretFromEnv,omitempty"`

	// Username is the HTTP basic auth username (http, webdav).
	Username string `yaml:"username,omitempty" json:"username,omitempty"`

	// BearerTokenFromEnv is the environment variable containing the bearer token (http).
	BearerTokenFromEnv string `yaml:"bearerTokenFromEnv,omitempty" json:"bearerTokenFromEnv,omitempty"`

	// ConnectionStringFromEnv is the environment variable containing the connection string (azure).
	ConnectionStringFromEnv string `yaml:"connectionStringFromEnv,omitempty" json:"connectionStringFromEnv,omitempty"`

	// AccountKeyFromEnv is the environment variable containing the account key (azure).
	AccountKeyFromEnv string `yaml:"accountKeyFromEnv,omitempty" json:"accountKeyFromEnv,omitempty"`
}

// LayoutPageSpec represents the spec section of a LayoutPage manifest.
type LayoutPageSpec struct {
	// Children is a list of component references.
	// Each element should be a reference like "$component_name".
	Children []string `yaml:"children" json:"children"`
}

// LayoutCardSpec represents the spec section of a LayoutCard manifest.
type LayoutCardSpec struct {
	// Title is the card title.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`

	// Children is a list of component references.
	// Each element should be a reference like "$component_name".
	Children []string `yaml:"children" json:"children"`
}

// TextSpec represents the spec section of a Text manifest.
type TextSpec struct {
	// Dataset is a reference to a DataSet for dynamic text.
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset,omitempty" json:"dataset,omitempty"`

	// Value is the static text value.
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
}

// ComponentStyleSpec represents the spec section of a ComponentStyle manifest.
type ComponentStyleSpec struct {
	// Content is the CSS content or style object.
	Content any `yaml:"content,omitempty" json:"content,omitempty"`
}

// InternationalizationSpec represents the spec section of an Internationalization manifest.
type InternationalizationSpec struct {
	// Code is the locale code (e.g., "en", "de", "fr").
	Code string `yaml:"code" json:"code"`

	// Content is a map of translation keys to values.
	Content map[string]string `yaml:"content,omitempty" json:"content,omitempty"`
}

// AssetType is the type of asset.
type AssetType string

// AssetType constants for all supported asset types.
const (
	AssetTypeImage AssetType = "image"
	AssetTypeFont  AssetType = "font"
	AssetTypeFile  AssetType = "file"
)

// AssetSpec represents the spec section of an Asset manifest.
type AssetSpec struct {
	// Type is the asset type (required).
	Type AssetType `yaml:"type" json:"type"`

	// MediaType is the MIME type (e.g., "image/png").
	MediaType string `yaml:"mediaType,omitempty" json:"mediaType,omitempty"`

	// Source defines where the asset data comes from.
	Source *AssetSource `yaml:"source,omitempty" json:"source,omitempty"`
}

// AssetSource represents the source of an asset.
type AssetSource struct {
	// LocalPath is the path to a local file.
	LocalPath string `yaml:"localPath,omitempty" json:"localPath,omitempty"`

	// RemoteURL is a URL to fetch the asset from.
	RemoteURL string `yaml:"remoteURL,omitempty" json:"remoteURL,omitempty"`

	// InlineBase64 is base64-encoded inline data.
	InlineBase64 string `yaml:"inlineBase64,omitempty" json:"inlineBase64,omitempty"`
}

// TableSpec represents the spec section of a Table manifest.
type TableSpec struct {
	// Dataset is a reference to a DataSet (required).
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset" json:"dataset"`

	// TableTitle is the table title.
	TableTitle string `yaml:"tableTitle,omitempty" json:"tableTitle,omitempty"`
}

// ChartStructureSpec represents the spec section of a ChartStructure manifest.
type ChartStructureSpec struct {
	// Dataset is a reference to a DataSet (required).
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset" json:"dataset"`

	// ChartTitle is the chart title.
	ChartTitle string `yaml:"chartTitle,omitempty" json:"chartTitle,omitempty"`

	// Type is the chart type (bar, pie, donut, etc.).
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// ChartTimeSpec represents the spec section of a ChartTime manifest.
type ChartTimeSpec struct {
	// Dataset is a reference to a DataSet (required).
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset" json:"dataset"`

	// ChartTitle is the chart title.
	ChartTitle string `yaml:"chartTitle,omitempty" json:"chartTitle,omitempty"`

	// Type is the chart type (line, bar, area).
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// GridSpec represents the spec section of a Grid manifest.
// Grid creates a CSS grid-based layout with row and column headers
// for organizing child components in a tabular structure.
type GridSpec struct {
	// ChartTitle is displayed at the top-left of the grid.
	ChartTitle string `yaml:"chartTitle,omitempty" json:"chartTitle,omitempty"`

	// RowHeaders defines the row headers as labels or objects with label and id.
	// Can be a YAML array like ["Row 1", "Row 2"] or [{label: "Row 1", id: "r1"}].
	RowHeaders any `yaml:"rowHeaders" json:"rowHeaders"`

	// ColumnHeaders defines the column headers as labels or objects with label and id.
	// Can be a YAML array like ["Col 1", "Col 2"] or [{label: "Col 1", id: "c1"}].
	ColumnHeaders any `yaml:"columnHeaders" json:"columnHeaders"`

	// ShowRowHeaders controls whether row headers are displayed.
	ShowRowHeaders *bool `yaml:"showRowHeaders,omitempty" json:"showRowHeaders,omitempty"`

	// ShowColumnHeaders controls whether column headers are displayed.
	ShowColumnHeaders *bool `yaml:"showColumnHeaders,omitempty" json:"showColumnHeaders,omitempty"`

	// ShowBorders controls whether borders/dividers are shown between cells.
	ShowBorders *bool `yaml:"showBorders,omitempty" json:"showBorders,omitempty"`

	// RowHeaderWidth is the CSS width of the row header column (e.g., "auto", "100px", "20%").
	RowHeaderWidth string `yaml:"rowHeaderWidth,omitempty" json:"rowHeaderWidth,omitempty"`

	// CellGap is the CSS gap between cells (e.g., "0px", "8px").
	CellGap string `yaml:"cellGap,omitempty" json:"cellGap,omitempty"`

	// Children defines the grid cell contents as an array of child objects.
	// Each child has a row, column (string or int), and either a ref to an existing component or inline spec.
	Children []any `yaml:"children" json:"children"`
}
