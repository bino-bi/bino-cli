package cli

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestDaemonListenAddrFlag asserts the --listen-addr flag exists with the right
// default. The flag lets containerized deployments (e.g. the bino sandbox) tell
// the daemon to bind to 0.0.0.0 instead of the local-only 127.0.0.1 default.
func TestDaemonListenAddrFlag(t *testing.T) {
	cmd := newDaemonCommand()
	f := cmd.Flags().Lookup("listen-addr")
	if f == nil {
		t.Fatal("--listen-addr flag missing")
	}
	if got, want := f.DefValue, "127.0.0.1"; got != want {
		t.Errorf("--listen-addr default = %q, want %q", got, want)
	}

	if err := cmd.Flags().Set("listen-addr", "0.0.0.0"); err != nil {
		t.Fatalf("set --listen-addr=0.0.0.0: %v", err)
	}
	if got := f.Value.String(); got != "0.0.0.0" {
		t.Errorf("--listen-addr after set = %q, want 0.0.0.0", got)
	}
}

func TestWatchIdleExitsAfterTimeout(t *testing.T) {
	var active atomic.Int64
	active.Store(1)
	// lastSeen is the last time watchIdle saw a client. Its idle clock starts
	// no earlier than this, however late the ticks arrive.
	var lastSeen atomic.Int64
	countActive := func() int {
		n := int(active.Load())
		if n > 0 {
			lastSeen.Store(time.Now().UnixNano())
		}
		return n
	}
	idle := make(chan struct{})
	go watchIdle(context.Background(), 5*time.Millisecond, 50*time.Millisecond, countActive, func() { close(idle) })

	// An attached client keeps the daemon alive past the timeout.
	time.Sleep(120 * time.Millisecond)
	select {
	case <-idle:
		t.Fatal("watchIdle fired while a client was active")
	default:
	}

	active.Store(0)
	select {
	case <-idle:
	case <-time.After(5 * time.Second):
		t.Fatal("watchIdle did not fire after the client left")
	}
	if waited := time.Since(time.Unix(0, lastSeen.Load())); waited < 50*time.Millisecond {
		t.Errorf("fired %v after a client was last seen, before the 50ms timeout", waited)
	}
}

func TestWatchIdleStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchIdle(ctx, 5*time.Millisecond, time.Hour, func() int { return 0 }, func() { t.Error("onIdle called") })
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchIdle did not return after cancel")
	}
}
