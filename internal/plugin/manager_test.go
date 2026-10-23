package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
)

func TestManager_LoadAll_NoPlugins(t *testing.T) {
	mgr := NewManager(logx.Nop())
	cfg := &pathutil.ProjectConfig{}

	if err := mgr.LoadAll(context.Background(), cfg, "/tmp", "1.0.0"); err != nil {
		t.Fatalf("expected no error for empty plugins, got: %v", err)
	}

	if len(mgr.Registry().AllPlugins()) != 0 {
		t.Fatal("expected empty registry")
	}
}

func TestManager_LoadAll_BinaryNotFound(t *testing.T) {
	mgr := NewManager(logx.Nop())
	cfg := &pathutil.ProjectConfig{
		Plugins: map[string]pathutil.PluginDeclaration{
			"nonexistent-xyzzy": {},
		},
	}

	err := mgr.LoadAll(context.Background(), cfg, "/tmp/no-such-project", "1.0.0")
	if err == nil {
		t.Fatal("expected error when plugin binary not found")
	}
}

func TestManager_ShutdownAll_Empty(t *testing.T) {
	mgr := NewManager(logx.Nop())
	// Should not panic.
	mgr.ShutdownAll(context.Background())
}

func TestManager_ShutdownAll_RemovesDatasetFiles(t *testing.T) {
	mgr := NewManager(logx.Nop())
	h := mgr.HostService()
	h.SetDuckDBOpener(testDuckDBOpener(t))
	h.SetDatasets([]DatasetPayload{{Name: "sales", JSONRows: []byte(`[{"ac":1}]`)}})
	t.Cleanup(h.Close)

	queryRows(t, h, "SELECT * FROM sales")
	h.mu.RLock()
	dir := h.viewDir
	h.mu.RUnlock()

	mgr.ShutdownAll(context.Background())
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("ShutdownAll kept %s (stat err %v)", dir, err)
	}
	// A reload reuses the host service, so it must still answer queries.
	if rows, _ := queryRows(t, h, "SELECT ac FROM sales"); len(rows) != 1 {
		t.Fatalf("after ShutdownAll: rows = %+v, want 1 row", rows)
	}
}

func TestManager_Registry_NotNil(t *testing.T) {
	mgr := NewManager(logx.Nop())
	if mgr.Registry() == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestCheckVersionPin(t *testing.T) {
	tests := []struct {
		name     string
		pin      string
		reported string
		wantErr  bool
	}{
		{name: "exact match", pin: "1.2.3", reported: "1.2.3", wantErr: false},
		{name: "exact match with v prefix", pin: "1.2.3", reported: "v1.2.3", wantErr: false},
		{name: "exact mismatch", pin: "1.2.3", reported: "1.2.4", wantErr: true},
		{name: "range satisfied", pin: ">=1.0 <2.0", reported: "1.5.0", wantErr: false},
		{name: "range violated", pin: ">=1.0 <2.0", reported: "2.0.0", wantErr: true},
		{name: "caret satisfied", pin: "^1.2", reported: "1.9.0", wantErr: false},
		{name: "caret violated", pin: "^1.2", reported: "2.0.0", wantErr: true},
		{name: "invalid pin", pin: "not-a-version", reported: "1.0.0", wantErr: true},
		{name: "unparseable reported version", pin: "1.2.3", reported: "garbage", wantErr: true},
		{name: "empty reported version", pin: "1.2.3", reported: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkVersionPin(tt.pin, tt.reported)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkVersionPin(%q, %q) error = %v, wantErr %v", tt.pin, tt.reported, err, tt.wantErr)
			}
		})
	}
}

type warnRecorder struct {
	logx.Logger
	warns []string
}

func (w *warnRecorder) Warnf(format string, args ...any) {
	w.warns = append(w.warns, fmt.Sprintf(format, args...))
}

func TestWarnUndispatchedHooks(t *testing.T) {
	rec := &warnRecorder{Logger: logx.Nop()}
	warnUndispatchedHooks(rec, PluginManifest{Name: "gauges", Hooks: []string{"post-load", "post-render-html"}})
	if len(rec.warns) != 1 {
		t.Fatalf("want one warning, got %q", rec.warns)
	}
	if !strings.Contains(rec.warns[0], `"gauges"`) || !strings.Contains(rec.warns[0], "post-load") {
		t.Errorf("warning must name plugin and hook, got %q", rec.warns[0])
	}

	rec.warns = nil
	warnUndispatchedHooks(rec, PluginManifest{Name: "gauges", Hooks: []string{"post-dataset-execute", "post-render-html"}})
	if len(rec.warns) != 0 {
		t.Errorf("want no warning for dispatched hooks, got %q", rec.warns)
	}
}

func TestManager_Changed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the plugin binary")
	}
	root := t.TempDir()
	tomlPath := filepath.Join(root, "bino.toml")
	if err := os.WriteFile(tomlPath, []byte("[plugins.fake]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The script exits at once, so the load fails, but the files are stamped
	// before launch.
	bin := createExecutable(t, filepath.Join(root, ".bino", "plugins"), "bino-plugin-fake")
	cfg := &pathutil.ProjectConfig{Plugins: map[string]pathutil.PluginDeclaration{"fake": {}}}

	tests := []struct {
		name   string
		change func(t *testing.T)
	}{
		{"binary touched", func(t *testing.T) { touch(t, bin) }},
		{"binary rewritten", func(t *testing.T) {
			if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"binary removed", func(t *testing.T) {
			if err := os.Remove(bin); err != nil {
				t.Fatal(err)
			}
		}},
		{"bino.toml touched", func(t *testing.T) { touch(t, tomlPath) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createExecutable(t, filepath.Dir(bin), filepath.Base(bin))
			mgr := NewManager(logx.Nop())
			if mgr.Changed() {
				t.Fatal("Changed before any load")
			}
			if err := mgr.LoadAll(context.Background(), cfg, root, "1.0.0"); err == nil {
				t.Fatal("the fake plugin loaded")
			}
			if mgr.Changed() {
				t.Fatal("Changed right after load")
			}
			tt.change(t)
			if !mgr.Changed() {
				t.Fatal("Changed is false after the change")
			}
		})
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}
