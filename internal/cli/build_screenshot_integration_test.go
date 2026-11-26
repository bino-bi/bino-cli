//go:build integration

package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/pipeline"
)

func TestBuildScreenshots(t *testing.T) {
	engineVersion, cacheDir, chromePath := renderStack(t)
	dir := t.TempDir()
	docs := loadScreenshotDocs(t, statelessSampleYAML)
	jobs, err := resolveScreenshotJobs(docs, []string{"ReportArtefact/sample-report", "sample-page", "ChartStructure/revenue_chart"})
	if err != nil {
		t.Fatal(err)
	}
	out := NewOutput(OutputConfig{Stdout: io.Discard, Stderr: io.Discard, NoColor: true})
	entries, _, err := runScreenshotJobs(context.Background(), out, docs, jobs, 2, buildExecutionConfig{
		CaptureLayoutState: true,
		Builder:            &pipeline.Builder{Workdir: dir, EngineVersion: engineVersion, CacheDir: cacheDir},
		Logger:             logx.Nop(),
		OutputDir:          dir,
		ChromePath:         chromePath,
		Stdout:             io.Discard,
		NoColor:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ReportArtefact/sample-report": "reportartefact-sample-report-001.png",
		"LayoutPage/sample-page":       "layoutpage-sample-page.png",
		"ChartStructure/revenue_chart": "chartstructure-revenue_chart.png",
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d screenshots, want %d: %+v", len(entries), len(want), entries)
	}
	for _, e := range entries {
		if filepath.Join(dir, screenshotDirName, want[e.Ref]) != e.Path {
			t.Errorf("%s: path %s, want %s", e.Ref, e.Path, want[e.Ref])
		}
		if _, err := os.Stat(e.Path); err != nil {
			t.Errorf("%s: %v", e.Ref, err)
		}
		if e.Width == 0 || e.Height == 0 {
			t.Errorf("%s: no image size", e.Ref)
		}
	}
	// xga at scale 2.
	if e := entries[1]; e.Width != 2048 || e.Height != 1536 {
		t.Errorf("page size %dx%d, want 2048x1536", e.Width, e.Height)
	}
}
