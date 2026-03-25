package hooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"bino.bi/bino/internal/logx"
)

// ExitCodeSkip is the exit code that means "skip" — the hook succeeded but
// opted out. It is logged at info level and treated as success.
const ExitCodeSkip = 78

// HookEnv carries the BINO_* environment variables passed to hook commands.
type HookEnv struct {
	Mode          string // build, preview, serve
	Workdir       string
	ReportID      string
	Hook          string // checkpoint name, set automatically by Run
	Verbose       bool
	ArtefactName  string // artifact-scoped hooks only
	ArtefactKind  string // artifact-scoped hooks only
	OutputDir     string // build only
	Include       string // build only (comma-separated)
	Exclude       string // build only (comma-separated)
	PDFPath       string // post-render only
	ListenAddr    string // preview/serve only
	RefreshReason string // pre-refresh only
	LiveArtefact  string // serve only
}

// Runner executes hook commands for resolved checkpoints.
type Runner struct {
	cfg     *Config
	logger  logx.Logger
	workdir string
}

// NewRunner creates a Runner from a resolved Config.
// If cfg is nil or has no hooks, Run is a no-op.
func NewRunner(cfg *Config, logger logx.Logger, workdir string) *Runner {
	return &Runner{cfg: cfg, logger: logger, workdir: workdir}
}

// Run executes all commands registered for the given checkpoint.
// Commands are executed sequentially in order. If a command fails with a
// non-zero, non-78 exit code, Run returns an error immediately.
// Exit code 78 means "skip" and is logged but treated as success.
func (r *Runner) Run(ctx context.Context, checkpoint string, env HookEnv) error {
	if r == nil || r.cfg == nil {
		return nil
	}
	cmds, ok := r.cfg.Hooks[checkpoint]
	if !ok || len(cmds) == 0 {
		return nil
	}

	env.Hook = checkpoint

	envSlice := buildEnvSlice(env)

	for _, cmdStr := range cmds {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.logger.Infof("[hook:%s] running: %s", checkpoint, cmdStr)

		if err := r.execCommand(ctx, cmdStr, envSlice); err != nil {
			return fmt.Errorf("hook %s: %w", checkpoint, err)
		}
	}
	return nil
}

// execCommand runs a single shell command and handles exit codes.
func (r *Runner) execCommand(ctx context.Context, cmdStr string, extraEnv []string) error {
	cmd := shellCommandContext(ctx, cmdStr)
	cmd.Dir = r.workdir
	cmd.Env = append(os.Environ(), extraEnv...)

	// Capture output for logging
	cmd.Stdout = &logWriter{logger: r.logger, level: "info"}
	cmd.Stderr = &logWriter{logger: r.logger, level: "warn"}

	err := cmd.Run()
	if err == nil {
		return nil
	}

	// Check for skip exit code
	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code == ExitCodeSkip {
			r.logger.Infof("[hook:%s] skipped (exit 78)", cmd.Args)
			return nil
		}
		return fmt.Errorf("command %q failed with exit code %d", cmdStr, code)
	}
	return fmt.Errorf("command %q: %w", cmdStr, err)
}

// shellCommandContext returns an exec.Cmd that runs the given command string
// through the platform shell, respecting context cancellation.
func shellCommandContext(ctx context.Context, cmdStr string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", cmdStr)
	}
	return exec.CommandContext(ctx, "sh", "-c", cmdStr)
}

// buildEnvSlice converts a HookEnv into BINO_* key=value pairs.
func buildEnvSlice(env HookEnv) []string {
	pairs := []string{
		"BINO_MODE=" + env.Mode,
		"BINO_WORKDIR=" + env.Workdir,
		"BINO_REPORT_ID=" + env.ReportID,
		"BINO_HOOK=" + env.Hook,
	}
	if env.Verbose {
		pairs = append(pairs, "BINO_VERBOSE=1")
	} else {
		pairs = append(pairs, "BINO_VERBOSE=0")
	}
	if env.ArtefactName != "" {
		pairs = append(pairs, "BINO_ARTIFACT_NAME="+env.ArtefactName, "BINO_ARTEFACT_NAME="+env.ArtefactName)
	}
	if env.ArtefactKind != "" {
		pairs = append(pairs, "BINO_ARTIFACT_KIND="+env.ArtefactKind, "BINO_ARTEFACT_KIND="+env.ArtefactKind)
	}
	if env.OutputDir != "" {
		pairs = append(pairs, "BINO_OUTPUT_DIR="+env.OutputDir)
	}
	if env.Include != "" {
		pairs = append(pairs, "BINO_INCLUDE="+env.Include)
	}
	if env.Exclude != "" {
		pairs = append(pairs, "BINO_EXCLUDE="+env.Exclude)
	}
	if env.PDFPath != "" {
		pairs = append(pairs, "BINO_PDF_PATH="+env.PDFPath)
	}
	if env.ListenAddr != "" {
		pairs = append(pairs, "BINO_LISTEN_ADDR="+env.ListenAddr)
	}
	if env.RefreshReason != "" {
		pairs = append(pairs, "BINO_REFRESH_REASON="+env.RefreshReason)
	}
	if env.LiveArtefact != "" {
		pairs = append(pairs, "BINO_LIVE_ARTIFACT="+env.LiveArtefact, "BINO_LIVE_ARTEFACT="+env.LiveArtefact)
	}
	return pairs
}

// logWriter adapts a Logger to an io.Writer, writing each line at the
// configured level.
type logWriter struct {
	logger logx.Logger
	level  string
	buf    []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := strings.IndexByte(string(w.buf), '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if line == "" {
			continue
		}
		switch w.level {
		case "warn":
			w.logger.Warnf("[hook] %s", line)
		default:
			w.logger.Infof("[hook] %s", line)
		}
	}
	return len(p), nil
}
