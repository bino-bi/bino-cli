package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/pathutil"
	tmpl "bino.bi/bino/internal/template"
	"bino.bi/bino/internal/version"
)

var errInitCanceled = errors.New("init canceled")

func newInitCommand() *cobra.Command {
	var (
		flagDir      string
		flagName     string
		flagTitle    string
		flagLanguage string
		flagYes      bool
		flagForce    bool
	)

	cmd := &cobra.Command{
		Use:   "init [template]",
		Short: "Create a starter report workspace with sample manifests",
		Long: strings.TrimSpace(`bino init bootstraps a report bundle with example YAML manifests,
an inline datasource, and a .bnignore file so you can run bino build or bino preview immediately.

The optional [template] selects a built-in scaffold: 'minimal' (a flat bundle, the
default) or 'standard' (the same report organized into the canonical folder layout).`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			templateName := "minimal"
			explicitSource := len(args) == 1 && strings.TrimSpace(args[0]) != ""
			if explicitSource {
				templateName = strings.TrimSpace(args[0])
			}

			answers := initAnswers{
				Directory:   flagDir,
				ReportName:  flagName,
				ReportTitle: flagTitle,
				Language:    flagLanguage,
			}
			lockReportName := cmd.Flags().Changed("name")
			lockLanguage := cmd.Flags().Changed("language")
			applyInitDefaults(&answers)
			if !flagYes {
				chosen, err := runInitWizard(cmd, &answers, wizardOptions{
					lockReportName:  lockReportName,
					lockLanguage:    lockLanguage,
					offerTemplate:   !explicitSource,
					defaultTemplate: templateName,
				})
				if err != nil {
					if errors.Is(err, errInitCanceled) {
						return ConfigError(err)
					}
					return RuntimeError(err)
				}
				templateName = chosen
			}

			if !tmpl.IsBuiltin(templateName) {
				return ConfigError(fmt.Errorf("unknown template %q (built-ins: %s)", templateName, strings.Join(tmpl.BuiltinNames(), ", ")))
			}

			data, err := buildInitTemplateData(answers)
			if err != nil {
				return ConfigError(err)
			}
			created, absDir, err := renderBuiltinBundle(templateName, data, flagForce)
			if err != nil {
				return RuntimeError(err)
			}
			printInitSummary(cmd.OutOrStdout(), absDir, created)
			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.Flags().StringVarP(&flagDir, "directory", "d", "", "Target directory for the new bundle (default ./rainbow-report)")
	cmd.Flags().StringVarP(&flagDir, "output", "o", "", "Alias for --directory")
	cmd.Flags().StringVar(&flagName, "name", "", "metadata.name to assign to the sample ReportArtefact")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Display title for the sample ReportArtefact")
	cmd.Flags().StringVar(&flagLanguage, "language", "", "Default locale for the bundle (en or de)")
	cmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Accept defaults and skip the interactive wizard")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite files if they already exist")

	return cmd
}

type initAnswers struct {
	Directory   string
	ReportName  string
	ReportTitle string
	Language    string
}

type wizardOptions struct {
	lockReportName  bool
	lockLanguage    bool
	offerTemplate   bool
	defaultTemplate string
}

func applyInitDefaults(ans *initAnswers) {
	if ans == nil {
		return
	}
	if strings.TrimSpace(ans.Directory) == "" {
		ans.Directory = "./rainbow-report"
	}
	if strings.TrimSpace(ans.ReportTitle) == "" {
		ans.ReportTitle = "Rainbow Sample Report"
	}
	if strings.TrimSpace(ans.ReportName) == "" {
		ans.ReportName = sanitizeManifestName(ans.ReportTitle, "rainbow-report")
	}
	ans.Language = normalizeLanguage(ans.Language)
	if ans.Language == "" {
		ans.Language = "en"
	}
}

func runInitWizard(cmd *cobra.Command, ans *initAnswers, opts wizardOptions) (string, error) {
	if ans == nil {
		return "", fmt.Errorf("init wizard: answers missing")
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	fmt.Fprintln(out, "Generates a sample report bundle and the required starter manifests for a new Bino project.")
	fmt.Fprintln(out, "Press Enter to keep the default in brackets.")
	fmt.Fprintln(out)

	templateName := opts.defaultTemplate
	if opts.offerTemplate {
		chosen, err := promptTemplateChoice(reader, out, templateName)
		if err != nil {
			return "", err
		}
		templateName = chosen
	}

	dir, err := promptString(reader, out, "Target folder", ans.Directory)
	if err != nil {
		return "", err
	}
	ans.Directory = dir
	if !opts.lockReportName {
		base := filepath.Base(dir)
		ans.ReportName = sanitizeManifestName(base, ans.ReportName)
	}

	name, err := promptString(reader, out, "Report identifier (metadata.name)", ans.ReportName)
	if err != nil {
		return "", err
	}
	ans.ReportName = name

	title, err := promptString(reader, out, "Report title", ans.ReportTitle)
	if err != nil {
		return "", err
	}
	ans.ReportTitle = title

	langDefault := ans.Language
	if opts.lockLanguage {
		langDefault = normalizeLanguage(ans.Language)
	}
	lang, err := promptLanguage(reader, out, langDefault)
	if err != nil {
		return "", err
	}
	ans.Language = lang

	fmt.Fprintln(out)
	confirmed, err := promptConfirm(reader, out, fmt.Sprintf("Create sample project in %s?", ans.Directory), true)
	if err != nil {
		return "", err
	}
	if !confirmed {
		return "", errInitCanceled
	}
	return templateName, nil
}

// promptTemplateChoice asks the user to pick a built-in template, looping until
// a valid name is entered.
func promptTemplateChoice(reader *bufio.Reader, out io.Writer, def string) (string, error) {
	fmt.Fprintln(out, "Templates:")
	fmt.Fprintln(out, "  minimal  - a flat starter bundle")
	fmt.Fprintln(out, "  standard - the same report in the canonical folder layout")
	for {
		value, err := promptString(reader, out, "Template (minimal/standard)", def)
		if err != nil {
			return "", err
		}
		if tmpl.IsBuiltin(value) {
			return value, nil
		}
		fmt.Fprintln(out, "Please choose 'minimal' or 'standard'.")
	}
}

func promptString(reader *bufio.Reader, out io.Writer, label, def string) (string, error) {
	def = strings.TrimSpace(def)
	if def == "" {
		def = "-"
	}
	if _, err := fmt.Fprintf(out, "%s [%s]: ", label, def); err != nil {
		return "", err
	}
	input, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && input == "" {
			return strings.TrimSpace(def), nil
		}
		return "", err
	}
	value := strings.TrimSpace(input)
	if value == "" || value == "-" {
		return strings.TrimSpace(def), nil
	}
	return value, nil
}

func promptLanguage(reader *bufio.Reader, out io.Writer, def string) (string, error) {
	def = normalizeLanguage(def)
	for {
		value, err := promptString(reader, out, "Language (en/de)", def)
		if err != nil {
			return "", err
		}
		lang := normalizeLanguage(value)
		if lang != "" {
			return lang, nil
		}
		fmt.Fprintln(out, "Please enter 'en' or 'de'.")
	}
}

func promptConfirm(reader *bufio.Reader, out io.Writer, question string, def bool) (bool, error) {
	var label string
	if def {
		label = "Y/n"
	} else {
		label = "y/N"
	}
	for {
		if _, err := fmt.Fprintf(out, "%s [%s]: ", question, label); err != nil {
			return false, err
		}
		input, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && input == "" {
				return def, nil
			}
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(input)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Please answer yes or no.")
		}
	}
}

