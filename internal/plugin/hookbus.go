package plugin

import (
	"context"
	"fmt"
	"time"

	"bino.bi/bino/internal/logx"
)

// HookBus dispatches pipeline events to interested plugins.
type HookBus struct {
	registry       *PluginRegistry
	logger         logx.Logger
	defaultTimeout time.Duration
	strictMode     bool
	lastFindings   []LintFinding
}

// NewHookBus creates a hook bus backed by the given registry.
func NewHookBus(registry *PluginRegistry, logger logx.Logger) *HookBus {
	return &HookBus{
		registry:       registry,
		logger:         logger,
		defaultTimeout: 30 * time.Second,
	}
}

// SetStrict enables strict mode where hook errors are fatal.
func (h *HookBus) SetStrict(strict bool) {
	h.strictMode = strict
}

// SetDefaultTimeout overrides the default per-hook timeout.
func (h *HookBus) SetDefaultTimeout(d time.Duration) {
	h.defaultTimeout = d
}

// Dispatch sends a hook event to all interested plugins in order.
// Returns the (possibly modified) payload and any diagnostics.
//
// If a plugin returns modified=true, subsequent plugins and the
// caller receive the modified payload.
//
// If a plugin returns an error:
//   - In non-strict mode: logged as warning, payload passes through
//   - In strict mode: returns the error immediately
func (h *HookBus) Dispatch(ctx context.Context, checkpoint string, payload *HookPayload) (*HookPayload, []Diagnostic, error) {
	plugins := h.registry.PluginsForHook(checkpoint)
	if len(plugins) == 0 {
		return payload, nil, nil
	}

	current := payload
	var allDiagnostics []Diagnostic
	var allFindings []LintFinding

	for _, p := range plugins {
		h.logger.Debugf("dispatching hook %q to plugin %q", checkpoint, p.Manifest().Name)

		hookCtx, cancel := context.WithTimeout(ctx, h.defaultTimeout)
		result, err := p.OnHook(hookCtx, checkpoint, current)
		cancel()

		if err != nil {
			diag := Diagnostic{
				Source:   p.Manifest().Name,
				Stage:    checkpoint,
				Message:  fmt.Sprintf("hook error: %v", err),
				Severity: SeverityWarning,
			}
			if h.strictMode {
				diag.Severity = SeverityError
				allDiagnostics = append(allDiagnostics, diag)
				return current, allDiagnostics, fmt.Errorf("plugin %q hook %q failed (strict mode): %w", p.Manifest().Name, checkpoint, err)
			}
			allDiagnostics = append(allDiagnostics, diag)
			h.logger.Warnf("plugin %q hook %q error (non-fatal): %v", p.Manifest().Name, checkpoint, err)
			continue
		}

		if result != nil {
			allDiagnostics = append(allDiagnostics, result.Diagnostics...)
			allFindings = append(allFindings, result.Findings...)
			if result.Modified && result.Payload != nil {
				current = result.Payload
			}
		}
	}

	// Store findings in the payload metadata for callers that check it.
	h.lastFindings = allFindings

	return current, allDiagnostics, nil
}

// LastFindings returns structured lint findings from the most recent Dispatch call.
func (h *HookBus) LastFindings() []LintFinding {
	return h.lastFindings
}

// DispatchPostLoad dispatches the post-load checkpoint with loaded documents.
func (h *HookBus) DispatchPostLoad(ctx context.Context, docs []DocumentPayload) ([]DocumentPayload, []Diagnostic, error) {
	result, diags, err := h.Dispatch(ctx, "post-load", &HookPayload{Documents: docs})
	if err != nil {
		return docs, diags, err
	}
	return result.Documents, diags, nil
}

// DispatchPostRenderHTML dispatches after HTML generation.
func (h *HookBus) DispatchPostRenderHTML(ctx context.Context, html []byte) ([]byte, []Diagnostic, error) {
	result, diags, err := h.Dispatch(ctx, "post-render-html", &HookPayload{HTML: html})
	if err != nil {
		return html, diags, err
	}
	return result.HTML, diags, nil
}

// DispatchPostDatasetExecute dispatches after dataset SQL queries are executed.
func (h *HookBus) DispatchPostDatasetExecute(ctx context.Context, datasets []DatasetPayload) ([]DatasetPayload, []Diagnostic, error) {
	result, diags, err := h.Dispatch(ctx, "post-dataset-execute", &HookPayload{Datasets: datasets})
	if err != nil {
		return datasets, diags, err
	}
	return result.Datasets, diags, nil
}

// DispatchPostRenderPDF dispatches after PDF rendering (read-only).
func (h *HookBus) DispatchPostRenderPDF(ctx context.Context, pdfPath string) ([]Diagnostic, error) {
	_, diags, err := h.Dispatch(ctx, "post-render-pdf", &HookPayload{PDFPath: pdfPath})
	return diags, err
}
