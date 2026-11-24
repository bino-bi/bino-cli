//go:build !windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A port file whose PID now belongs to another user (reused after a crash or
// reboot) is stale and must not block a new daemon.
func TestReadPortFile_OtherUserPIDIsStale(t *testing.T) {
	if err := syscall.Kill(1, 0); !errors.Is(err, syscall.EPERM) {
		t.Skipf("pid 1 is not owned by another user here (kill: %v)", err)
	}
	dir := t.TempDir()
	path := PortFilePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"pid": 1, "port": 9999, "startedAt": "2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	pf, err := ReadPortFile(dir)
	if err != nil || pf != nil {
		t.Fatalf("ReadPortFile = %v, %v; want nil, nil", pf, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale port file should be removed")
	}
}
