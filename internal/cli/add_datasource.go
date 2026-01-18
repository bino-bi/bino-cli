package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/schema"
)

func newAddDataSourceCommand() *cobra.Command {
	var (
		flagType         string
		flagFile         string
		flagDBHost       string
		flagDBPort       int
		flagDBDatabase   string
		flagDBSchema     string
		flagDBUser       string
		flagDBSecret     string
		flagDBQuery      string
		flagCSVDelimiter string
		flagCSVHeader    bool
		flagCSVNoHeader  bool
		flagCSVSkipRows  int
		flagConstraint   []string
		flagOutput       string
		flagAppendTo     string
		flagDesc         string
		flagNoPrompt     bool
		flagOpenEditor   bool
	)

	cmd := &cobra.Command{
		Use:   "datasource [name]",
		Short: "Create a DataSource manifest",
		Long: strings.TrimSpace(`
Create a new DataSource manifest for defining data connections.

A DataSource provides access to data from databases (PostgreSQL, MySQL) or files
(CSV, Parquet, Excel, JSON). The wizard guides you through connection setup and
security best practices.

Modes:
  Interactive (default):   Step-by-step wizard for all options
  Semi-interactive:        Provide some flags, prompt for the rest
  Non-interactive:         Use --no-prompt with all required flags
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add datasource

  # CSV file with name provided
  bino add datasource sales_csv --type csv --file data/sales.csv

  # PostgreSQL with structured connection
  bino add datasource sales_db \
    --type postgres \
    --db-host localhost \
    --db-port 5432 \
    --db-database analytics \
    --db-user reporting \
    --db-secret postgresCredentials \
    --db-query "SELECT * FROM sales" \
    --output datasources/sales_db.yaml \
    --no-prompt

  # CSV with custom options
  bino add datasource custom_csv \
    --type csv \
    --file data/custom.csv \
    --csv-delimiter ";" \
    --csv-no-header \
    --csv-skip-rows 2 \
    --output datasources/custom.yaml \
    --no-prompt
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Resolve working directory
			workdir, err := pathutil.ResolveWorkdir(".")
			if err != nil {
				return ConfigError(err)
			}

			// Check if we're in non-interactive mode
			nonInteractive := flagNoPrompt || !isInteractive()

			// Get name from args if provided
			var name string
			if len(args) > 0 {
				name = args[0]
			}

			// Parse type from flag
			dsType := ParseDataSourceType(flagType)

			// Check for conflicting flags
			hasFileConfig := flagFile != ""
			hasDBConfig := flagDBHost != "" || flagDBPort != 0 || flagDBDatabase != "" || flagDBUser != ""
			if hasFileConfig && hasDBConfig {
				return ConfigError(fmt.Errorf("cannot specify both file and database connection flags"))
			}

			// In non-interactive mode, validate required flags
			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if dsType == DataSourceTypeNone {
					missing = append(missing, "--type")
				}
				if !hasFileConfig && !hasDBConfig {
					missing = append(missing, "--file or database connection flags (--db-host, etc.)")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s\n\nRun without --no-prompt for interactive mode", strings.Join(missing, "\n  ")))
				}
			}

			// Scan existing manifests
			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			// Build manifest data
			csvHeader := true
			if flagCSVNoHeader {
				csvHeader = false
			}
			if cmd.Flags().Changed("csv-header") {
				csvHeader = flagCSVHeader
			}

			data := DataSourceManifestData{
				Name:         name,
				Description:  flagDesc,
				Constraints:  flagConstraint,
				Type:         dsType,
				Path:         flagFile,
				DBHost:       flagDBHost,
				DBPort:       flagDBPort,
				DBDatabase:   flagDBDatabase,
				DBSchema:     flagDBSchema,
				DBUser:       flagDBUser,
				DBSecret:     flagDBSecret,
				DBQuery:      flagDBQuery,
				CSVDelimiter: flagCSVDelimiter,
				CSVHeader:    &csvHeader,
				CSVSkipRows:  flagCSVSkipRows,
			}

			// Determine output path from flags
			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
				appendMode = false
			}

			// If non-interactive, just write the manifest
			if nonInteractive {
				return writeDataSourceManifest(cmd, workdir, data, outputPath, appendMode)
			}

			// Run interactive wizard
			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new DataSource manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Step 1: Name & Description
			if data.Name == "" {
				var err error
				data.Name, err = promptDataSourceName(reader, out, manifests)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			} else {
				// Validate provided name
				if err := ValidateName(data.Name); err != nil {
					return ConfigError(err)
				}
				if !IsNameUnique(manifests, "DataSource", data.Name) {
					existing := FindByName(manifests, "DataSource", data.Name)
					return ConfigError(fmt.Errorf("a DataSource named %q already exists in %s:%d", data.Name, existing.File, existing.Position))
				}
			}

			if data.Description == "" {
				desc, err := addPromptString(reader, out, "Description (optional)", "")
				if err != nil {
					return RuntimeError(err)
				}
				data.Description = desc
			}

			// Step 2: Type Selection
			if data.Type == DataSourceTypeNone {
				dsType, err := promptDataSourceType(reader, out)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
				data.Type = dsType
			}

			// Step 3: Connection/File Config
			needsConfig := data.Path == "" && data.DBHost == ""
			if needsConfig {
				if err := promptConnectionConfig(reader, out, workdir, &data); err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Step 4: Constraints (Optional)
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm(reader, out, "Add constraints to conditionally include this DataSource?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					constraints, err := addPromptConstraintBuilder(reader, out)
					if err != nil {
						return RuntimeError(err)
					}
					data.Constraints = constraints
				}
			}

			// Step 5: File Location
			if outputPath == "" {
				var err error
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "DataSource", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Step 6: Preview & Confirmation
			doc := buildDataSourceDocument(data)
			manifestBytes, err := renderDataSourceManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")
			fmt.Fprintln(out)

			if appendMode {
				fmt.Fprintf(out, "Will append to: %s\n", outputPath)
			} else {
				fmt.Fprintf(out, "Will create: %s\n", outputPath)
			}

			confirmed, err := addPromptConfirm(reader, out, "Proceed?", true)
			if err != nil {
				return RuntimeError(err)
			}
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			// Step 7: Write Manifest
			if err := writeDataSourceManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			// Step 8: Post-Creation Actions
			if flagOpenEditor {
				editor := getEditor()
				if editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...)
					execCmd.Stdin = os.Stdin
					execCmd.Stdout = os.Stdout
					execCmd.Stderr = os.Stderr
					_ = execCmd.Run()
				}
			}

			return promptDataSourcePostActions(reader, out)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagType, "type", "", "DataSource type (postgres, mysql, csv, parquet, excel, json)")
	cmd.Flags().StringVar(&flagFile, "file", "", "File path (for file-based types)")
	cmd.Flags().StringVar(&flagDBHost, "db-host", "", "Database host")
	cmd.Flags().IntVar(&flagDBPort, "db-port", 0, "Database port")
	cmd.Flags().StringVar(&flagDBDatabase, "db-database", "", "Database name")
	cmd.Flags().StringVar(&flagDBSchema, "db-schema", "", "Database schema (optional)")
	cmd.Flags().StringVar(&flagDBUser, "db-user", "", "Database username")
	cmd.Flags().StringVar(&flagDBSecret, "db-secret", "", "ConnectionSecret name for credentials")
	cmd.Flags().StringVar(&flagDBQuery, "db-query", "", "SQL query for database sources")
	cmd.Flags().StringVar(&flagCSVDelimiter, "csv-delimiter", "", "CSV delimiter character")
	cmd.Flags().BoolVar(&flagCSVHeader, "csv-header", true, "CSV has header row")
	cmd.Flags().BoolVar(&flagCSVNoHeader, "csv-no-header", false, "CSV has no header row")
	cmd.Flags().IntVar(&flagCSVSkipRows, "csv-skip-rows", 0, "Number of rows to skip in CSV")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing multi-doc YAML file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode (fail if required values missing)")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	// Shell completion for flags
	_ = cmd.RegisterFlagCompletionFunc("type", completeDataSourceTypes)
	_ = cmd.RegisterFlagCompletionFunc("db-secret", completeConnectionSecrets)

	return cmd
}

// completeDataSourceTypes provides shell completion for DataSource types.
func completeDataSourceTypes(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"postgres\tPostgreSQL database",
		"mysql\tMySQL database",
		"csv\tCSV file",
		"parquet\tParquet file",
		"excel\tExcel spreadsheet",
		"json\tJSON file",
	}, cobra.ShellCompDirectiveNoFileComp
}

// completeConnectionSecrets provides shell completion for ConnectionSecret names.
func completeConnectionSecrets(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	workdir, err := pathutil.ResolveWorkdir(".")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	manifests, err := ScanManifests(ctx, workdir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	secrets := FilterByKind(manifests, "ConnectionSecret")
	names := make([]string, len(secrets))
	for i, m := range secrets {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// promptDataSourceName prompts for a valid, unique DataSource name.
func promptDataSourceName(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo) (string, error) {
	return addPromptAddString(reader, out, "Name for this DataSource", "", func(name string) error {
		if err := ValidateName(name); err != nil {
			return err
		}
		if !IsNameUnique(manifests, "DataSource", name) {
			existing := FindByName(manifests, "DataSource", name)
			return fmt.Errorf("a DataSource named %q already exists in %s:%d", name, existing.File, existing.Position)
		}
		return nil
	})
}

// promptDataSourceType prompts for data source type selection.
func promptDataSourceType(reader *bufio.Reader, out io.Writer) (DataSourceType, error) {
	options := []SelectOption{
		{Label: "CSV file", Description: "Comma-separated values file"},
		{Label: "Parquet file", Description: "Apache Parquet columnar format"},
		{Label: "Excel file", Description: "Microsoft Excel spreadsheet (.xlsx, .xls)"},
		{Label: "JSON file", Description: "JSON data file"},
		{Label: "PostgreSQL", Description: "PostgreSQL database query"},
		{Label: "MySQL", Description: "MySQL database query"},
	}

	idx, err := addPromptSelect(reader, out, "What type of data source?", options, 0)
	if err != nil {
		return DataSourceTypeNone, err
	}

	types := []DataSourceType{
		DataSourceTypeCSV,
		DataSourceTypeParquet,
		DataSourceTypeExcel,
		DataSourceTypeJSON,
		DataSourceTypePostgres,
		DataSourceTypeMySQL,
	}

	return types[idx], nil
}

// promptConnectionConfig prompts for connection or file configuration.
func promptConnectionConfig(reader *bufio.Reader, out io.Writer, workdir string, data *DataSourceManifestData) error {
	switch data.Type {
	case DataSourceTypePostgres, DataSourceTypeMySQL:
		return promptDatabaseConnection(reader, out, data)
	case DataSourceTypeCSV:
		if err := promptFileSource(reader, out, workdir, data, ".csv"); err != nil {
			return err
		}
		return promptCSVOptions(reader, out, data)
	case DataSourceTypeParquet:
		return promptFileSource(reader, out, workdir, data, ".parquet")
	case DataSourceTypeExcel:
		return promptFileSource(reader, out, workdir, data, ".xlsx")
	case DataSourceTypeJSON:
		return promptFileSource(reader, out, workdir, data, ".json")
	default:
		return fmt.Errorf("unsupported data source type: %s", data.Type)
	}
}

// promptDatabaseConnection prompts for database connection details.
func promptDatabaseConnection(reader *bufio.Reader, out io.Writer, data *DataSourceManifestData) error {
	fmt.Fprintln(out, "\nDatabase Connection Configuration")
	fmt.Fprintln(out, "For security, credentials should be stored in a ConnectionSecret.")
	fmt.Fprintln(out)

	// Default port based on database type
	defaultPort := "5432"
	if data.Type == DataSourceTypeMySQL {
		defaultPort = "3306"
	}

	// Host
	host, err := addPromptString(reader, out, "Host", "localhost")
	if err != nil {
		return err
	}
	data.DBHost = host

	// Port
	portStr, err := addPromptString(reader, out, "Port", defaultPort)
	if err != nil {
		return err
	}
	if port, parseErr := strconv.Atoi(portStr); parseErr == nil {
		data.DBPort = port
	}

	// Database
	database, err := addPromptString(reader, out, "Database name", "")
	if err != nil {
		return err
	}
	data.DBDatabase = database

	// Schema (optional, mainly for PostgreSQL)
	if data.Type == DataSourceTypePostgres {
		schema, err := addPromptString(reader, out, "Schema (optional, default: public)", "")
		if err != nil {
			return err
		}
		if schema != "" && schema != "public" {
			data.DBSchema = schema
		}
	}

	// User
	user, err := addPromptString(reader, out, "Username", "")
	if err != nil {
		return err
	}
	data.DBUser = user

	// Secret (ConnectionSecret name)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Credentials should be stored in a ConnectionSecret manifest.")
	fmt.Fprintln(out, "Create one with: bino add connectionsecret (coming soon)")
	fmt.Fprintln(out)

	secret, err := addPromptString(reader, out, "ConnectionSecret name (leave empty to configure later)", "")
	if err != nil {
		return err
	}
	data.DBSecret = secret

	// Query
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Enter the SQL query to execute against this database.")

	query, err := addPromptString(reader, out, "SQL query (or press Enter to open editor)", "")
	if err != nil {
		return err
	}

	if query == "" {
		// Open editor for query
		template := "-- Enter your SQL query here\nSELECT * FROM your_table;"
		editedQuery, err := promptWithEditor("query", ".sql", template)
		if err != nil {
			// Fall back to simple prompt
			query, err = addPromptString(reader, out, "SQL query", "SELECT * FROM your_table")
			if err != nil {
				return err
			}
		} else {
			query = editedQuery
		}
	}
	data.DBQuery = query

	return nil
}

// promptFileSource prompts for file path selection.
func promptFileSource(reader *bufio.Reader, out io.Writer, workdir string, data *DataSourceManifestData, ext string) error {
	// Search for existing files
	files, _ := searchDataFiles(workdir, ext)

	options := []SelectOption{
		{Label: "Select existing file", Description: fmt.Sprintf("Choose from %d found %s files", len(files), ext)},
		{Label: "Enter path manually", Description: "Type a file path"},
	}

	if len(files) == 0 {
		options = options[1:] // Remove "Select existing" if no files
	}

	idx, err := addPromptSelect(reader, out, "File source", options, 0)
	if err != nil {
		return err
	}

	// Adjust index if we removed an option
	if len(files) == 0 {
		idx++
	}

	switch idx {
	case 0: // Select existing
		items := FilesToFuzzyItems(files, ext+" file")
		item, err := addPromptFuzzySearch(reader, out, "Select file", items, false)
		if err != nil {
			return err
		}
		if item == nil {
			return errAddCanceled
		}
		data.Path = item.Name

	case 1: // Enter path manually
		path, err := addPromptString(reader, out, "File path", "")
		if err != nil {
			return err
		}
		data.Path = path
	}

	return nil
}

// promptCSVOptions prompts for CSV-specific options.
func promptCSVOptions(reader *bufio.Reader, out io.Writer, data *DataSourceManifestData) error {
	// Auto-detect delimiter?
	autoDetect, err := addPromptConfirm(reader, out, "Auto-detect CSV delimiter?", true)
	if err != nil {
		return err
	}

	if !autoDetect {
		delimiter, err := addPromptString(reader, out, "Delimiter character", ",")
		if err != nil {
			return err
		}
		data.CSVDelimiter = delimiter
	}

	// Header row?
	hasHeader, err := addPromptConfirm(reader, out, "CSV has header row?", true)
	if err != nil {
		return err
	}
	data.CSVHeader = &hasHeader

	// Skip rows?
	skipRowsStr, err := addPromptString(reader, out, "Rows to skip at beginning", "0")
	if err != nil {
		return err
	}
	if skipRows, parseErr := strconv.Atoi(skipRowsStr); parseErr == nil && skipRows > 0 {
		data.CSVSkipRows = skipRows
	}

	return nil
}

// searchDataFiles searches for data files in common locations.
func searchDataFiles(dir string, ext string) ([]string, error) {
	var files []string

	// Search in common locations
	searchDirs := []string{
		".",
		"data",
		"input",
		"files",
	}

	for _, searchDir := range searchDirs {
		searchPath := filepath.Join(dir, searchDir)
		if _, err := os.Stat(searchPath); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip errors
			}
			if d.IsDir() {
				// Skip hidden directories and common ignores
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ext) {
				// Get relative path
				rel := path
				if r, err := filepath.Rel(dir, path); err == nil {
					rel = r
				}
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			continue
		}
	}

	return files, nil
}

// writeDataSourceManifest writes the DataSource manifest to the specified path.
func writeDataSourceManifest(cmd *cobra.Command, workdir string, data DataSourceManifestData, outputPath string, appendMode bool) error {
	doc := buildDataSourceDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

// promptDataSourcePostActions shows post-creation action menu.
func promptDataSourcePostActions(reader *bufio.Reader, out io.Writer) error {
	options := []SelectOption{
		{Label: "Done", Description: "Exit the wizard"},
		{Label: "Add a DataSet using this DataSource", Description: "Create a query on this data"},
		{Label: "Add another DataSource", Description: "Run the wizard again"},
		{Label: "Run lint", Description: "Validate manifests with bino lint"},
	}

	idx, err := addPromptSelect(reader, out, "What next?", options, 0)
	if err != nil {
		return nil // Don't error on post-action failures
	}

	switch idx {
	case 0: // Done
		return nil
	case 1: // Add DataSet
		fmt.Fprintln(out, "\nRun 'bino add dataset' to create a DataSet using this DataSource.")
		return nil
	case 2: // Add another
		fmt.Fprintln(out, "\nRun 'bino add datasource' to create another DataSource.")
		return nil
	case 3: // Lint
		fmt.Fprintln(out, "\nRun 'bino lint' to validate manifests.")
		return nil
	}

	return nil
}

// buildDataSourceDocument creates a schema.Document from DataSourceManifestData.
func buildDataSourceDocument(data DataSourceManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindDataSource, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	spec := &schema.DataSourceSpec{
		Type: convertDataSourceType(data.Type),
	}

	// Configure based on type
	switch data.Type {
	case DataSourceTypePostgres, DataSourceTypeMySQL:
		// Database connection
		spec.Connection = &schema.ConnectionSpec{
			Host:     data.DBHost,
			Port:     data.DBPort,
			Database: data.DBDatabase,
			Schema:   data.DBSchema,
			User:     data.DBUser,
			Secret:   data.DBSecret,
		}
		spec.Query = data.DBQuery

	case DataSourceTypeCSV:
		spec.Path = data.Path
		if data.CSVDelimiter != "" && data.CSVDelimiter != "," {
			spec.Delimiter = data.CSVDelimiter
		}
		if data.CSVHeader != nil && !*data.CSVHeader {
			spec.Header = data.CSVHeader
		}
		if data.CSVSkipRows > 0 {
			spec.SkipRows = data.CSVSkipRows
		}

	case DataSourceTypeParquet, DataSourceTypeExcel, DataSourceTypeJSON:
		spec.Path = data.Path
	}

	doc.Spec = spec
	return doc
}

// convertDataSourceType converts CLI DataSourceType to schema.DataSourceType.
func convertDataSourceType(t DataSourceType) schema.DataSourceType {
	switch t {
	case DataSourceTypePostgres:
		return schema.DataSourceTypePostgresQuery
	case DataSourceTypeMySQL:
		return schema.DataSourceTypeMySQLQuery
	case DataSourceTypeCSV:
		return schema.DataSourceTypeCSV
	case DataSourceTypeParquet:
		return schema.DataSourceTypeParquet
	case DataSourceTypeExcel:
		return schema.DataSourceTypeExcel
	case DataSourceTypeJSON:
		return schema.DataSourceTypeJSON
	case DataSourceTypeInline:
		return schema.DataSourceTypeInline
	default:
		return ""
	}
}

// renderDataSourceManifest renders a schema.Document to YAML bytes.
func renderDataSourceManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}
