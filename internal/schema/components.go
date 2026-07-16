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
// Credentials are nested under a type-specific auth block (postgres, mysql, s3,
// gcs, http, r2, azure, huggingface) that must match the secret Type. Connection
// details (host, port, database, user) live on the DataSource, not here.
type ConnectionSecretSpec struct {
	// Type is the connection secret type (required).
	Type ConnectionSecretType `yaml:"type" json:"type"`

	// Scope optionally limits where the secret applies (for example "s3://my-bucket").
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`

	// Provider is the secret provider (defaults to "config"); use "credential_chain" for auto-discovery.
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`

	// Type-specific credential blocks. Set the one matching Type.
	Postgres    *PostgresAuthSpec    `yaml:"postgres,omitempty" json:"postgres,omitempty"`
	MySQL       *MySQLAuthSpec       `yaml:"mysql,omitempty" json:"mysql,omitempty"`
	S3          *S3AuthSpec          `yaml:"s3,omitempty" json:"s3,omitempty"`
	GCS         *GCSAuthSpec         `yaml:"gcs,omitempty" json:"gcs,omitempty"`
	HTTP        *HTTPAuthSpec        `yaml:"http,omitempty" json:"http,omitempty"`
	R2          *R2AuthSpec          `yaml:"r2,omitempty" json:"r2,omitempty"`
	Azure       *AzureAuthSpec       `yaml:"azure,omitempty" json:"azure,omitempty"`
	Huggingface *HuggingfaceAuthSpec `yaml:"huggingface,omitempty" json:"huggingface,omitempty"`
}

// PostgresAuthSpec holds PostgreSQL credentials.
type PostgresAuthSpec struct {
	Password        string `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFromEnv string `yaml:"passwordFromEnv,omitempty" json:"passwordFromEnv,omitempty"`
}

// MySQLAuthSpec holds MySQL credentials.
type MySQLAuthSpec struct {
	Password        string `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFromEnv string `yaml:"passwordFromEnv,omitempty" json:"passwordFromEnv,omitempty"`
}

// S3AuthSpec holds AWS S3 credentials and configuration.
type S3AuthSpec struct {
	KeyID               string `yaml:"keyId,omitempty" json:"keyId,omitempty"`
	KeyIDFromEnv        string `yaml:"keyIdFromEnv,omitempty" json:"keyIdFromEnv,omitempty"`
	Secret              string `yaml:"secret,omitempty" json:"secret,omitempty"`
	SecretFromEnv       string `yaml:"secretFromEnv,omitempty" json:"secretFromEnv,omitempty"`
	Region              string `yaml:"region,omitempty" json:"region,omitempty"`
	SessionToken        string `yaml:"sessionToken,omitempty" json:"sessionToken,omitempty"`
	SessionTokenFromEnv string `yaml:"sessionTokenFromEnv,omitempty" json:"sessionTokenFromEnv,omitempty"`
	Endpoint            string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	URLStyle            string `yaml:"urlStyle,omitempty" json:"urlStyle,omitempty"`
}

// GCSAuthSpec holds Google Cloud Storage credentials.
type GCSAuthSpec struct {
	KeyID         string `yaml:"keyId,omitempty" json:"keyId,omitempty"`
	KeyIDFromEnv  string `yaml:"keyIdFromEnv,omitempty" json:"keyIdFromEnv,omitempty"`
	Secret        string `yaml:"secret,omitempty" json:"secret,omitempty"`
	SecretFromEnv string `yaml:"secretFromEnv,omitempty" json:"secretFromEnv,omitempty"`
}

// HTTPAuthSpec holds HTTP/HTTPS authentication credentials and proxy configuration.
type HTTPAuthSpec struct {
	Username                 string `yaml:"username,omitempty" json:"username,omitempty"`
	UsernameFromEnv          string `yaml:"usernameFromEnv,omitempty" json:"usernameFromEnv,omitempty"`
	Password                 string `yaml:"password,omitempty" json:"password,omitempty"`
	PasswordFromEnv          string `yaml:"passwordFromEnv,omitempty" json:"passwordFromEnv,omitempty"`
	BearerToken              string `yaml:"bearerToken,omitempty" json:"bearerToken,omitempty"`
	BearerTokenFromEnv       string `yaml:"bearerTokenFromEnv,omitempty" json:"bearerTokenFromEnv,omitempty"`
	HTTPProxy                string `yaml:"httpProxy,omitempty" json:"httpProxy,omitempty"`
	HTTPProxyFromEnv         string `yaml:"httpProxyFromEnv,omitempty" json:"httpProxyFromEnv,omitempty"`
	HTTPProxyUsername        string `yaml:"httpProxyUsername,omitempty" json:"httpProxyUsername,omitempty"`
	HTTPProxyUsernameFromEnv string `yaml:"httpProxyUsernameFromEnv,omitempty" json:"httpProxyUsernameFromEnv,omitempty"`
	HTTPProxyPassword        string `yaml:"httpProxyPassword,omitempty" json:"httpProxyPassword,omitempty"`
	HTTPProxyPasswordFromEnv string `yaml:"httpProxyPasswordFromEnv,omitempty" json:"httpProxyPasswordFromEnv,omitempty"`
}

