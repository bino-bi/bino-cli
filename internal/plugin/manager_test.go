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
