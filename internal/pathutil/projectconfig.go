package pathutil

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

// ProjectConfig represents the configuration stored in bino.toml.
type ProjectConfig struct {
	// ReportID is a unique identifier for this reporting project.
	// Defaults to a UUID when created by 'bino init'.
	ReportID string `toml:"report-id"`
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
func WriteProjectConfig(dir, reportID string) error {
	if reportID == "" {
		reportID = uuid.NewString()
	}

	cfg := ProjectConfig{
		ReportID: reportID,
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
