package mcp

import (
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/report/buildlog"
)

func TestProgressMessage(t *testing.T) {
	if got := progressMessage(`{"level":"info","msg":"rendering weekly"}`); got != "rendering weekly" {
		t.Errorf("json msg: got %q", got)
	}
	if got := progressMessage("plain line"); got != "plain line" {
		t.Errorf("plain: got %q", got)
	}
}

func TestTail(t *testing.T) {
	if got := tail("short", 100); got != "short" {
		t.Errorf("no truncation: got %q", got)
	}
	got := tail("0123456789", 4)
	if got != "...[truncated]...\n6789" {
		t.Errorf("truncation: got %q", got)
	}
}

func TestNewestBuildLog(t *testing.T) {
	dir := t.TempDir()

	// No logs yet.
	if path, log := newestBuildLog(dir, nil); log != nil || path != "" {
		t.Errorf("expected no log, got %q %+v", path, log)
	}

	// A newly written log (not in the exclude set) is parsed.
	content := `{"run_id":"abc","artefacts":[{"name":"weekly","pdf":"dist/weekly.pdf"}],"warnings":["heads up"]}`
	logPath := filepath.Join(dir, "bino-build-abc.json")
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	gotPath, log := newestBuildLog(dir, nil)
	if log == nil {
		t.Fatal("expected a parsed log")
		return
	}
	if gotPath != logPath {
		t.Errorf("path: got %q want %q", gotPath, logPath)
	}
	if len(log.Artifacts) != 1 || log.Artifacts[0].Name != "weekly" || log.Artifacts[0].PDF != "dist/weekly.pdf" {
		t.Errorf("artefacts: %+v", log.Artifacts)
	}
	if len(log.Warnings) != 1 || log.Warnings[0] != "heads up" {
		t.Errorf("warnings: %+v", log.Warnings)
	}

	// A log already present before the build (in the exclude set) is ignored.
	if _, log := newestBuildLog(dir, map[string]struct{}{logPath: {}}); log != nil {
		t.Error("expected pre-existing log to be excluded")
	}
}

func TestScreenshotResult(t *testing.T) {
	if res := screenshotResult(buildOutput{}); res != nil {
		t.Errorf("no screenshots: want nil result, got %+v", res)
	}

	dir := t.TempDir()
	small := filepath.Join(dir, "small.png")
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(small, []byte("\x89PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, make([]byte, maxInlineScreenshot+1), 0o600); err != nil {
		t.Fatal(err)
	}
	res := screenshotResult(buildOutput{Screenshots: []buildlog.ScreenshotEntry{{Ref: "LayoutPage/a", Path: small}, {Ref: "LayoutPage/b", Path: big}}})
	if res == nil || len(res.Content) != 2 {
		t.Fatalf("want JSON text plus one image, got %+v", res)
	}
	if _, ok := res.Content[0].(*mcpsdk.TextContent); !ok {
		t.Errorf("first content is %T, want text", res.Content[0])
	}
	if img, ok := res.Content[1].(*mcpsdk.ImageContent); !ok || img.MIMEType != "image/png" {
		t.Errorf("second content is %T, want a PNG image", res.Content[1])
	}
}
