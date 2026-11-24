package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteAndReadPortFile(t *testing.T) {
	dir := t.TempDir()

	// Write port file
	if err := WritePortFile(dir, 12345, "tok"); err != nil {
		t.Fatalf("WritePortFile: %v", err)
	}

	// Verify file exists
	path := PortFilePath(dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("port file should exist: %v", err)
	}

	// Read it back - the PID is the current process, so it should be alive
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

	// Write a port file with PID 1 (init - can't be killed) vs a dead PID
	// Use a very high PID that is almost certainly not running
	stalePath := PortFilePath(dir)
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		stalePath,
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
	if _, err := os.Stat(PortFilePath(dir)); !os.IsNotExist(err) {
		t.Error("stale port file should be removed")
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("processAlive(own pid) = false, want true")
	}
	if processAlive(0) || processAlive(-1) {
		t.Error("processAlive must be false for pid <= 0")
	}

	// A child that has exited and been reaped is gone.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run child: %v", err)
	}
	if processAlive(cmd.Process.Pid) {
		t.Errorf("processAlive(%d) = true for an exited process", cmd.Process.Pid)
	}
}

func TestRemovePortFileKeepsAnotherDaemonsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(PortFilePath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	other := []byte(`{"pid":` + strconv.Itoa(os.Getpid()+1) + `,"port":1}`)
	if err := os.WriteFile(PortFilePath(root), other, 0o600); err != nil {
		t.Fatal(err)
	}
	RemovePortFile(root)
	if _, err := os.Stat(PortFilePath(root)); err != nil {
		t.Fatalf("file of another daemon was removed: %v", err)
	}

	own := []byte(`{"pid":` + strconv.Itoa(os.Getpid()) + `,"port":1}`)
	if err := os.WriteFile(PortFilePath(root), own, 0o600); err != nil {
		t.Fatal(err)
	}
	RemovePortFile(root)
	if _, err := os.Stat(PortFilePath(root)); !os.IsNotExist(err) {
		t.Fatalf("own file was not removed: %v", err)
	}
}
