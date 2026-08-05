package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/layoutstate"
)

func TestLayoutStatePath(t *testing.T) {
	tests := []struct {
		name    string
		pdfPath string
		want    string
	}{
		{"beside the pdf", filepath.Join("dist", "report.pdf"), filepath.Join("dist", "report.layout.json")},
		{"dotted filename", filepath.Join("dist", "q1.2026.pdf"), filepath.Join("dist", "q1.2026.layout.json")},
		{"no extension", filepath.Join("dist", "report"), filepath.Join("dist", "report.layout.json")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := layoutStatePath(tt.pdfPath); got != tt.want {
				t.Errorf("layoutStatePath(%q) = %q, want %q", tt.pdfPath, got, tt.want)
			}
		})
	}
}

// emptyChartCapture is a snapshot of one chart that rendered no bars.
const emptyChartCapture = `{
  "state": {
    "version": 1,
    "detail": "summary",
    "context": {"viewport": {"x": 0, "y": 0, "width": 794, "height": 1123}},
    "components": [{
      "version": 1, "tag": "bn-chart-time", "id": "bn-chart-time[0]",
      "rect": {"context": {"x": 0, "y": 0, "width": 640, "height": 240},
               "component": {"x": 0, "y": 0, "width": 640, "height": 240}},
      "em": {"fontSizePx": 13.33, "appliedScaleFactor": 1},
      "metadata": {"barCount": 0, "hasNoData": true},
      "diagnostics": []
    }]
  },
  "sources": {"bn-chart-time[0]": {"kind": "ChartTime", "name": "revenueTrend"}}
}`

func TestLayoutStateCaptureHandle(t *testing.T) {
	dir := t.TempDir()
	capture := &layoutStateCapture{
		SnapshotPath: filepath.Join(dir, "nested", "report.layout.json"),
		Logger:       logx.Nop(),
	}

	capture.Handle([]byte(emptyChartCapture))

	// The raw snapshot is written verbatim, so it stays byte-stable and can be
	// committed as a golden file.
	written, err := os.ReadFile(capture.SnapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(written) != emptyChartCapture {
		t.Error("snapshot was not written verbatim")
	}

	if len(capture.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(capture.Findings), capture.Findings)
	}
	if capture.Findings[0].Rule != layoutstate.RuleEmptyComponent {
		t.Errorf("rule = %q, want %q", capture.Findings[0].Rule, layoutstate.RuleEmptyComponent)
	}

	warnings := capture.Warnings("monthly")
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warnings))
	}
	// The artefact prefix is what makes a warning readable in a multi-artefact
	// build log.
	if !strings.HasPrefix(warnings[0], "monthly: ") {
		t.Errorf("warning = %q, want it prefixed with the artefact name", warnings[0])
	}
}

func TestLayoutStateCaptureTolerance(t *testing.T) {
	tests := []struct {
		name     string
		snapshot string
	}{
		// A capture that cannot be read must never fail a build: the PDF has
		// already rendered, and the snapshot is extra information about it.
		{"malformed json", `{"state": `},
		{"future schema version", `{"state":{"version":99,"components":[]}}`},
		{"empty object", `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &layoutStateCapture{
				SnapshotPath: filepath.Join(t.TempDir(), "report.layout.json"),
				Logger:       logx.Nop(),
			}

			capture.Handle([]byte(tt.snapshot))

			if len(capture.Findings) != 0 {
				t.Errorf("findings = %+v, want none", capture.Findings)
			}
			if len(capture.Warnings("monthly")) != 0 {
				t.Error("warnings were produced from an unreadable capture")
			}
		})
	}
}

// TestLayoutStateCaptureIsValidJSON guards the fixture itself, so a broken
// fixture cannot make the tolerance test pass for the wrong reason.
func TestLayoutStateCaptureIsValidJSON(t *testing.T) {
	var snap layoutstate.Snapshot
	if err := json.Unmarshal([]byte(emptyChartCapture), &snap); err != nil {
		t.Fatalf("fixture is not valid: %v", err)
	}
	if snap.State.Version != layoutstate.Version {
		t.Errorf("fixture version = %d, want %d", snap.State.Version, layoutstate.Version)
	}
}
