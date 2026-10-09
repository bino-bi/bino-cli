//go:build integration

package chrome

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// readyPage is a page that publishes the engine's readiness values by hand:
// the two window values first, then the console line.
func readyPage(script string) string {
	return "<!doctype html><html><body><p id='bino-text-note'>report</p><script>" + script + "</script></body></html>"
}

// failedPage has settled with two failed components.
const failedPage = `
	window.componentRegisterFailures = [
		{ tag: 'bn-table-renderer', id: '', message: 'boom' },
		{ tag: 'bn-chart-time-renderer', id: 'trend', message: 'bang' },
	];
	window.componentRegisterIsRenderedResult = false;
	console.log('componentRegisterIsRendered:', false);`

// readyDelay is how long the late pages take to report true. It must be well
// above the 500 ms without network activity that Chrome needs for networkIdle:
// only then is the wait already running when the line arrives, and has read
// the failure list for the not ready line before it.
const readyDelay = 1500 * time.Millisecond

func chromeForTest(t *testing.T) string {
	t.Helper()
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("chrome manager unavailable: %v", err)
	}
	chromePath, err := mgr.ResolveExecPath()
	if err != nil {
		t.Fatalf("chrome-headless-shell not installed (run 'bino setup' or set CHROME_PATH): %v", err)
	}
	return chromePath
}

func servePage(t *testing.T, script string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(readyPage(script)))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// The readiness wait is the CLI side of a contract with the template engine:
// a console line plus two values on window. These tests drive RenderPDF with a
// page that writes them by hand, in a real Chrome.
func TestIntegration_RenderPDF_ComponentReadiness(t *testing.T) {
	chromePath := chromeForTest(t)
	delay := strconv.FormatInt(readyDelay.Milliseconds(), 10)

	tests := []struct {
		name   string
		script string
		// wantFailures are the tags of the failed components RenderPDF must
		// report as an error, nil for a page that prints.
		wantFailures []string
		// late marks a page that reports true only after readyDelay.
		late bool
	}{
		{
			// A failed component: the engine reports false and never true.
			name:         "failed components fail the render at once",
			script:       failedPage,
			wantFailures: []string{"bn-table-renderer", "bn-chart-time-renderer"},
		},
		{
			// Still working: false with an empty list, true later.
			name: "not ready without failures waits for ready",
			script: `
				window.componentRegisterFailures = [];
				window.componentRegisterIsRenderedResult = false;
				console.log('componentRegisterIsRendered:', false);
				setTimeout(function () {
					window.componentRegisterIsRenderedResult = true;
					console.log('componentRegisterIsRendered:', true);
				}, ` + delay + `);`,
			late: true,
		},
		{
			// An engine that does not publish a failure list at all.
			name: "engine without a failure list",
			script: `
				console.log('componentRegisterIsRendered:', false);
				setTimeout(function () { console.log('componentRegisterIsRendered:', true); }, ` + delay + `);`,
			late: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdfPath := filepath.Join(t.TempDir(), "out.pdf")

			start := time.Now()
			err := RenderPDF(context.Background(), PDFOptions{
				URL:                   servePage(t, tt.script),
				PDFPath:               pdfPath,
				ChromePath:            chromePath,
				Timeout:               time.Minute,
				WaitForComponentReady: true,
			})
			// Far below the timeout: a wait that ignores the failure list or
			// the ready line would run the full minute.
			elapsed := time.Since(start)
			if elapsed > 30*time.Second {
				t.Errorf("RenderPDF() took %s, want it to stop waiting early", elapsed)
			}
			// A wait that stops at the first not ready line prints too early.
			if tt.late && elapsed < readyDelay {
				t.Errorf("RenderPDF() took %s, want it to wait the %s until the page is ready", elapsed, readyDelay)
			}

			if tt.wantFailures == nil {
				if err != nil {
					t.Fatalf("RenderPDF() error = %v", err)
				}
				if info, err := os.Stat(pdfPath); err != nil || info.Size() == 0 {
					t.Errorf("no PDF written: %v", err)
				}
				return
			}

			var failed *ComponentFailuresError
			if !errors.As(err, &failed) {
				t.Fatalf("RenderPDF() error = %v, want a ComponentFailuresError", err)
			}
			got := failed.Failures
			if len(got) != len(tt.wantFailures) {
				t.Fatalf("failures = %v, want tags %v", got, tt.wantFailures)
			}
			for i, tag := range tt.wantFailures {
				if got[i].Tag != tag {
					t.Errorf("failures[%d].Tag = %q, want %q", i, got[i].Tag, tag)
				}
			}
			if got[0].Message != "boom" || got[1].ID != "trend" {
				t.Errorf("failures = %+v, want message and id carried over", got)
			}
			// An incomplete page must not leave a PDF behind.
			if _, err := os.Stat(pdfPath); !os.IsNotExist(err) {
				t.Errorf("a PDF was written for a page with failed components (stat error: %v)", err)
			}
		})
	}
}

// A failed component keeps its layout box, so a screenshot of it would be an
// empty image. No ref is captured then, and each ref carries the failure.
func TestIntegration_RenderScreenshots_ComponentFailures(t *testing.T) {
	chromePath := chromeForTest(t)
	outDir := t.TempDir()
	refs := []ScreenshotRef{{Kind: "Text", Name: "note"}, {Kind: "Table", Name: "sales"}}

	start := time.Now()
	results, err := RenderScreenshots(context.Background(), ScreenshotOptions{
		URL:                   servePage(t, failedPage),
		OutputDir:             outDir,
		ChromePath:            chromePath,
		Timeout:               time.Minute,
		WaitForComponentReady: true,
		Refs:                  refs,
		FilenamePrefix:        "shot",
		FilenamePattern:       "ref",
	})
	if err != nil {
		t.Fatalf("RenderScreenshots() error = %v, want the failure on each result", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("RenderScreenshots() took %s, want it to stop waiting early", elapsed)
	}
	if len(results) != len(refs) {
		t.Fatalf("got %d results, want one per ref", len(results))
	}
	for i, r := range results {
		if r.Ref != refs[i] {
			t.Errorf("results[%d].Ref = %v, want %v", i, r.Ref, refs[i])
		}
		var failed *ComponentFailuresError
		if !errors.As(r.Error, &failed) || len(failed.Failures) != 2 {
			t.Errorf("results[%d].Error = %v, want a ComponentFailuresError with both failures", i, r.Error)
		}
	}
	// The first ref exists on the page and is visible: without the check it
	// would be captured.
	files, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("%d file(s) written for a page with failed components, want none", len(files))
	}
}
