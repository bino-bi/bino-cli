package diagnostics

import (
	"errors"
	"strconv"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// FromLoadError converts a loader/validation error into Diagnostic entries.
// Typed errors carry their own location: *spec.SchemaValidationError yields
// one schema-validation diagnostic per issue, *config.DocumentError yields a
// positioned yaml-syntax or validation-error diagnostic. String parsing of
// the error message is the fallback for anything untyped only.
func FromLoadError(err error) []Diagnostic {
	var schemaErr *spec.SchemaValidationError
	if errors.As(err, &schemaErr) {
		diagnostics := make([]Diagnostic, 0, len(schemaErr.Errors))
		for _, se := range schemaErr.Errors {
			diagnostics = append(diagnostics, Diagnostic{
				File:     schemaErr.File,
				Position: schemaErr.DocPosition,
				Line:     se.Line,
				Column:   se.Column,
				Severity: "error",
				Message:  se.Description,
				Field:    se.Field,
				Code:     "schema-validation",
				Hint:     spec.Hint(se),
			})
		}
		return diagnostics
	}

	var docErr *config.DocumentError
	if errors.As(err, &docErr) {
		if docErr.Op == "decode" {
			if tail, ok := strings.CutPrefix(docErr.Err.Error(), "yaml: "); ok {
				msg, line := yamlErrorLine(tail)
				return []Diagnostic{{
					File:     docErr.File,
					Line:     line,
					Severity: "error",
					Message:  "YAML syntax error: " + msg,
					Code:     "yaml-syntax",
				}}
			}
		}
		return []Diagnostic{{
			File:     docErr.File,
			Position: docErr.Position,
			Severity: "error",
			Message:  docErr.Err.Error(),
			Code:     "validation-error",
		}}
	}

	errStr := err.Error()
	if d, ok := parseYAMLSyntaxError(errStr); ok {
		return []Diagnostic{d}
	}
	file, position, message := parseFileError(errStr)
	if file != "" {
		return []Diagnostic{{
			File:     file,
			Position: position,
			Severity: "error",
			Message:  message,
			Code:     "validation-error",
		}}
	}
	return []Diagnostic{{
		Severity: "error",
		Message:  errStr,
		Code:     "validation-error",
	}}
}

// yamlErrorLine extracts the anchoring line number from a yaml.v3 error tail
// (the text after "yaml: "). A yaml.TypeError embeds several "line N:"
// fragments — the first one anchors the diagnostic and the full message is
// kept; a single-error message ("line N: <msg>") is stripped to <msg>.
func yamlErrorLine(tail string) (msg string, line int) {
	msg = tail
	if idx := strings.Index(tail, "line "); idx >= 0 {
		digits := tail[idx+len("line "):]
		n := 0
		for n < len(digits) && digits[n] >= '0' && digits[n] <= '9' {
			n++
		}
		if n > 0 {
			line, _ = strconv.Atoi(digits[:n]) //nolint:errcheck // digits-only slice by construction
			if idx == 0 {
				msg = strings.TrimPrefix(digits[n:], ": ")
			}
		}
	}
	return msg, line
}

// parseYAMLSyntaxError destructures a stringified decode failure
// ("decode <path>: yaml: line N: <msg>", or without a line) into a positioned
// diagnostic. Splitting on ": yaml: " keeps Windows drive colons in the path
// intact. Legacy fallback — typed *config.DocumentError decode failures are
// handled in FromLoadError without touching the message text.
func parseYAMLSyntaxError(errStr string) (Diagnostic, bool) {
	rest, ok := strings.CutPrefix(errStr, "decode ")
	if !ok {
		return Diagnostic{}, false
	}
	path, tail, found := strings.Cut(rest, ": yaml: ")
	if !found {
		return Diagnostic{}, false
	}
	msg, line := yamlErrorLine(tail)
	return Diagnostic{
		File:     path,
		Line:     line,
		Severity: "error",
		Message:  "YAML syntax error: " + msg,
		Code:     "yaml-syntax",
	}, true
}

// parseFileError attempts to extract file path and position from error
// messages. Legacy fallback for untyped errors only.
func parseFileError(errStr string) (file string, position int, message string) {
	parts := strings.SplitN(errStr, " document ", 2)
	if len(parts) == 2 {
		file = strings.TrimSpace(parts[0])
		for _, prefix := range []string{"decode ", "read ", "validate ", "marshal ", "header "} {
			file = strings.TrimPrefix(file, prefix)
		}
		rest := parts[1]
		var posStr string
		for i, c := range rest {
			if c >= '0' && c <= '9' {
				posStr += string(c)
			} else {
				message = strings.TrimPrefix(rest[i:], ": ")
				break
			}
		}
		if posStr != "" {
			position, _ = strconv.Atoi(posStr) //nolint:errcheck // zero position on parse failure is the intended fallback
		}
		return file, position, message
	}

	parts = strings.SplitN(errStr, " #", 2)
	if len(parts) == 2 {
		file = strings.TrimSpace(parts[0])
		rest := parts[1]
		var posStr string
		for i, c := range rest {
			if c >= '0' && c <= '9' {
				posStr += string(c)
			} else {
				message = strings.TrimSpace(rest[i:])
				if idx := strings.Index(message, ")"); idx > 0 {
					message = strings.TrimSpace(message[idx+1:])
				}
				break
			}
		}
		if posStr != "" {
			position, _ = strconv.Atoi(posStr) //nolint:errcheck // zero position on parse failure is the intended fallback
		}
		return file, position, message
	}

	return "", 0, errStr
}
