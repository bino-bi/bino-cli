package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/schema"
)

// ReportArtefactManifestData holds data for rendering a ReportArtefact manifest.
type ReportArtefactManifestData struct {
	Name           string
	Description    string
	Constraints    []string
	Filename       string
	Title          string
	Format         string // pdf, xga
	Orientation    string // portrait, landscape
	Language       string
	LayoutPages    []string            // Simple page refs (backward compat)
	LayoutPageRefs []LayoutPageRefData // Parameterized page refs
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
	Artifact    string
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

func newAddReportArtefactCommand() *cobra.Command { //nolint:gocognit,funlen // grandfathered complexity — refactor before extending
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

			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ReportArtefact manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(manifests, "ReportArtefact")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString("Description (optional)", "")
			}

			// Filename
			if data.Filename == "" {
				defaultFilename := fmt.Sprintf("%s.pdf", data.Name)
				data.Filename, _ = addPromptString("Output filename", defaultFilename)
			}

			// Title
			if data.Title == "" {
				data.Title, _ = addPromptString("Report title (optional)", "")
			}

			// Format
			if data.Format == "" {
				options := []SelectOption{
					{Label: "pdf", Description: "PDF document"},
					{Label: "xga", Description: "XGA format (screen)"},
				}
				idx, err := addPromptSelect("Output format", options)
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
				idx, err := addPromptSelect("Page orientation", options)
				if err != nil {
					return RuntimeError(err)
				}
				orientations := []string{"portrait", "landscape"}
				data.Orientation = orientations[idx]
			}

			// Language
			if data.Language == "" {
				data.Language, _ = addPromptString("Language code (optional, e.g., en, de)", "")
			}

			// LayoutPages
			if len(data.LayoutPages) == 0 && len(data.LayoutPageRefs) == 0 {
				pages := FilterByKind(manifests, "LayoutPage")
				if len(pages) > 0 {
					addPages, err := addPromptConfirm("Select LayoutPages to include?", true)
					if err != nil {
						return RuntimeError(err)
					}
					if addPages {
						// Get params info for all pages
						pageParams := getPageParamsInfo(manifests)

						items := ManifestsToFuzzyItems(pages)
						selected, err := addPromptMultiFuzzySearch("Select LayoutPages", items)
						if err != nil {
							return RuntimeError(err)
						}

						for _, item := range selected {
							params, hasParams := pageParams[item.Name]
							if hasParams && len(params) > 0 {
								// This page has params - ask if user wants to configure them
								fmt.Fprintf(out, "\n%s has %d parameter(s).\n", item.Name, len(params))
								configureParams, err := addPromptConfirm(fmt.Sprintf("Configure parameters for %s?", item.Name), true)
								if err != nil {
									return RuntimeError(err)
								}

								if configureParams {
									// Allow adding multiple instances with different params
									for {
										values, err := promptParamValues(item.Name, params)
										if err != nil {
											if errors.Is(err, errAddCanceled) {
												break
											}
											return RuntimeError(err)
										}
										data.LayoutPageRefs = append(data.LayoutPageRefs, LayoutPageRefData{
											Page:   item.Name,
											Params: values,
										})

										addAnother, err := addPromptConfirm(fmt.Sprintf("Add another instance of %s with different params?", item.Name), false)
										if err != nil || !addAnother {
											break
										}
									}
								} else {
									// Add without params (will use defaults)
									data.LayoutPages = append(data.LayoutPages, item.Name)
								}
							} else {
								// No params - just add the page name
								data.LayoutPages = append(data.LayoutPages, item.Name)
							}
						}
					}
				}
			}

			// Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm("Add constraints?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					data.Constraints, _ = addPromptConstraintBuilder()
				}
			}

			// Output
			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(workdir, manifests, "ReportArtefact", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview, confirm, write. Parameterized page refs need the
			// map-based document (raw write path); plain refs use the
			// typed one.
			var doc any
			var notes []string
			if len(data.LayoutPageRefs) > 0 {
				doc = buildReportArtefactDocumentWithParams(data)
				notes = append(notes, fmt.Sprintf("\nNote: This artefact includes %d parameterized page instance(s).", len(data.LayoutPageRefs)))
			} else {
				doc = buildReportArtefactDocument(data)
			}
			_, err = finishWizard(cmd, doc, workdir, outputPath, appendMode, flagOpenEditor, notes, nil)
			return err
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

func newAddLiveReportArtefactCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
	var (
		flagTitle       string
		flagArtefact    string
		flagLayoutPages []string
		flagConstraint  []string
		flagOutput      string
		flagAppendTo    string
		flagDesc        string
		flagNoPrompt    bool
	)

	cmd := &cobra.Command{
		Use:   "livereportartefact [name]",
		Short: "Create a LiveReportArtefact manifest",
		Long: strings.TrimSpace(`
Create a new LiveReportArtefact manifest for web-based live reports.

LiveReportArtefact defines routes for serving reports via the bino serve command.
Each route maps a URL path to either a ReportArtefact or LayoutPages.

IMPORTANT: A root route "/" is required and must reference a ReportArtefact
or at least one LayoutPage.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add livereportartefact

  # Root route serving a ReportArtefact
  bino add livereportartefact main_app \
    --title "Report Dashboard" \
    --artefact monthly_report \
    --output reports/live.yaml \
    --no-prompt

  # Root route serving LayoutPages directly
  bino add livereportartefact pages_app \
    --title "Pages App" \
    --layout-pages summary_page,detail_page \
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
				if flagTitle == "" {
					missing = append(missing, "--title")
				}
				if flagArtefact == "" && len(flagLayoutPages) == 0 {
					missing = append(missing, "--artefact or --layout-pages (root route content)")
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

			// Root route from flags (both modes) — the schema rejects a
			// route that references neither an artefact nor layout pages.
			if flagArtefact != "" {
				data.Routes["/"] = LiveRoute{Artifact: flagArtefact}
			} else if len(flagLayoutPages) > 0 {
				data.Routes["/"] = LiveRoute{LayoutPages: flagLayoutPages}
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
				return writeLiveReportArtefactManifest(cmd, workdir, data, outputPath, appendMode)
			}

			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new LiveReportArtefact manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(manifests, "LiveReportArtefact")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString("Description (optional)", "")
			}

			// Title — required by the schema.
			if data.Title == "" {
				data.Title, err = addPromptRequiredString("Application title")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Root route, unless the flags already configured it.
			if _, ok := data.Routes["/"]; !ok {
				fmt.Fprintln(out, "\nConfiguring the root route \"/\" (required):")

				rootRoute, err := promptLiveRootRoute(out, manifests)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
				data.Routes["/"] = rootRoute
			}

			// Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm("Add constraints?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					data.Constraints, _ = addPromptConstraintBuilder()
				}
			}

			// Output
			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(workdir, manifests, "LiveReportArtefact", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			_, err = finishWizard(cmd, buildLiveReportArtefactDocument(data), workdir, outputPath, appendMode, false,
				[]string{"\nNote: Add additional routes by editing the manifest file."}, nil)
			return err
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagTitle, "title", "", "Application title (required)")
	cmd.Flags().StringVar(&flagArtefact, "artefact", "", "ReportArtefact the root route serves")
	cmd.Flags().StringSliceVar(&flagLayoutPages, "layout-pages", nil, "LayoutPage names for the root route (comma-separated)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")

	cmd.MarkFlagsMutuallyExclusive("artefact", "layout-pages")

	_ = cmd.RegisterFlagCompletionFunc("artefact", completeReportArtefacts)
	_ = cmd.RegisterFlagCompletionFunc("layout-pages", completeLayoutPages)

	return cmd
}

// promptLiveRootRoute collects the mandatory "/" route content: a
// ReportArtefact reference or a list of LayoutPages. The schema rejects a
// route without either, so the prompt loops until one target is chosen.
func promptLiveRootRoute(out io.Writer, manifests []ManifestInfo) (LiveRoute, error) {
	artifacts := FilterByKind(manifests, "ReportArtefact")
	pages := FilterByKind(manifests, "LayoutPage")

	if len(artifacts) == 0 && len(pages) == 0 {
		fmt.Fprintln(out, "No ReportArtefacts or LayoutPages found. Enter the name of the ReportArtefact to serve (you can create it later).")
		name, err := addPromptRequiredString("ReportArtefact name")
		if err != nil {
			return LiveRoute{}, err
		}
		return LiveRoute{Artifact: name}, nil
	}

	for {
		useArtefact := len(artifacts) > 0
		if len(artifacts) > 0 && len(pages) > 0 {
			options := []SelectOption{
				{Label: "Use ReportArtefact", Description: "Reference an existing ReportArtefact"},
				{Label: "Use LayoutPages", Description: "Specify LayoutPages directly"},
			}
			idx, err := addPromptSelect("Root route content", options)
			if err != nil {
				return LiveRoute{}, err
			}
			useArtefact = idx == 0
		}

		if useArtefact {
			items := ManifestsToFuzzyItems(artifacts)
			item, err := addPromptFuzzySearch("Select ReportArtefact", items)
			if err != nil {
				return LiveRoute{}, err
			}
			if item != nil {
				return LiveRoute{Artifact: item.Name}, nil
			}
		} else {
			items := ManifestsToFuzzyItems(pages)
			selected, err := addPromptMultiFuzzySearch("Select LayoutPages", items)
			if err != nil {
				return LiveRoute{}, err
			}
			if len(selected) > 0 {
				route := LiveRoute{}
				for _, item := range selected {
					route.LayoutPages = append(route.LayoutPages, item.Name)
				}
				return route, nil
			}
		}

		fmt.Fprintln(out, "The root route needs a ReportArtefact or at least one LayoutPage.")
	}
}

func newAddSigningProfileCommand() *cobra.Command { //nolint:gocognit // grandfathered complexity — refactor before extending
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
				if flagCertificate == "" {
					missing = append(missing, "--certificate")
				}
				if flagPrivateKey == "" {
					missing = append(missing, "--private-key")
				}
				if flagSignerName == "" {
					missing = append(missing, "--signer-name")
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

			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new SigningProfile manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(manifests, "SigningProfile")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString("Description (optional)", "")
			}

			// Certificate, key, and signer — all required by the schema.
			if data.CertificatePath == "" {
				data.CertificatePath, err = addPromptRequiredString("Certificate file path")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.PrivateKeyPath == "" {
				data.PrivateKeyPath, err = addPromptRequiredString("Private key file path")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			if data.SignerName == "" {
				data.SignerName, err = addPromptRequiredString("Signer name")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm("Add constraints?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					data.Constraints, _ = addPromptConstraintBuilder()
				}
			}

			// Output
			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(workdir, manifests, "SigningProfile", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCanceled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			_, err = finishWizard(cmd, buildSigningProfileDocument(data), workdir, outputPath, appendMode, false, nil, nil)
			return err
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagCertificate, "certificate", "", "Certificate file path (required)")
	cmd.Flags().StringVar(&flagPrivateKey, "private-key", "", "Private key file path (required)")
	cmd.Flags().StringVar(&flagSignerName, "signer-name", "", "Signer name (required)")
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
	workdir, _ := pathutil.ResolveWorkdir(".")  //nolint:errcheck // shell completion; errors mean no suggestions
	manifests, _ := ScanManifests(ctx, workdir) //nolint:errcheck // shell completion; errors mean no suggestions
	pages := FilterByKind(manifests, "LayoutPage")
	names := make([]string, len(pages))
	for i, m := range pages {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeReportArtefacts(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	workdir, _ := pathutil.ResolveWorkdir(".")  //nolint:errcheck // shell completion; errors mean no suggestions
	manifests, _ := ScanManifests(ctx, workdir) //nolint:errcheck // shell completion; errors mean no suggestions
	artifacts := FilterByKind(manifests, "ReportArtefact")
	names := make([]string, len(artifacts))
	for i, m := range artifacts {
		names[i] = m.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// Write functions

func writeReportArtefactManifest(cmd *cobra.Command, workdir string, data ReportArtefactManifestData, outputPath string, appendMode bool) error {
	// If there are parameterized refs, use the map-based builder
	if len(data.LayoutPageRefs) > 0 {
		return writeReportArtefactManifestWithParams(cmd, workdir, data, outputPath, appendMode)
	}
	doc := buildReportArtefactDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

func writeReportArtefactManifestWithParams(cmd *cobra.Command, workdir string, data ReportArtefactManifestData, outputPath string, appendMode bool) error {
	return WriteRawDocument(buildReportArtefactDocumentWithParams(data), workdir, outputPath, appendMode, cmd.OutOrStdout())
}

func writeLiveReportArtefactManifest(cmd *cobra.Command, workdir string, data LiveReportArtefactManifestData, outputPath string, appendMode bool) error {
	doc := buildLiveReportArtefactDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

func writeSigningProfileManifest(cmd *cobra.Command, workdir string, data SigningProfileManifestData, outputPath string, appendMode bool) error {
	doc := buildSigningProfileDocument(data)
	return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
}

// Build and render functions

// buildReportArtefactDocument creates a schema.Document from ReportArtefactManifestData.
// For simple string refs only - no parameterized pages.
func buildReportArtefactDocument(data ReportArtefactManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindReportArtefact, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	layoutPages := make([]string, len(data.LayoutPages))
	copy(layoutPages, data.LayoutPages)

	spec := &schema.ReportArtefactSpec{
		Filename:    data.Filename,
		Title:       data.Title,
		Format:      data.Format,
		Orientation: data.Orientation,
		Language:    data.Language,
		LayoutPages: layoutPages,
	}

	doc.Spec = spec
	return doc
}

// buildReportArtefactDocumentWithParams creates a map-based document that supports
// both string refs and parameterized page refs in layoutPages.
func buildReportArtefactDocumentWithParams(data ReportArtefactManifestData) map[string]any {
	doc := map[string]any{
		"apiVersion": schema.APIVersion,
		"kind":       schema.KindReportArtefact,
		"metadata": map[string]any{
			"name": data.Name,
		},
		"spec": map[string]any{},
	}

	// Add description if present
	if data.Description != "" {
		if m, ok := doc["metadata"].(map[string]any); ok {
			m["description"] = data.Description
		}
	}

	// Add constraints if present
	if len(data.Constraints) > 0 {
		if m, ok := doc["metadata"].(map[string]any); ok {
			m["constraints"] = data.Constraints
		}
	}

	spec, _ := doc["spec"].(map[string]any)

	// Add spec fields
	if data.Filename != "" {
		spec["filename"] = data.Filename
	}
	if data.Title != "" {
		spec["title"] = data.Title
	}
	if data.Format != "" {
		spec["format"] = data.Format
	}
	if data.Orientation != "" {
		spec["orientation"] = data.Orientation
	}
	if data.Language != "" {
		spec["language"] = data.Language
	}

	// Build mixed layoutPages array
	layoutPages := make([]any, 0, len(data.LayoutPages)+len(data.LayoutPageRefs))

	// Add simple string refs first
	for _, page := range data.LayoutPages {
		layoutPages = append(layoutPages, page)
	}

	// Add parameterized refs
	for _, ref := range data.LayoutPageRefs {
		pageRef := map[string]any{
			"page": ref.Page,
		}
		if len(ref.Params) > 0 {
			pageRef["params"] = ref.Params
		}
		layoutPages = append(layoutPages, pageRef)
	}

	if len(layoutPages) > 0 {
		spec["layoutPages"] = layoutPages
	}

	return doc
}

// buildLiveReportArtefactDocument creates a schema.Document from LiveReportArtefactManifestData.
func buildLiveReportArtefactDocument(data LiveReportArtefactManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindLiveReportArtefact, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	routes := make(map[string]schema.LiveRouteSpec)
	for path, route := range data.Routes {
		routeSpec := schema.LiveRouteSpec{}
		if route.Artifact != "" {
			routeSpec.Artifact = "$" + route.Artifact
		}
		if len(route.LayoutPages) > 0 {
			layoutPages := make([]string, len(route.LayoutPages))
			copy(layoutPages, route.LayoutPages)
			routeSpec.LayoutPages = layoutPages
		}
		routes[path] = routeSpec
	}

	spec := &schema.LiveReportArtefactSpec{
		Title:  data.Title,
		Routes: routes,
	}

	doc.Spec = spec
	return doc
}

// buildSigningProfileDocument creates a schema.Document from SigningProfileManifestData.
func buildSigningProfileDocument(data SigningProfileManifestData) *schema.Document {
	doc := schema.NewDocument(schema.KindSigningProfile, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)

	spec := &schema.SigningProfileSpec{}

	// Certificate and key are referenced by path — key material is never
	// inlined into the manifest.
	if data.CertificatePath != "" {
		spec.Certificate = &schema.PEMSource{Path: data.CertificatePath}
	}
	if data.PrivateKeyPath != "" {
		spec.PrivateKey = &schema.PEMSource{Path: data.PrivateKeyPath}
	}
	if data.SignerName != "" {
		spec.Signer = &schema.SigningProfileSigner{Name: data.SignerName}
	}

	doc.Spec = spec
	return doc
}
