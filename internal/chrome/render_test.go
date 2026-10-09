package chrome

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/logx"
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

// warnCaptureLogger records Warnf calls for assertions.
type warnCaptureLogger struct {
	logx.Logger
	warns []string
}

func (l *warnCaptureLogger) Warnf(format string, args ...any) {
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}

// newSignals returns the channels observeComponentReady would fill.
func newSignals() *readySignals {
	return &readySignals{ready: make(chan struct{}, 1), notReady: make(chan struct{}, 1)}
}

// noFailures is a page without failed components.
func noFailures() []ComponentFailure { return nil }

func TestWaitForComponentReady(t *testing.T) {
	failed := []ComponentFailure{{Tag: "bn-table-renderer", Message: "boom"}}

	t.Run("ready signal returns without warning", func(t *testing.T) {
		signals := newSignals()
		signals.ready <- struct{}{}
		logger := &warnCaptureLogger{Logger: logx.Nop()}

		got, err := waitForComponentReady(context.Background(), signals, time.Second, noFailures, logger)
		if err != nil {
			t.Fatalf("waitForComponentReady() error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("waitForComponentReady() failures = %v, want none", got)
		}
		if len(logger.warns) != 0 {
			t.Errorf("waitForComponentReady() logged warnings %v, want none", logger.warns)
		}
	})

	t.Run("timeout logs warning", func(t *testing.T) {
		logger := &warnCaptureLogger{Logger: logx.Nop()}

		got, err := waitForComponentReady(context.Background(), newSignals(), 10*time.Millisecond, noFailures, logger)
		if err != nil {
			t.Fatalf("waitForComponentReady() error = %v, want nil (timeout is tolerated)", err)
		}
		if len(got) != 0 {
			t.Errorf("waitForComponentReady() failures = %v, want none", got)
		}
		if len(logger.warns) != 1 {
			t.Fatalf("waitForComponentReady() logged %d warnings, want 1", len(logger.warns))
		}
		if !strings.Contains(logger.warns[0], "may be incomplete") {
			t.Errorf("warning %q should mention incomplete output", logger.warns[0])
		}
	})

	t.Run("cancellation returns error without warning", func(t *testing.T) {
		logger := &warnCaptureLogger{Logger: logx.Nop()}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := waitForComponentReady(ctx, newSignals(), time.Second, noFailures, logger)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForComponentReady() error = %v, want context.Canceled", err)
		}
		if len(logger.warns) != 0 {
			t.Errorf("waitForComponentReady() logged warnings %v, want none", logger.warns)
		}
	})

	// The engine never reports a page with a failed component as rendered, so
	// the wait must end on the failure list and not run into the timeout.
	t.Run("failed components end the wait", func(t *testing.T) {
		signals := newSignals()
		signals.notReady <- struct{}{}
		logger := &warnCaptureLogger{Logger: logx.Nop()}

		start := time.Now()
		got, err := waitForComponentReady(context.Background(), signals, time.Minute, func() []ComponentFailure { return failed }, logger)
		if err != nil {
			t.Fatalf("waitForComponentReady() error = %v, want nil", err)
		}
		if len(got) != 1 || got[0] != failed[0] {
			t.Errorf("waitForComponentReady() failures = %v, want %v", got, failed)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("waitForComponentReady() took %s, want it to return at once", elapsed)
		}
		if len(logger.warns) != 0 {
			t.Errorf("waitForComponentReady() logged warnings %v, want none (the caller reports failures)", logger.warns)
		}
	})

	// "Not rendered" without failures means a component is still working.
	t.Run("not ready without failures keeps waiting", func(t *testing.T) {
		signals := newSignals()
		signals.notReady <- struct{}{}
		asked := make(chan struct{})
		sent := make(chan struct{})
		failures := func() []ComponentFailure {
			close(asked)
			return nil
		}
		go func() {
			<-asked
			signals.ready <- struct{}{}
			close(sent)
		}()
		logger := &warnCaptureLogger{Logger: logx.Nop()}

		got, err := waitForComponentReady(context.Background(), signals, 10*time.Second, failures, logger)
		if err != nil {
			t.Fatalf("waitForComponentReady() error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("waitForComponentReady() failures = %v, want none", got)
		}
		if len(logger.warns) != 0 {
			t.Errorf("waitForComponentReady() logged warnings %v, want none", logger.warns)
		}
		// A wait that went on until the ready line has taken its token.
		<-sent
		select {
		case <-signals.ready:
			t.Error("waitForComponentReady() returned before the ready line")
		default:
		}
	})

	// A component that is slow and then throws: the first line comes while it
	// still works, the second one after it failed. The list is read each time.
	t.Run("a later not ready line carries the failures", func(t *testing.T) {
		signals := newSignals()
		signals.notReady <- struct{}{}
		reads := 0
		failures := func() []ComponentFailure {
			reads++
			if reads == 1 {
				signals.notReady <- struct{}{}
				return nil
			}
			return failed
		}

		got, err := waitForComponentReady(context.Background(), signals, 10*time.Second, failures, logx.Nop())
		if err != nil {
			t.Fatalf("waitForComponentReady() error = %v, want nil", err)
		}
		if reads != 2 {
			t.Errorf("failures were read %d times, want once per not ready line", reads)
		}
		if len(got) != 1 || got[0] != failed[0] {
			t.Errorf("waitForComponentReady() failures = %v, want %v", got, failed)
		}
	})

	t.Run("not ready without failures runs into the timeout", func(t *testing.T) {
		signals := newSignals()
		signals.notReady <- struct{}{}
		logger := &warnCaptureLogger{Logger: logx.Nop()}

		got, err := waitForComponentReady(context.Background(), signals, 10*time.Millisecond, noFailures, logger)
		if err != nil {
			t.Fatalf("waitForComponentReady() error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("waitForComponentReady() failures = %v, want none", got)
		}
		if len(logger.warns) != 1 {
			t.Fatalf("waitForComponentReady() logged %d warnings, want 1", len(logger.warns))
		}
	})

	// A page that is rendered is ready, whatever an older line said. The
	// failure list is not consulted then.
	t.Run("ready wins over an older not ready line", func(t *testing.T) {
		signals := newSignals()
		signals.notReady <- struct{}{}
		signals.ready <- struct{}{}
		failures := func() []ComponentFailure {
			t.Error("failures were read although the page is ready")
			return failed
		}

		for range 20 { // select picks at random, so one pass proves little
			got, err := waitForComponentReady(context.Background(), signals, time.Second, failures, logx.Nop())
			if err != nil || len(got) != 0 {
				t.Fatalf("waitForComponentReady() = %v, %v, want no failures and no error", got, err)
			}
			signals.ready <- struct{}{}
			select {
			case signals.notReady <- struct{}{}:
			default:
			}
		}
	})
}

func TestClassifyReadyLine(t *testing.T) {
	const prefix = "componentregisterisrendered:"
	tests := []struct {
		line         string
		wantRendered bool
		wantOK       bool
	}{
		{line: "componentRegisterIsRendered: true", wantRendered: true, wantOK: true},
		{line: "componentRegisterIsRendered: false", wantOK: true},
		{line: "componentRegisterIsRendered:true", wantRendered: true, wantOK: true},
		{line: `componentRegisterIsRendered: "true"`, wantRendered: true, wantOK: true},
		{line: "COMPONENTREGISTERISRENDERED: TRUE", wantRendered: true, wantOK: true},
		// The engine wrote no value: not rendered, but still its line.
		{line: "componentRegisterIsRendered:", wantOK: true},
		{line: "componentRegisterIsRendered: undefined", wantOK: true},
		{line: "some other log line"},
		{line: "note: componentRegisterIsRendered: true"},
		{line: ""},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			rendered, ok := classifyReadyLine(tt.line, prefix)
			if rendered != tt.wantRendered || ok != tt.wantOK {
				t.Errorf("classifyReadyLine(%q) = %v, %v, want %v, %v", tt.line, rendered, ok, tt.wantRendered, tt.wantOK)
			}
		})
	}
}