// R2AuthSpec holds Cloudflare R2 credentials and configuration.
type R2AuthSpec struct {
	KeyID         string `yaml:"keyId,omitempty" json:"keyId,omitempty"`
	KeyIDFromEnv  string `yaml:"keyIdFromEnv,omitempty" json:"keyIdFromEnv,omitempty"`
	Secret        string `yaml:"secret,omitempty" json:"secret,omitempty"`
	SecretFromEnv string `yaml:"secretFromEnv,omitempty" json:"secretFromEnv,omitempty"`
	AccountID     string `yaml:"accountId,omitempty" json:"accountId,omitempty"`
	Endpoint      string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
}

// AzureAuthSpec holds Azure Blob Storage credentials.
type AzureAuthSpec struct {
	ConnectionString        string `yaml:"connectionString,omitempty" json:"connectionString,omitempty"`
	ConnectionStringFromEnv string `yaml:"connectionStringFromEnv,omitempty" json:"connectionStringFromEnv,omitempty"`
	AccountName             string `yaml:"accountName,omitempty" json:"accountName,omitempty"`
	AccountKey              string `yaml:"accountKey,omitempty" json:"accountKey,omitempty"`
	AccountKeyFromEnv       string `yaml:"accountKeyFromEnv,omitempty" json:"accountKeyFromEnv,omitempty"`
}

// HuggingfaceAuthSpec holds Hugging Face credentials.
type HuggingfaceAuthSpec struct {
	Token        string `yaml:"token,omitempty" json:"token,omitempty"`
	TokenFromEnv string `yaml:"tokenFromEnv,omitempty" json:"tokenFromEnv,omitempty"`
}

