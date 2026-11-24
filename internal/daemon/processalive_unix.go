//go:build !windows

package daemon

import (
	"errors"
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
	// Signal 0 checks existence without affecting the process. EPERM means it
	// exists but belongs to another user.
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
