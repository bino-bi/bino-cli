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

// TableManifestData holds data for rendering a Table manifest.
type TableManifestData struct {
	Name        string
	Description string
	Constraints []string
	Dataset     string
	Title       string
}

// ChartStructureManifestData holds data for rendering a ChartStructure manifest.
type ChartStructureManifestData struct {
	Name        string
	Description string
	Constraints []string
	Dataset     string
	Title       string
	ChartType   string // bar, pie, donut, etc.
}

// ChartTimeManifestData holds data for rendering a ChartTime manifest.
type ChartTimeManifestData struct {
	Name        string
	Description string
	Constraints []string
	Dataset     string
	Title       string
	ChartType   string // line, bar, area
}

func newAddTableCommand() *cobra.Command {
	var (
		flagDataset    string
		flagTitle      string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "table [name]",
		Short: "Create a Table manifest",
		Long: strings.TrimSpace(`
Create a new Table manifest for displaying data in tabular format.

A Table component displays data from a DataSet in a formatted table.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add table

  # With dataset reference
  bino add table sales_table \
    --dataset sales_data \
    --title "Monthly Sales" \
    --output components/tables.yaml \
    --no-prompt
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			workdir, err := pathutil.ResolveWorkdir(".")
			if err != nil {
				return ConfigError(err)
			}

			nonInteractive := flagNoPrompt || !isInteractive()

			var name string
			if len(args) > 0 {
				name = args[0]
			}

			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if flagDataset == "" {
					missing = append(missing, "--dataset")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := TableManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Dataset:     flagDataset,
				Title:       flagTitle,
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
			}

			if nonInteractive {
				return writeTableManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new Table manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "Table")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString(reader, out, "Description (optional)", "")
			}

			// Dataset selection
			if data.Dataset == "" {
				data.Dataset, err = promptDatasetSelection(reader, out, manifests)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Title
			if data.Title == "" {
				data.Title, _ = addPromptString(reader, out, "Table title (optional)", "")
			}

			// Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm(reader, out, "Add constraints?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					data.Constraints, _ = addPromptConstraintBuilder(reader, out)
				}
			}

			// Output
			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "Table", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			doc := buildTableDocument(data)
			manifestBytes, err := renderTableManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeTableManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...)
					execCmd.Stdin = os.Stdin
					execCmd.Stdout = os.Stdout
					execCmd.Stderr = os.Stderr
					_ = execCmd.Run()
				}
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagDataset, "dataset", "", "DataSet name (required)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Table title")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)

	return cmd
}

func newAddChartStructureCommand() *cobra.Command {
	var (
		flagDataset    string
		flagTitle      string
		flagType       string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "chartstructure [name]",
		Short: "Create a ChartStructure manifest",
		Long: strings.TrimSpace(`
Create a new ChartStructure manifest for structural charts.

ChartStructure displays data from a DataSet as a structural chart:
  - bar: Horizontal or vertical bar chart
  - pie: Pie chart
  - donut: Donut chart
  - radar: Radar/spider chart
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add chartstructure

  # Bar chart
  bino add chartstructure sales_by_region \
    --dataset region_sales \
    --type bar \
    --title "Sales by Region" \
    --output components/charts.yaml \
    --no-prompt

  # Pie chart
  bino add chartstructure category_breakdown \
    --dataset category_data \
    --type pie \
    --output components/charts.yaml \
    --no-prompt
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			workdir, err := pathutil.ResolveWorkdir(".")
			if err != nil {
				return ConfigError(err)
			}

			nonInteractive := flagNoPrompt || !isInteractive()

			var name string
			if len(args) > 0 {
				name = args[0]
			}

			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if flagDataset == "" {
					missing = append(missing, "--dataset")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := ChartStructureManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Dataset:     flagDataset,
				Title:       flagTitle,
				ChartType:   flagType,
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
			}

			if nonInteractive {
				return writeChartStructureManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ChartStructure manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "ChartStructure")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString(reader, out, "Description (optional)", "")
			}

			// Dataset selection
			if data.Dataset == "" {
				data.Dataset, err = promptDatasetSelection(reader, out, manifests)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Chart type
			if data.ChartType == "" {
				options := []SelectOption{
					{Label: "bar", Description: "Bar chart (horizontal or vertical)"},
					{Label: "pie", Description: "Pie chart"},
					{Label: "donut", Description: "Donut chart"},
					{Label: "radar", Description: "Radar/spider chart"},
				}

				idx, err := addPromptSelect(reader, out, "Chart type", options, 0)
				if err != nil {
					return RuntimeError(err)
				}

				types := []string{"bar", "pie", "donut", "radar"}
				data.ChartType = types[idx]
			}

			// Title
			if data.Title == "" {
				data.Title, _ = addPromptString(reader, out, "Chart title (optional)", "")
			}

			// Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm(reader, out, "Add constraints?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					data.Constraints, _ = addPromptConstraintBuilder(reader, out)
				}
			}

			// Output
			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "ChartStructure", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			doc := buildChartStructureDocument(data)
			manifestBytes, err := renderChartStructureManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeChartStructureManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...)
					execCmd.Stdin = os.Stdin
					execCmd.Stdout = os.Stdout
					execCmd.Stderr = os.Stderr
					_ = execCmd.Run()
				}
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagDataset, "dataset", "", "DataSet name (required)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Chart title")
	cmd.Flags().StringVar(&flagType, "type", "", "Chart type (bar, pie, donut, radar)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)
	_ = cmd.RegisterFlagCompletionFunc("type", completeChartStructureTypes)

	return cmd
}

