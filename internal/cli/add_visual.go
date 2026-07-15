package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	Type        string
	SumTitle    string
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
}

// ChartScatterManifestData holds data for rendering a ChartScatter manifest.
type ChartScatterManifestData struct {
	Name        string
	Description string
	Constraints []string
	Dataset     string
	X           string
	Y           string
	Title       string
}

// ChartBubbleManifestData holds data for rendering a ChartBubble manifest.
type ChartBubbleManifestData struct {
	Name        string
	Description string
	Constraints []string
	Dataset     string
	X           string
	Y           string
	Size        string
	Title       string
}

func newAddTableCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
	var (
		flagDataset    string
		flagType       string
		flagSumTitle   string
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
				Type:        flagType,
				SumTitle:    flagSumTitle,
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
						fmt.Fprintln(out, "\nCanceled.")
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
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Table type
			if data.Type == "" {
				data.Type, _ = addPromptString(reader, out, "Table type (list, sum, opt, sumnototal, optnototal)", "list")
			}

			// Sum row label — only the sum and opt types render a total row to label.
			if data.SumTitle == "" && (data.Type == "sum" || data.Type == "opt") {
				data.SumTitle, _ = addPromptString(reader, out, "Label for the grand-total row (optional)", "")
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
						fmt.Fprintln(out, "\nCanceled.")
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
				fmt.Fprintln(out, "\nCanceled.")
				return nil
			}

			if err := writeTableManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: intentionally launching user's editor; interactive editor, no cancellation needed
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
	cmd.Flags().StringVar(&flagType, "type", "", "Table type: list, sum, opt, sumnototal, optnototal (default list)")
	cmd.Flags().StringVar(&flagSumTitle, "sum-title", "", "Label for the grand-total row; only rendered for --type sum or opt")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)

	return cmd
}

func newAddChartStructureCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
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
						fmt.Fprintln(out, "\nCanceled.")
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
						fmt.Fprintln(out, "\nCanceled.")
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

				idx, err := addPromptSelect(reader, out, "Chart type", options)
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
						fmt.Fprintln(out, "\nCanceled.")
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
				fmt.Fprintln(out, "\nCanceled.")
				return nil
			}

			if err := writeChartStructureManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: intentionally launching user's editor; interactive editor, no cancellation needed
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

func newAddChartTimeCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
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
		Use:   "charttime [name]",
		Short: "Create a ChartTime manifest",
		Long: strings.TrimSpace(`
Create a new ChartTime manifest for time-series charts.

ChartTime displays time-series data from a DataSet.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add charttime

  # With options
  bino add charttime sales_trend \
    --dataset monthly_sales \
    --title "Sales Trend" \
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
						fmt.Fprintln(out, "\nCanceled.")
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
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
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
						fmt.Fprintln(out, "\nCanceled.")
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
				fmt.Fprintln(out, "\nCanceled.")
				return nil
			}

			if err := writeChartTimeManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: intentionally launching user's editor; interactive editor, no cancellation needed
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
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)

	return cmd
}

func newAddChartScatterCommand() *cobra.Command { //nolint:gocognit // mirrors the other visualization wizards
	var (
		flagDataset    string
		flagX          string
		flagY          string
		flagTitle      string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "chartscatter [name]",
		Short: "Create a ChartScatter manifest",
		Long: strings.TrimSpace(`
Create a new ChartScatter manifest for XY scatter charts (IBCS C09).

ChartScatter plots one point per row from a DataSet on two numeric value
axes. The x and y measures are scenario slots (ac1-ac4, pp1-pp4, fc1-fc4,
pl1-pl4) or variance tokens (e.g. dac1_pp1, drac1_pl1).
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add chartscatter

  # With options
  bino add chartscatter product_margin \
    --dataset products \
    --x ac1 --y ac2 \
    --title "Margin vs. net sales" \
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
				if flagX == "" {
					missing = append(missing, "--x")
				}
				if flagY == "" {
					missing = append(missing, "--y")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
			}
			for _, token := range []struct{ flag, value string }{{"--x", flagX}, {"--y", flagY}} {
				if token.value != "" && !measureTokenRegex.MatchString(token.value) {
					return ConfigError(fmt.Errorf("%s: invalid measure token %q (expected a scenario slot like ac1 or a variance token like dac1_pp1)", token.flag, token.value))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := ChartScatterManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Dataset:     flagDataset,
				X:           flagX,
				Y:           flagY,
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
				return writeChartScatterManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ChartScatter manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "ChartScatter")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
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
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Axis measures
			if data.X == "" {
				data.X, err = promptMeasureToken(reader, out, "X-axis measure (e.g. ac1 or dac1_pp1)")
				if err != nil {
					return RuntimeError(err)
				}
			}
			if data.Y == "" {
				data.Y, err = promptMeasureToken(reader, out, "Y-axis measure (e.g. ac2)")
				if err != nil {
					return RuntimeError(err)
				}
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "ChartScatter", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			doc := buildChartScatterDocument(data)
			manifestBytes, err := renderChartScatterManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCanceled.")
				return nil
			}

			if err := writeChartScatterManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: intentionally launching user's editor; interactive editor, no cancellation needed
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
	cmd.Flags().StringVar(&flagX, "x", "", "X-axis measure token (required, e.g. ac1 or dac1_pp1)")
	cmd.Flags().StringVar(&flagY, "y", "", "Y-axis measure token (required, e.g. ac2)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Chart title")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)
	_ = cmd.RegisterFlagCompletionFunc("x", completeMeasureTokens)
	_ = cmd.RegisterFlagCompletionFunc("y", completeMeasureTokens)

	return cmd
}

