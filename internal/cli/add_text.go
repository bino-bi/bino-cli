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

	"bino.bi/bino/internal/pathutil"
)

// TextManifestData holds data for rendering a Text manifest.
type TextManifestData struct {
	Name        string
	Description string
	Constraints []string
	Value       string
	Dataset     string // Optional dataset reference
}

// ComponentStyleManifestData holds data for rendering a ComponentStyle manifest.
type ComponentStyleManifestData struct {
	Name        string
	Description string
	Constraints []string
	Content     string // CSS content or JSON object
}

// InternationalizationManifestData holds data for rendering an Internationalization manifest.
type InternationalizationManifestData struct {
	Name        string
	Description string
	Constraints []string
	Code        string            // Locale code (e.g., "en", "de", "fr")
	Content     map[string]string // Translation key-value pairs
}

func newAddTextCommand() *cobra.Command {
	var (
		flagValue      string
		flagDataset    string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "text [name]",
		Short: "Create a Text manifest",
		Long: strings.TrimSpace(`
Create a new Text manifest for text content in reports.

Text components can display:
  - Static text content
  - Dynamic text bound to a DataSet value
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add text

  # Static text
  bino add text report_title \
    --value "Monthly Sales Report" \
    --output components/text.yaml \
    --no-prompt

  # Dynamic text from DataSet
  bino add text total_sales \
    --dataset sales_summary \
    --output components/text.yaml \
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
				if flagValue == "" && flagDataset == "" {
					missing = append(missing, "--value or --dataset")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s\n\nRun without --no-prompt for interactive mode", strings.Join(missing, "\n  ")))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := TextManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Value:       flagValue,
				Dataset:     flagDataset,
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
				appendMode = false
			}

			if nonInteractive {
				return writeTextManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new Text manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "Text")
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

			// Value or Dataset
			if data.Value == "" && data.Dataset == "" {
				options := []SelectOption{
					{Label: "Static text", Description: "Fixed text content"},
					{Label: "From DataSet", Description: "Dynamic text from a DataSet value"},
				}

				idx, err := addPromptSelect(reader, out, "Text source", options, 0)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}

				if idx == 0 {
					data.Value, err = addPromptString(reader, out, "Text value", "")
					if err != nil {
						return RuntimeError(err)
					}
				} else {
					datasets := FilterByKind(manifests, "DataSet")
					if len(datasets) == 0 {
						fmt.Fprintln(out, "No DataSets found. Enter a name manually.")
						data.Dataset, err = addPromptString(reader, out, "DataSet name", "")
					} else {
						items := ManifestsToFuzzyItems(datasets)
						item, err := addPromptFuzzySearch(reader, out, "Select DataSet", items, false)
						if err != nil {
							return RuntimeError(err)
						}
						if item != nil {
							data.Dataset = item.Name
						}
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "Text", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			manifest := RenderTextManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeTextManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
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

	cmd.Flags().StringVar(&flagValue, "value", "", "Static text value")
	cmd.Flags().StringVar(&flagDataset, "dataset", "", "DataSet name for dynamic text")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("dataset", completeDatasets)

	return cmd
}

func newAddComponentStyleCommand() *cobra.Command {
	var (
		flagContent    string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
	)

	cmd := &cobra.Command{
		Use:   "componentstyle [name]",
		Short: "Create a ComponentStyle manifest",
		Long: strings.TrimSpace(`
Create a new ComponentStyle manifest for CSS styling.

ComponentStyle defines CSS properties that can be applied to report components.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add componentstyle

  # With inline content
  bino add componentstyle header_style \
    --content '{"fontFamily": "Arial", "fontSize": "24px"}' \
    --output styles/header.yaml \
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

			data := ComponentStyleManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Content:     flagContent,
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
				return writeComponentStyleManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ComponentStyle manifest.")
			fmt.Fprintln(out)

			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "ComponentStyle")
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

			// Content
			if data.Content == "" {
				fmt.Fprintln(out, "\nEnter CSS properties as JSON or press Enter to open editor:")
				data.Content, _ = addPromptString(reader, out, "Content (JSON)", "")
				if data.Content == "" {
					template := `{
  "fontFamily": "Arial, sans-serif",
  "fontSize": "12px",
  "color": "#333333"
}`
					data.Content, err = promptWithEditor("bino-style-", ".json", template)
					if err != nil {
						data.Content = "{}"
					}
				}
			}

			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "ComponentStyle", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			manifest := RenderComponentStyleManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			return writeComponentStyleManifest(cmd, workdir, data, outputPath, appendMode)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagContent, "content", "", "CSS properties as JSON")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")

	return cmd
}

