package chrome

import (
	"math"
	"testing"
)

func TestFormatDimensionsPx(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		orientation string
		wantW       int
		wantH       int
		wantOK      bool
	}{
		// Custom formats - landscape (default orientation for custom formats)
		{name: "xga landscape", format: "xga", orientation: "landscape", wantW: 1024, wantH: 768, wantOK: true},
		{name: "hd landscape", format: "hd", orientation: "landscape", wantW: 1280, wantH: 720, wantOK: true},
		{name: "full_hd landscape", format: "full_hd", orientation: "landscape", wantW: 1920, wantH: 1080, wantOK: true},
		{name: "full-hd landscape", format: "full-hd", orientation: "landscape", wantW: 1920, wantH: 1080, wantOK: true},
		{name: "fullhd landscape", format: "fullhd", orientation: "landscape", wantW: 1920, wantH: 1080, wantOK: true},
		{name: "4k landscape", format: "4k", orientation: "landscape", wantW: 3840, wantH: 2160, wantOK: true},
		{name: "4k2k landscape", format: "4k2k", orientation: "landscape", wantW: 4096, wantH: 2160, wantOK: true},

		// Custom formats - portrait (swapped)
		{name: "xga portrait", format: "xga", orientation: "portrait", wantW: 768, wantH: 1024, wantOK: true},
		{name: "hd portrait", format: "hd", orientation: "portrait", wantW: 720, wantH: 1280, wantOK: true},
		{name: "full_hd portrait", format: "full_hd", orientation: "portrait", wantW: 1080, wantH: 1920, wantOK: true},
		{name: "4k portrait", format: "4k", orientation: "portrait", wantW: 2160, wantH: 3840, wantOK: true},
		{name: "4k2k portrait", format: "4k2k", orientation: "portrait", wantW: 2160, wantH: 4096, wantOK: true},

		// Standard formats - portrait (default for paper sizes)
		{name: "a3 portrait", format: "a3", orientation: "portrait", wantW: 1123, wantH: 1587, wantOK: true},
		{name: "a4 portrait", format: "a4", orientation: "portrait", wantW: 794, wantH: 1123, wantOK: true},
		{name: "a5 portrait", format: "a5", orientation: "portrait", wantW: 559, wantH: 794, wantOK: true},
		{name: "letter portrait", format: "letter", orientation: "portrait", wantW: 816, wantH: 1056, wantOK: true},
		{name: "legal portrait", format: "legal", orientation: "portrait", wantW: 816, wantH: 1344, wantOK: true},
		{name: "tabloid portrait", format: "tabloid", orientation: "tabloid", wantW: 1056, wantH: 1632, wantOK: true},

		// Standard formats - landscape (swapped)
		{name: "a3 landscape", format: "a3", orientation: "landscape", wantW: 1587, wantH: 1123, wantOK: true},
		{name: "a4 landscape", format: "a4", orientation: "landscape", wantW: 1123, wantH: 794, wantOK: true},
		{name: "a5 landscape", format: "a5", orientation: "landscape", wantW: 794, wantH: 559, wantOK: true},
		{name: "letter landscape", format: "letter", orientation: "landscape", wantW: 1056, wantH: 816, wantOK: true},
		{name: "legal landscape", format: "legal", orientation: "landscape", wantW: 1344, wantH: 816, wantOK: true},
		{name: "tabloid landscape", format: "tabloid", orientation: "landscape", wantW: 1632, wantH: 1056, wantOK: true},

		// Case insensitivity
		{name: "XGA upper", format: "XGA", orientation: "Landscape", wantW: 1024, wantH: 768, wantOK: true},
		{name: "A4 upper", format: "A4", orientation: "Portrait", wantW: 794, wantH: 1123, wantOK: true},
		{name: "FULL_HD upper", format: "FULL_HD", orientation: "PORTRAIT", wantW: 1080, wantH: 1920, wantOK: true},

		// Whitespace trimming
		{name: "a4 with spaces", format: "  a4  ", orientation: "portrait", wantW: 794, wantH: 1123, wantOK: true},
		{name: "hd with spaces", format: " hd ", orientation: "landscape", wantW: 1280, wantH: 720, wantOK: true},

		// Unknown/empty format
		{name: "empty format", format: "", orientation: "landscape", wantW: 0, wantH: 0, wantOK: false},
		{name: "unknown format", format: "b5", orientation: "portrait", wantW: 0, wantH: 0, wantOK: false},
		{name: "whitespace only", format: "   ", orientation: "portrait", wantW: 0, wantH: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH, gotOK := formatDimensionsPx(tt.format, tt.orientation)
			if gotOK != tt.wantOK {
				t.Fatalf("formatDimensionsPx(%q, %q) ok = %v, want %v", tt.format, tt.orientation, gotOK, tt.wantOK)
			}
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Errorf("formatDimensionsPx(%q, %q) = (%d, %d), want (%d, %d)", tt.format, tt.orientation, gotW, gotH, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestPxToInches(t *testing.T) {
	tests := []struct {
		px   int
		want float64
	}{
		{96, 1.0},
		{192, 2.0},
		{48, 0.5},
		{0, 0.0},
	}
	for _, tt := range tests {
		got := pxToInches(tt.px)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("pxToInches(%d) = %f, want %f", tt.px, got, tt.want)
		}
	}
}

func TestMmToInches(t *testing.T) {
	tests := []struct {
		mm   float64
		want float64
	}{
		{25.4, 1.0},
		{0, 0.0},
		{254, 10.0},
	}
	for _, tt := range tests {
		got := mmToInches(tt.mm)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("mmToInches(%f) = %f, want %f", tt.mm, got, tt.want)
		}
	}
}

func TestParseMargin(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"20mm", mmToInches(20)},
		{"15mm", mmToInches(15)},
		{"1in", 1.0},
		{"2.5in", 2.5},
		{"2cm", cmToInches(2)},
		{"96px", 1.0},
		{"48px", 0.5},
		{"20", mmToInches(20)}, // default to mm
		{"", 0},
	}
	for _, tt := range tests {
		got := parseMargin(tt.input)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("parseMargin(%q) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestPaperSizeInches(t *testing.T) {
	tests := []struct {
		format string
		wantW  float64
		wantH  float64
	}{
		{"a4", mmToInches(210), mmToInches(297)},
		{"A4", mmToInches(210), mmToInches(297)},
		{"letter", 8.5, 11},
		{"legal", 8.5, 14},
		{"tabloid", 11, 17},
		{"unknown", 0, 0},
	}
	for _, tt := range tests {
		w, h := paperSizeInches(tt.format)
		if math.Abs(w-tt.wantW) > 0.001 || math.Abs(h-tt.wantH) > 0.001 {
			t.Errorf("paperSizeInches(%q) = (%f, %f), want (%f, %f)", tt.format, w, h, tt.wantW, tt.wantH)
		}
	}
}

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"y", true},
		{"Y", true},
		{"0", false},
		{"false", false},
		{"no", false},
		{"", false},
		{"maybe", false},
	}
	for _, tt := range tests {
		got := isTruthy(tt.input)
		if got != tt.want {
			t.Errorf("isTruthy(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