func newAddChartBubbleCommand() *cobra.Command { //nolint:gocognit // mirrors the other visualization wizards
	var (
		flagDataset    string
		flagX          string
		flagY          string
		flagSize       string
		flagTitle      string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "chartbubble [name]",
		Short: "Create a ChartBubble manifest",
		Long: strings.TrimSpace(`
Create a new ChartBubble manifest for XY bubble portfolio charts (IBCS C10).

ChartBubble plots one bubble per row from a DataSet on two numeric value
axes, with the bubble area sized by a third measure. The x, y, and size
measures are scenario slots (ac1-ac4, pp1-pp4, fc1-fc4, pl1-pl4) or
variance tokens (e.g. dac1_pp1); size values must be >= 0.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add chartbubble

  # With options
  bino add chartbubble portfolio \
    --dataset business_units \
    --x ac1 --y ac2 --size ac3 \
    --title "Portfolio" \
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
				if flagX == "" {
					missing = append(missing, "--x")
				}
				if flagY == "" {
					missing = append(missing, "--y")
				}
				if flagSize == "" {
					missing = append(missing, "--size")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
			}
			for _, token := range []struct{ flag, value string }{{"--x", flagX}, {"--y", flagY}, {"--size", flagSize}} {
				if token.value != "" && !measureTokenRegex.MatchString(token.value) {
					return ConfigError(fmt.Errorf("%s: invalid measure token %q (expected a scenario slot like ac1 or a variance token like dac1_pp1)", token.flag, token.value))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := ChartBubbleManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Dataset:     flagDataset,
				X:           flagX,
				Y:           flagY,
				Size:        flagSize,
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
				return writeChartBubbleManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ChartBubble manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "ChartBubble")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
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
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Axis and size measures
			if data.X == "" {
				data.X, err = promptMeasureToken(reader, out, "X-axis measure (e.g. ac1 or dac1_pp1)")
				if err != nil {
					return RuntimeError(err)
				}
			}
			if data.Y == "" {
				data.Y, err = promptMeasureToken(reader, out, "Y-axis measure (e.g. ac2)")
				if err != nil {
					return RuntimeError(err)
				}
			}
			if data.Size == "" {
				data.Size, err = promptMeasureToken(reader, out, "Size measure (e.g. ac3)")
				if err != nil {
					return RuntimeError(err)
				}
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "ChartBubble", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			doc := buildChartBubbleDocument(data)
			manifestBytes, err := renderChartBubbleManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCanceled.")
				return nil
			}

			if err := writeChartBubbleManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: intentionally launching user's editor; interactive editor, no cancellation needed
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
	cmd.Flags().StringVar(&flagX, "x", "", "X-axis measure token (required, e.g. ac1 or dac1_pp1)")
	cmd.Flags().StringVar(&flagY, "y", "", "Y-axis measure token (required, e.g. ac2)")
	cmd.Flags().StringVar(&flagSize, "size", "", "Bubble size measure token (required, e.g. ac3)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Chart title")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)
	_ = cmd.RegisterFlagCompletionFunc("x", completeMeasureTokens)
	_ = cmd.RegisterFlagCompletionFunc("y", completeMeasureTokens)
	_ = cmd.RegisterFlagCompletionFunc("size", completeMeasureTokens)

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
	item, err := addPromptFuzzySearch(reader, out, "Select DataSet", items)
	if err != nil {
		return "", err
	}
	if item == nil {
		return "", errAddCanceled
	}

	return item.Name, nil
}

// measureTokenRegex validates XY chart measure tokens: a scenario slot
// (ac1-ac4, pp1-pp4, fc1-fc4, pl1-pl4) or a variance token
// (d/dr + base_delta with an optional _pos/_neg/_neu sentiment suffix).
var measureTokenRegex = regexp.MustCompile(`^((ac|pp|fc|pl)[1-4]|(dr|d)(ac|pp|fc|pl)[1-4]_(ac|pp|fc|pl)[1-4](_(pos|neg|neu))?)$`)

// promptMeasureToken prompts for a required XY chart measure token until the
// input is valid.
func promptMeasureToken(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	for {
		value, err := addPromptString(reader, out, label, "")
		if err != nil {
			return "", err
		}
		if measureTokenRegex.MatchString(value) {
			return value, nil
		}
		fmt.Fprintf(out, "Invalid measure token %q. Expected a scenario slot like ac1 or a variance token like dac1_pp1.\n", value)
	}
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

func completeMeasureTokens(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	var tokens []string
	for _, family := range []string{"ac", "pp", "fc", "pl"} {
		for slot := 1; slot <= 4; slot++ {
			tokens = append(tokens, fmt.Sprintf("%s%d\tScenario slot", family, slot))
		}
	}
	return tokens, cobra.ShellCompDirectiveNoFileComp
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

func writeChartScatterManifest(cmd *cobra.Command, workdir string, data ChartScatterManifestData, outputPath string, appendMode bool) error {
	doc := buildChartScatterDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

func writeChartBubbleManifest(cmd *cobra.Command, workdir string, data ChartBubbleManifestData, outputPath string, appendMode bool) error {
	doc := buildChartBubbleDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

// Build and render functions

func buildTableDocument(data TableManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindTable, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	spec := &schema.TableSpec{
		Dataset:  "$" + data.Dataset,
		Type:     data.Type,
		SumTitle: data.SumTitle,
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
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

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
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	spec := &schema.ChartTimeSpec{
		Dataset:    "$" + data.Dataset,
		ChartTitle: data.Title,
	}

	doc.Spec = spec
	return doc
}

func renderChartTimeManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

func buildChartScatterDocument(data ChartScatterManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindChartScatter, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	spec := &schema.ChartScatterSpec{
		Dataset:    "$" + data.Dataset,
		X:          data.X,
		Y:          data.Y,
		ChartTitle: data.Title,
	}

	doc.Spec = spec
	return doc
}

func renderChartScatterManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

func buildChartBubbleDocument(data ChartBubbleManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindChartBubble, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	spec := &schema.ChartBubbleSpec{
		Dataset:    "$" + data.Dataset,
		X:          data.X,
		Y:          data.Y,
		Size:       data.Size,
		ChartTitle: data.Title,
	}

	doc.Spec = spec
	return doc
}

func renderChartBubbleManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}
