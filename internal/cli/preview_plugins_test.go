package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/render"
)

func TestPreviewPlugins_NilDoesNothing(t *testing.T) {
	var p *previewPlugins
	if reloaded, err := p.reloadIfChanged(context.Background()); reloaded || err != nil {
		t.Fatalf("nil reloadIfChanged = %v, %v", reloaded, err)
	}
	p.poll(context.Background(), func() { t.Error("nil poll called back") })
}

func TestPreviewPlugins_ReloadErrorState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the plugin binary")
	}
	ctx := context.Background()
	root := t.TempDir()
	tomlPath := filepath.Join(root, "bino.toml")
	if err := os.WriteFile(tomlPath, []byte(missingPluginToml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := pathutil.LoadProjectConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	mgr := plugin.NewManager(logx.Nop())
	if err := mgr.LoadAll(ctx, cfg, root, "1.0.0"); err == nil {
		t.Fatal("the missing plugin loaded")
	}
	server, err := httpserver.New(httpserver.Config{})
	if err != nil {
		t.Fatal(err)
	}
	opts := &render.PluginOptions{ExtraHeadMarkup: "<script src=\"/plugins/old/a.js\"></script>", HasComponentKinds: true}
	p := &previewPlugins{mgr: mgr, opts: opts, server: server, logger: logx.Nop()}

	if reloaded, err := p.reloadIfChanged(ctx); reloaded || err != nil {
		t.Fatalf("without a change: %v, %v", reloaded, err)
	}

	// The binary shows up, but it is broken.
	bin := filepath.Join(root, ".bino", "plugins", "bino-plugin-nonexistent-xyzzy")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	polled := make(chan struct{}, 1)
	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go p.poll(pollCtx, func() {
		select {
		case polled <- struct{}{}:
		default:
		}
	})
	select {
	case <-polled:
	case <-time.After(5 * time.Second):
		t.Fatal("poll did not report the new binary")
	}
	cancel()

	_, err = p.reloadIfChanged(ctx)
	if err == nil || !strings.Contains(err.Error(), "plugin declared in bino.toml did not load") {
		t.Fatalf("reload error = %v", err)
	}
	if opts.ExtraHeadMarkup != "" || opts.HasComponentKinds {
		t.Errorf("plugin options not refreshed after the failed reload: %+v", opts)
	}
	if _, again := p.reloadIfChanged(ctx); again == nil || again.Error() != err.Error() {
		t.Fatalf("stored error = %v, want %v", again, err)
	}

	// Removing the declaration fixes it without a restart.
	if err := os.WriteFile(tomlPath, []byte("report-id = \"t\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if full, err := p.reloadIfChanged(ctx); !full || err != nil {
		t.Fatalf("after the fix: %v, %v", full, err)
	}
	// A refresh that failed after the reload leaves the pages stale.
	if full, err := p.reloadIfChanged(ctx); !full || err != nil {
		t.Fatalf("before a good refresh: %v, %v", full, err)
	}
	p.refreshed()
	if full, err := p.reloadIfChanged(ctx); full || err != nil {
		t.Fatalf("after recovery: %v, %v", full, err)
	}
}
