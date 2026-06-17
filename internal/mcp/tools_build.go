package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/report/buildlog"
)

// buildMu serializes builds across the process so concurrent agent-triggered
// builds cannot clobber the same output directory.
var buildMu sync.Mutex

// maxBuildOutput caps the captured build output returned to the agent.
const maxBuildOutput = 16 * 1024

type buildInput struct {
	Artefacts []string `json:"artefacts,omitempty" jsonschema:"metadata.name entries to build (default: all)"`
	OutDir    string   `json:"out_dir,omitempty" jsonschema:"output directory relative to the project root (default: dist)"`
}

type buildOutput struct {
	Success   bool                     `json:"success"`
	ExitCode  int                      `json:"exitCode"`
	Output    string                   `json:"output"`
	Artefacts []buildlog.ArtefactEntry `json:"artefacts,omitempty"`
	Warnings  []string                 `json:"warnings,omitempty"`
	LogPath   string                   `json:"logPath,omitempty"`
}

func (h *handlers) registerBuildTool(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "build",
		Description: "Build report artefacts by running `bino build` (renders PDFs via headless Chrome — slow, writes files). Streams progress; returns the exit code, build output, and the produced artefacts.",
	}, h.runBuild)
}

func (h *handlers) runBuild(ctx context.Context, req *mcpsdk.CallToolRequest, in buildInput) (*mcpsdk.CallToolResult, buildOutput, error) {
	buildMu.Lock()
	defer buildMu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		return nil, buildOutput{}, fmt.Errorf("resolve executable: %w", err)
	}

	outDir := in.OutDir
	if outDir == "" {
		outDir = "dist"
	}

	args := []string{"build", "--work-dir", h.deps.State.ProjectRoot(), "--out-dir", outDir, "--log-format", "json"}
	for _, a := range in.Artefacts {
		args = append(args, "--artefact", a)
	}

	cmd := exec.CommandContext(ctx, exe, args...) //nolint:gosec // G204: exe is our own binary, args are controlled
	cmd.Env = append(os.Environ(), "BINO_DISABLE_UPDATE_CHECK=1", "NO_COLOR=1")

	// Merge stdout+stderr into one pipe so we capture the full build log and can
	// stream it as progress line by line.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, buildOutput{}, fmt.Errorf("pipe: %w", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	started := time.Now()
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return nil, buildOutput{}, fmt.Errorf("start build: %w", err)
	}
	_ = pw.Close() // parent drops its write end; the child keeps its copy

	var captured strings.Builder
	progressToken := req.Params.GetProgressToken()
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var step float64
	for scanner.Scan() {
		line := scanner.Text()
		captured.WriteString(line)
		captured.WriteByte('\n')
		if progressToken != nil && req.Session != nil {
			step++
			_ = req.Session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{
				ProgressToken: progressToken,
				Progress:      step,
				Message:       progressMessage(line),
			})
		}
	}
	_ = pr.Close()

	exitCode := 0
	if waitErr := cmd.Wait(); waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	out := buildOutput{
		Success:  exitCode == 0,
		ExitCode: exitCode,
		Output:   tail(captured.String(), maxBuildOutput),
	}

	// Parse the JSON build log written by this run (newest log modified after we
	// started) to surface the produced artefacts and warnings.
	if logPath, log := newestBuildLog(filepath.Join(h.deps.State.ProjectRoot(), outDir), started); log != nil {
		out.Artefacts = log.Artifacts
		out.Warnings = log.Warnings
		out.LogPath = logPath
	}
	return nil, out, nil
}

// progressMessage extracts a human-readable message from a build output line.
// With --log-format json each line is a JSON log event; fall back to the raw line.
func progressMessage(line string) string {
	var evt struct {
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(line), &evt) == nil {
		if evt.Msg != "" {
			return evt.Msg
		}
		if evt.Message != "" {
			return evt.Message
		}
	}
	return line
}

// newestBuildLog finds the most recent bino-build-*.json in dir written at or
// after start, and parses it. Returns ("", nil) when none is found.
func newestBuildLog(dir string, start time.Time) (string, *buildlog.JSONBuildLog) {
	matches, err := filepath.Glob(filepath.Join(dir, "bino-build-*.json"))
	if err != nil || len(matches) == 0 {
		return "", nil
	}
	sort.Slice(matches, func(i, j int) bool {
		return modTime(matches[i]).After(modTime(matches[j]))
	})
	newest := matches[0]
	if modTime(newest).Before(start) {
		return "", nil // stale log from an earlier build
	}
	data, err := os.ReadFile(newest) //nolint:gosec // G304: path derived from our own out-dir glob
	if err != nil {
		return "", nil
	}
	var log buildlog.JSONBuildLog
	if json.Unmarshal(data, &log) != nil {
		return "", nil
	}
	return newest, &log
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// tail returns at most limit bytes from the end of s, prefixed with a marker
// when truncated.
func tail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return "...[truncated]...\n" + s[len(s)-limit:]
}
