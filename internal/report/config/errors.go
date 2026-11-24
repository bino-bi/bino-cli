package config

import "fmt"

// DocumentError locates a loader failure in a manifest file. Editor
// diagnostic converters unwrap it with errors.As to position squiggles
// without parsing the error message text - rewording a loader message can
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

// SourceError ties a build-time failure (a query, a render) to the manifest
// document it came from, so `--log-format json` can report file and line.
// Error() is the cause's message unchanged.
type SourceError struct {
	File     string
	Position int
	Err      error
}

// WithSource wraps err with the location of doc. A nil err stays nil.
func WithSource(doc Document, err error) error {
	if err == nil {
		return nil
	}
	return &SourceError{File: doc.File, Position: doc.Position, Err: err}
}

func (e *SourceError) Error() string { return e.Err.Error() }

func (e *SourceError) Unwrap() error { return e.Err }
