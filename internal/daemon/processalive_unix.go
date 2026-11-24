//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// processAlive reports whether a process with the given PID is running.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks existence without affecting the process. EPERM means the
	// PID belongs to another user, so it cannot be our daemon: a reused PID.
	return proc.Signal(syscall.Signal(0)) == nil
}
