package cli

import (
	"errors"
	"os"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// LogErrorJSON writes err as JSON log entries for --log-format json: one per
// schema problem, else one. An error that comes from a manifest document
// carries its file and line, so an editor can show it in place.
func LogErrorJSON(logger *logx.JSONLogger, err error) {
	kind := string(errorKind(err))
	var schemaErr *spec.SchemaValidationError
	if errors.As(err, &schemaErr) && len(schemaErr.Errors) > 0 {
		for _, e := range schemaErr.Errors {
			fields := map[string]any{"kind": kind}
			msg := e.Description
			if e.Field != "" && e.Field != "(root)" {
				msg = e.Field + ": " + msg
			}
			line, column := e.Line, e.Column
			if line <= 0 {
				line, column = documentStartLine(err, schemaErr.File, schemaErr.DocPosition), 0
			}
			addLocation(err, fields, schemaErr.File, line, column)
			logger.Record("error", msg, fields)
		}
		return
	}
	fields := map[string]any{"kind": kind}
	if file, pos := errorSource(err); file != "" {
		addLocation(err, fields, file, documentStartLine(err, file, pos), 0)
	}
	if h := errorHint(err); h != "" {
		fields["hint"] = h
	}
	logger.Record("error", extractCoreMessage(err), fields)
}

// addLocation sets file, line and column. A file whose content came from
// --overlay also gets "overlay", the file that was read.
func addLocation(err error, fields map[string]any, file string, line, column int) {
	if file == "" {
		return
	}
	fields["file"] = file
	if source, ok := overlaySource(err, file); ok {
		fields["overlay"] = source
	}
	if line > 0 {
		fields["line"] = line
	}
	if column > 0 {
		fields["column"] = column
	}
}

// errorSource returns the innermost manifest document named in err's chain:
// a failing dataset is a better place to point at than its artefact.
func errorSource(err error) (file string, position int) {
	for err != nil {
		switch e := err.(type) { //nolint:errorlint // walks the chain one level at a time to find the innermost match
		case *config.SourceError:
			file, position = e.File, e.Position
		case *config.DocumentError:
			file, position = e.File, e.Position
		}
		if multi, ok := err.(interface{ Unwrap() []error }); ok {
			for _, child := range multi.Unwrap() {
				if f, p := errorSource(child); f != "" {
					return f, p
				}
			}
			return file, position
		}
		err = errors.Unwrap(err)
	}
	return file, position
}

// documentStartLine is the 1-based line of the first content line of the
// position-th YAML document in file, or 0 when it cannot be found. Documents
// are counted like the loader does, so empty ones are skipped.
func documentStartLine(cause error, file string, position int) int {
	if file == "" || position <= 0 {
		return 0
	}
	if source, ok := overlaySource(cause, file); ok {
		file = source
	}
	data, err := os.ReadFile(file) //nolint:gosec // G304: the manifest path comes from the loader
	if err != nil {
		return 0
	}
	nodes, _ := spec.ParseYAMLNodes(string(data)) //nolint:errcheck // the documents before a parse error are enough
	nodes = spec.ManifestNodes(nodes)
	if position > len(nodes) {
		return 0
	}
	return nodes[position-1].Line
}

// logGeneratedJSON names every built PDF with its path, so a caller can open it.
func logGeneratedJSON(logger *logx.JSONLogger, reports []artefactResult, documents []documentArtefactResult, screenshots []buildlog.ScreenshotEntry) {
	for _, r := range reports {
		logger.Record("info", "Generated "+r.Name, map[string]any{"artefact": r.Name, "path": r.PDFPath})
	}
	for _, d := range documents {
		logger.Record("info", "Generated "+d.Name, map[string]any{"artefact": d.Name, "path": d.PDFPath})
	}
	for _, sh := range screenshots {
		logger.Record("info", "Generated "+sh.Ref, map[string]any{"screenshot": sh.Ref, "path": sh.Path, "width": sh.Width, "height": sh.Height})
	}
}
