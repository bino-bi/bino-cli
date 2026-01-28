package pathutil

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

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
func (c CommandArgs) GetBool(key string) (bool, bool) {
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
		os.Setenv(key, tomlVal)
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
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	return nil
}

// GenerateReportID creates a new unique report ID (UUID).
func GenerateReportID() string {
	return uuid.NewString()
}
