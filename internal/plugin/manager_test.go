package plugin

import (
	"context"
	"testing"

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

func TestManager_Registry_NotNil(t *testing.T) {
	mgr := NewManager(logx.Nop())
	if mgr.Registry() == nil {
		t.Fatal("expected non-nil registry")
	}
}
