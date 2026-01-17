package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"bino.bi/bino/internal/pathutil"
)

// addConfigDir is the directory where bino stores user preferences.
const addConfigDir = ".bino"

// addConfigFile is the filename for add command preferences.
const addConfigFile = "config.json"

// AddConfig holds user preferences for the add commands.
type AddConfig struct {
	Dataset              *KindConfig `json:"dataset,omitempty"`
	Datasource           *KindConfig `json:"datasource,omitempty"`
	ConnectionSecret     *KindConfig `json:"connectionsecret,omitempty"`
	Asset                *KindConfig `json:"asset,omitempty"`
	LayoutPage           *KindConfig `json:"layoutpage,omitempty"`
	LayoutCard           *KindConfig `json:"layoutcard,omitempty"`
	Text                 *KindConfig `json:"text,omitempty"`
	Table                *KindConfig `json:"table,omitempty"`
	ChartStructure       *KindConfig `json:"chartstructure,omitempty"`
	ChartTime            *KindConfig `json:"charttime,omitempty"`
	ComponentStyle       *KindConfig `json:"componentstyle,omitempty"`
	Internationalization *KindConfig `json:"internationalization,omitempty"`
	ReportArtefact       *KindConfig `json:"reportartefact,omitempty"`
	LiveReportArtefact   *KindConfig `json:"livereportartefact,omitempty"`
	SigningProfile       *KindConfig `json:"signingprofile,omitempty"`
}

// KindConfig holds preferences for a specific manifest kind.
type KindConfig struct {
	Mode      string `json:"mode,omitempty"`      // "separate-files" or "multi-document"
	Directory string `json:"directory,omitempty"` // For separate-files mode
	File      string `json:"file,omitempty"`      // For multi-document mode
}

// LoadAddConfig loads the add command configuration from the project directory.
// Returns an empty config if the file doesn't exist.
func LoadAddConfig(dir string) (*AddConfig, error) {
	path := filepath.Join(dir, addConfigDir, addConfigFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AddConfig{}, nil
		}
		return nil, err
	}

	var cfg AddConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Return empty config if file is malformed
		return &AddConfig{}, nil
	}

	return &cfg, nil
}

// SaveAddConfig saves the add command configuration to the project directory.
func SaveAddConfig(dir string, cfg *AddConfig) error {
	configDir := filepath.Join(dir, addConfigDir)
	if err := pathutil.EnsureDir(configDir); err != nil {
		return err
	}

	path := filepath.Join(configDir, addConfigFile)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

// GetKindConfig returns the config for a specific kind, or nil if not set.
func (c *AddConfig) GetKindConfig(kind string) *KindConfig {
	if c == nil {
		return nil
	}
	switch kind {
	case "DataSet":
		return c.Dataset
	case "DataSource":
		return c.Datasource
	case "ConnectionSecret":
		return c.ConnectionSecret
	case "Asset":
		return c.Asset
	case "LayoutPage":
		return c.LayoutPage
	case "LayoutCard":
		return c.LayoutCard
	case "Text":
		return c.Text
	case "Table":
		return c.Table
	case "ChartStructure":
		return c.ChartStructure
	case "ChartTime":
		return c.ChartTime
	case "ComponentStyle":
		return c.ComponentStyle
	case "Internationalization":
		return c.Internationalization
	case "ReportArtefact":
		return c.ReportArtefact
	case "LiveReportArtefact":
		return c.LiveReportArtefact
	case "SigningProfile":
		return c.SigningProfile
	default:
		return nil
	}
}

// SetKindConfig sets the config for a specific kind.
func (c *AddConfig) SetKindConfig(kind string, kc *KindConfig) {
	if c == nil {
		return
	}
	switch kind {
	case "DataSet":
		c.Dataset = kc
	case "DataSource":
		c.Datasource = kc
	case "ConnectionSecret":
		c.ConnectionSecret = kc
	case "Asset":
		c.Asset = kc
	case "LayoutPage":
		c.LayoutPage = kc
	case "LayoutCard":
		c.LayoutCard = kc
	case "Text":
		c.Text = kc
	case "Table":
		c.Table = kc
	case "ChartStructure":
		c.ChartStructure = kc
	case "ChartTime":
		c.ChartTime = kc
	case "ComponentStyle":
		c.ComponentStyle = kc
	case "Internationalization":
		c.Internationalization = kc
	case "ReportArtefact":
		c.ReportArtefact = kc
	case "LiveReportArtefact":
		c.LiveReportArtefact = kc
	case "SigningProfile":
		c.SigningProfile = kc
	}
}

// FilePatternToKindConfig converts a FilePattern to a KindConfig.
func FilePatternToKindConfig(pattern FilePattern) *KindConfig {
	return &KindConfig{
		Mode:      pattern.Mode,
		Directory: pattern.Directory,
		File:      pattern.File,
	}
}

// KindConfigToFilePattern converts a KindConfig to a FilePattern.
func KindConfigToFilePattern(kc *KindConfig) FilePattern {
	if kc == nil {
		return FilePattern{}
	}
	return FilePattern{
		Mode:      kc.Mode,
		Directory: kc.Directory,
		File:      kc.File,
	}
}
