package cli

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// KindCategory groups related manifest Kinds.
type KindCategory struct {
	Name  string
	Kinds []KindInfo
}

// KindInfo describes a manifest Kind for the selector.
type KindInfo struct {
	Name        string
	Description string
}

// errAddCanceled is returned when the user cancels the add wizard.
var errAddCanceled = errors.New("add canceled")

// kindCategories defines the available manifest Kinds organized by category.
var kindCategories = []KindCategory{
	{
		Name: "Data",
		Kinds: []KindInfo{
			{Name: "dataset", Description: "SQL/PRQL query transforming data"},
			{Name: "datasource", Description: "Data connection (file or database)"},
			{Name: "connectionsecret", Description: "Credentials for database connections"},
		},
	},
	{
		Name: "Layout",
		Kinds: []KindInfo{
			{Name: "layoutpage", Description: "Page container for report content"},
			{Name: "layoutcard", Description: "Card container for grouped components"},
		},
	},
	{
		Name: "Visualization",
		Kinds: []KindInfo{
			{Name: "text", Description: "Text content with optional data binding"},
			{Name: "table", Description: "Data table from a DataSet"},
			{Name: "chartstructure", Description: "Structural chart (bar, pie, etc.)"},
			{Name: "charttime", Description: "Time-series chart"},
		},
	},
	{
		Name: "Resources",
		Kinds: []KindInfo{
			{Name: "asset", Description: "Image, font, or file resource"},
			{Name: "componentstyle", Description: "CSS styling for components"},
			{Name: "internationalization", Description: "Translations for a locale"},
		},
	},
	{
		Name: "Reports",
		Kinds: []KindInfo{
			{Name: "reportartefact", Description: "PDF report configuration"},
			{Name: "livereportartefact", Description: "Web-based live report"},
			{Name: "signingprofile", Description: "Digital signature configuration"},
		},
	},
}

// allKindNames returns all available Kind subcommand names for completion.
func allKindNames() []string {
	var names []string
	for _, cat := range kindCategories {
		for _, k := range cat.Kinds {
			names = append(names, k.Name)
		}
	}
	return names
}

// nameRegex validates manifest names: lowercase letters, digits, underscores.
// Must start with a letter, max 64 characters.
var nameRegex = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// reservedPrefixes are name prefixes reserved for internal use.
var reservedPrefixes = []string{"_inline_"}

func newAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add manifests to the current project",
		Long: strings.TrimSpace(`
bino add provides interactive wizards to create manifest files for your reporting project.

When run without a subcommand in interactive mode, displays a Kind selector.

Subcommands (Data):
  dataset           Create a DataSet manifest (SQL/PRQL queries)
  datasource        Create a DataSource manifest (data connections)
  connectionsecret  Create a ConnectionSecret manifest (credentials)

Subcommands (Layout):
  layoutpage        Create a LayoutPage manifest (page container)
  layoutcard        Create a LayoutCard manifest (card container)

Subcommands (Visualization):
  text              Create a Text manifest (text content)
  table             Create a Table manifest (data table)
  chartstructure    Create a ChartStructure manifest (structural chart)
  charttime         Create a ChartTime manifest (time-series chart)

Subcommands (Resources):
  asset             Create an Asset manifest (image/font/file)
  componentstyle    Create a ComponentStyle manifest (CSS styling)
  internationalization  Create an Internationalization manifest (translations)

Subcommands (Reports):
  reportartefact       Create a ReportArtefact manifest (PDF report)
  livereportartefact   Create a LiveReportArtefact manifest (web report)
  signingprofile       Create a SigningProfile manifest (digital signature)

Three interaction modes are supported:
  1. Fully Interactive:    No arguments → step-by-step wizard
  2. Semi-Interactive:     Partial flags → prompt only for missing values
  3. Non-Interactive:      --no-prompt flag → fail if required values missing
`),
		Example: strings.TrimSpace(`
  # Interactive Kind selector
  bino add

  # Interactive wizard for specific Kind
  bino add dataset
  bino add datasource

  # Semi-interactive (provide some flags)
  bino add dataset sales_monthly --sql-file queries/sales.sql

  # Non-interactive (all flags required)
  bino add dataset sales_monthly \
    --sql "SELECT * FROM sales" \
    --output datasets/sales.yaml \
    --no-prompt
`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isInteractive() {
				return cmd.Help()
			}
			return runAddKindSelector(cmd)
		},
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return allKindNames(), cobra.ShellCompDirectiveNoFileComp
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Data
	cmd.AddCommand(newAddDataSetCommand())
	cmd.AddCommand(newAddDataSourceCommand())
	cmd.AddCommand(newAddConnectionSecretCommand())

	// Layout
	cmd.AddCommand(newAddLayoutPageCommand())
	cmd.AddCommand(newAddLayoutCardCommand())

	// Visualization
	cmd.AddCommand(newAddTextCommand())
	cmd.AddCommand(newAddTableCommand())
	cmd.AddCommand(newAddChartStructureCommand())
	cmd.AddCommand(newAddChartTimeCommand())

	// Resources
	cmd.AddCommand(newAddAssetCommand())
	cmd.AddCommand(newAddComponentStyleCommand())
	cmd.AddCommand(newAddInternationalizationCommand())

	// Reports
	cmd.AddCommand(newAddReportArtefactCommand())
	cmd.AddCommand(newAddLiveReportArtefactCommand())
	cmd.AddCommand(newAddSigningProfileCommand())

	return cmd
}

