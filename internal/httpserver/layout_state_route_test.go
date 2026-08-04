package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/layoutstate"
)

// countingLogger records how many warnings were emitted.
type countingLogger struct {
	mu    sync.Mutex
	warns int
}

func (l *countingLogger) Warnf(string, ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns++
}

func (l *countingLogger) warnings() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.warns
}

func (*countingLogger) Infof(string, ...any)         {}
func (*countingLogger) Successf(string, ...any)      {}
func (*countingLogger) Errorf(string, ...any)        {}
func (*countingLogger) Debugf(string, ...any)        {}
func (l *countingLogger) Channel(string) logx.Logger { return l }

// postLayoutState drives the layout-state route through the full mux.
func postLayoutState(t *testing.T, srv *Server, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/__bino/layout-state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	return w.Result()
}

// emptyChartSnapshot is a minimal version-1 capture of one chart that rendered
// no bars — enough to produce exactly one finding.
const emptyChartSnapshot = `{
  "state": {
    "version": 1,
    "detail": "summary",
    "context": {"viewport": {"x": 0, "y": 0, "width": 794, "height": 1123}},
    "components": [{
      "version": 1,
      "tag": "bn-chart-time",
      "id": "bn-chart-time[0]",
      "rect": {"context": {"x": 0, "y": 0, "width": 640, "height": 240},
               "component": {"x": 0, "y": 0, "width": 640, "height": 240}},
      "em": {"fontSizePx": 13.33, "appliedScaleFactor": 1},
      "metadata": {"barCount": 0, "hasNoData": true},
      "diagnostics": []
    }]
  },
  "sources": {"bn-chart-time[0]": {"kind": "ChartTime", "name": "revenueTrend"}}
}`

func TestLayoutStateRouteReturnsFindings(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	resp := postLayoutState(t, srv, emptyChartSnapshot)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Findings []layoutstate.Finding `json:"findings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(body.Findings), body.Findings)
	}
	got := body.Findings[0]
	if got.Rule != layoutstate.RuleEmptyComponent {
		t.Errorf("rule = %q, want %q", got.Rule, layoutstate.RuleEmptyComponent)
	}
	// The manifest identity the browser supplied must survive the round trip,
	// otherwise a finding cannot name the document it belongs to.
	if got.Kind != "ChartTime" || got.Name != "revenueTrend" {
		t.Errorf("identity = (%q,%q), want (ChartTime,revenueTrend)", got.Kind, got.Name)
	}
}

func TestLayoutStateRouteRejectsBadRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "malformed json",
			body: `{"state": `,
			want: http.StatusBadRequest,
		},
		{
			// An engine older than v1.0.0-next.24 has no getLayoutState at
			// all, but a future breaking schema change would post a version
			// this build cannot read.
			name: "unsupported version",
			body: `{"state":{"version":2,"components":[]}}`,
			want: http.StatusUnprocessableEntity,
		},
		{
			name: "missing version",
			body: `{"state":{"components":[]}}`,
			want: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, err := New(Config{})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}

			resp := postLayoutState(t, srv, tt.body)
			defer resp.Body.Close()

			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

// TestLayoutStateRouteLogsOnce guards the dedup: the inspector re-posts after
// every hot reload, so an unchanged report must not spam the terminal.
func TestLayoutStateRouteLogsOnce(t *testing.T) {
	t.Parallel()
	logger := &countingLogger{}
	srv, err := New(Config{Logger: logger})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	resp := postLayoutState(t, srv, emptyChartSnapshot)
	_ = resp.Body.Close()
	first := logger.warnings()
	if first == 0 {
		t.Fatal("first post logged nothing, want the findings reported")
	}

	resp1 := postLayoutState(t, srv, emptyChartSnapshot)
	_ = resp1.Body.Close()
	if logger.warnings() != first {
		t.Errorf("warnings = %d after a repeat post, want %d", logger.warnings(), first)
	}

	// A report that now renders cleanly must not log, but must reset the
	// fingerprint so the next regression is reported again.
	clean := strings.Replace(emptyChartSnapshot, `"barCount": 0, "hasNoData": true`, `"barCount": 4, "hasNoData": false`, 1)
	resp2 := postLayoutState(t, srv, clean)
	_ = resp2.Body.Close()
	if logger.warnings() != first {
		t.Errorf("warnings = %d after a clean report, want %d", logger.warnings(), first)
	}

	resp3 := postLayoutState(t, srv, emptyChartSnapshot)
	_ = resp3.Body.Close()
	if logger.warnings() <= first {
		t.Error("a returning finding was not logged again")
	}
}
