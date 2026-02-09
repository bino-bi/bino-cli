package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State represents the persisted state of the updater.
type State struct {
	LastUpdateCheck time.Time `json:"last_update_check"`
	SetupCompleted  bool      `json:"setup_completed,omitempty"`
	ChromeVersion   string    `json:"chrome_version,omitempty"`
}

// getStatePath returns the path to the state file.
func getStatePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "bino", "state.json"), nil
}

// LoadState loads the updater state from disk.
func LoadState() (*State, error) {
	path, err := getStatePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SaveState saves the updater state to disk.
func SaveState(state *State) error {
	path, err := getStatePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
