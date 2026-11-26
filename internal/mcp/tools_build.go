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
// builds cannot clobber the same output directory, and so registry writes
// (tools_packages.go), which rewrite the files a build reads, cannot
// interleave with a build.
var buildMu sync.Mutex

// maxBuildOutput caps the captured build output returned to the agent.
const maxBuildOutput = 16 * 1024

// maxInlineScreenshot caps the size of one PNG returned as image content; a
// larger one is only listed by path.
const maxInlineScreenshot = 1 << 20

// maxInlineTotal and maxInlineImages cap all inline PNGs of one result, so a
// report with many pages does not make a result of many megabytes.
const (
	maxInlineTotal  = 4 << 20
	maxInlineImages = 10
)

type buildInput struct {
	Artefacts   []string `json:"artefacts,omitempty" jsonschema:"metadata.name entries to build (default: all)"`
	OutDir      string   `json:"out_dir,omitempty" jsonschema:"output directory relative to the project root (default: dist)"`
	Screenshots []string `json:"screenshots,omitempty" jsonschema:"PNG screenshots to render: ReportArtefact/<name> (every page), LayoutPage/<name>, <ComponentKind>/<name>, or a page or report name. Without artefacts only the screenshots are built. Images up to 1 MB are returned inline, at most 10 and 4 MB in total; the rest by path."`
}

type buildOutput struct {
	Success     bool                       `json:"success"`
	ExitCode    int                        `json:"exitCode"`
	Output      string                     `json:"output"`
	Artefacts   []buildlog.ArtefactEntry   `json:"artefacts,omitempty"`
	Screenshots []buildlog.ScreenshotEntry `json:"screenshots,omitempty"`
	Warnings    []string                   `json:"warnings,omitempty"`
	LogPath     string                     `json:"logPath,omitempty"`
	// ScreenshotNote says how many screenshots are not inline and why.
	ScreenshotNote string `json:"screenshotNote,omitempty"`
}

func (h *handlers) registerBuildTool(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "build",
		Title:       "Build report artefacts",
		Annotations: writeTool("Build report artefacts"),
		Description: "Build report artefacts by running `bino build` (renders PDFs via headless Chrome - slow, writes files). Streams progress; returns the exit code, build output, and the produced artefacts. With screenshots, renders PNGs of pages or components instead and returns them as images.",
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

	// --log-format json makes the logger emit JSON lines (parsed for progress);
	// --build-log-format json makes build write the bino-build-*.json file
	// parsed below for artefacts and warnings.
	args := []string{"build", "--work-dir", h.root(), "--out-dir", outDir, "--log-format", "json", "--build-log-format", "json"}
	for _, a := range in.Artefacts {
		args = append(args, "--artefact", a)
	}
	for _, s := range in.Screenshots {
		args = append(args, "--screenshot", s)
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

	// Snapshot existing build logs so we can identify the one this run writes
	// without relying on wall-clock vs filesystem mtime resolution (which varies
	// by platform).
	logDir := filepath.Join(h.root(), outDir)
	preexisting := buildLogSet(logDir)

	if err := cmd.Start(); err != nil {
		_ = pw.Close() //nolint:errcheck // best-effort pipe cleanup; the start error is returned
		_ = pr.Close() //nolint:errcheck // best-effort pipe cleanup; the start error is returned
		return nil, buildOutput{}, fmt.Errorf("start build: %w", err)
	}
	_ = pw.Close() //nolint:errcheck // parent drops its write end; the child keeps its copy

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
			_ = req.Session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{ //nolint:errcheck // progress relay; a gone client must not fail the build
				ProgressToken: progressToken,
				Progress:      step,
				Message:       progressMessage(line),
			})
		}
	}
	_ = scanner.Err() //nolint:errcheck // best-effort log capture; the exit code decides the outcome
	_ = pr.Close()    //nolint:errcheck // read end drained; the wait below reaps the child

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

	// Parse the JSON build log written by this run (a bino-build-*.json that
	// wasn't present before) to surface the produced artefacts and warnings.
	if logPath, log := newestBuildLog(logDir, preexisting); log != nil {
		out.Artefacts = log.Artifacts
		out.Warnings = log.Warnings
		out.LogPath = logPath
		if len(in.Screenshots) > 0 {
			out.Screenshots = log.Screenshots
		}
	}
	res := screenshotResult(&out)
	return res, out, nil
}

// screenshotResult returns the screenshots as image content after the JSON
// output, or nil (the SDK then sends the JSON alone) when there are none.
// Images are inlined in order until the total budget is used; it sets
// out.ScreenshotNote when some are only listed by path.
func screenshotResult(out *buildOutput) *mcpsdk.CallToolResult {
	var images []mcpsdk.Content
	total, tooBig, overBudget := 0, 0, 0
	for _, s := range out.Screenshots {
		if overBudget > 0 {
			overBudget++
			continue
		}
		data, err := os.ReadFile(s.Path)
		if err != nil {
			continue
		}
		if len(data) > maxInlineScreenshot {
			tooBig++
			continue
		}
		if len(images) == maxInlineImages || total+len(data) > maxInlineTotal {
			overBudget++
			continue
		}
		total += len(data)
		images = append(images, &mcpsdk.ImageContent{Data: data, MIMEType: "image/png"})
	}
	var notes []string
	if tooBig > 0 {
		notes = append(notes, fmt.Sprintf("%d screenshot(s) are over 1 MB", tooBig))
	}
	if overBudget > 0 {
		notes = append(notes, fmt.Sprintf("%d screenshot(s) are over the limit of %d images and 4 MB in one result", overBudget, maxInlineImages))
	}
	if len(notes) > 0 {
		out.ScreenshotNote = strings.Join(notes, "; ") + ". They are not inline: read them from their path."
	}
	if len(images) == 0 {
		return nil
	}
	text, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return &mcpsdk.CallToolResult{Content: append([]mcpsdk.Content{&mcpsdk.TextContent{Text: string(text)}}, images...)}
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

// buildLogSet returns the set of bino-build-*.json paths currently in dir.
func buildLogSet(dir string) map[string]struct{} {
	set := map[string]struct{}{}
	matches, _ := filepath.Glob(filepath.Join(dir, "bino-build-*.json")) //nolint:errcheck // constant pattern; Glob errors only on malformed patterns
	for _, m := range matches {
		set[m] = struct{}{}
	}
	return set
}

// newestBuildLog finds the most recent bino-build-*.json in dir that is not in
// the exclude set (i.e. written by the current build) and parses it. Returns
// ("", nil) when none is found.
func newestBuildLog(dir string, exclude map[string]struct{}) (string, *buildlog.JSONBuildLog) {
	matches, err := filepath.Glob(filepath.Join(dir, "bino-build-*.json"))
	if err != nil {
		return "", nil
	}
	var candidates []string
	for _, m := range matches {
		if _, skip := exclude[m]; !skip {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return modTime(candidates[i]).After(modTime(candidates[j]))
	})
	newest := candidates[0]
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
