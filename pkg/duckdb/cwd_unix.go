//go:build !windows

package duckdb

import "syscall"

// cwdWritable reports whether the process may create files in the working
// directory. It asks access(2) instead of writing a probe file, so a file
// watcher on the project sees no change.
func cwdWritable() bool {
	return syscall.Access(".", 0x2) == nil // 0x2 is W_OK
}
