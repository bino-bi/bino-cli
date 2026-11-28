package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/numbervalues"
	"bino.bi/bino/internal/report/pipeline"
)

// numberValuesCapture writes what the tables and charts of a report show to
// a file next to the PDF and, with Verify, checks it against the DataSet
// rows. Like the layout state, nothing here can fail a build.
type numberValuesCapture struct {
	Path   string
	Verify bool
	Logger logx.Logger

	raw []byte
	// File is the written values file, nil when nothing was captured.
	File *numbervalues.File
}

// newNumberValuesCapture returns the capture for a report PDF, nil without
// --number-values. The methods below are no-ops on nil.
func newNumberValuesCapture(cfg buildArtefactConfig, pdfPath string, logger logx.Logger) *numberValuesCapture {
	if !cfg.NumberValues {
		return nil
	}
	return &numberValuesCapture{Path: numberValuesPath(pdfPath), Verify: cfg.VerifyNumbers, Logger: logger}
}

// handler is the chrome.PDFOptions.OnNumberValues callback, nil without a capture.
func (c *numberValuesCapture) handler() func([]byte) {
	if c == nil {
		return nil
	}
	return func(raw []byte) { c.raw = raw }
}

// apply writes the values file and records it in the artefact result.
func (c *numberValuesCapture) apply(result *artefactResult, datasets []pipeline.DatasetPayload) {
	if c == nil {
		return
	}
	c.Finish(datasets)
	if c.File == nil {
		return
	}
	result.ValuesPath = c.Path
	result.NumberCheck = c.File.Check
	result.NumbersWarning = c.Warning(result.Name)
}

// checkStatelessDraftFlags rejects the overlay and number flags in stateless
// mode, which has no project and writes no files.
func checkStatelessDraftFlags(overlayDir string, numberValues, verifyNumbers bool) error {
	if overlayDir != "" || numberValues || verifyNumbers {
		return ConfigErrorf("--overlay, --number-values and --verify-numbers cannot be combined with --stateless")
	}
	return nil
}

// Finish builds and writes the values file once the PDF is printed. An engine
// without layout state gives no capture, and then no file.
func (c *numberValuesCapture) Finish(datasets []pipeline.DatasetPayload) {
	if c.raw == nil {
		c.Logger.Warnf("number values unavailable: no capture (the template engine has no layout state, or the capture failed)")
		return
	}
	file, err := numbervalues.Build(c.raw)
	if err != nil {
		c.Logger.Warnf("%v", err)
		return
	}
	if c.Verify {
		rows := make(map[string]json.RawMessage, len(datasets))
		for _, ds := range datasets {
			rows[ds.Name] = ds.JSONRows
		}
		numbervalues.Verify(file, rows)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		c.Logger.Warnf("failed to encode number values: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		c.Logger.Warnf("failed to write number values: %v", err)
		return
	}
	if err := os.WriteFile(c.Path, append(data, '\n'), 0o644); err != nil { //nolint:gosec // G306: build output files need standard read perms
		c.Logger.Warnf("failed to write number values: %v", err)
		return
	}
	c.File = file
}

// Warning is the build-log line for shown numbers that differ from the data.
func (c *numberValuesCapture) Warning(artefactName string) string {
	if c.File == nil || c.File.Check == nil || c.File.Check.Differences == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %d shown number(s) differ from the DataSet rows, see %s", artefactName, c.File.Check.Differences, c.Path)
}

// numberValuesPath derives the values file from the PDF path:
// report.pdf -> report.values.json.
func numberValuesPath(pdfPath string) string {
	return strings.TrimSuffix(pdfPath, filepath.Ext(pdfPath)) + ".values.json"
}
