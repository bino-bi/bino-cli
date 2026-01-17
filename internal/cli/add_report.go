package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
)

// ReportArtefactManifestData holds data for rendering a ReportArtefact manifest.
type ReportArtefactManifestData struct {
	Name        string
	Description string
	Constraints []string
	Filename    string
	Title       string
	Format      string // pdf, xga
	Orientation string // portrait, landscape
	Language    string
	LayoutPages []string
}

// LiveReportArtefactManifestData holds data for rendering a LiveReportArtefact manifest.
type LiveReportArtefactManifestData struct {
	Name        string
	Description string
	Constraints []string
	Title       string
	Routes      map[string]LiveRoute
}

// LiveRoute represents a route in a LiveReportArtefact.
type LiveRoute struct {
	Artefact    string
	LayoutPages []string
}

// SigningProfileManifestData holds data for rendering a SigningProfile manifest.
type SigningProfileManifestData struct {
	Name            string
	Description     string
	Constraints     []string
	CertificatePath string
	PrivateKeyPath  string
	SignerName      string
}

func newAddReportArtefactCommand() *cobra.Command {
	var (
		flagFilename    string
		flagTitle       string
		flagFormat      string
		flagOrientation string
		flagLanguage    string
		flagLayoutPages []string
		flagConstraint  []string
		flagOutput      string
		flagAppendTo    string
		flagDesc        string
		flagNoPrompt    bool
		flagOpenEditor  bool
	)

	cmd := &cobra.Command{
		Use:   "reportartefact [name]",
		Short: "Create a ReportArtefact manifest",
		Long: strings.TrimSpace(`
Create a new ReportArtefact manifest for PDF report generation.

ReportArtefact defines the configuration for generating a PDF report,
including the filename, format, orientation, and which LayoutPages to include.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add reportartefact

  # PDF report
  bino add reportartefact monthly_report \
    --filename "report_{{date}}.pdf" \
    --title "Monthly Report" \
    --format pdf \
    --orientation portrait \
    --layout-pages summary_page,detail_page \
    --output reports/monthly.yaml \
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
				if flagFilename == "" {
					missing = append(missing, "--filename")
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

			data := ReportArtefactManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Filename:    flagFilename,
				Title:       flagTitle,
				Format:      flagFormat,
				Orientation: flagOrientation,
				Language:    flagLanguage,
				LayoutPages: flagLayoutPages,
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
				return writeReportArtefactManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ReportArtefact manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "ReportArtefact")
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

			// Filename
			if data.Filename == "" {
				defaultFilename := fmt.Sprintf("%s.pdf", data.Name)
				data.Filename, _ = addPromptString(reader, out, "Output filename", defaultFilename)
			}

			// Title
			if data.Title == "" {
				data.Title, _ = addPromptString(reader, out, "Report title (optional)", "")
			}

			// Format
			if data.Format == "" {
				options := []SelectOption{
					{Label: "pdf", Description: "PDF document"},
					{Label: "xga", Description: "XGA format (screen)"},
				}
				idx, err := addPromptSelect(reader, out, "Output format", options, 0)
				if err != nil {
					return RuntimeError(err)
				}
				formats := []string{"pdf", "xga"}
				data.Format = formats[idx]
			}

			// Orientation
			if data.Orientation == "" && data.Format == "pdf" {
				options := []SelectOption{
					{Label: "portrait", Description: "Vertical orientation"},
					{Label: "landscape", Description: "Horizontal orientation"},
				}
				idx, err := addPromptSelect(reader, out, "Page orientation", options, 0)
				if err != nil {
					return RuntimeError(err)
				}
				orientations := []string{"portrait", "landscape"}
				data.Orientation = orientations[idx]
			}

			// Language
			if data.Language == "" {
				data.Language, _ = addPromptString(reader, out, "Language code (optional, e.g., en, de)", "")
			}

			// LayoutPages
			if len(data.LayoutPages) == 0 {
				pages := FilterByKind(manifests, "LayoutPage")
				if len(pages) > 0 {
					addPages, err := addPromptConfirm(reader, out, "Select LayoutPages to include?", true)
					if err != nil {
						return RuntimeError(err)
					}
					if addPages {
						items := ManifestsToFuzzyItems(pages)
						selected, err := addPromptMultiFuzzySearch(reader, out, "Select LayoutPages", items)
						if err != nil {
							return RuntimeError(err)
						}
						for _, item := range selected {
							data.LayoutPages = append(data.LayoutPages, item.Name)
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "ReportArtefact", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			manifest := RenderReportArtefactManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeReportArtefactManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
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

	cmd.Flags().StringVar(&flagFilename, "filename", "", "Output filename (required)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Report title")
	cmd.Flags().StringVar(&flagFormat, "format", "", "Output format (pdf, xga)")
	cmd.Flags().StringVar(&flagOrientation, "orientation", "", "Page orientation (portrait, landscape)")
	cmd.Flags().StringVar(&flagLanguage, "language", "", "Language code")
	cmd.Flags().StringSliceVar(&flagLayoutPages, "layout-pages", nil, "LayoutPage names (comma-separated)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("format", completeReportFormats)
	_ = cmd.RegisterFlagCompletionFunc("orientation", completeOrientations)
	_ = cmd.RegisterFlagCompletionFunc("layout-pages", completeLayoutPages)

	return cmd
}

func newAddLiveReportArtefactCommand() *cobra.Command {
	var (
		flagTitle      string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
	)

	cmd := &cobra.Command{
		Use:   "livereportartefact [name]",
		Short: "Create a LiveReportArtefact manifest",
		Long: strings.TrimSpace(`
Create a new LiveReportArtefact manifest for web-based live reports.

LiveReportArtefact defines routes for serving reports via the bino serve command.
Each route maps a URL path to either a ReportArtefact or LayoutPages.

IMPORTANT: A root route "/" is required.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add livereportartefact

  # Basic live report
  bino add livereportartefact main_app \
    --title "Report Dashboard" \
    --output reports/live.yaml \
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

			data := LiveReportArtefactManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Title:       flagTitle,
				Routes:      make(map[string]LiveRoute),
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
				// Add default root route
				data.Routes["/"] = LiveRoute{}
				return writeLiveReportArtefactManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new LiveReportArtefact manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "LiveReportArtefact")
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

			// Title
			if data.Title == "" {
				data.Title, _ = addPromptString(reader, out, "Application title (optional)", "")
			}

			// Root route
			fmt.Fprintln(out, "\nConfiguring the root route \"/\" (required):")

			artefacts := FilterByKind(manifests, "ReportArtefact")
			pages := FilterByKind(manifests, "LayoutPage")

			rootRoute := LiveRoute{}

			if len(artefacts) > 0 {
				options := []SelectOption{
					{Label: "Use ReportArtefact", Description: "Reference an existing ReportArtefact"},
					{Label: "Use LayoutPages", Description: "Specify LayoutPages directly"},
				}
				idx, err := addPromptSelect(reader, out, "Root route content", options, 0)
				if err != nil {
					return RuntimeError(err)
				}

				if idx == 0 {
					items := ManifestsToFuzzyItems(artefacts)
					item, err := addPromptFuzzySearch(reader, out, "Select ReportArtefact", items, false)
					if err != nil {
						return RuntimeError(err)
					}
					if item != nil {
						rootRoute.Artefact = item.Name
					}
				} else if len(pages) > 0 {
					items := ManifestsToFuzzyItems(pages)
					selected, err := addPromptMultiFuzzySearch(reader, out, "Select LayoutPages", items)
					if err != nil {
						return RuntimeError(err)
					}
					for _, item := range selected {
						rootRoute.LayoutPages = append(rootRoute.LayoutPages, item.Name)
					}
				}
			} else if len(pages) > 0 {
				items := ManifestsToFuzzyItems(pages)
				selected, err := addPromptMultiFuzzySearch(reader, out, "Select LayoutPages for root route", items)
				if err != nil {
					return RuntimeError(err)
				}
				for _, item := range selected {
					rootRoute.LayoutPages = append(rootRoute.LayoutPages, item.Name)
				}
			}

			data.Routes["/"] = rootRoute

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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "LiveReportArtefact", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			manifest := RenderLiveReportArtefactManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")
			fmt.Fprintln(out, "\nNote: Add additional routes by editing the manifest file.")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			return writeLiveReportArtefactManifest(cmd, workdir, data, outputPath, appendMode)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagTitle, "title", "", "Application title")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")

	return cmd
}

