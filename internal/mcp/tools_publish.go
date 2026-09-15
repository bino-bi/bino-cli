package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/registry"
)

type registryPublishInput struct {
	Bump       string `json:"bump,omitempty" jsonschema:"version increment: patch, minor or major (required unless dry_run)"`
	DryRun     bool   `json:"dry_run,omitempty" jsonschema:"run the registry's validation gate without minting a version"`
	Visibility string `json:"visibility,omitempty" jsonschema:"visibility for a first publish: public or private (the registry owns it afterwards)"`
}

// publishOutcome mirrors the --json shape of `bino publish` (internal/cli
// publishOutcome) so the agent and other tooling read one shape.
type publishOutcome struct {
	Package   string               `json:"package"`
	Version   string               `json:"version"`
	Digest    string               `json:"digest"`
	Tag       string               `json:"tag,omitempty"`
	Kinds     []string             `json:"kinds,omitempty"`
	Files     []registry.FileEntry `json:"files"`
	Unchanged bool                 `json:"unchanged"`
	DryRun    bool                 `json:"dryRun"`
	Warnings  []string             `json:"warnings,omitempty"`
}

type registryPublishOutput struct {
	Success  bool `json:"success"`
	ExitCode int  `json:"exitCode"`
	// Output is `bino publish`'s stderr: progress, lint advice, and on a
	// rejection the registry's gate findings with the bino/engine it ran.
	Output string          `json:"output"`
	Result *publishOutcome `json:"result,omitempty"`
}

func (h *handlers) registerPublishTool(srv *mcpsdk.Server) {
	if !h.deps.AllowPublish {
		return
	}
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "registry_publish",
		Description: "Publish this project's [package] to the registry by running `bino publish --json` — this is IRREVERSIBLE: it mints an immutable version that cannot be deleted, and a first publish with visibility=public makes the package public. Only available because the human started the server with --allow-publish. Use dry_run=true first: the registry runs its validation gate without minting anything. bump (patch|minor|major) is required unless dry_run. Republishing unchanged content succeeds with unchanged=true. When the registry rejects the package, `output` carries its gate findings — fix those, not the local lint. Needs a credential; if none resolves the human must run `bino registry login` (never try to obtain one).",
	}, h.runPublish)
}

func (h *handlers) runPublish(ctx context.Context, _ *mcpsdk.CallToolRequest, in registryPublishInput) (*mcpsdk.CallToolResult, registryPublishOutput, error) {
	buildMu.Lock()
	defer buildMu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		return nil, registryPublishOutput{}, fmt.Errorf("resolve executable: %w", err)
	}
	args := []string{"publish", "--json"}
	if in.Bump != "" {
		args = append(args, "--bump", in.Bump)
	}
	if in.DryRun {
		args = append(args, "--dry-run")
	}
	if in.Visibility != "" {
		args = append(args, "--visibility", in.Visibility)
	}

	cmd := exec.CommandContext(ctx, exe, args...) //nolint:gosec // G204: exe is our own binary, args are controlled
	// `bino publish` has no --work-dir and resolves the project from its cwd.
	// The server is long-lived and shared, so never os.Chdir here.
	cmd.Dir = h.deps.State.ProjectRoot()
	cmd.Env = append(os.Environ(), "BINO_DISABLE_UPDATE_CHECK=1", "NO_COLOR=1")
	// With --json stdout carries only the JSON object; progress and the
	// registry's gate findings go to stderr.
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	exitCode := 0
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return nil, registryPublishOutput{}, fmt.Errorf("run bino publish: %w", runErr)
		}
		exitCode = exitErr.ExitCode()
	}
	out := registryPublishOutput{Success: exitCode == 0, ExitCode: exitCode, Output: tail(stderr.String(), maxBuildOutput)}
	if exitCode != 0 {
		msg := strings.TrimSpace(out.Output)
		if msg == "" {
			msg = fmt.Sprintf("bino publish exited with code %d", exitCode)
		}
		return errorResult(errors.New(msg)), out, nil
	}
	var res publishOutcome
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return errorResult(fmt.Errorf("parse `bino publish --json` output: %w", err)), out, nil
	}
	out.Result = &res
	return nil, out, nil
}
