// Package render provides HTML generation from report manifests.
package render

import (
	"errors"
)

// RenderMode describes the caller context for rendering.
type RenderMode string

const (
	// RenderModeBuild indicates a build (PDF generation) context.
	RenderModeBuild RenderMode = "build"
	// RenderModePreview indicates a live preview (HTTP server) context.
	RenderModePreview RenderMode = "preview"
)

// InvalidLayoutPolicy describes how callers should react to an invalid layout error.
type InvalidLayoutPolicy struct {
	// IsInvalidRoot reports whether the error is an InvalidRootError.
	IsInvalidRoot bool
	// Message is the standardized user-facing explanation.
	Message string
	// Hint provides actionable guidance to fix the issue.
	Hint string
}

// ClassifyInvalidLayout inspects err and returns policy info for handling invalid layouts.
// The mode parameter allows future mode-specific behavior if needed.
func ClassifyInvalidLayout(err error, mode RenderMode) InvalidLayoutPolicy {
	if err == nil {
		return InvalidLayoutPolicy{}
	}
	var target *InvalidRootError
	if !errors.As(err, &target) {
		return InvalidLayoutPolicy{}
	}
	return InvalidLayoutPolicy{
		IsInvalidRoot: true,
		Message:       target.Error(),
		Hint:          "Ensure at least one LayoutPage is defined and referenced by your report artefact.",
	}
}