func TestComponentFailureString(t *testing.T) {
	tests := []struct {
		name    string
		failure ComponentFailure
		want    string
	}{
		{name: "tag and message", failure: ComponentFailure{Tag: "bn-table-renderer", Message: "boom"}, want: "bn-table-renderer: boom"},
		{name: "with id", failure: ComponentFailure{Tag: "bn-table", ID: "sales", Message: "boom"}, want: "bn-table#sales: boom"},
		{name: "without message", failure: ComponentFailure{Tag: "bn-tree"}, want: "bn-tree"},
		{name: "empty entry", failure: ComponentFailure{}, want: "component"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.failure.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComponentFailuresError(t *testing.T) {
	one := &ComponentFailuresError{Failures: []ComponentFailure{{Tag: "bn-table-renderer", Message: "boom"}}}
	if got, want := one.Error(), "1 component failed to render: bn-table-renderer: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	two := &ComponentFailuresError{Failures: []ComponentFailure{
		{Tag: "bn-table-renderer", Message: "boom"},
		{Tag: "bn-chart-time-renderer", ID: "trend", Message: "bang"},
	}}
	want := "2 components failed to render: bn-table-renderer: boom; bn-chart-time-renderer#trend: bang"
	if got := two.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	// Callers wrap the error with the artefact name and must still find it.
	wrapped := fmt.Errorf("artefact demo: %w", two)
	var target *ComponentFailuresError
	if !errors.As(wrapped, &target) || len(target.Failures) != 2 {
		t.Errorf("errors.As did not find the failures in %v", wrapped)
	}
}