type initTemplateData struct {
	Directory      string
	ReportName     string
	ReportTitle    string
	Language       string
	Filename       string
	LayoutName     string
	DataSourceName string
}

func buildInitTemplateData(ans initAnswers) (initTemplateData, error) {
	trimmedDir := strings.TrimSpace(ans.Directory)
	if trimmedDir == "" {
		return initTemplateData{}, fmt.Errorf("directory is required")
	}
	reportTitle := strings.TrimSpace(ans.ReportTitle)
	if reportTitle == "" {
		reportTitle = "Rainbow Sample Report"
	}
	reportName := sanitizeManifestName(ans.ReportName, "rainbow-report")
	lang := normalizeLanguage(ans.Language)
	if lang == "" {
		lang = "en"
	}
	absDir, err := pathutil.ResolveInitDir(trimmedDir, "./rainbow-report")
	if err != nil {
		return initTemplateData{}, err
	}
	layoutName := sanitizeManifestName(reportName+"-page", reportName+"-page")
	dsName := sanitizeSQLIdentifier(reportName + "_data")
	data := initTemplateData{
		Directory:      absDir,
		ReportName:     reportName,
		ReportTitle:    reportTitle,
		Language:       lang,
		Filename:       reportName + ".pdf",
		LayoutName:     layoutName,
		DataSourceName: dsName,
	}
	return data, nil
}

func normalizeLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "en", "en-us":
		return "en"
	case "de", "de-de":
		return "de"
	default:
		return ""
	}
}

func sanitizeManifestName(raw, fallback string) string {
	if candidate := normalizeManifestSegment(raw); candidate != "" {
		return candidate
	}
	if candidate := normalizeManifestSegment(fallback); candidate != "" {
		return candidate
	}
	return "rainbow-report"
}

func normalizeManifestSegment(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if b.Len() == 0 || lastDash {
				continue
			}
			b.WriteRune(r)
			lastDash = true
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func sanitizeSQLIdentifier(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-':
			if b.Len() == 0 || lastUnderscore {
				continue
			}
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	result := strings.Trim(b.String(), "_")
	if result == "" || result[0] < 'a' || result[0] > 'z' {
		result = "ds_" + result
	}
	return result
}

// renderVars assembles the substitution context for a built-in template. The
// derived report fields come from buildInitTemplateData; the injected vars
// (.ReportID/.EngineVersion/.Date/.BinoVersion) reproduce the historical
// best-effort behavior (fresh UUID, latest local engine version, generation
// timestamp).
func (d initTemplateData) renderVars() map[string]any {
	engineVersion := ""
	if mgr, err := engine.NewManager(); err == nil {
		if info, err := mgr.LatestLocalVersion(); err == nil {
			engineVersion = info.Version
		}
	}
	return map[string]any{
		"ReportName":     d.ReportName,
		"ReportTitle":    d.ReportTitle,
		"Language":       d.Language,
		"Filename":       d.Filename,
		"LayoutName":     d.LayoutName,
		"DataSourceName": d.DataSourceName,
		"ReportID":       pathutil.GenerateReportID(),
		"EngineVersion":  engineVersion,
		"Date":           time.Now().Format(time.RFC3339),
		"BinoVersion":    version.Version,
	}
}

// renderBuiltinBundle renders a built-in template (minimal or standard) into the
// target directory via the shared template engine.
func renderBuiltinBundle(name string, data initTemplateData, force bool) (created []string, dir string, err error) {
	root, err := tmpl.BuiltinRoot(name)
	if err != nil {
		return nil, "", err
	}
	manifest, err := tmpl.BuiltinManifest(name)
	if err != nil {
		return nil, "", err
	}
	created, err = tmpl.RenderTree(root, manifest, data.Directory, data.renderVars(), force)
	if err != nil {
		return nil, "", err
	}
	return created, data.Directory, nil
}

func printInitSummary(out io.Writer, absDir string, created []string) {
	if out == nil {
		return
	}
	rel := absDir
	if wd, err := os.Getwd(); err == nil {
		if candidate := pathutil.RelPath(wd, absDir); candidate != "" {
			rel = candidate
		}
	}
	fmt.Fprintf(out, "\nCreated sample bundle in %s:\n", rel)
	for _, name := range created {
		fmt.Fprintf(out, "  - %s\n", name)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Next steps:\n  1. cd %s\n  2. bino preview\n  3. bino build\n", rel)
}
