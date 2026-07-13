package registry

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// globalConfigFileName is the per-user config under ~/.bino. Unlike
// credentials.json it holds no secrets; `bino cache clean --global`
// deliberately spares it.
const globalConfigFileName = "config.toml"

// GlobalConfig is the per-user configuration stored at ~/.bino/config.toml.
type GlobalConfig struct {
	Registry GlobalRegistry `toml:"registry,omitempty"`
}

// GlobalRegistry is the [registry] table of the global config. It holds only
// the URL — tokens belong in credentials.json or BINO_REGISTRY_TOKEN.
type GlobalRegistry struct {
	URL string `toml:"url,omitempty"`
}

// GlobalConfigPath returns ~/.bino/config.toml.
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".bino", globalConfigFileName), nil
}

// LoadGlobalConfig reads the global config. A missing file yields a zero
// config; any other failure (unreadable, malformed TOML) is an error — a
// broken global config must not silently redirect to the public registry.
func LoadGlobalConfig() (GlobalConfig, error) {
	var cfg GlobalConfig
	path, err := GlobalConfigPath()
	if err != nil {
		return GlobalConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return GlobalConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
