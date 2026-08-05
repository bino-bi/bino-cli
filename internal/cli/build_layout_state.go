package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/layoutstate"
)

// layoutStateCapture turns a headless-Chrome layout snapshot into a file next
// to the PDF plus build warnings.
//
// It is the build-side counterpart of the preview inspector: the same snapshot
// shape, analyzed by the same package, so a report that is clean in the
// inspector is clean in CI. Nothing here can fail a build — a capture is extra
// information about a PDF that has already rendered.
type layoutStateCapture struct {
	// SnapshotPath is where the raw capture is written.
	SnapshotPath string
	Logger       logx.Logger

	// Findings are the analyzed results, filled by Handle.
	Findings []layoutstate.Finding
}

// Handle is the chrome.PDFOptions.OnLayoutState callback.
func (c *layoutStateCapture) Handle(snapshot []byte) {
	if err := c.write(snapshot); err != nil {
		c.Logger.Warnf("failed to write layout state: %v", err)
	}

	var snap layoutstate.Snapshot
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		c.Logger.Warnf("failed to read layout state: %v", err)
		return
	}
	if !layoutstate.SupportedVersion(snap.State.Version) {
		c.Logger.Warnf("layout state version %d is not supported by this CLI; skipping render checks", snap.State.Version)
		return
	}
	c.Findings = layoutstate.Analyze(snap)
}

func (c *layoutStateCapture) write(snapshot []byte) error {
	if err := os.MkdirAll(filepath.Dir(c.SnapshotPath), 0o755); err != nil {
		return fmt.Errorf("create layout state dir: %w", err)
	}
	if err := os.WriteFile(c.SnapshotPath, snapshot, 0o644); err != nil { //nolint:gosec // G306: build output files need standard read perms
		return fmt.Errorf("write %s: %w", c.SnapshotPath, err)
	}
	return nil
}

// Warnings renders the findings as build-log lines, prefixed with the artefact
// so they read correctly in a multi-artefact build.
func (c *layoutStateCapture) Warnings(artefactName string) []string {
	out := make([]string, 0, len(c.Findings))
	for _, f := range c.Findings {
		out = append(out, artefactName+": "+f.String())
	}
	return out
}

// layoutStatePath derives the snapshot path from the artefact's PDF path, so
// the two land side by side: report.pdf -> report.layout.json.
func layoutStatePath(pdfPath string) string {
	return strings.TrimSuffix(pdfPath, filepath.Ext(pdfPath)) + ".layout.json"
}
