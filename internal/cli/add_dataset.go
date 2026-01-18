package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/schema"
)

func newAddDataSetCommand() *cobra.Command {
	var (
		flagSQL        string
		flagSQLFile    string
		flagPRQL       string
		flagPRQLFile   string
		flagSource     string
		flagDeps       []string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "dataset [name]",
		Short: "Create a DataSet manifest",
		Long: strings.TrimSpace(`
Create a new DataSet manifest for defining SQL or PRQL queries.

A DataSet transforms data from DataSources or other DataSets using SQL or PRQL queries.
The wizard guides you through query definition, dependency selection, and file placement.

Modes:
  Interactive (default):   Step-by-step wizard for all options
  Semi-interactive:        Provide some flags, prompt for the rest
  Non-interactive:         Use --no-prompt with all required flags
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add dataset

  # Semi-interactive (name provided, prompt for rest)
  bino add dataset sales_monthly

  # Fully specified with SQL file
  bino add dataset sales_monthly \
    --sql-file queries/sales.sql \
    --deps sales_csv \
    --output datasets/sales_monthly.yaml \
    --no-prompt

  # Inline SQL
  bino add dataset quick_stats \
    --sql "SELECT COUNT(*) FROM orders" \
    --output datasets/quick_stats.yaml \
    --no-prompt

  # Pass-through (use DataSource directly)
  bino add dataset raw_sales \
    --source sales_csv \
    --output datasets/raw_sales.yaml \
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

			// Validate flags
			queryCount := 0
			if flagSQL != "" {
				queryCount++
			}
			if flagSQLFile != "" {
				queryCount++
			}
			if flagPRQL != "" {
				queryCount++
			}
			if flagPRQLFile != "" {
				queryCount++
			}
			if flagSource != "" {
				queryCount++
			}
			if queryCount > 1 {
				return ConfigError(fmt.Errorf("only one of --sql, --sql-file, --prql, --prql-file, or --source can be specified"))
			}

			// In non-interactive mode, validate required flags
			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if queryCount == 0 {
					missing = append(missing, "--sql, --sql-file, --prql, --prql-file, or --source")
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

			// Build wizard data
			data := DataSetManifestData{
				Name:         name,
				Description:  flagDesc,
				Constraints:  flagConstraint,
				Dependencies: flagDeps,
			}

			// Set query from flags
			switch {
			case flagSQL != "":
				data.Query = flagSQL
			case flagSQLFile != "":
				data.QueryFile = flagSQLFile
			case flagPRQL != "":
				data.PRQL = flagPRQL
			case flagPRQLFile != "":
				data.PRQLFile = flagPRQLFile
			case flagSource != "":
				data.Source = flagSource
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
				return writeDataSetManifest(cmd, workdir, data, outputPath, appendMode)
			}

			// Run interactive wizard
			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new DataSet manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Step 1: Name & Description
			if data.Name == "" {
				var err error
				data.Name, err = promptDataSetName(reader, out, manifests)
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
				if !IsNameUnique(manifests, "DataSet", data.Name) {
					existing := FindByName(manifests, "DataSet", data.Name)
					return ConfigError(fmt.Errorf("a DataSet named %q already exists in %s:%d", data.Name, existing.File, existing.Position))
				}
			}

			if data.Description == "" {
				desc, err := addPromptString(reader, out, "Description (optional)", "")
				if err != nil {
					return RuntimeError(err)
				}
				data.Description = desc
			}

			// Step 2: Query Type Selection
			if data.Query == "" && data.QueryFile == "" && data.PRQL == "" && data.PRQLFile == "" && data.Source == "" {
				if err := promptQueryType(reader, out, workdir, manifests, &data); err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Step 3: Dependencies Selection
			if len(data.Dependencies) == 0 && data.Source == "" {
				deps, err := promptDependencies(reader, out, manifests)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
				data.Dependencies = deps
			}

			// Step 4: Constraints (Optional)
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm(reader, out, "Add constraints to conditionally include this DataSet?", false)
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "DataSet", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Step 6: Preview & Confirmation
			doc := buildDataSetDocument(data)
			manifestBytes, err := renderDataSetManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render manifest: %w", err))
			}
			manifest := string(manifestBytes)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
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
			if err := writeDataSetManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
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

			return promptPostActions(reader, out)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagSQL, "sql", "", "SQL query (inline)")
	cmd.Flags().StringVar(&flagSQLFile, "sql-file", "", "Path to SQL file")
	cmd.Flags().StringVar(&flagPRQL, "prql", "", "PRQL query (inline)")
	cmd.Flags().StringVar(&flagPRQLFile, "prql-file", "", "Path to PRQL file")
	cmd.Flags().StringVar(&flagSource, "source", "", "DataSource name (pass-through mode)")
	cmd.Flags().StringSliceVar(&flagDeps, "deps", nil, "Dependencies (comma-separated)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing multi-doc YAML file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode (fail if required values missing)")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	// Shell completion for flags
	_ = cmd.RegisterFlagCompletionFunc("source", completeDatasources)
	_ = cmd.RegisterFlagCompletionFunc("deps", completeDatasetsAndDatasources)

	return cmd
}

// completeDatasources provides shell completion for DataSource names.
func completeDatasources(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	workdir, err := pathutil.ResolveWorkdir(".")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	manifests, err := ScanManifests(ctx, workdir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	sources := FilterByKind(manifests, "DataSource")
	names := make([]string, len(sources))
	for i, m := range sources {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeDatasetsAndDatasources provides shell completion for DataSet and DataSource names.
func completeDatasetsAndDatasources(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	workdir, err := pathutil.ResolveWorkdir(".")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	manifests, err := ScanManifests(ctx, workdir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	available := FilterByKind(manifests, "DataSet", "DataSource")
	names := make([]string, len(available))
	for i, m := range available {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// promptDataSetName prompts for a valid, unique DataSet name.
func promptDataSetName(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo) (string, error) {
	return addPromptAddString(reader, out, "Name for this DataSet", "", func(name string) error {
		if err := ValidateName(name); err != nil {
			return err
		}
		if !IsNameUnique(manifests, "DataSet", name) {
			existing := FindByName(manifests, "DataSet", name)
			return fmt.Errorf("a DataSet named %q already exists in %s:%d", name, existing.File, existing.Position)
		}
		return nil
	})
}

// promptQueryType prompts for query type selection.
func promptQueryType(reader *bufio.Reader, out io.Writer, workdir string, manifests []ManifestInfo, data *DataSetManifestData) error {
	options := []SelectOption{
		{Label: "SQL (write inline)", Description: "Open editor to write SQL query"},
		{Label: "SQL (from file)", Description: "Reference an external .sql file"},
		{Label: "PRQL (write inline)", Description: "Open editor to write PRQL query"},
		{Label: "PRQL (from file)", Description: "Reference an external .prql file"},
		{Label: "Pass-through", Description: "Use a DataSource directly without transformation"},
	}

	idx, err := addPromptSelect(reader, out, "How do you want to define your data?", options, 0)
	if err != nil {
		return err
	}

	switch idx {
	case 0: // SQL inline
		template := fmt.Sprintf("-- DataSet: %s\n-- Description: %s\n\nSELECT \n  -- Add your columns here\nFROM your_table\n-- WHERE\n-- GROUP BY\n-- ORDER BY\n", data.Name, data.Description)
		query, err := promptWithEditor("bino-sql-", ".sql", template)
		if err != nil {
			// Fallback to multiline input
			fmt.Fprintln(out, "Editor not available. Enter SQL query (end with empty line):")
			query, err = promptMultiline(reader, out)
			if err != nil {
				return err
			}
		}
		data.Query = strings.TrimSpace(query)
		fmt.Fprintln(out, "\nQuery preview:")
		fmt.Fprintln(out, previewLines(data.Query, 5))

	case 1: // SQL file
		path, err := promptQueryFile(reader, out, workdir, ".sql", data.Name)
		if err != nil {
			return err
		}
		data.QueryFile = path

	case 2: // PRQL inline
		template := fmt.Sprintf("# DataSet: %s\n# Description: %s\n\nfrom your_table\n# filter condition\n# select [columns]\n# sort column\n", data.Name, data.Description)
		query, err := promptWithEditor("bino-prql-", ".prql", template)
		if err != nil {
			// Fallback to multiline input
			fmt.Fprintln(out, "Editor not available. Enter PRQL query (end with empty line):")
			query, err = promptMultiline(reader, out)
			if err != nil {
				return err
			}
		}
		data.PRQL = strings.TrimSpace(query)
		fmt.Fprintln(out, "\nQuery preview:")
		fmt.Fprintln(out, previewLines(data.PRQL, 5))

	case 3: // PRQL file
		path, err := promptQueryFile(reader, out, workdir, ".prql", data.Name)
		if err != nil {
			return err
		}
		data.PRQLFile = path

	case 4: // Pass-through
		source, err := promptDataSource(reader, out, manifests)
		if err != nil {
			return err
		}
		data.Source = source
	}

	return nil
}

// promptQueryFile prompts for an SQL or PRQL file path.
func promptQueryFile(reader *bufio.Reader, out io.Writer, workdir, ext, datasetName string) (string, error) {
	// Search for existing files
	files, _ := SearchQueryFiles(workdir, ext)

	options := []SelectOption{
		{Label: "Select existing file", Description: fmt.Sprintf("Choose from %d found %s files", len(files), ext)},
		{Label: "Create new file", Description: fmt.Sprintf("Create queries/%s%s", datasetName, ext)},
		{Label: "Custom path", Description: "Enter a custom file path"},
	}

	if len(files) == 0 {
		options = options[1:] // Remove "Select existing" if no files
	}

	idx, err := addPromptSelect(reader, out, "File source", options, 0)
	if err != nil {
		return "", err
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
			return "", err
		}
		if item == nil {
			return "", errAddCanceled
		}
		// Return relative path
		rel := item.Name
		return rel, nil

	case 1: // Create new
		suggestedPath := filepath.Join("queries", datasetName+ext)
		path, err := addPromptString(reader, out, "File path", suggestedPath)
		if err != nil {
			return "", err
		}

		// Create the file with template
		absPath := filepath.Join(workdir, path)
		if err := pathutil.EnsureDir(filepath.Dir(absPath)); err != nil {
			return "", err
		}

		var template string
		if ext == ".sql" {
			template = fmt.Sprintf("-- DataSet: %s\n\nSELECT \n  *\nFROM your_table\n", datasetName)
		} else {
			template = fmt.Sprintf("# DataSet: %s\n\nfrom your_table\n", datasetName)
		}

		if err := os.WriteFile(absPath, []byte(template), 0o644); err != nil {
			return "", fmt.Errorf("create file: %w", err)
		}

		fmt.Fprintf(out, "Created %s\n", path)

		// Offer to edit
		edit, err := addPromptConfirm(reader, out, "Open in editor now?", true)
		if err != nil {
			return "", err
		}
		if edit {
			editor := getEditor()
			if editor != "" {
				args := buildEditorArgs(editor, absPath)
				execCmd := exec.Command(args[0], args[1:]...)
				execCmd.Stdin = os.Stdin
				execCmd.Stdout = os.Stdout
				execCmd.Stderr = os.Stderr
				_ = execCmd.Run()
			}
		}

		return path, nil

	case 2: // Custom path
		path, err := addPromptString(reader, out, "File path", "")
		if err != nil {
			return "", err
		}
		return path, nil
	}

	return "", nil
}

// promptDataSource prompts for a DataSource selection.
func promptDataSource(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo) (string, error) {
	sources := FilterByKind(manifests, "DataSource")
	if len(sources) == 0 {
		fmt.Fprintln(out, "No DataSources found. Enter a name manually.")
		return addPromptString(reader, out, "DataSource name", "")
	}

	items := ManifestsToFuzzyItems(sources)
	item, err := addPromptFuzzySearch(reader, out, "Select DataSource", items, false)
	if err != nil {
		return "", err
	}
	if item == nil {
		return "", errAddCanceled
	}

	return item.Name, nil
}

// promptDependencies prompts for dependency selection.
func promptDependencies(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo) ([]string, error) {
	addDeps, err := addPromptConfirm(reader, out, "Does this query depend on other DataSets or DataSources?", false)
	if err != nil {
		return nil, err
	}
	if !addDeps {
		return nil, nil
	}

	// Filter to DataSets and DataSources
	available := FilterByKind(manifests, "DataSet", "DataSource")
	if len(available) == 0 {
		fmt.Fprintln(out, "No DataSets or DataSources found. You can add dependencies manually later.")
		return nil, nil
	}

	items := ManifestsToFuzzyItems(available)
	selected, err := addPromptMultiFuzzySearch(reader, out, "Select dependencies", items)
	if err != nil {
		return nil, err
	}

	deps := make([]string, len(selected))
	for i, item := range selected {
		deps[i] = item.Name
	}

	return deps, nil
}

// promptOutputLocation prompts for output file location.
func promptOutputLocation(reader *bufio.Reader, out io.Writer, workdir string, manifests []ManifestInfo, kind, name string) (string, bool, error) {
	// Load user preferences
	cfg, _ := LoadAddConfig(workdir)
	kindCfg := cfg.GetKindConfig(kind)

	// Detect file pattern
	pattern := DetectFilePattern(manifests, kind)

	// Override with user preference if set
	if kindCfg != nil && kindCfg.Mode != "" {
		pattern = KindConfigToFilePattern(kindCfg)
	}

	// Suggest path based on pattern
	suggestedPath := SuggestOutputPath(pattern, name, kind)

	// Get multi-document files
	multiDocFiles := GetMultiDocFiles(manifests)

	// Build options
	var options []SelectOption
	options = append(options, SelectOption{
		Label:       fmt.Sprintf("Auto: %s", suggestedPath),
		Description: "Recommended based on project pattern",
	})

	for _, f := range multiDocFiles {
		rel := f
		if wd, err := os.Getwd(); err == nil {
			if r, err := filepath.Rel(wd, f); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		options = append(options, SelectOption{
			Label:       fmt.Sprintf("Append to: %s", rel),
			Description: "Add to existing multi-document file",
		})
	}

	options = append(options, SelectOption{
		Label:       "Custom path...",
		Description: "Enter a custom file path",
	})

	idx, err := addPromptSelect(reader, out, "Where to save the manifest?", options, 0)
	if err != nil {
		return "", false, err
	}

	switch {
	case idx == 0:
		// Auto suggestion
		if pattern.Mode == "multi-document" && pattern.File != "" {
			return pattern.File, true, nil
		}
		return suggestedPath, false, nil

	case idx > 0 && idx <= len(multiDocFiles):
		// Append to multi-doc file
		return multiDocFiles[idx-1], true, nil

	default:
		// Custom path
		path, err := addPromptString(reader, out, "File path", suggestedPath)
		if err != nil {
			return "", false, err
		}
		return path, false, nil
	}
}

// buildDataSetDocument converts DataSetManifestData to a schema.Document.
func buildDataSetDocument(data DataSetManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindDataSet, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = data.Constraints

	spec := &schema.DataSetSpec{}

	// Set query/prql/source
	switch {
	case data.Query != "":
		spec.Query = &schema.QueryField{Inline: data.Query}
	case data.QueryFile != "":
		spec.Query = &schema.QueryField{File: data.QueryFile}
	case data.PRQL != "":
		spec.Prql = &schema.QueryField{Inline: data.PRQL}
	case data.PRQLFile != "":
		spec.Prql = &schema.QueryField{File: data.PRQLFile}
	case data.Source != "":
		spec.Source = &schema.DataSourceRef{Ref: data.Source}
	}

	// Set dependencies
	for _, dep := range data.Dependencies {
		spec.Dependencies = append(spec.Dependencies, schema.DataSourceRef{Ref: dep})
	}

	doc.Spec = spec
	return doc
}

// renderDataSetManifest marshals a schema.Document to YAML bytes.
func renderDataSetManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

// writeDataSetManifest writes the DataSet manifest to the specified path.
func writeDataSetManifest(cmd *cobra.Command, workdir string, data DataSetManifestData, outputPath string, appendMode bool) error {
	doc := buildDataSetDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

// promptPostActions shows post-creation action menu.
func promptPostActions(reader *bufio.Reader, out io.Writer) error {
	options := []SelectOption{
		{Label: "Done", Description: "Exit the wizard"},
		{Label: "Add another DataSet", Description: "Run the wizard again"},
		{Label: "Run lint", Description: "Validate manifests with bino lint"},
		{Label: "Run preview", Description: "Start development preview server"},
	}

	idx, err := addPromptSelect(reader, out, "What next?", options, 0)
	if err != nil {
		return nil // Don't error on post-action failures
	}

	switch idx {
	case 0: // Done
		return nil
	case 1: // Add another
		fmt.Fprintln(out, "\nRun 'bino add dataset' to create another DataSet.")
		return nil
	case 2: // Lint
		fmt.Fprintln(out, "\nRun 'bino lint' to validate manifests.")
		return nil
	case 3: // Preview
		fmt.Fprintln(out, "\nRun 'bino preview' to start the development server.")
		return nil
	}

	return nil
}

// promptMultiline reads multiple lines until an empty line.
func promptMultiline(reader *bufio.Reader, out io.Writer) (string, error) {
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		trimmed := strings.TrimRight(line, "\n\r")
		if trimmed == "" {
			break
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n"), nil
}
