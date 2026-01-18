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

// LayoutPageManifestData holds data for rendering a LayoutPage manifest.
type LayoutPageManifestData struct {
	Name        string
	Description string
	Constraints []string
	Children    []string // Component names
}

// LayoutCardManifestData holds data for rendering a LayoutCard manifest.
type LayoutCardManifestData struct {
	Name        string
	Description string
	Constraints []string
	Title       string
	Children    []string // Component names
}

func newAddLayoutPageCommand() *cobra.Command {
	var (
		flagChildren   []string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "layoutpage [name]",
		Short: "Create a LayoutPage manifest",
		Long: strings.TrimSpace(`
Create a new LayoutPage manifest as a page container.

LayoutPage is the top-level container for report content. It contains
child components like Text, Table, Charts, and LayoutCards.

The wizard creates a skeleton with an empty children array that you can
populate with component references later.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add layoutpage

  # Create with name
  bino add layoutpage summary_page \
    --output layouts/summary.yaml \
    --no-prompt

  # With initial children
  bino add layoutpage detail_page \
    --children header_text,sales_table,footer_text \
    --output layouts/detail.yaml \
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

			data := LayoutPageManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Children:    flagChildren,
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
				return writeLayoutPageManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new LayoutPage manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "LayoutPage")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			} else {
				if err := ValidateName(data.Name); err != nil {
					return ConfigError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString(reader, out, "Description (optional)", "")
			}

			// Children selection
			if len(data.Children) == 0 {
				addChildren, err := addPromptConfirm(reader, out, "Add child components now?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addChildren {
					data.Children, err = promptLayoutChildren(reader, out, manifests)
					if err != nil {
						return RuntimeError(err)
					}
				}
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "LayoutPage", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			doc := buildLayoutPageDocument(data)
			manifestBytes, err := renderLayoutPageManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")

			if len(data.Children) == 0 {
				fmt.Fprintln(out, "\nNote: The children array is empty. Add components to the page after creation.")
			}

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeLayoutPageManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
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

	cmd.Flags().StringSliceVar(&flagChildren, "children", nil, "Child component names (comma-separated)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("children", completeLayoutComponents)

	return cmd
}

func newAddLayoutCardCommand() *cobra.Command {
	var (
		flagTitle      string
		flagChildren   []string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "layoutcard [name]",
		Short: "Create a LayoutCard manifest",
		Long: strings.TrimSpace(`
Create a new LayoutCard manifest as a card container.

LayoutCard is a grouping container that can have a title and contains
child components. Cards are typically used within LayoutPages to group
related content.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add layoutcard

  # Create with title
  bino add layoutcard sales_summary_card \
    --title "Sales Summary" \
    --output layouts/cards.yaml \
    --no-prompt

  # With children
  bino add layoutcard metrics_card \
    --title "Key Metrics" \
    --children total_revenue,total_orders \
    --output layouts/cards.yaml \
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

			data := LayoutCardManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Title:       flagTitle,
				Children:    flagChildren,
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
				return writeLayoutCardManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new LayoutCard manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "LayoutCard")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			} else {
				if err := ValidateName(data.Name); err != nil {
					return ConfigError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString(reader, out, "Description (optional)", "")
			}

			// Title
			if data.Title == "" {
				data.Title, _ = addPromptString(reader, out, "Card title (optional)", "")
			}

			// Children selection
			if len(data.Children) == 0 {
				addChildren, err := addPromptConfirm(reader, out, "Add child components now?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addChildren {
					data.Children, err = promptLayoutChildren(reader, out, manifests)
					if err != nil {
						return RuntimeError(err)
					}
				}
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "LayoutCard", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			doc := buildLayoutCardDocument(data)
			manifestBytes, err := renderLayoutCardManifest(doc)
			if err != nil {
				return RuntimeError(fmt.Errorf("render preview: %w", err))
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, string(manifestBytes))
			fmt.Fprintln(out, "===============")

			if len(data.Children) == 0 {
				fmt.Fprintln(out, "\nNote: The children array is empty. Add components to the card after creation.")
			}

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeLayoutCardManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
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

	cmd.Flags().StringVar(&flagTitle, "title", "", "Card title")
	cmd.Flags().StringSliceVar(&flagChildren, "children", nil, "Child component names (comma-separated)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("children", completeLayoutComponents)

	return cmd
}

// promptLayoutChildren prompts for child component selection.
func promptLayoutChildren(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo) ([]string, error) {
	// Filter to component kinds that can be children
	components := FilterByKind(manifests, "Text", "Table", "ChartStructure", "ChartTime", "LayoutCard")

	if len(components) == 0 {
		fmt.Fprintln(out, "No components found. You can add children manually later.")
		return nil, nil
	}

	items := ManifestsToFuzzyItems(components)
	selected, err := addPromptMultiFuzzySearch(reader, out, "Select child components", items)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(selected))
	for i, item := range selected {
		names[i] = item.Name
	}

	return names, nil
}

// completeLayoutComponents provides shell completion for layout child components.
func completeLayoutComponents(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	workdir, _ := pathutil.ResolveWorkdir(".")
	manifests, _ := ScanManifests(ctx, workdir)
	components := FilterByKind(manifests, "Text", "Table", "ChartStructure", "ChartTime", "LayoutCard")
	names := make([]string, len(components))
	for i, m := range components {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// Write functions

func writeLayoutPageManifest(cmd *cobra.Command, workdir string, data LayoutPageManifestData, outputPath string, appendMode bool) error {
	doc := buildLayoutPageDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

func writeLayoutCardManifest(cmd *cobra.Command, workdir string, data LayoutCardManifestData, outputPath string, appendMode bool) error {
	doc := buildLayoutCardDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

// Build and render functions

func buildLayoutPageDocument(data LayoutPageManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindLayoutPage, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = data.Constraints

	// Add $ prefix to children for reference syntax
	children := make([]string, len(data.Children))
	for i, child := range data.Children {
		children[i] = "$" + child
	}

	spec := &schema.LayoutPageSpec{
		Children: children,
	}

	doc.Spec = spec
	return doc
}

func renderLayoutPageManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

func buildLayoutCardDocument(data LayoutCardManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindLayoutCard, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = data.Constraints

	// Add $ prefix to children for reference syntax
	children := make([]string, len(data.Children))
	for i, child := range data.Children {
		children[i] = "$" + child
	}

	spec := &schema.LayoutCardSpec{
		Title:    data.Title,
		Children: children,
	}

	doc.Spec = spec
	return doc
}

func renderLayoutCardManifest(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}
