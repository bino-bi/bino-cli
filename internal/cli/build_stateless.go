package cli

// Stateless single-shot build mode.
//
// `bino build --stateless [--format pdf|png] [<file>|-]` renders a
// SELF-CONTAINED report YAML (a single file that carries every DataSource,
// DataSet, LayoutPage, and artefact it needs) into artifact bytes on stdout,
// with no project-root discovery, no bino.toml, and no dist/ files written.
// It exists so an external caller (bino-cloud) can shell out to bino and turn a
// report payload into PDF/PNG bytes with nothing on disk.
//
// Contract:
//   - Input: a self-contained report YAML read from the positional <file>
//     argument, or from stdin when the argument is "-" or omitted.
//   - Output: the raw artifact bytes on stdout — a PDF (default, --format pdf)
//     or a PNG (--format png). Nothing else is written to stdout.
//   - Errors: a single JSON object {"code":"...","message":"..."} on stderr and
//     a non-zero exit code. The code is drawn from a stable set so callers can
//     branch on it programmatically.
//
// Error codes and exit codes:
//   - invalid_input  (exit 1): bad flag/arg, unreadable input, or the YAML has
//     no artefact of the requested kind.
//   - invalid_yaml   (exit 1): the YAML failed to parse or schema-validate.
//   - engine_error   (exit 2): the template engine or CDN cache could not be
//     prepared.
//   - render_failed  (exit 2): HTML rendering or the Chrome PDF/PNG capture
//     failed.
//   - timeout        (exit 124): the context was canceled or a deadline was
//     exceeded during rendering.
//
// The bytes stream on stdout, so callers should keep stdout as a binary pipe.
// To keep stderr clean for the JSON error, run with BINO_DISABLE_UPDATE_CHECK=1
// (or CI=1): the root command's background update check otherwise prints to
// stderr. Datasource secrets resolve from the process environment via the
// existing <Field>FromEnv mechanism, which passes through unchanged here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/engine"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
)

// statelessErrorCode is a stable, machine-readable error code emitted on stderr
// as JSON when `bino build --stateless` fails. External callers branch on it.
type statelessErrorCode string

const (
	statelessErrInvalidInput statelessErrorCode = "invalid_input"
	statelessErrInvalidYAML  statelessErrorCode = "invalid_yaml"
	statelessErrEngineError  statelessErrorCode = "engine_error"
	statelessErrRenderFailed statelessErrorCode = "render_failed"
	statelessErrTimeout      statelessErrorCode = "timeout"
)

// statelessError is the JSON error shape written to stderr in stateless mode.
type statelessError struct {
	Code    statelessErrorCode `json:"code"`
	Message string             `json:"message"`
}

func (e *statelessError) Error() string { return string(e.Code) + ": " + e.Message }

func newStatelessError(code statelessErrorCode, err error) *statelessError {
	return &statelessError{Code: code, Message: err.Error()}
}

// exitCode maps an error code to the process exit code.
func (e *statelessError) exitCode() int {
	switch e.Code {
	case statelessErrInvalidInput, statelessErrInvalidYAML:
		return 1
	case statelessErrTimeout:
		return 124
	default:
		return 2
	}
}

// emitStatelessError writes the structured error as one JSON line to stderr.
func emitStatelessError(cmd *cobra.Command, serr *statelessError) {
	_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(serr)
}

// runStatelessBuild reads a self-contained report YAML (from inputArg or stdin),
// renders it into a temp directory, and streams the resulting artifact bytes to
// stdout. It never writes to the caller's filesystem beyond os.MkdirTemp scratch
// space, which it removes before returning.
func runStatelessBuild(cmd *cobra.Command, format, inputArg string) *statelessError {
	// Silence all pipeline/engine logging so stdout stays a pure byte stream.
	ctx := logx.WithLogger(cmd.Context(), logx.Nop())

	yamlBytes, serr := readStatelessInput(cmd, inputArg)
	if serr != nil {
		return serr
	}

	tmpDir, err := os.MkdirTemp("", "bino-stateless-*")
	if err != nil {
		return newStatelessError(statelessErrRenderFailed, fmt.Errorf("create temp dir: %w", err))
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "report.yaml"), yamlBytes, 0o600); err != nil {
		return newStatelessError(statelessErrRenderFailed, fmt.Errorf("write temp report: %w", err))
	}

	cacheDir, err := pathutil.CacheDir("cdn")
	if err != nil {
		return newStatelessError(statelessErrEngineError, err)
	}

	engineMgr, err := engine.NewManager()
	if err != nil {
		return newStatelessError(statelessErrEngineError, err)
	}
	engineInfo, err := engineMgr.EnsureVersion(ctx, "")
	if err != nil {
		return newStatelessError(statelessErrEngineError, err)
	}

	docs, err := config.LoadDirWithOptions(ctx, tmpDir, config.LoadOptions{})
	if err != nil {
		return classifyStatelessErr(ctx, statelessErrInvalidYAML, err)
	}
	if len(docs) == 0 {
		return newStatelessError(statelessErrInvalidInput, errors.New("no YAML documents found in input"))
	}

	builder := &pipeline.Builder{
		Workdir:       tmpDir,
		EngineVersion: engineInfo.Version,
		CacheDir:      cacheDir,
	}

	switch format {
	case "pdf":
		return statelessRenderPDF(ctx, cmd, builder, docs)
	case "png":
		return statelessRenderPNG(ctx, cmd, builder, docs)
	default:
		return newStatelessError(statelessErrInvalidInput, fmt.Errorf("invalid --format %q: expected \"pdf\" or \"png\"", format))
	}
}

