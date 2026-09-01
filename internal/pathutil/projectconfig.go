package pathutil

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

var engineVersionLinePattern = regexp.MustCompile(`^[ \t]*engine-version[ \t]*=`)

var templateLinePattern = regexp.MustCompile(`(?m)^[ \t]*template[ \t]*=`)

// StampTemplateProvenance appends a `template = "<source>"` line to the
// project's bino.toml so a scaffold records the template it came from and is
// re-runnable. It edits textually (rather than re-marshaling) so a
// template-authored bino.toml keeps its formatting, and is a no-op when the
// file is missing or already declares a template line.
func StampTemplateProvenance(dir, source string) error {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	path := ProjectConfigPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if templateLinePattern.Match(data) {
		return nil
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += fmt.Sprintf("template = %q\n", source)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: config files need standard read perms
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// HooksConfig maps checkpoint names to ordered lists of shell commands.
type HooksConfig map[string][]string

// ProjectConfig represents the configuration stored in bino.toml.
type ProjectConfig struct {
	// ReportID is a unique identifier for this reporting project.
	// Defaults to a UUID when created by 'bino init'.
	ReportID string `toml:"report-id"`

	// EngineVersion specifies the template engine version to use (e.g., "v1.2.3").
	// If not specified, the latest locally installed version is used.
	EngineVersion string `toml:"engine-version,omitempty"`

	// Plugins declares plugin binaries and their configuration.
	Plugins map[string]PluginDeclaration `toml:"plugins,omitempty"`

	// Registry configures the package registry connection.
	Registry RegistryConfig `toml:"registry,omitempty"`

	// Dependencies maps a package coordinate "@scope/name" to an exact
	// version ("1.2.3" = pinned) or a tag name ("latest", "stable", ...).
	Dependencies map[string]string `toml:"dependencies,omitempty"`

	// Package, when non-nil, marks this project as a predef project.
	Package *PackageConfig `toml:"package,omitempty"`

	// Lint configures linting behavior.
	Lint LintConfig `toml:"lint,omitempty"`

	// Hooks contains shared lifecycle hooks that apply to all commands
	// unless overridden per-command.
	Hooks HooksConfig `toml:"hooks,omitempty"`

	// Build contains default arguments and environment variables for the 'bino build' command.
	Build CommandConfig `toml:"build,omitempty"`

	// Preview contains default arguments and environment variables for the 'bino preview' command.
	Preview CommandConfig `toml:"preview,omitempty"`

	// Serve contains default arguments and environment variables for the 'bino serve' command.
	Serve CommandConfig `toml:"serve,omitempty"`
}

// RegistryConfig is the [registry] table in bino.toml.
type RegistryConfig struct {
	// URL of the registry service. Defaults to the public bino registry.
	URL string `toml:"url,omitempty"`
	// Token authenticates access to private packages: a literal value or a
	// single "${VAR}" environment reference. Falls back to the
	// BINO_REGISTRY_TOKEN environment variable; empty means anonymous.
	Token string `toml:"token,omitempty"`
}

// PackageConfig is the [package] table in bino.toml. Its presence marks the
// project as a predef project: one that authors a reusable registry package.
// There is deliberately no type= key and no separate predef.toml — the table
// itself is the marker. A project may carry both report-id and [package].
type PackageConfig struct {
	Name         string   `toml:"name"`
	Description  string   `toml:"description,omitempty"`
	Tags         []string `toml:"tags,omitempty"`
	Category     string   `toml:"category,omitempty"`
	Visibility   string   `toml:"visibility,omitempty"`    // "public" | "private", default "private"
	CompatEngine string   `toml:"compat-engine,omitempty"` // semver range
	CompatCLI    string   `toml:"compat-cli,omitempty"`    // semver range
	Include      []string `toml:"include,omitempty"`       // project-relative dirs/files
	Preview      string   `toml:"preview,omitempty"`       // "path#definition-name"
}

// packageNameRe is the registry package name grammar. internal/registry/ref.go
// encodes the same grammar, but that package is out of scope here.
var packageNameRe = regexp.MustCompile(`^@[a-z0-9][a-z0-9_-]*/[a-z0-9][a-z0-9_-]*$`)

// Validate checks the [package] table for the constraints a registry publish
// will enforce. It is deliberately not called by LoadProjectConfig: an
// unrelated command must not fail because a package field carries a bad value.
// (A [package] key of the wrong TOML *type* still fails at unmarshal time, as
// every other table does; that is unreachable without a [package] table.)
// `bino lint` surfaces the result as a finding. It returns the first error and
// does no disk I/O.
func (p *PackageConfig) Validate() error {
	if p == nil {
		return nil
	}
	if p.Name == "" {
		return fmt.Errorf("[package] name is required")
	}
	if !packageNameRe.MatchString(p.Name) {
		return fmt.Errorf("invalid [package] name %q: expected @scope/name with lowercase a-z0-9_- segments", p.Name)
	}
	switch strings.ToLower(p.Visibility) {
	case "", "public", "private":
	default:
		return fmt.Errorf("invalid [package] visibility %q: expected \"public\" or \"private\"", p.Visibility)
	}
	if p.CompatEngine != "" {
		if _, err := semver.NewConstraint(p.CompatEngine); err != nil {
			return fmt.Errorf("parse [package] compat-engine %q: %w", p.CompatEngine, err)
		}
	}
	if p.CompatCLI != "" {
		if _, err := semver.NewConstraint(p.CompatCLI); err != nil {
			return fmt.Errorf("parse [package] compat-cli %q: %w", p.CompatCLI, err)
		}
	}
	for _, entry := range p.Include {
		if _, ok := cleanProjectRelative(entry); !ok {
			return fmt.Errorf("invalid [package] include entry %q: must be a project-relative path inside the project", entry)
		}
	}
	if p.Preview != "" {
		previewPath, def, ok := strings.Cut(p.Preview, "#")
		if !ok || previewPath == "" || def == "" || strings.Contains(def, "#") {
			return fmt.Errorf("invalid [package] preview %q: expected \"path#definition-name\"", p.Preview)
		}
		if _, ok := cleanProjectRelative(previewPath); !ok {
			return fmt.Errorf("invalid [package] preview %q: path must be project-relative", p.Preview)
		}
	}
	return nil
}

// EffectiveVisibility returns the declared visibility, defaulting to "private".
// Safe on a nil receiver.
func (p *PackageConfig) EffectiveVisibility() string {
	if p == nil || p.Visibility == "" {
		return "private"
	}
	return strings.ToLower(p.Visibility)
}

// cleanProjectRelative normalizes a project-relative entry to slash form and
// reports whether it stays inside the project.
func cleanProjectRelative(entry string) (string, bool) {
	if entry == "" || filepath.IsAbs(entry) || strings.HasPrefix(entry, "/") {
		return "", false
	}
	if len(entry) >= 2 && entry[1] == ':' {
		return "", false
	}
	cleaned := path.Clean(filepath.ToSlash(entry))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

// PluginDeclaration describes a single plugin entry in bino.toml.
type PluginDeclaration struct {
	Version     string            `toml:"version,omitempty"`      // Optional semver pin
	Path        string            `toml:"path,omitempty"`         // Optional explicit binary path
	HookTimeout string            `toml:"hook_timeout,omitempty"` // Optional, e.g., "60s"
	Config      map[string]string `toml:"config,omitempty"`       // Plugin-specific key-value config
}

// LintConfig configures linting behavior.
type LintConfig struct {
	Disable  []string          `toml:"disable,omitempty"`  // Rule IDs to disable
	Severity map[string]string `toml:"severity,omitempty"` // Rule ID -> severity override
}

// CommandConfig holds default arguments and environment variables for a CLI command.
type CommandConfig struct {
	Args  CommandArgs `toml:"args,omitempty"`
	Env   CommandEnv  `toml:"env,omitempty"`
	Hooks HooksConfig `toml:"hooks,omitempty"`
}

// CommandArgs holds default arguments for a CLI command.
// These can be overridden by explicit command-line flags.
// Users can write [serve.args] port = 8080 directly without nesting.
type CommandArgs map[string]any

// GetString returns the string value for a key, or empty string if not found or not a string.
func (c CommandArgs) GetString(key string) (string, bool) {
	if c == nil {
		return "", false
	}
	v, ok := c[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// GetInt returns the int value for a key, or 0 if not found or not an int.
func (c CommandArgs) GetInt(key string) (int, bool) {
	if c == nil {
		return 0, false
	}
	v, ok := c[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// GetBool returns the bool value for a key, or false if not found or not a bool.
func (c CommandArgs) GetBool(key string) (val bool, ok bool) {
	if c == nil {
		return false, false
	}
	v, ok := c[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// GetStringSlice returns the []string value for a key, or nil if not found.
func (c CommandArgs) GetStringSlice(key string) ([]string, bool) {
	if c == nil {
		return nil, false
	}
	v, ok := c[key]
	if !ok {
		return nil, false
	}
	switch s := v.(type) {
	case []string:
		return s, true
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result, len(result) == len(s)
	default:
		return nil, false
	}
}

// CommandEnv holds environment variables to set for a CLI command.
// These are applied before command execution but can be overridden by actual environment variables.
type CommandEnv struct {
	Values map[string]string `toml:"values,omitempty"`
}

// Apply sets environment variables from the TOML config.
// It returns a list of variables that were set and a list that were skipped
// because they already exist in the environment.
func (e CommandEnv) Apply(logOverride func(key, tomlVal, envVal string)) {
	if e.Values == nil {
		return
	}
	for key, tomlVal := range e.Values {
		if envVal, exists := os.LookupEnv(key); exists {
			if envVal != tomlVal && logOverride != nil {
				logOverride(key, tomlVal, envVal)
			}
			// Environment variable takes precedence, don't override
			continue
		}
		os.Setenv(key, tomlVal) //nolint:errcheck // best-effort env projection; Setenv fails only on malformed keys
	}
}

// LoadProjectConfig reads and parses the bino.toml file from the given project root.
func LoadProjectConfig(projectRoot string) (*ProjectConfig, error) {
	configPath := ProjectConfigPath(projectRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}

	var cfg ProjectConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}

	return &cfg, nil
}

// WriteProjectConfig writes a bino.toml file to the given directory.
// If reportID is empty, a new UUID is generated.
// If engineVersion is provided, it is included in the config.
func WriteProjectConfig(dir, reportID, engineVersion string) error {
	if reportID == "" {
		reportID = uuid.NewString()
	}

	cfg := ProjectConfig{
		ReportID:      reportID,
		EngineVersion: engineVersion,
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}

	configPath := ProjectConfigPath(dir)
	if err := os.WriteFile(configPath, data, 0o644); err != nil { //nolint:gosec // G306: config files need standard read perms
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	return nil
}

// GenerateReportID creates a new unique report ID (UUID).
func GenerateReportID() string {
	return uuid.NewString()
}

// FindEngineVersionLine scans bino.toml for the first non-commented
// `engine-version = ...` line and returns its 1-based line number and the
// 1-based column of the first non-whitespace character. Returns (1, 1) if
// the file cannot be read or the key is absent.
func FindEngineVersionLine(configPath string) (line, col int) {
	f, err := os.Open(configPath)
	if err != nil {
		return 1, 1
	}
	defer f.Close() //nolint:errcheck // read-only handle

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		trimmed := strings.TrimLeft(raw, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if engineVersionLinePattern.MatchString(raw) {
			return lineNum, len(raw) - len(trimmed) + 1
		}
	}
	return 1, 1
}