func newAddSigningProfileCommand() *cobra.Command {
	var (
		flagCertificate string
		flagPrivateKey  string
		flagSignerName  string
		flagConstraint  []string
		flagOutput      string
		flagAppendTo    string
		flagDesc        string
		flagNoPrompt    bool
	)

	cmd := &cobra.Command{
		Use:   "signingprofile [name]",
		Short: "Create a SigningProfile manifest",
		Long: strings.TrimSpace(`
Create a new SigningProfile manifest for digital signatures.

SigningProfile defines the certificate and private key used to
digitally sign PDF reports.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add signingprofile

  # With certificate paths
  bino add signingprofile company_signing \
    --certificate certs/company.pem \
    --private-key certs/company-key.pem \
    --signer-name "Company Inc." \
    --output signing/profile.yaml \
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

			data := SigningProfileManifestData{
				Name:            name,
				Description:     flagDesc,
				Constraints:     flagConstraint,
				CertificatePath: flagCertificate,
				PrivateKeyPath:  flagPrivateKey,
				SignerName:      flagSignerName,
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
				return writeSigningProfileManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new SigningProfile manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "SigningProfile")
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

			// Certificate
			if data.CertificatePath == "" {
				data.CertificatePath, _ = addPromptString(reader, out, "Certificate file path", "")
			}

			// Private key
			if data.PrivateKeyPath == "" {
				data.PrivateKeyPath, _ = addPromptString(reader, out, "Private key file path", "")
			}

			// Signer name
			if data.SignerName == "" {
				data.SignerName, _ = addPromptString(reader, out, "Signer name (optional)", "")
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
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "SigningProfile", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			manifest := RenderSigningProfileManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			return writeSigningProfileManifest(cmd, workdir, data, outputPath, appendMode)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagCertificate, "certificate", "", "Certificate file path")
	cmd.Flags().StringVar(&flagPrivateKey, "private-key", "", "Private key file path")
	cmd.Flags().StringVar(&flagSignerName, "signer-name", "", "Signer name")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")

	return cmd
}

// Completion functions

func completeReportFormats(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"pdf\tPDF document",
		"xga\tXGA screen format",
	}, cobra.ShellCompDirectiveNoFileComp
}

func completeOrientations(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"portrait\tVertical orientation",
		"landscape\tHorizontal orientation",
	}, cobra.ShellCompDirectiveNoFileComp
}

func completeLayoutPages(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	workdir, _ := pathutil.ResolveWorkdir(".")
	manifests, _ := ScanManifests(ctx, workdir)
	pages := FilterByKind(manifests, "LayoutPage")
	names := make([]string, len(pages))
	for i, m := range pages {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// Write functions

func writeReportArtefactManifest(cmd *cobra.Command, workdir string, data ReportArtefactManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderReportArtefactManifest(data)

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

func writeLiveReportArtefactManifest(cmd *cobra.Command, workdir string, data LiveReportArtefactManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderLiveReportArtefactManifest(data)

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

func writeSigningProfileManifest(cmd *cobra.Command, workdir string, data SigningProfileManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderSigningProfileManifest(data)

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

// RenderReportArtefactManifest renders a ReportArtefact manifest from the given data.
func RenderReportArtefactManifest(data ReportArtefactManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: ReportArtefact\n")
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
	b.WriteString(fmt.Sprintf("  filename: %s\n", quoteYAMLIfNeeded(data.Filename)))

	if data.Title != "" {
		b.WriteString(fmt.Sprintf("  title: %s\n", quoteYAMLIfNeeded(data.Title)))
	}

	if data.Format != "" {
		b.WriteString(fmt.Sprintf("  format: %s\n", data.Format))
	}

	if data.Orientation != "" {
		b.WriteString(fmt.Sprintf("  orientation: %s\n", data.Orientation))
	}

	if data.Language != "" {
		b.WriteString(fmt.Sprintf("  language: %s\n", data.Language))
	}

	if len(data.LayoutPages) > 0 {
		b.WriteString("  layoutPages:\n")
		for _, page := range data.LayoutPages {
			b.WriteString(fmt.Sprintf("    - $%s\n", page))
		}
	}

	return b.String()
}

// RenderLiveReportArtefactManifest renders a LiveReportArtefact manifest from the given data.
func RenderLiveReportArtefactManifest(data LiveReportArtefactManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: LiveReportArtefact\n")
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

	if data.Title != "" {
		b.WriteString(fmt.Sprintf("  title: %s\n", quoteYAMLIfNeeded(data.Title)))
	}

	b.WriteString("  routes:\n")
	for path, route := range data.Routes {
		b.WriteString(fmt.Sprintf("    %s:\n", quoteYAMLIfNeeded(path)))
		if route.Artefact != "" {
			b.WriteString(fmt.Sprintf("      artefact: $%s\n", route.Artefact))
		} else if len(route.LayoutPages) > 0 {
			b.WriteString("      layoutPages:\n")
			for _, page := range route.LayoutPages {
				b.WriteString(fmt.Sprintf("        - $%s\n", page))
			}
		} else {
			b.WriteString("      # Configure artefact or layoutPages\n")
		}
	}

	return b.String()
}

// RenderSigningProfileManifest renders a SigningProfile manifest from the given data.
func RenderSigningProfileManifest(data SigningProfileManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: SigningProfile\n")
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

	if data.CertificatePath != "" {
		b.WriteString("  certificate:\n")
		b.WriteString(fmt.Sprintf("    localPath: %s\n", data.CertificatePath))
	}

	if data.PrivateKeyPath != "" {
		b.WriteString("  privateKey:\n")
		b.WriteString(fmt.Sprintf("    localPath: %s\n", data.PrivateKeyPath))
	}

	if data.SignerName != "" {
		b.WriteString(fmt.Sprintf("  signerName: %s\n", quoteYAMLIfNeeded(data.SignerName)))
	}

	return b.String()
}