func newAddChartTimeCommand() *cobra.Command {
	var (
		flagDataset    string
		flagTitle      string
		flagType       string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "charttime [name]",
		Short: "Create a ChartTime manifest",
		Long: strings.TrimSpace(`
Create a new ChartTime manifest for time-series charts.

ChartTime displays time-series data from a DataSet:
  - line: Line chart for trends
  - bar: Bar chart for time periods
  - area: Area chart for cumulative data
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add charttime

  # Line chart
  bino add charttime sales_trend \
    --dataset monthly_sales \
    --type line \
    --title "Sales Trend" \
    --output components/charts.yaml \
    --no-prompt

  # Area chart
  bino add charttime cumulative_revenue \
    --dataset revenue_data \
    --type area \
    --output components/charts.yaml \
    --no-prompt
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			workdir, err := pathutil.ResolveWorkdir(".")
			if err != nil {
				return ConfigError(err)
			}

			nonInteractive := flagNoPrompt || !isInteractive()

			var name string
			if len(args) > 0 {
				name = args[0]
			}

			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if flagDataset == "" {
					missing = append(missing, "--dataset")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := ChartTimeManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Dataset:     flagDataset,
				Title:       flagTitle,
				ChartType:   flagType,
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
			}

			if nonInteractive {
				return writeChartTimeManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ChartTime manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "ChartTime")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString(reader, out, "Description (optional)", "")
			}

			// Dataset selection
			if data.Dataset == "" {
				data.Dataset, err = promptDatasetSelection(reader, out, manifests)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Chart type
			if data.ChartType == "" {
				options := []SelectOption{
					{Label: "line", Description: "Line chart for trends"},
					{Label: "bar", Description: "Bar chart for time periods"},
					{Label: "area", Description: "Area chart for cumulative data"},
				}

				idx, err := addPromptSelect(reader, out, "Chart type", options, 0)
				if err != nil {
					return RuntimeError(err)
				}

				types := []string{"line", "bar", "area"}
				data.ChartType = types[idx]
			}

			// Title
			if data.Title == "" {
				data.Title, _ = addPromptString(reader, out, "Chart title (optional)", "")
			}

			// Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm(reader, out, "Add constraints?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					data.Constraints, _ = addPromptConstraintBuilder(reader, out)
				}
			}

			// Output
			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "ChartTime", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			doc := buildChartTimeDocument(data)
			manifestBytes, err := renderChartTimeManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeChartTimeManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...)
					execCmd.Stdin = os.Stdin
					execCmd.Stdout = os.Stdout
					execCmd.Stderr = os.Stderr
					_ = execCmd.Run()
				}
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagDataset, "dataset", "", "DataSet name (required)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Chart title")
	cmd.Flags().StringVar(&flagType, "type", "", "Chart type (line, bar, area)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)
	_ = cmd.RegisterFlagCompletionFunc("type", completeChartTimeTypes)

	return cmd
}

// Helper functions

func promptDatasetSelection(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo) (string, error) {
	datasets := FilterByKind(manifests, "DataSet")

	if len(datasets) == 0 {
		fmt.Fprintln(out, "No DataSets found. Enter a name manually.")
		return addPromptString(reader, out, "DataSet name", "")
	}

	items := ManifestsToFuzzyItems(datasets)
	item, err := addPromptFuzzySearch(reader, out, "Select DataSet", items, false)
	if err != nil {
		return "", err
	}
	if item == nil {
		return "", errAddCanceled
	}

	return item.Name, nil
}

// Completion functions

func completeChartStructureTypes(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"bar\tBar chart",
		"pie\tPie chart",
		"donut\tDonut chart",
		"radar\tRadar chart",
	}, cobra.ShellCompDirectiveNoFileComp
}

func completeChartTimeTypes(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"line\tLine chart",
		"bar\tBar chart",
		"area\tArea chart",
	}, cobra.ShellCompDirectiveNoFileComp
}

// Write functions

func writeTableManifest(cmd *cobra.Command, workdir string, data TableManifestData, outputPath string, appendMode bool) error {
	doc := buildTableDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

func writeChartStructureManifest(cmd *cobra.Command, workdir string, data ChartStructureManifestData, outputPath string, appendMode bool) error {
	doc := buildChartStructureDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

func writeChartTimeManifest(cmd *cobra.Command, workdir string, data ChartTimeManifestData, outputPath string, appendMode bool) error {
	doc := buildChartTimeDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

// Build and render functions

func buildTableDocument(data TableManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindTable, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = data.Constraints

	spec := &schema.TableSpec{
		Dataset:    "$" + data.Dataset,
		TableTitle: data.Title,
	}

	doc.Spec = spec
	return doc
}

func renderTableManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

func buildChartStructureDocument(data ChartStructureManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindChartStructure, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = data.Constraints

	spec := &schema.ChartStructureSpec{
		Dataset:    "$" + data.Dataset,
		ChartTitle: data.Title,
		Type:       data.ChartType,
	}

	doc.Spec = spec
	return doc
}

func renderChartStructureManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

func buildChartTimeDocument(data ChartTimeManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindChartTime, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = data.Constraints

	spec := &schema.ChartTimeSpec{
		Dataset:    "$" + data.Dataset,
		ChartTitle: data.Title,
		Type:       data.ChartType,
	}

	doc.Spec = spec
	return doc
}

func renderChartTimeManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}
