package render

import (
	"fmt"
	"testing"
)

func TestClassifyInvalidLayout(t *testing.T) {
	tests := []struct {
		name             string
		err              error
		mode             Mode
		wantInvalid      bool
		wantMessage      string
		wantHintNonEmpty bool
	}{
		{
			name:        "nil error",
			err:         nil,
			mode:        ModeBuild,
			wantInvalid: false,
		},
		{
			name:        "unrelated error",
			err:         fmt.Errorf("some other error"),
			mode:        ModePreview,
			wantInvalid: false,
		},
		{
			name:             "direct InvalidRootError build mode",
			err:              &InvalidRootError{Kind: "LayoutCard", Name: "card"},
			mode:             ModeBuild,
			wantInvalid:      true,
			wantMessage:      "document card of kind LayoutCard cannot render as root; define a LayoutPage instead",
			wantHintNonEmpty: true,
		},
		{
			name:             "direct InvalidRootError preview mode",
			err:              &InvalidRootError{Kind: "Text", Name: "intro"},
			mode:             ModePreview,
			wantInvalid:      true,
			wantMessage:      "document intro of kind Text cannot render as root; define a LayoutPage instead",
			wantHintNonEmpty: true,
		},
		{
			name:             "wrapped InvalidRootError",
			err:              fmt.Errorf("render failed: %w", &InvalidRootError{Kind: "Chart", Name: "sales"}),
			mode:             ModeBuild,
			wantInvalid:      true,
			wantMessage:      "document sales of kind Chart cannot render as root; define a LayoutPage instead",
			wantHintNonEmpty: true,
		},
		{
			name:             "InvalidRootError without name",
			err:              &InvalidRootError{Kind: "LayoutCard"},
			mode:             ModePreview,
			wantInvalid:      true,
			wantMessage:      "document kind LayoutCard cannot render as root; define a LayoutPage instead",
			wantHintNonEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := ClassifyInvalidLayout(tt.err, tt.mode)

			if policy.IsInvalidRoot != tt.wantInvalid {
				t.Errorf("IsInvalidRoot = %v, want %v", policy.IsInvalidRoot, tt.wantInvalid)
			}

			if tt.wantInvalid {
				if policy.Message != tt.wantMessage {
					t.Errorf("Message = %q, want %q", policy.Message, tt.wantMessage)
				}
				if tt.wantHintNonEmpty && policy.Hint == "" {
					t.Error("Hint is empty, expected non-empty hint")
				}
			} else {
				if policy.Message != "" {
					t.Errorf("Message = %q, want empty for non-invalid error", policy.Message)
				}
				if policy.Hint != "" {
					t.Errorf("Hint = %q, want empty for non-invalid error", policy.Hint)
				}
			}
		})
	}
}

func TestModeConstants(t *testing.T) {
	if ModeBuild != "build" {
		t.Errorf("ModeBuild = %q, want %q", ModeBuild, "build")
	}
	if ModePreview != "preview" {
		t.Errorf("ModePreview = %q, want %q", ModePreview, "preview")
	}
}
