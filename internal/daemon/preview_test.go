package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain lets this test binary stand in for `bino preview`: PreviewProcess
// runs os.Executable(), which under `go test` is this binary.
// BINO_FAKE_PREVIEW picks the behavior: "serve" prints the ready line and
// runs until interrupted, "args" does the same after printing its arguments,
// "fail" prints an error and exits 1.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "preview" {
		os.Exit(fakePreviewMain(os.Getenv("BINO_FAKE_PREVIEW")))
	}
	os.Exit(m.Run())
}

func fakePreviewMain(mode string) int {
	if mode == "fail" {
		fmt.Println("listen tcp 127.0.0.1:45678: bind: address already in use")
		return 1
	}
	if mode == "args" {
		fmt.Println("args:", strings.Join(os.Args[1:], " "))
		fmt.Println("Serving preview at http://127.0.0.1:45678")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		<-ctx.Done()
		return 0
	}
	// A line longer than the scanner limit must not stall the watcher.
	fmt.Println(strings.Repeat("x", 70_000))
	fmt.Println("Serving preview at http://127.0.0.1:45678")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for i := 0; ctx.Err() == nil; i++ {
		fmt.Println("refresh", i) // keeps the pipe busy after the long line
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	return 0
}

// statusLog records the onStatus callbacks.
type statusLog struct {
	mu  sync.Mutex
	got []string
}

func (l *statusLog) add(info PreviewInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, info.Status)
}

func (l *statusLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

func waitStatus(t *testing.T, p *PreviewProcess, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for p.Info().Status != want {
		if time.Now().After(deadline) {
			t.Fatalf("status = %q, want %q; output:\n%s", p.Info().Status, want, p.Output())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPreviewProcessLifecycle(t *testing.T) {
	t.Setenv("BINO_FAKE_PREVIEW", "serve")
	p := NewPreviewProcess(t.TempDir(), nil)
	var log statusLog
	p.SetOnStatus(log.add)

	info, err := p.Start(45999, "--no-open")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if info.Status != "starting" || info.URL != "http://127.0.0.1:45999" || info.Port != 45999 {
		t.Errorf("Start info = %+v", info)
	}
	waitStatus(t, p, "running")

	// A second Start reuses the running process and ignores the new port.
	again, err := p.Start(1234)
	if err != nil || again != p.Info() || again.Port != 45999 {
		t.Errorf("second Start = %+v, %v; want the running preview", again, err)
	}

	// The fake keeps printing after a line over the scanner limit; Shutdown
	// only returns once the child is reaped, so a stalled pipe would hang here.
	done := make(chan struct{})
	go func() { p.Shutdown(5 * time.Second); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Shutdown did not return")
	}
	if got := p.Info(); got != (PreviewInfo{Status: "stopped"}) {
		t.Errorf("after Shutdown info = %+v", got)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(log.list()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := strings.Join(log.list(), ","); got != "running,stopped" {
		t.Errorf("onStatus = %s, want running,stopped", got)
	}
}

func TestPreviewProcessExitKeepsOutput(t *testing.T) {
	t.Setenv("BINO_FAKE_PREVIEW", "fail")
	p := NewPreviewProcess(t.TempDir(), nil)

	if _, err := p.Start(0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitStatus(t, p, "stopped")
	if out := p.Output(); !strings.Contains(out, "address already in use") {
		t.Errorf("Output = %q, want the bind error", out)
	}

	// A stopped preview starts again.
	t.Setenv("BINO_FAKE_PREVIEW", "serve")
	if _, err := p.Start(0); err != nil {
		t.Fatalf("restart: %v", err)
	}
	waitStatus(t, p, "running")
	p.Shutdown(5 * time.Second)
}

// TestPreviewStartHandlerPassesNoOpen guards against the daemon's preview
// opening a browser tab: /preview/start must run `bino preview --no-open`.
func TestPreviewStartHandlerPassesNoOpen(t *testing.T) {
	t.Setenv("BINO_FAKE_PREVIEW", "args")
	srv := newTestServer(t)
	t.Cleanup(func() { srv.preview.Shutdown(5 * time.Second) })

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/preview/start", strings.NewReader(`{"port":45998}`))
	w := httptest.NewRecorder()
	srv.handlePreviewStart(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	waitStatus(t, srv.preview, "running")

	var args string
	for _, line := range strings.Split(srv.preview.Output(), "\n") {
		if strings.HasPrefix(line, "args:") {
			args = line
		}
	}
	if !strings.Contains(args, " --no-open") || !strings.Contains(args, "--port 45998") {
		t.Errorf("preview args = %q, want --no-open and --port 45998", args)
	}
}