func newAddInternationalizationCommand() *cobra.Command {
	var (
		flagCode       string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
	)

	cmd := &cobra.Command{
		Use:     "internationalization [name]",
		Aliases: []string{"i18n"},
		Short:   "Create an Internationalization manifest",
		Long: strings.TrimSpace(`
Create a new Internationalization manifest for translations.

Internationalization manifests define translations for a specific locale.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add internationalization

  # German translations
  bino add i18n german \
    --code de \
    --output i18n/de.yaml \
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
				if flagCode == "" {
					missing = append(missing, "--code")
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

			data := InternationalizationManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Code:        flagCode,
				Content:     make(map[string]string),
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
				return writeInternationalizationManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new Internationalization manifest.")
			fmt.Fprintln(out)

			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "Internationalization")
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

			// Locale code
			if data.Code == "" {
				options := []SelectOption{
					{Label: "en", Description: "English"},
					{Label: "de", Description: "German"},
					{Label: "fr", Description: "French"},
					{Label: "es", Description: "Spanish"},
					{Label: "Other", Description: "Enter custom code"},
				}

				idx, err := addPromptSelect(reader, out, "Locale code", options, 0)
				if err != nil {
					return RuntimeError(err)
				}

				if idx == 4 {
					data.Code, _ = addPromptString(reader, out, "Locale code", "")
				} else {
					codes := []string{"en", "de", "fr", "es"}
					data.Code = codes[idx]
				}
			}

			// Sample translations
			fmt.Fprintln(out, "\nAdd sample translations (you can edit the file later):")
			data.Content["report.title"] = "Report Title"
			data.Content["report.date"] = "Date"

			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "Internationalization", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			manifest := RenderInternationalizationManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			return writeInternationalizationManifest(cmd, workdir, data, outputPath, appendMode)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagCode, "code", "", "Locale code (e.g., en, de, fr)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")

	_ = cmd.RegisterFlagCompletionFunc("code", completeLocaleCodes)

	return cmd
}

// Completion functions

func completeDatasets(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	workdir, _ := pathutil.ResolveWorkdir(".")
	manifests, _ := ScanManifests(ctx, workdir)
	datasets := FilterByKind(manifests, "DataSet")
	names := make([]string, len(datasets))
	for i, m := range datasets {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeLocaleCodes(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"en\tEnglish",
		"de\tGerman",
		"fr\tFrench",
		"es\tSpanish",
		"it\tItalian",
		"pt\tPortuguese",
		"nl\tDutch",
		"ja\tJapanese",
		"zh\tChinese",
	}, cobra.ShellCompDirectiveNoFileComp
}

// Helper functions

func promptGenericName(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo, kind string) (string, error) {
	return addPromptAddString(reader, out, fmt.Sprintf("Name for this %s", kind), "", func(name string) error {
		if err := ValidateName(name); err != nil {
			return err
		}
		if !IsNameUnique(manifests, kind, name) {
			existing := FindByName(manifests, kind, name)
			return fmt.Errorf("a %s named %q already exists in %s:%d", kind, name, existing.File, existing.Position)
		}
		return nil
	})
}

func writeTextManifest(cmd *cobra.Command, workdir string, data TextManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderTextManifest(data)

	absPath := outputPath
	if !filepath.IsAbs(outputPath) {
		absPath = filepath.Join(workdir, outputPath)
	}

	if appendMode {
		if err := AppendToManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Appended to %s\n", outputPath)
	} else {
		if err := WriteManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Created %s\n", outputPath)
	}

	return nil
}

func writeComponentStyleManifest(cmd *cobra.Command, workdir string, data ComponentStyleManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderComponentStyleManifest(data)

	absPath := outputPath
	if !filepath.IsAbs(outputPath) {
		absPath = filepath.Join(workdir, outputPath)
	}

	if appendMode {
		if err := AppendToManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Appended to %s\n", outputPath)
	} else {
		if err := WriteManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Created %s\n", outputPath)
	}

	return nil
}

func writeInternationalizationManifest(cmd *cobra.Command, workdir string, data InternationalizationManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderInternationalizationManifest(data)

	absPath := outputPath
	if !filepath.IsAbs(outputPath) {
		absPath = filepath.Join(workdir, outputPath)
	}

	if appendMode {
		if err := AppendToManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Appended to %s\n", outputPath)
	} else {
		if err := WriteManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Created %s\n", outputPath)
	}

	return nil
}

// Render functions

// RenderTextManifest renders a Text manifest from the given data.
func RenderTextManifest(data TextManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: Text\n")
	b.WriteString("metadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n", data.Name))

	if data.Description != "" {
		b.WriteString(fmt.Sprintf("  description: %s\n", quoteYAMLIfNeeded(data.Description)))
	}

	if len(data.Constraints) > 0 {
		b.WriteString("  constraints:\n")
		for _, c := range data.Constraints {
			b.WriteString(fmt.Sprintf("    - %s\n", quoteYAMLIfNeeded(c)))
		}
	}

	b.WriteString("spec:\n")

	if data.Dataset != "" {
		b.WriteString(fmt.Sprintf("  dataset: $%s\n", data.Dataset))
	}

	if data.Value != "" {
		if strings.Contains(data.Value, "\n") {
			b.WriteString("  value: |\n")
			for _, line := range strings.Split(data.Value, "\n") {
				b.WriteString(fmt.Sprintf("    %s\n", line))
			}
		} else {
			b.WriteString(fmt.Sprintf("  value: %s\n", quoteYAMLIfNeeded(data.Value)))
		}
	}

	return b.String()
}

// RenderComponentStyleManifest renders a ComponentStyle manifest from the given data.
func RenderComponentStyleManifest(data ComponentStyleManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: ComponentStyle\n")
	b.WriteString("metadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n", data.Name))

	if data.Description != "" {
		b.WriteString(fmt.Sprintf("  description: %s\n", quoteYAMLIfNeeded(data.Description)))
	}

	if len(data.Constraints) > 0 {
		b.WriteString("  constraints:\n")
		for _, c := range data.Constraints {
			b.WriteString(fmt.Sprintf("    - %s\n", quoteYAMLIfNeeded(c)))
		}
	}

	b.WriteString("spec:\n")

	// Parse content as JSON or use as-is
	if data.Content != "" {
		if strings.HasPrefix(strings.TrimSpace(data.Content), "{") {
			b.WriteString("  content:\n")
			// Simple JSON to YAML conversion for single-level objects
			content := strings.TrimSpace(data.Content)
			content = strings.TrimPrefix(content, "{")
			content = strings.TrimSuffix(content, "}")
			for _, line := range strings.Split(content, ",") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					key := strings.Trim(strings.TrimSpace(parts[0]), "\"")
					val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
					b.WriteString(fmt.Sprintf("    %s: %s\n", key, quoteYAMLIfNeeded(val)))
				}
			}
		} else {
			b.WriteString(fmt.Sprintf("  content: %s\n", quoteYAMLIfNeeded(data.Content)))
		}
	}

	return b.String()
}

// RenderInternationalizationManifest renders an Internationalization manifest from the given data.
func RenderInternationalizationManifest(data InternationalizationManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: Internationalization\n")
	b.WriteString("metadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n", data.Name))

	if data.Description != "" {
		b.WriteString(fmt.Sprintf("  description: %s\n", quoteYAMLIfNeeded(data.Description)))
	}

	if len(data.Constraints) > 0 {
		b.WriteString("  constraints:\n")
		for _, c := range data.Constraints {
			b.WriteString(fmt.Sprintf("    - %s\n", quoteYAMLIfNeeded(c)))
		}
	}

	b.WriteString("spec:\n")
	b.WriteString(fmt.Sprintf("  code: %s\n", data.Code))

	if len(data.Content) > 0 {
		b.WriteString("  content:\n")
		for key, val := range data.Content {
			b.WriteString(fmt.Sprintf("    %s: %s\n", quoteYAMLIfNeeded(key), quoteYAMLIfNeeded(val)))
		}
	}

	return b.String()
}