// statelessRenderPDF renders the first ReportArtefact to a temp PDF and streams it.
func statelessRenderPDF(ctx context.Context, cmd *cobra.Command, builder *pipeline.Builder, docs []config.Document) *statelessError {
	artifacts, err := config.CollectArtefacts(docs)
	if err != nil {
		return newStatelessError(statelessErrInvalidYAML, err)
	}
	if len(artifacts) == 0 {
		return newStatelessError(statelessErrInvalidInput, errors.New("no ReportArtefact found in input"))
	}
	artifact := artifacts[0]

	renderResult, err := builder.RenderArtefactHTML(ctx, docs, artifact)
	if err != nil {
		return classifyStatelessErr(ctx, statelessErrRenderFailed, err)
	}

	pdfPath, err := builder.RenderPDFToTempFileWithData(ctx, renderResult.HTML, renderResult.LocalAssets, renderResult.EmittedData, pipeline.PDFRenderOptions{
		Format:                artifact.Spec.Format,
		Orientation:           artifact.Spec.Orientation,
		WaitForComponentReady: true,
		ReadyConsolePrefix:    componentReadyConsolePrefix,
	})
	if err != nil {
		return classifyStatelessErr(ctx, statelessErrRenderFailed, err)
	}
	defer os.Remove(pdfPath)

	return streamStatelessFile(cmd, pdfPath)
}

// statelessRenderPNG renders the first ScreenshotArtefact into a temp directory
// and streams the first captured PNG. Chrome writes screenshots to a directory,
// so this points OutputDir at temp scratch and reads the file back.
func statelessRenderPNG(ctx context.Context, cmd *cobra.Command, builder *pipeline.Builder, docs []config.Document) *statelessError {
	artefacts, err := config.CollectScreenshotArtefacts(docs)
	if err != nil {
		return newStatelessError(statelessErrInvalidYAML, err)
	}
	if len(artefacts) == 0 {
		return newStatelessError(statelessErrInvalidInput, errors.New("no ScreenshotArtefact found in input"))
	}
	artifact := artefacts[0]

	renderResult, err := builder.RenderScreenshotHTML(ctx, docs, artifact)
	if err != nil {
		return classifyStatelessErr(ctx, statelessErrRenderFailed, err)
	}

	outDir, err := os.MkdirTemp("", "bino-stateless-png-*")
	if err != nil {
		return newStatelessError(statelessErrRenderFailed, fmt.Errorf("create temp dir: %w", err))
	}
	defer os.RemoveAll(outDir)

	refs := make([]pipeline.ScreenshotRef, len(artifact.Spec.Refs))
	for i, ref := range artifact.Spec.Refs {
		refs[i] = pipeline.ScreenshotRef{Kind: ref.Kind, Name: ref.Name}
	}
	var scaleFactor float64
	if strings.EqualFold(artifact.Spec.Scale, "device") {
		scaleFactor = 2.0
	}

	results, err := builder.CaptureScreenshotsWithData(ctx, renderResult.HTML, renderResult.LocalAssets, renderResult.EmittedData, pipeline.ScreenshotRenderOptions{
		OutputDir:             outDir,
		Format:                artifact.Spec.Format,
		Orientation:           artifact.Spec.Orientation,
		WaitForComponentReady: true,
		ReadyConsolePrefix:    componentReadyConsolePrefix,
		Refs:                  refs,
		FilenamePrefix:        artifact.Spec.FilenamePrefix,
		FilenamePattern:       artifact.Spec.FilenamePattern,
		Scale:                 scaleFactor,
	})
	if err != nil {
		return classifyStatelessErr(ctx, statelessErrRenderFailed, err)
	}
	if len(results) == 0 {
		return newStatelessError(statelessErrRenderFailed, errors.New("no screenshot produced"))
	}
	if results[0].Error != nil {
		return classifyStatelessErr(ctx, statelessErrRenderFailed, results[0].Error)
	}
	return streamStatelessFile(cmd, results[0].FilePath)
}

// streamStatelessFile copies the file at path to the command's stdout.
func streamStatelessFile(cmd *cobra.Command, path string) *statelessError {
	f, err := os.Open(path)
	if err != nil {
		return newStatelessError(statelessErrRenderFailed, err)
	}
	defer f.Close() //nolint:errcheck // read-only handle
	if _, err := io.Copy(cmd.OutOrStdout(), f); err != nil {
		return newStatelessError(statelessErrRenderFailed, fmt.Errorf("write artifact to stdout: %w", err))
	}
	return nil
}

// readStatelessInput returns the YAML bytes from inputArg, or from stdin when
// inputArg is empty or "-".
func readStatelessInput(cmd *cobra.Command, inputArg string) ([]byte, *statelessError) {
	if inputArg == "" || inputArg == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, newStatelessError(statelessErrInvalidInput, fmt.Errorf("read stdin: %w", err))
		}
		return data, nil
	}
	data, err := os.ReadFile(inputArg)
	if err != nil {
		return nil, newStatelessError(statelessErrInvalidInput, fmt.Errorf("read %s: %w", inputArg, err))
	}
	return data, nil
}

// classifyStatelessErr maps a context cancellation/deadline to the timeout code
// and everything else to fallback.
func classifyStatelessErr(ctx context.Context, fallback statelessErrorCode, err error) *statelessError {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return newStatelessError(statelessErrTimeout, err)
	}
	return newStatelessError(fallback, err)
}
