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
// inspector is clean in CI. Nothing here can fail a build - a capture is extra
// information about a PDF that has already rendered.
type layoutStateCapture struct {
	// SnapshotPath is where the raw capture is written. Empty: no file, and
	// only content that is lost is reported: what is cut off, and a component
	// that reports an error and so is not drawn. This is a build without
	// --layout-state: lost content must not be silent, the rest is detail.
	SnapshotPath string
	Logger       logx.Logger

	// Findings are the analyzed results, filled by Handle.
	Findings []layoutstate.Finding
}

// Handle is the chrome.PDFOptions.OnLayoutState callback.
func (c *layoutStateCapture) Handle(snapshot []byte) {
	quiet := c.SnapshotPath == ""
	if !quiet {
		if err := c.write(snapshot); err != nil {
			c.Logger.Warnf("failed to write layout state: %v", err)
		}
	}

	var snap layoutstate.Snapshot
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		if !quiet {
			c.Logger.Warnf("failed to read layout state: %v", err)
		}
		return
	}
	if !layoutstate.SupportedVersion(snap.State.Version) {
		if !quiet {
			c.Logger.Warnf("layout state version %d is not supported by this CLI; skipping render checks", snap.State.Version)
		}
		return
	}
	c.Findings = layoutstate.Analyze(snap)
	if quiet {
		c.Findings = overflowOnly(c.Findings)
	}
}

// overflowOnly keeps the findings about content that is lost (cut off, or not
// drawn because of an error), one per rule and component: a small multiple reports every panel, and a build log is no place
// for forty lines about one chart.
func overflowOnly(findings []layoutstate.Finding) []layoutstate.Finding {
	var out []layoutstate.Finding
	first := map[string]int{}
	more := map[string]int{}
	for _, f := range findings {
		if f.Rule != layoutstate.RuleOverflow && f.Rule != layoutstate.RuleError {
			continue
		}
		key := f.Rule + "\x00" + f.Kind + "\x00" + f.Name
		if f.Name == "" {
			key = f.Rule + "\x00" + f.ComponentID
		}
		if f.Rule == layoutstate.RuleError {
			key += "\x00" + f.Hint // every error has its own cause
		}
		if _, seen := first[key]; seen {
			more[key]++
			continue
		}
		first[key] = len(out)
		out = append(out, f)
	}
	for key, n := range more {
		f := &out[first[key]]
		f.Message = strings.TrimSuffix(f.Message, ".") + fmt.Sprintf(", and %d more of this component", n)
	}
	return out
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
