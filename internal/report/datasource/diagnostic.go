package datasource

import "fmt"

// Diagnostic captures a non-fatal issue encountered while resolving a datasource.
//
//nolint:errname // named for domain role, not error interface
type Diagnostic struct {
	Datasource string
	Stage      string
	Err        error
}

// Error implements the error interface for Diagnostic so it can be logged directly.
func (d Diagnostic) Error() string {
	switch {
	case d.Datasource != "" && d.Stage != "":
		return fmt.Sprintf("%s (%s): %v", d.Datasource, d.Stage, d.Err)
	case d.Datasource != "":
		return fmt.Sprintf("%s: %v", d.Datasource, d.Err)
	default:
		return d.Err.Error()
	}
}

// Unwrap exposes the underlying error for errors.Is/As.
func (d Diagnostic) Unwrap() error {
	return d.Err
}

func diagnostic(name, stage string, err error) Diagnostic {
	return Diagnostic{Datasource: name, Stage: stage, Err: err}
}
