package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadPortFile(t *testing.T) {
	dir := t.TempDir()

	// Write port file
	if err := WritePortFile(dir, 12345); err != nil {
		t.Fatalf("WritePortFile: %v", err)
	}

	// Verify file exists
	path := filepath.Join(dir, portFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("port file should exist: %v", err)
	}

	// Read it back — the PID is the current process, so it should be alive
	pf, err := ReadPortFile(dir)
	if err != nil {
		t.Fatalf("ReadPortFile: %v", err)
	}
	if pf == nil {
		t.Fatal("ReadPortFile returned nil for a file written by the current process")
	}
	if pf.Port != 12345 {
		t.Errorf("port = %d, want 12345", pf.Port)
	}
	if pf.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", pf.PID, os.Getpid())
	}

	// Remove and verify
	RemovePortFile(dir)
	pf2, err := ReadPortFile(dir)
	if err != nil {
		t.Fatalf("ReadPortFile after remove: %v", err)
	}
	if pf2 != nil {
		t.Error("expected nil after removal")
	}
}

func TestReadPortFile_NonExistent(t *testing.T) {
	pf, err := ReadPortFile(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf != nil {
		t.Error("expected nil for non-existent port file")
	}
}

func TestReadPortFile_StalePID(t *testing.T) {
	dir := t.TempDir()

	// Write a port file with PID 1 (init — can't be killed) vs a dead PID
	// Use a very high PID that is almost certainly not running
	if err := os.WriteFile(
		filepath.Join(dir, portFileName),
		[]byte(`{"pid": 2000000000, "port": 9999, "startedAt": "2026-01-01T00:00:00Z"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	pf, err := ReadPortFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf != nil {
		t.Error("expected nil for stale PID")
	}

	// Verify the stale file was cleaned up
	if _, err := os.Stat(filepath.Join(dir, portFileName)); !os.IsNotExist(err) {
		t.Error("stale port file should be removed")
	}
}
