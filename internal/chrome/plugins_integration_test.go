//go:build integration

package chrome

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"bino.bi/bino/internal/logx"
)

// testChartScript defines <test-chart>. It tracks a draw that sets
// window.done after the delay attribute (300 ms by default), rejects with
// mode="reject", and throws with mode="throw".
const testChartScript = `customElements.define("test-chart", class extends HTMLElement {
  connectedCallback() {
    const mode = this.getAttribute("mode");
    if (mode === "throw") throw new Error("sync boom");
    const draw = new Promise((resolve, reject) => setTimeout(() => {
      if (mode === "reject") { reject(new Error("draw boom")); return; }
      window.done = true;
      resolve();
    }, Number(this.getAttribute("delay") || 300)));
    window.binoPlugin?.track(this, draw);
  }
});`

// requireChrome fails when chrome-headless-shell is missing: the integration
// job installs it, so a skip would hide a broken setup.
func requireChrome(t *testing.T) string {
	t.Helper()
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("chrome manager unavailable: %v", err)
	}
	path, err := mgr.ResolveExecPath()
	if err != nil {
		t.Fatalf("chrome-headless-shell not installed (run 'bino setup' or set CHROME_PATH): %v", err)
	}
	return path
}

// servePluginPage serves a page whose <bn-context> holds body, with the
// test-chart script loaded from /plugins/.
func servePluginPage(t *testing.T, body string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/plugins/test/chart.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = io.WriteString(w, testChartScript)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><html><head><script src="/plugins/test/chart.js"></script></head>`+
			`<body><bn-context><div id="bino-test-shot">shot</div>`+body+`</bn-context></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// settlePage loads body, runs the plugin step and reports window.done.
func settlePage(t *testing.T, chromePath, body string, inject bool) (done bool, settleErr error) {
	t.Helper()
	url := servePluginPage(t, body)
	allocCtx, allocCancel := newExecAllocator(context.Background(), chromePath, false)
	defer allocCancel()
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()
	taskCtx, timeoutCancel := context.WithTimeout(taskCtx, time.Minute)
	defer timeoutCancel()

	actions := []chromedp.Action{chromedp.Navigate(url), waitNetworkIdle(logx.Nop())}
	if inject {
		actions = append([]chromedp.Action{injectPluginRuntime()}, actions...)
	}
	if err := chromedp.Run(taskCtx, actions...); err != nil {
		t.Fatalf("load page: %v", err)
	}
	settleErr = waitForPluginComponents(taskCtx)
	if err := chromedp.Run(taskCtx, chromedp.Evaluate(`window.done === true`, &done)); err != nil {
		t.Fatalf("read window.done: %v", err)
	}
	return done, settleErr
}

func TestIntegration_WaitForPluginComponents(t *testing.T) {
	chromePath := requireChrome(t)
	tests := []struct {
		name     string
		body     string
		inject   bool
		wantErr  []string
		wantDone bool
	}{
		// The delay outlasts the network idle wait, so only the plugin step
		// can make window.done true before it is read.
		{name: "tracked draw", body: `<test-chart id="ok" delay="1500"></test-chart>`, inject: true, wantDone: true},
		{name: "rejected draw", body: `<test-chart id="bad" mode="reject"></test-chart>`, inject: true,
			wantErr: []string{"plugin component test-chart#bad failed to render: draw boom"}},
		{name: "sync throw", body: `<test-chart mode="throw"></test-chart>`, inject: true,
			wantErr: []string{"plugin script error", "sync boom", "/plugins/test/chart.js"}},
		{name: "undefined plugin element", body: `<foo-bar></foo-bar>`, inject: true,
			wantErr: []string{"custom element foo-bar was never defined"}},
		{name: "undefined engine element", body: `<bn-foo></bn-foo>`, inject: true},
		{name: "no runtime", body: `<test-chart delay="1500"></test-chart><foo-bar></foo-bar>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := settlePage(t, chromePath, tt.body, tt.inject)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("err = nil, want an error")
				}
				for _, w := range tt.wantErr {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("err = %q, want it to contain %q", err, w)
					}
				}
			}
			if done != tt.wantDone {
				t.Errorf("window.done = %v, want %v", done, tt.wantDone)
			}
		})
	}
}

func TestIntegration_RenderPDFWaitsForPlugins(t *testing.T) {
	chromePath := requireChrome(t)
	ctx := context.Background()
	opts := func(body string) PDFOptions {
		return PDFOptions{
			URL:            servePluginPage(t, body),
			PDFPath:        filepath.Join(t.TempDir(), "out.pdf"),
			ChromePath:     chromePath,
			Format:         "a4",
			Timeout:        time.Minute,
			WaitForPlugins: true,
		}
	}

	t.Run("failure fails the render", func(t *testing.T) {
		o := opts(`<test-chart id="bad" mode="reject"></test-chart>`)
		err := RenderPDF(ctx, o)
		if err == nil || !strings.Contains(err.Error(), "test-chart#bad") {
			t.Fatalf("err = %v, want a failure naming test-chart#bad", err)
		}
		if _, statErr := os.Stat(o.PDFPath); !os.IsNotExist(statErr) {
			t.Errorf("a PDF was written despite the failure")
		}
	})

	t.Run("allow incomplete warns", func(t *testing.T) {
		o := opts(`<test-chart id="bad" mode="reject"></test-chart>`)
		o.AllowIncompletePlugins = true
		var logged strings.Builder
		logCtx := logx.WithLogger(ctx, logx.NewTerminalWithColor(&logged, &logged, false, true))
		if err := RenderPDF(logCtx, o); err != nil {
			t.Fatalf("RenderPDF: %v", err)
		}
		if _, err := os.Stat(o.PDFPath); err != nil {
			t.Errorf("PDF not written: %v", err)
		}
		if !strings.Contains(logged.String(), "test-chart#bad failed to render") {
			t.Errorf("no warning logged; log: %q", logged.String())
		}
	})

	t.Run("screenshots fail too", func(t *testing.T) {
		_, err := RenderScreenshots(ctx, ScreenshotOptions{
			URL:            servePluginPage(t, `<foo-bar></foo-bar>`),
			OutputDir:      t.TempDir(),
			ChromePath:     chromePath,
			Timeout:        time.Minute,
			Refs:           []ScreenshotRef{{Kind: "test", Name: "shot"}},
			FilenamePrefix: "s",
			WaitForPlugins: true,
		})
		if err == nil || !strings.Contains(err.Error(), "foo-bar was never defined") {
			t.Fatalf("err = %v, want a never-defined error", err)
		}
	})
}
