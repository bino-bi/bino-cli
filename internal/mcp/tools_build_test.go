package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	start := time.Now()

	// No logs yet.
	if path, log := newestBuildLog(dir, start); log != nil || path != "" {
		t.Errorf("expected no log, got %q %+v", path, log)
	}

	// A fresh log written after start is parsed.
	content := `{"run_id":"abc","artefacts":[{"name":"weekly","pdf":"dist/weekly.pdf"}],"warnings":["heads up"]}`
	logPath := filepath.Join(dir, "bino-build-abc.json")
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	gotPath, log := newestBuildLog(dir, start)
	if log == nil {
		t.Fatal("expected a parsed log")
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

	// A stale log (older than start) is ignored.
	staleStart := time.Now().Add(time.Hour)
	if _, log := newestBuildLog(dir, staleStart); log != nil {
		t.Error("expected stale log to be ignored")
	}
}
