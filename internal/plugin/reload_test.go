package plugin

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

// reloadPluginBin is the test plugin from testdata/reloadplugin, built once
// in TestMain. It is empty under -short.
var reloadPluginBin string

func TestMain(m *testing.M) {
	flag.Parse()
	var dir string
	if !testing.Short() {
		var err error
		dir, err = os.MkdirTemp("", "bino-reload-plugin-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		reloadPluginBin = filepath.Join(dir, "bino-plugin-reloadtest")
		if runtime.GOOS == "windows" {
			reloadPluginBin += ".exe"
		}
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", reloadPluginBin, "./testdata/reloadplugin")
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build test plugin: %v\n%s", err, out)
			_ = os.RemoveAll(dir)
			os.Exit(1)
		}
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

// installBinary puts src at dst the way go build does: a new file renamed
// into place.
func installBinary(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		t.Fatal(err)
	}
}

func pluginVersion(t *testing.T, mgr *Manager) string {
	t.Helper()
	p, ok := mgr.Registry().Get("reloadtest")
	if !ok {
		t.Fatal("plugin reloadtest is not in the registry")
	}
	return p.Manifest().Version
}

func TestManager_Reload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and starts a plugin")
	}
	ctx := context.Background()
	root := t.TempDir()
	bin := filepath.Join(root, "bin", filepath.Base(reloadPluginBin))
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	installBinary(t, reloadPluginBin, bin)
	toml := fmt.Sprintf("[plugins.reloadtest]\npath = %q\n", bin)
	if err := os.WriteFile(filepath.Join(root, "bino.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := pathutil.LoadProjectConfig(root)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("BINO_RELOAD_TEST_VERSION", "1.0.0")
	mgr := NewManager(logx.Nop())
	t.Cleanup(func() { mgr.ShutdownAll(context.Background()) })
	if err := mgr.LoadAll(ctx, cfg, root, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if v := pluginVersion(t, mgr); v != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", v)
	}
	if mgr.Changed() {
		t.Fatal("Changed right after load")
	}
	old, _ := mgr.Registry().Get("reloadtest")

	// A rebuild that reports another version.
	t.Setenv("BINO_RELOAD_TEST_VERSION", "2.0.0")
	installBinary(t, reloadPluginBin, bin)
	if !mgr.Changed() {
		t.Fatal("Changed is false after the binary was replaced")
	}
	if err := mgr.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if v := pluginVersion(t, mgr); v != "2.0.0" {
		t.Fatalf("version after reload = %q, want 2.0.0", v)
	}
	if mgr.Changed() {
		t.Fatal("Changed right after reload")
	}
	process := old.(*grpcClient).process
	deadline := time.Now().Add(10 * time.Second)
	for !process.Exited() {
		if time.Now().After(deadline) {
			t.Fatal("the old plugin process did not exit")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// A broken rebuild empties the registry.
	broken := filepath.Join(root, "broken")
	if err := os.WriteFile(broken, []byte("not a binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	installBinary(t, broken, bin)
	if err := mgr.Reload(ctx); err == nil {
		t.Fatal("Reload of a broken binary succeeded")
	}
	if len(mgr.Registry().AllPlugins()) != 0 || len(mgr.Registry().AllKinds()) != 0 {
		t.Fatalf("registry after a failed reload: %v, want empty", mgr.Registry().AllPlugins())
	}
	if mgr.Changed() {
		t.Fatal("a broken binary looks changed again right after the reload")
	}

	// A plugin that fails after another one loaded must not leave the
	// first one behind. Names load in order, so zbroken comes second.
	installBinary(t, reloadPluginBin, bin)
	partial := toml + fmt.Sprintf("[plugins.zbroken]\npath = %q\n", broken)
	if err := os.WriteFile(filepath.Join(root, "bino.toml"), []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Reload(ctx); err == nil {
		t.Fatal("Reload with a broken second plugin succeeded")
	}
	if len(mgr.Registry().AllPlugins()) != 0 {
		t.Fatalf("registry after a partial reload: %v, want empty", mgr.Registry().AllPlugins())
	}

	// The next good bino.toml recovers.
	if err := os.WriteFile(filepath.Join(root, "bino.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mgr.Changed() {
		t.Fatal("Changed is false after the fix")
	}
	if err := mgr.Reload(ctx); err != nil {
		t.Fatalf("Reload after the fix: %v", err)
	}
	if v := pluginVersion(t, mgr); v != "2.0.0" {
		t.Fatalf("version after recovery = %q, want 2.0.0", v)
	}
}