// LayoutChild references a standalone component document rendered as a child
// of a LayoutPage or LayoutCard.
type LayoutChild struct {
	// Kind is the component kind of the referenced document (e.g. "Table").
	Kind string `yaml:"kind" json:"kind"`

	// Ref is the metadata.name of the referenced document.
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// LayoutPageSpec represents the spec section of a LayoutPage manifest.
type LayoutPageSpec struct {
	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`

	// Children is a list of component references.
	Children []LayoutChild `yaml:"children" json:"children"`
}

// LayoutCardSpec represents the spec section of a LayoutCard manifest.
type LayoutCardSpec struct {
	// TitleBusinessUnit is the free-text title line of the card header.
	TitleBusinessUnit string `yaml:"titleBusinessUnit,omitempty" json:"titleBusinessUnit,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`

	// Children is a list of component references.
	Children []LayoutChild `yaml:"children" json:"children"`
}

// TextSpec represents the spec section of a Text manifest.
type TextSpec struct {
	// Dataset is a reference to a DataSet for dynamic text.
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset,omitempty" json:"dataset,omitempty"`

	// Value is the text content. Supports Markdown (converted to HTML at build
	// time) and template expressions like ${data.myDataset[0].ac1}.
	Value string `yaml:"value,omitempty" json:"value,omitempty"`

	// Scale controls font-size scaling relative to the available parent space.
	// Valid values: "none", "auto", or a positive number (e.g. "0.8").
	// When omitted the component auto-scales and emits a warning with the
	// applied factor; "auto" auto-scales silently; "none" disables scaling
	// (warns on overflow); a number sets an explicit factor.
	Scale string `yaml:"scale,omitempty" json:"scale,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`
}

// ComponentStyleSpec represents the spec section of a ComponentStyle manifest.
type ComponentStyleSpec struct {
	// Content is the CSS content or style object.
	Content any `yaml:"content,omitempty" json:"content,omitempty"`
}

// RuleSetSpec represents the spec section of a RuleSet manifest.
type RuleSetSpec struct {
	// Content is the IBCS rule-set configuration object or JSON string.
	Content any `yaml:"content,omitempty" json:"content,omitempty"`
}

// InternationalizationSpec represents the spec section of an Internationalization manifest.
type InternationalizationSpec struct {
	// Code is the locale code (e.g., "en", "de", "fr").
	Code string `yaml:"code" json:"code"`

	// Content is a map of translation keys to values.
	Content map[string]string `yaml:"content,omitempty" json:"content,omitempty"`
}

// ScalingGroupSpec represents the spec section of a ScalingGroup manifest.
// ScalingGroup defines a named scaling value that chart and table components
// can reference via their unitScaling or percentageScaling attributes,
// allowing synchronized scaling across multiple components.
type ScalingGroupSpec struct {
	// Value is the scaling factor (pixels per data unit or pixels per percentage point).
	Value float64 `yaml:"value" json:"value"`
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

	// Type is the table type. Only "sum" and "opt" render a grand-total row.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// SumTitle labels the grand-total row. It is only rendered for the "sum"
	// and "opt" table types; when empty the IBCS total symbol is used.
	SumTitle string `yaml:"sumTitle,omitempty" json:"sumTitle,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`
}

// StackConfig configures stacked chart rendering.
type StackConfig struct {
	// By determines what to stack: "scenarios" (stack scenario slots) or "dimensions" (auto-derive from level).
	By string `yaml:"by" json:"by"`

	// Mode controls how stacked values are displayed: "absolute", "relative", or "absolute-relative".
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`

	// Order controls segment ordering within each stack: "asc", "desc", or "dataset".
	Order string `yaml:"order,omitempty" json:"order,omitempty"`
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

	// Stack configures stacked bar rendering.
	Stack *StackConfig `yaml:"stack,omitempty" json:"stack,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`
}

// ChartTimeSpec represents the spec section of a ChartTime manifest.
type ChartTimeSpec struct {
	// Dataset is a reference to a DataSet (required).
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset" json:"dataset"`

	// ChartTitle is the chart title.
	ChartTitle string `yaml:"chartTitle,omitempty" json:"chartTitle,omitempty"`

	// Stack configures stacked column/area rendering.
	Stack *StackConfig `yaml:"stack,omitempty" json:"stack,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`
}

// ChartScatterSpec represents the spec section of a ChartScatter manifest.
type ChartScatterSpec struct {
	// Dataset is a reference to a DataSet (required).
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset" json:"dataset"`

	// X is the horizontal axis measure token (required), e.g. "ac1" or "dac1_pp1".
	// The object form with label/unit/min/max is authored in YAML directly.
	X string `yaml:"x" json:"x"`

	// Y is the vertical axis measure token (required).
	Y string `yaml:"y" json:"y"`

	// ChartTitle is the chart title.
	ChartTitle string `yaml:"chartTitle,omitempty" json:"chartTitle,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`
}

// ChartBubbleSpec represents the spec section of a ChartBubble manifest.
type ChartBubbleSpec struct {
	// Dataset is a reference to a DataSet (required).
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset" json:"dataset"`

	// X is the horizontal axis measure token (required), e.g. "ac1" or "dac1_pp1".
	// The object form with label/unit/min/max is authored in YAML directly.
	X string `yaml:"x" json:"x"`

	// Y is the vertical axis measure token (required).
	Y string `yaml:"y" json:"y"`

	// Size is the bubble size measure token (required). Bubble area is
	// proportional to the value; values must be >= 0.
	Size string `yaml:"size" json:"size"`

	// ChartTitle is the chart title.
	ChartTitle string `yaml:"chartTitle,omitempty" json:"chartTitle,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`
}

// ChartBulletSpec represents the spec section of a ChartBullet manifest.
type ChartBulletSpec struct {
	// Dataset is a reference to a DataSet (required).
	// Should be a reference like "$dataset_name".
	Dataset string `yaml:"dataset" json:"dataset"`

	// Actual is the actual-value measure token (optional), e.g. "ac1".
	// Only plain scenario slots are allowed; empty auto-detects (ac1).
	// The object form with label/unit is authored in YAML directly.
	Actual string `yaml:"actual,omitempty" json:"actual,omitempty"`

	// Target is the target measure token (optional), e.g. "pl1".
	// Empty auto-detects (pl1 > pp1 > fc1).
	Target string `yaml:"target,omitempty" json:"target,omitempty"`

	// ChartTitle is the chart title.
	ChartTitle string `yaml:"chartTitle,omitempty" json:"chartTitle,omitempty"`

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Ruleset names a RuleSet manifest selecting the IBCS scenario rule set
	// (merged over the _system and _default rule sets).
	Ruleset string `yaml:"ruleset,omitempty" json:"ruleset,omitempty"`
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

	// SelectedStyle names a ComponentStyle manifest applied to this component
	// (merged over the _system and _default styles).
	SelectedStyle string `yaml:"selectedStyle,omitempty" json:"selectedStyle,omitempty"`

	// Children defines the grid cell contents as an array of child objects.
	// Each child has a row, column (string or int), and either a ref to an existing component or inline spec.
	Children []any `yaml:"children" json:"children"`
}
