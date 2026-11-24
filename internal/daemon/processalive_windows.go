//go:build windows

package daemon

import (
	"errors"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code GetExitCodeProcess reports for a running process.
const stillActive = 259

// processAlive reports whether a process with the given PID is running.
// os.Process.Signal(0) is not supported on Windows, so ask for the exit code.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // G115: a PID fits in uint32
	if err != nil {
		// Access denied means the process exists but we may not query it.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h) //nolint:errcheck // nothing to do if closing fails
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
