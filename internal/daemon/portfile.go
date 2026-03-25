package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const portFileName = "daemon.json"

// PortFile holds the daemon's connection information, written to disk for client discovery.
type PortFile struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"startedAt"`
}

// PortFilePath returns the path to the port file for a given project root.
func PortFilePath(projectRoot string) string {
	return filepath.Join(projectRoot, ".bino", portFileName)
}

// WritePortFile writes the daemon's connection info to the project root.
func WritePortFile(projectRoot string, port int) error {
	pf := PortFile{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal port file: %w", err)
	}
	path := PortFilePath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create port file directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // G306: port file needs standard read perms
		return fmt.Errorf("write port file: %w", err)
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
		// Corrupt file — remove and treat as absent (intentionally discard err).
		_ = os.Remove(path)
		return nil, nil //nolint:nilerr // corrupt port file is treated the same as missing
	}

	// Check if the process is still alive
	if !processAlive(pf.PID) {
		// Stale port file — remove it
		_ = os.Remove(path)
		return nil, nil
	}

	return &pf, nil
}

// RemovePortFile removes the daemon port file.
func RemovePortFile(projectRoot string) {
	_ = os.Remove(PortFilePath(projectRoot))
}

// processAlive checks if a process with the given PID is still running.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks process existence without affecting it
	return proc.Signal(syscall.Signal(0)) == nil
}
