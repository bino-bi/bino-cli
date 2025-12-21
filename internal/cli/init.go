package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
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
		Use:   "init",
		Short: "Create a starter report workspace with sample manifests",
		Long: strings.TrimSpace(`bino init bootstraps a report bundle with example YAML manifests,
an inline datasource, and a .bnignore file so you can run bino preview/build immediately.`),
		RunE: func(cmd *cobra.Command, _ []string) error {
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
				if err := runInitWizard(cmd, &answers, wizardOptions{
					lockReportName: lockReportName,
					lockLanguage:   lockLanguage,
				}); err != nil {
					if errors.Is(err, errInitCanceled) {
						return ConfigError(err)
					}
					return RuntimeError(err)
				}
			}

			data, err := buildInitTemplateData(answers)
			if err != nil {
				return ConfigError(err)
			}
			created, absDir, err := writeInitBundle(data, flagForce)
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
	lockReportName bool
	lockLanguage   bool
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

func runInitWizard(cmd *cobra.Command, ans *initAnswers, opts wizardOptions) error {
	if ans == nil {
		return fmt.Errorf("init wizard: answers missing")
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	fmt.Fprintln(out, "Generates a sample report bundle and the required starter manifests for a new Bino project.")
	fmt.Fprintln(out, "Press Enter to keep the default in brackets.")
	fmt.Fprintln(out)

	dir, err := promptString(reader, out, "Target folder", ans.Directory)
	if err != nil {
		return err
	}
	ans.Directory = dir
	if !opts.lockReportName {
		base := filepath.Base(dir)
		ans.ReportName = sanitizeManifestName(base, ans.ReportName)
	}

	name, err := promptString(reader, out, "Report identifier (metadata.name)", ans.ReportName)
	if err != nil {
		return err
	}
	ans.ReportName = name

	title, err := promptString(reader, out, "Report title", ans.ReportTitle)
	if err != nil {
		return err
	}
	ans.ReportTitle = title

	langDefault := ans.Language
	if opts.lockLanguage {
		langDefault = normalizeLanguage(ans.Language)
	}
	lang, err := promptLanguage(reader, out, langDefault)
	if err != nil {
		return err
	}
	ans.Language = lang

	fmt.Fprintln(out)
	confirmed, err := promptConfirm(reader, out, fmt.Sprintf("Create sample project in %s?", ans.Directory), true)
	if err != nil {
		return err
	}
	if !confirmed {
		return errInitCanceled
	}
	return nil
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
		if errors.Is(err, io.EOF) && len(input) == 0 {
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
			if errors.Is(err, io.EOF) && len(input) == 0 {
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
	Locale         string
	Filename       string
	LayoutName     string
	DataSourceName string
	StyleName      string
	I18nName       string
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
	styleName := sanitizeManifestName(reportName+"-style", reportName+"-style")
	i18nName := sanitizeManifestName(reportName+"-copy", reportName+"-copy")
	dsName := sanitizeSQLIdentifier(reportName + "_data")
	data := initTemplateData{
		Directory:      absDir,
		ReportName:     reportName,
		ReportTitle:    reportTitle,
		Language:       lang,
		Locale:         defaultLocaleForLanguage(lang),
		Filename:       reportName + ".pdf",
		LayoutName:     layoutName,
		DataSourceName: dsName,
		StyleName:      styleName,
		I18nName:       i18nName,
	}
	return data, nil
}

func defaultLocaleForLanguage(lang string) string {
	switch lang {
	case "de":
		return "de-DE"
	default:
		return "en-US"
	}
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

type initFile struct {
	Path    string
	Content string
}

func (d initTemplateData) files() []initFile {
	timestamp := time.Now().Format(time.RFC3339)
	description := fmt.Sprintf("Starter report bundle generated by 'bino init' on %s.", timestamp)
	report := fmt.Sprintf(`apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: %s
spec:
  format: xga
  orientation: portrait
  language: %s
  filename: %s
  title: %s
  description: %s
  author: %s
  keywords:
    - sample
    - bino
    - report
`, d.ReportName, d.Language, d.Filename, quoteYAML(d.ReportTitle), quoteYAML(description), quoteYAML("Rainbow Reporting Team"))

	dataSource := fmt.Sprintf(`apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: %s
spec:
  type: inline
  content:
    - name: Ada Lovelace
      metric: 42
    - name: Niklaus Wirth
      metric: 37
`, d.DataSourceName)

	pages := fmt.Sprintf(`apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: %s
spec:
  children:
    - kind: Text
      spec:
        value: >-
          Welcome to %s!
          \${this.data['$%s'][0].name} just shipped the latest KPI export.
        dataset: $%s
---
apiVersion: bino.bi/v1alpha1
kind: ComponentStyle
metadata:
  name: %s
spec:
  content:
    tokens:
      brandColor: "#2563eb"
    selectors:
      ".hero":
        fontWeight: 600
        color: "#0f172a"
---
apiVersion: bino.bi/v1alpha1
kind: Internationalization
metadata:
  name: %s
spec:
  code: %s
  namespace: intro
  content:
    messages:
      welcome: %s
`, d.LayoutName, d.ReportTitle, d.DataSourceName, d.DataSourceName, d.StyleName, d.I18nName, d.Locale, quoteYAML(fmt.Sprintf("Welcome to %s", d.ReportTitle)))

	bnignore := "# Bino build output\ndist/\n.bncache/\n"

	gitignore := "# Bino build output\n.bncache/\ndist/\n"

	// Generate a new UUID for the report-id
	binoToml := fmt.Sprintf(`# Bino project configuration
# This file marks the root of a bino reporting project.
# Generated by 'bino init' on %s

report-id = "%s"
`, timestamp, pathutil.GenerateReportID())

	return []initFile{
		{Path: pathutil.ProjectConfigFile, Content: binoToml},
		{Path: "report.yaml", Content: report},
		{Path: "data.yaml", Content: dataSource},
		{Path: "pages.yaml", Content: pages},
		{Path: ".bnignore", Content: bnignore},
		{Path: ".gitignore", Content: gitignore},
	}
}

func quoteYAML(value string) string {
	escaped := strings.ReplaceAll(value, "\"", "\\\"")
	return fmt.Sprintf("\"%s\"", escaped)
}

func writeInitBundle(data initTemplateData, force bool) ([]string, string, error) {
	files := data.files()
	created := make([]string, 0, len(files))
	for _, file := range files {
		absPath := filepath.Join(data.Directory, file.Path)
		if err := pathutil.EnsureDir(filepath.Dir(absPath)); err != nil {
			return nil, "", fmt.Errorf("create directory %s: %w", filepath.Dir(absPath), err)
		}
		if !force {
			if _, err := os.Stat(absPath); err == nil {
				return nil, "", fmt.Errorf("%s already exists; use --force to overwrite", file.Path)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, "", fmt.Errorf("stat %s: %w", absPath, err)
			}
		}
		if err := os.WriteFile(absPath, []byte(file.Content), 0o644); err != nil {
			return nil, "", fmt.Errorf("write %s: %w", absPath, err)
		}
		created = append(created, file.Path)
	}
	sort.Strings(created)
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
