package cli

import (
	"testing"
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
