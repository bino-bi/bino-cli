//go:build !windows

package archive

import "syscall"

// oNoFollow makes os.OpenFile fail if the final path component is a symlink,
// closing the symlink-then-write-through hole on platforms that support it.
const oNoFollow = syscall.O_NOFOLLOW
