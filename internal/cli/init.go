package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
		flagSet      []string
		flagYes      bool
		flagForce    bool
		flagOffline  bool
		flagJSON     bool
		flagTrust    bool
	)

	cmd := &cobra.Command{
		Use:   "init [SOURCE]",
		Short: "Create a starter report workspace from a built-in or remote template",
		Long: strings.TrimSpace(`bino init bootstraps a report bundle so you can run bino build or
bino preview immediately.

With no SOURCE it renders the built-in 'minimal' scaffold; 'standard' renders a full
reference bundle — CSV data source, dataset, IBCS table, chart, style, translations and
assets — in the canonical folder layout; 'predef' renders a predef project, a reusable
registry package with an active [package] table and mock data to preview it against.
A SOURCE may also be a remote template: owner/repo[/subdir]#ref, a full archive URL,
or a local ./path. Remote templates are fetched from GitHub, cached by commit SHA,
and never execute code.`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawSource := ""
			if len(args) == 1 {
				rawSource = strings.TrimSpace(args[0])
			}
			src, err := tmpl.ParseSource(rawSource)
			if err != nil {
				return ConfigError(err)
			}
			sets, err := parseSetFlags(flagSet)
			if err != nil {
				return ConfigError(err)
			}
			offline := flagOffline || strings.TrimSpace(os.Getenv("BINO_OFFLINE")) != ""

			if src.Kind == tmpl.SourceBuiltin {
				return runBuiltinInit(cmd, src, builtinInitFlags{
					dir: flagDir, name: flagName, title: flagTitle, language: flagLanguage,
					yes: flagYes, force: flagForce, jsonOut: flagJSON, explicitSource: rawSource != "",
				})
			}
			return runRemoteInit(cmd, src, remoteInitFlags{
				dir: flagDir, sets: sets,
				yes: flagYes, force: flagForce, offline: offline, trust: flagTrust, jsonOut: flagJSON,
			})
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.Flags().StringVarP(&flagDir, "directory", "d", "", "Target directory for the new bundle (default ./rainbow-report)")
	cmd.Flags().StringVarP(&flagDir, "output", "o", "", "Alias for --directory")
	cmd.Flags().StringVar(&flagName, "name", "", "metadata.name to assign to the sample ReportArtefact (built-in templates)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Display title for the sample ReportArtefact (built-in templates)")
	cmd.Flags().StringVar(&flagLanguage, "language", "", "Default locale for the bundle (en or de)")
	cmd.Flags().StringArrayVar(&flagSet, "set", nil, "Set a template field value as key=value (repeatable)")
	cmd.Flags().BoolVarP(&flagYes, "yes", "y", false, "Accept defaults and skip the interactive wizard")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite files if they already exist")
	cmd.Flags().BoolVar(&flagOffline, "offline", false, "Never reach the network; require a cached template")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Emit the result as JSON")
	cmd.Flags().BoolVar(&flagTrust, "trust", false, "Skip the confirmation prompt for an uncurated remote source")

	return cmd
}

type builtinInitFlags struct {
	dir, name, title, language          string
	yes, force, jsonOut, explicitSource bool
}

func runBuiltinInit(cmd *cobra.Command, src tmpl.Source, f builtinInitFlags) error {
	answers := initAnswers{Directory: f.dir, ReportName: f.name, ReportTitle: f.title, Language: f.language}
	lockReportName := cmd.Flags().Changed("name")
	lockLanguage := cmd.Flags().Changed("language")
	applyInitDefaults(&answers)
	templateName := src.Name
	if !f.yes {
		chosen, err := runInitWizard(cmd, &answers, wizardOptions{
			lockReportName:  lockReportName,
			lockLanguage:    lockLanguage,
			offerTemplate:   !f.explicitSource,
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
	data, err := buildInitTemplateData(answers)
	if err != nil {
		return ConfigError(err)
	}
	created, absDir, err := renderBuiltinBundle(templateName, data, f.force)
	if err != nil {
		return RuntimeError(err)
	}
	return emitInitOutcome(cmd, initOutcome{
		Directory: absDir,
		Files:     created,
		Template:  "builtin:" + templateName,
		Folders:   foldersOf(created),
	}, f.jsonOut)
}

type remoteInitFlags struct {
	dir                                 string
	sets                                map[string]string
	yes, force, offline, trust, jsonOut bool
}

func runRemoteInit(cmd *cobra.Command, src tmpl.Source, f remoteInitFlags) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	absDir, err := resolveRemoteDir(cmd, f.dir, f.yes)
	if err != nil {
		return ConfigError(err)
	}

	if src.Kind == tmpl.SourceShorthand {
		if err := confirmFetch(cmd, src, f.trust, f.yes); err != nil {
			if errors.Is(err, errInitCanceled) {
				return ConfigError(err)
			}
			return RuntimeError(err)
		}
	}

	mgr, err := tmpl.NewManager()
	if err != nil {
		return RuntimeError(err)
	}
	resolved, err := mgr.Resolve(ctx, src, f.offline)
	if err != nil {
		return RuntimeError(err)
	}
	defer resolved.Close()

	if err := resolved.Manifest.Validate(version.Version); err != nil {
		return ConfigError(err)
	}
	// Engine pin: validate, never download. An out-of-range pin is stamped by the
	// template author into its bino.toml; build/lint surface it later.
	if ev := strings.TrimSpace(resolved.Manifest.Spec.EngineVersion); ev != "" {
		if cerr := engine.CheckCompatibility(ev); cerr != nil {
			fmt.Fprintf(out, "warning: template pins engine-version %s, outside this CLI's supported range; build will surface this.\n", ev)
		}
	}

	vars, err := collectFields(cmd, resolved.Manifest, f.sets, f.yes)
	if err != nil {
		return ConfigError(err)
	}

	created, err := tmpl.RenderTree(resolved.Root, resolved.Manifest, absDir, vars, f.force)
	if err != nil {
		return RuntimeError(err)
	}
	if err := pathutil.StampTemplateProvenance(absDir, resolved.Provenance); err != nil {
		return RuntimeError(err)
	}
	return emitInitOutcome(cmd, initOutcome{
		Directory:      absDir,
		Files:          created,
		Template:       resolved.Provenance,
		ResolvedSource: resolved.Provenance,
		ResolvedSHA:    resolved.SHA,
		Folders:        foldersOf(created),
	}, f.jsonOut)
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
	fmt.Fprintln(out, "  predef   - a reusable registry package with mock data to preview it")
	fmt.Fprintln(out, "  standard - a full reference bundle in the canonical folder layout")
	for {
		value, err := promptString(reader, out, "Template (minimal/predef/standard)", def)
		if err != nil {
			return "", err
		}
		if tmpl.IsBuiltin(value) {
			return value, nil
		}
		fmt.Fprintln(out, "Please choose 'minimal', 'predef' or 'standard'.")
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
	DataSetName    string
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
	dsetName := sanitizeSQLIdentifier(reportName + "_dataset")
	data := initTemplateData{
		Directory:      absDir,
		ReportName:     reportName,
		ReportTitle:    reportTitle,
		Language:       lang,
		Filename:       reportName + ".pdf",
		LayoutName:     layoutName,
		DataSourceName: dsName,
		DataSetName:    dsetName,
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

// injectedVars are the built-in template variables supplied to every template:
// a fresh report id, the latest local engine version (best-effort), the
// generation timestamp, and the running CLI version.
func injectedVars() map[string]any {
	engineVersion := ""
	if mgr, err := engine.NewManager(); err == nil {
		if info, err := mgr.LatestLocalVersion(); err == nil {
			engineVersion = info.Version
		}
	}
	return map[string]any{
		"ReportID":      pathutil.GenerateReportID(),
		"EngineVersion": engineVersion,
		"Date":          time.Now().Format(time.RFC3339),
		"BinoVersion":   version.Version,
	}
}

// renderVars assembles the substitution context for a built-in template: the
// derived report fields from buildInitTemplateData plus the injected vars.
func (d initTemplateData) renderVars() map[string]any {
	vars := injectedVars()
	vars["ReportName"] = d.ReportName
	vars["ReportTitle"] = d.ReportTitle
	vars["Language"] = d.Language
	vars["Filename"] = d.Filename
	vars["LayoutName"] = d.LayoutName
	vars["DataSourceName"] = d.DataSourceName
	vars["DataSetName"] = d.DataSetName
	return vars
}

// initOutcome is the result of a scaffold, rendered as text or JSON.
type initOutcome struct {
	Directory      string   `json:"directory"`
	Files          []string `json:"files"`
	Template       string   `json:"template"`
	ResolvedSource string   `json:"resolvedSource,omitempty"`
	ResolvedSHA    string   `json:"resolvedSHA,omitempty"`
	Folders        []string `json:"folders"`
}

func emitInitOutcome(cmd *cobra.Command, outcome initOutcome, jsonOut bool) error {
	out := cmd.OutOrStdout()
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(outcome); err != nil {
			return RuntimeError(err)
		}
		return nil
	}
	printInitSummary(out, outcome.Directory, outcome.Files)
	if outcome.ResolvedSource != "" {
		fmt.Fprintf(out, "\nTemplate: %s\n", outcome.ResolvedSource)
	}
	return nil
}

// foldersOf returns the sorted, distinct top-level folders among created files.
func foldersOf(created []string) []string {
	seen := map[string]bool{}
	dirs := []string{}
	for _, rel := range created {
		if d, _, nested := strings.Cut(rel, string(filepath.Separator)); nested && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	return dirs
}

func parseSetFlags(sets []string) (map[string]string, error) {
	out := make(map[string]string, len(sets))
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --set %q (expected key=value)", s)
		}
		out[k] = v
	}
	return out, nil
}

// resolveRemoteDir resolves the target directory for a remote template. Unlike
// built-ins there is no implicit default, so a target is required (prompted when
// interactive, an error in headless mode) — never silently scaffolding into CWD.
func resolveRemoteDir(cmd *cobra.Command, dir string, yes bool) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		if yes {
			return "", fmt.Errorf("remote templates require an explicit -o/--directory in non-interactive mode")
		}
		v, err := promptString(bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), "Target folder", "")
		if err != nil {
			return "", err
		}
		if v == "" || v == "-" {
			return "", fmt.Errorf("a target directory is required")
		}
		dir = v
	}
	return pathutil.ResolveInitDir(dir, dir)
}

// confirmFetch guards against typosquatting: an uncurated owner/repo must be
// confirmed interactively or via --trust before any network fetch.
func confirmFetch(cmd *cobra.Command, src tmpl.Source, trust, yes bool) error {
	if trust {
		return nil
	}
	target := fmt.Sprintf("github.com/%s/%s", src.Owner, src.Repo)
	if yes {
		return fmt.Errorf("refusing to fetch the uncurated template %s without --trust", target)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "About to fetch a template from an uncurated source: %s\n", target)
	ok, err := promptConfirm(bufio.NewReader(cmd.InOrStdin()), out, "Proceed?", false)
	if err != nil {
		return err
	}
	if !ok {
		return errInitCanceled
	}
	return nil
}

// collectFields gathers the template's declared field values from --set (headless)
// or interactive prompts, seeded with the injected vars so a field default may
// reference them (e.g. {{ .ReportName | title }}). Unknown --set keys and missing
// required fields are hard errors.
func collectFields(cmd *cobra.Command, manifest *tmpl.ProjectTemplate, sets map[string]string, yes bool) (map[string]any, error) {
	if yes {
		return collectFieldsHeadless(manifest, sets)
	}
	vars := injectedVars()
	if err := validateSetKeys(manifest, sets); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()
	for _, fld := range manifest.Spec.Fields {
		if v, ok := sets[fld.Name]; ok {
			vars[fld.Name] = v
			continue
		}
		def, err := renderFieldDefault(fld, vars)
		if err != nil {
			return nil, err
		}
		prompt := fld.Prompt
		if prompt == "" {
			prompt = fld.Name
		}
		for {
			v, err := promptString(reader, out, prompt, def)
			if err != nil {
				return nil, err
			}
			if v == "-" {
				v = ""
			}
			if v == "" && fld.Required {
				fmt.Fprintln(out, "This field is required.")
				continue
			}
			vars[fld.Name] = v
			break
		}
	}
	return vars, nil
}

// collectFieldsHeadless resolves field values without prompting: from --set/set,
// else the (possibly templated) default. A required field with no value errors.
func collectFieldsHeadless(manifest *tmpl.ProjectTemplate, sets map[string]string) (map[string]any, error) {
	vars := injectedVars()
	if err := validateSetKeys(manifest, sets); err != nil {
		return nil, err
	}
	for _, fld := range manifest.Spec.Fields {
		if v, ok := sets[fld.Name]; ok {
			vars[fld.Name] = v
			continue
		}
		def, err := renderFieldDefault(fld, vars)
		if err != nil {
			return nil, err
		}
		if fld.Required && strings.TrimSpace(def) == "" {
			return nil, fmt.Errorf("field %q is required; set %s=value", fld.Name, fld.Name)
		}
		vars[fld.Name] = def
	}
	return vars, nil
}

func validateSetKeys(manifest *tmpl.ProjectTemplate, sets map[string]string) error {
	declared := make(map[string]bool, len(manifest.Spec.Fields))
	for _, fld := range manifest.Spec.Fields {
		declared[fld.Name] = true
	}
	for k := range sets {
		if !declared[k] {
			return fmt.Errorf("unknown field %q", k)
		}
	}
	return nil
}

// renderFieldDefault evaluates a field default, which may itself be a template
// referencing earlier fields or injected vars.
func renderFieldDefault(fld tmpl.Field, vars map[string]any) (string, error) {
	def := fld.Default
	if strings.Contains(def, "{{") {
		rendered, err := tmpl.Render("default:"+fld.Name, []byte(def), vars)
		if err != nil {
			return "", fmt.Errorf("render default for field %q: %w", fld.Name, err)
		}
		def = string(rendered)
	}
	return def, nil
}

// renderBuiltinBundle renders a built-in template (minimal, predef or standard) into the
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
