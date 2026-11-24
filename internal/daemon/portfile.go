package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bino.bi/bino/internal/version"
)

const portFileName = "daemon.json"

// PortFile holds the daemon's connection information, written to disk for client discovery.
type PortFile struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"startedAt"`
	// Version is the bino version that started this daemon. Clients compare it
	// against their own binary to detect (and restart) a stale daemon. Empty in
	// port files written by older daemons, which clients treat as stale.
	Version string `json:"version,omitempty"`
	// Token must be sent as "Authorization: Bearer <token>" on every request.
	// Empty in port files written by older daemons, which need no token.
	Token string `json:"token,omitempty"`
}

// PortFilePath returns the path to the port file for a given project root.
func PortFilePath(projectRoot string) string {
	return filepath.Join(projectRoot, ".bino", portFileName)
}

// WritePortFile writes the daemon's connection info to the project root. The
// file holds the token, so only the user may read it.
func WritePortFile(projectRoot string, port int, token string) error {
	pf := PortFile{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().UTC(),
		Version:   version.Version,
		Token:     token,
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal port file: %w", err)
	}
	path := PortFilePath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create port file directory: %w", err)
	}
	// CreateTemp makes the file 0600; the rename also replaces a file an older
	// daemon left with wider permissions, and clients never see a partial file.
	tmp, err := os.CreateTemp(filepath.Dir(path), portFileName+".*")
	if err != nil {
		return fmt.Errorf("write port file: %w", err)
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write port file: %w", werr)
	}
	return nil
}

// ReadPortFile reads and validates the daemon port file.
// Returns nil if the file does not exist or the daemon process is no longer running.
func ReadPortFile(projectRoot string) (*PortFile, error) {
	path := PortFilePath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read port file: %w", err)
	}

	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		// Corrupt file - remove and treat as absent (intentionally discard err).
		_ = os.Remove(path)
		return nil, nil //nolint:nilerr // corrupt port file is treated the same as missing
	}

	// Check if the process is still alive
	if !processAlive(pf.PID) {
		// Stale port file - remove it
		_ = os.Remove(path)
		return nil, nil
	}

	return &pf, nil
}

// RemovePortFile removes the daemon port file.
func RemovePortFile(projectRoot string) {
	_ = os.Remove(PortFilePath(projectRoot))
}
