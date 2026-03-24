package plugin

// KindCategory determines how bino treats a plugin-provided kind internally.
type KindCategory int

const (
	KindCategoryDataSource KindCategory = iota
	KindCategoryComponent
	KindCategoryConfig
	KindCategoryArtifact
)

// KindRegistration describes a custom kind provided by a plugin.
type KindRegistration struct {
	KindName       string       // e.g., "SalesforceDataSource"
	Category       KindCategory // How bino routes this kind
	DataSourceType string       // Only for KindCategoryDataSource — the spec.type value
	PluginName     string       // Which plugin owns this kind
}

// PluginManifest describes a plugin's capabilities, returned from Init().
//
//nolint:revive // name used widely across codebase
type PluginManifest struct {
	Name             string
	Version          string
	Description      string
	Kinds            []KindRegistration
	DuckDBExtensions []string
	ProvidesLinter   bool
	ProvidesAssets   bool
	Commands         []CommandDescriptor
	Hooks            []string // checkpoint names this plugin subscribes to
}

// CommandDescriptor describes a CLI subcommand provided by a plugin.
type CommandDescriptor struct {
	Name  string
	Short string
	Long  string
	Usage string
	Flags []FlagDescriptor
}

// FlagDescriptor describes a flag on a plugin CLI command.
type FlagDescriptor struct {
	Name         string
	Shorthand    string
	Description  string
	DefaultValue string
	Type         string // "string", "bool", "int"
	Required     bool
}

// Diagnostic represents a non-fatal message from a plugin.
type Diagnostic struct {
	Source   string
	Stage    string
	Message  string
	Severity Severity
}

// Severity levels for findings and diagnostics.
type Severity int

const (
	SeverityWarning Severity = iota
	SeverityError
	SeverityInfo
)

// LintFinding represents a single finding from a plugin linter.
type LintFinding struct {
	RuleID   string
	Message  string
	File     string
	DocIdx   int
	Path     string
	Line     int
	Column   int
	Severity Severity
}

// AssetFile represents a JS/CSS asset provided by a plugin.
type AssetFile struct {
	URLPath   string // Serving path, e.g., "/plugins/salesforce/chart.js"
	Content   []byte // Inline content
	FilePath  string // Alternative: path on disk
	MediaType string // MIME type
	IsModule  bool   // For <script type="module">
}

// CollectResult is the response from a plugin's DataSource collection.
type CollectResult struct {
	JSONRows         []byte            // JSON array of row objects
	ColumnTypes      map[string]string // Optional type hints
	Ephemeral        bool              // Always re-fetch
	Diagnostics      []Diagnostic
	DuckDBExpression string // SQL expression registered as DuckDB view directly
}

// LintOptions provides optional enriched context for plugin linting.
type LintOptions struct {
	// Datasets contains pre-computed dataset results. Nil if not available.
	Datasets []DatasetPayload
	// DatasetsAvailable is true when Datasets is populated.
	DatasetsAvailable bool
	// RenderedHTML contains the rendered HTML output. Nil if not available.
	RenderedHTML []byte
}

// HookPayload carries data through pipeline hook checkpoints.
type HookPayload struct {
	Documents []DocumentPayload
	HTML      []byte
	PDFPath   string
	Datasets  []DatasetPayload
	Metadata  map[string]string
}

// DocumentPayload is a serializable document reference for hooks.
type DocumentPayload struct {
	File     string
	Position int
	Kind     string
	Name     string
	Raw      []byte // JSON
}

// DatasetPayload is a serializable dataset for hooks.
type DatasetPayload struct {
	Name     string
	JSONRows []byte
	Columns  []string
}

// HookResult is a plugin's response to a hook invocation.
type HookResult struct {
	Modified    bool
	Payload     *HookPayload
	Diagnostics []Diagnostic
	Findings    []LintFinding // Structured lint findings from hook processing.
}
