package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLayoutPageMatchesFormat(t *testing.T) {
	tests := []struct {
		name        string
		pageFormat  string
		target      string
		wantAllowed bool
	}{
		{name: "no target format includes all", pageFormat: "", target: "", wantAllowed: true},
		{name: "default page format matches xga", pageFormat: "", target: "xga", wantAllowed: true},
		{name: "case insensitive compare", pageFormat: "HD", target: "hd", wantAllowed: true},
		{name: "mismatch filtered", pageFormat: "a4", target: "hd", wantAllowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := layoutPageMatchesFormat(tt.pageFormat, tt.target)
			if got != tt.wantAllowed {
				t.Fatalf("layoutPageMatchesFormat(%q, %q) = %t, want %t", tt.pageFormat, tt.target, got, tt.wantAllowed)
			}
		})
	}
}

func TestIsInvalidRootError(t *testing.T) {
	wrapped := &InvalidRootError{Kind: "LayoutCard", Name: "card"}
	if !IsInvalidRootError(wrapped) {
		t.Fatalf("expected IsInvalidRootError to detect direct error")
	}
	if !IsInvalidRootError(fmt.Errorf("wrap: %w", wrapped)) {
		t.Fatalf("expected IsInvalidRootError to detect wrapped error")
	}
	if IsInvalidRootError(fmt.Errorf("other error")) {
		t.Fatalf("expected non-matching error to return false")
	}
}

func TestComponentStyleSpecNormalizedContent(t *testing.T) {
	tests := []struct {
		name    string
		value   json.RawMessage
		want    string
		wantErr bool
	}{
		{name: "object", value: json.RawMessage(`{"color":"red"}`), want: `{"color":"red"}`},
		{name: "string", value: json.RawMessage(`"{\"color\":\"blue\"}"`), want: `{"color":"blue"}`},
		{name: "invalid json string", value: json.RawMessage(`"not json"`), wantErr: true},
		{name: "empty", value: json.RawMessage(nil), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := componentStyleSpec{Content: tt.value}
			got, err := spec.normalizedContent()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizedContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInternationalizationSpecNormalizedContent(t *testing.T) {
	tests := []struct {
		name    string
		value   json.RawMessage
		want    string
		wantErr bool
	}{
		{name: "object", value: json.RawMessage(`{"a":1}`), want: `{"a":1}`},
		{name: "json string", value: json.RawMessage(`"{\"b\":2}"`), want: `{"b":2}`},
		{name: "invalid", value: json.RawMessage(`"oops"`), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := internationalizationSpec{Content: tt.value}
			got, err := spec.normalizedContent()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestRenderInternationalizations(t *testing.T) {
	entries := []internationalization{{code: "de-DE", namespace: "default", value: `{"hello":"Hallo"}`}}
	segments := renderInternationalizations(entries)
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	want := `<bn-internationalization code="de-DE" namespace="default">{&#34;hello&#34;:&#34;Hallo&#34;}</bn-internationalization>`
	got := strings.ReplaceAll(segments[0], "\n", "")
	if got != want {
		t.Fatalf("segment mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestRenderOrientationOnlyInBuildMode(t *testing.T) {
	tests := []struct {
		name            string
		mode            RenderMode
		orientation     string
		wantOrientation bool
	}{
		{
			name:            "build mode with orientation includes attribute",
			mode:            RenderModeBuild,
			orientation:     "landscape",
			wantOrientation: true,
		},
		{
			name:            "preview mode with orientation excludes attribute",
			mode:            RenderModePreview,
			orientation:     "landscape",
			wantOrientation: false,
		},
		{
			name:            "build mode without orientation excludes attribute",
			mode:            RenderModeBuild,
			orientation:     "",
			wantOrientation: false,
		},
		{
			name:            "preview mode without orientation excludes attribute",
			mode:            RenderModePreview,
			orientation:     "",
			wantOrientation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			result, _, err := GenerateHTMLFromDocuments(ctx, nil, "de", tt.orientation, "", tt.mode, "v1.0.0")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			html := string(result.HTML)
			hasOrientation := strings.Contains(html, "render-orientation")
			if hasOrientation != tt.wantOrientation {
				t.Errorf("render-orientation presence = %v, want %v", hasOrientation, tt.wantOrientation)
			}
		})
	}
}
