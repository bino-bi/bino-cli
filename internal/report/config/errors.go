package config

import "fmt"

// DocumentError locates a loader failure in a manifest file. Editor
// diagnostic converters unwrap it with errors.As to position squiggles
// without parsing the error message text — rewording a loader message can
// then never silently downgrade diagnostics to unpositioned.
//
// Error() reproduces the exact strings the loader has always emitted
// ("<op> <file>: <err>" / "<op> <file> document <n>: <err>"), so callers that
// still match on text keep working.
type DocumentError struct {
	// Op names the failing loader stage: "read", "decode", "marshal", "header".
	Op string
	// File is the manifest path as the loader reports it.
	File string
	// Position is the 1-based document index within a multi-doc file;
	// 0 means the failure concerns the whole file.
	Position int
	// Err is the underlying cause.
	Err error
}

func (e *DocumentError) Error() string {
	if e.Position > 0 {
		return fmt.Sprintf("%s %s document %d: %v", e.Op, e.File, e.Position, e.Err)
	}
	return fmt.Sprintf("%s %s: %v", e.Op, e.File, e.Err)
}

func (e *DocumentError) Unwrap() error { return e.Err }
