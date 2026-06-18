//go:build windows

package archive

// oNoFollow is unavailable on Windows; the string-prefix and resolved-parent
// guards still apply.
const oNoFollow = 0