// runAddKindSelector displays an interactive Kind selector.
func runAddKindSelector(cmd *cobra.Command) error {
	// Build options for huh Select
	var options []huh.Option[string]
	for _, cat := range kindCategories {
		// Add category header (disabled option)
		options = append(options, huh.NewOption[string](
			fmt.Sprintf("─── %s ───", cat.Name),
			"",
		))
		for _, k := range cat.Kinds {
			label := fmt.Sprintf("  %s - %s", k.Name, k.Description)
			options = append(options, huh.NewOption(label, k.Name))
		}
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What kind of manifest would you like to create?").
				Options(options...).
				Value(&selected).
				Height(20),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
			return nil
		}
		return err
	}

	if selected == "" {
		// User selected a category header
		return runAddKindSelector(cmd)
	}

	// Find and run the subcommand
	for _, sub := range cmd.Commands() {
		if sub.Name() == selected {
			sub.SetContext(cmd.Context())
			return sub.RunE(sub, nil)
		}
	}

	return fmt.Errorf("unknown kind: %s", selected)
}

// ValidateName validates a manifest name for DataSet or DataSource.
// Names must be snake_case: lowercase letters, digits, and underscores.
// Must start with a lowercase letter and be at most 64 characters.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}

	if len(name) > 64 {
		return fmt.Errorf("name must be at most 64 characters (got %d)", len(name))
	}

	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("name cannot start with reserved prefix %q", prefix)
		}
	}

	if !nameRegex.MatchString(name) {
		suggestion := SuggestNameFix(name)
		if suggestion != "" && suggestion != name {
			return fmt.Errorf("invalid name %q: use snake_case (suggestion: %s)", name, suggestion)
		}
		return fmt.Errorf("invalid name %q: must be snake_case (lowercase letters, digits, underscores, starting with a letter)", name)
	}

	return nil
}

// SuggestNameFix attempts to fix an invalid name by converting to snake_case.
// Returns the fixed name or empty string if no fix is possible.
func SuggestNameFix(name string) string {
	if name == "" {
		return ""
	}

	// Convert to lowercase
	result := strings.ToLower(strings.TrimSpace(name))

	// Replace common separators with underscores
	result = strings.ReplaceAll(result, "-", "_")
	result = strings.ReplaceAll(result, " ", "_")
	result = strings.ReplaceAll(result, ".", "_")

	// Remove invalid characters, keep only a-z, 0-9, _
	var b strings.Builder
	lastUnderscore := false
	for _, r := range result {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_':
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	result = strings.Trim(b.String(), "_")

	// Ensure it starts with a letter
	if result == "" || result[0] < 'a' || result[0] > 'z' {
		result = "ds_" + result
	}

	// Truncate to 64 characters
	if len(result) > 64 {
		result = result[:64]
		result = strings.TrimRight(result, "_")
	}

	// Validate the result
	if nameRegex.MatchString(result) {
		return result
	}

	return ""
}

// QueryType represents the type of query for a DataSet.
type QueryType int

const (
	QueryTypeNone QueryType = iota
	QueryTypeSQLInline
	QueryTypeSQLFile
	QueryTypePRQLInline
	QueryTypePRQLFile
	QueryTypePassThrough
)

// String returns a human-readable name for the query type.
func (q QueryType) String() string {
	switch q {
	case QueryTypeSQLInline:
		return "SQL (inline)"
	case QueryTypeSQLFile:
		return "SQL (from file)"
	case QueryTypePRQLInline:
		return "PRQL (inline)"
	case QueryTypePRQLFile:
		return "PRQL (from file)"
	case QueryTypePassThrough:
		return "Pass-through (use DataSource directly)"
	default:
		return "None"
	}
}

// DataSourceType represents the type of data source.
type DataSourceType int

const (
	DataSourceTypeNone DataSourceType = iota
	DataSourceTypePostgres
	DataSourceTypeMySQL
	DataSourceTypeCSV
	DataSourceTypeParquet
	DataSourceTypeExcel
	DataSourceTypeJSON
	DataSourceTypeInline
)

// String returns a human-readable name for the data source type.
func (d DataSourceType) String() string {
	switch d {
	case DataSourceTypePostgres:
		return "PostgreSQL"
	case DataSourceTypeMySQL:
		return "MySQL"
	case DataSourceTypeCSV:
		return "CSV file"
	case DataSourceTypeParquet:
		return "Parquet file"
	case DataSourceTypeExcel:
		return "Excel file"
	case DataSourceTypeJSON:
		return "JSON file"
	case DataSourceTypeInline:
		return "Inline data"
	default:
		return "None"
	}
}

// TypeString returns the YAML type string for the data source.
func (d DataSourceType) TypeString() string {
	switch d {
	case DataSourceTypePostgres:
		return "postgres_query"
	case DataSourceTypeMySQL:
		return "mysql_query"
	case DataSourceTypeCSV:
		return "csv"
	case DataSourceTypeParquet:
		return "parquet"
	case DataSourceTypeExcel:
		return "excel"
	case DataSourceTypeJSON:
		return "json"
	case DataSourceTypeInline:
		return "inline"
	default:
		return ""
	}
}

// ParseDataSourceType parses a string into a DataSourceType.
func ParseDataSourceType(s string) DataSourceType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "postgres", "postgresql":
		return DataSourceTypePostgres
	case "mysql":
		return DataSourceTypeMySQL
	case "csv":
		return DataSourceTypeCSV
	case "parquet":
		return DataSourceTypeParquet
	case "excel", "xlsx", "xls":
		return DataSourceTypeExcel
	case "json":
		return DataSourceTypeJSON
	case "inline":
		return DataSourceTypeInline
	default:
		return DataSourceTypeNone
	}
}
