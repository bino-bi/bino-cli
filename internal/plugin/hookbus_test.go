package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"bino.bi/bino/internal/logx"
)

func TestHookBus_NoPlugins(t *testing.T) {
	reg := NewRegistry()
	bus := NewHookBus(reg, logx.Nop())

	payload := &HookPayload{HTML: []byte("<html>")}
	result, diags, err := bus.Dispatch(context.Background(), "post-render-html", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatal("expected no diagnostics")
	}
	if string(result.HTML) != "<html>" {
		t.Fatal("payload should pass through unchanged")
	}
}

func TestHookBus_SinglePlugin_NoModification(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockPlugin{manifest: PluginManifest{
		Name:  "alpha",
		Hooks: []string{"post-load"},
	}})
	bus := NewHookBus(reg, logx.Nop())

	payload := &HookPayload{Documents: []DocumentPayload{{Name: "doc1"}}}
	result, _, err := bus.Dispatch(context.Background(), "post-load", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 || result.Documents[0].Name != "doc1" {
		t.Fatal("payload should pass through unchanged when modified=false")
	}
}

func TestHookBus_OnlyInterestedPlugins(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newMock("alpha", withHooks("post-load")))
	reg.Register(newMock("beta", withHooks("post-render-html")))

	bus := NewHookBus(reg, logx.Nop())

	payload := &HookPayload{Documents: []DocumentPayload{{Name: "doc1"}}}
	_, diags, err := bus.Dispatch(context.Background(), "post-load", payload)
	if err != nil {
		t.Fatal(err)
	}
	// Only alpha should be called. Since mockPlugin returns nil, no error.
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
}

type hookMockPlugin struct {
	mockPlugin
	onHookFn func(ctx context.Context, checkpoint string, payload *HookPayload) (*HookResult, error)
}

func (h *hookMockPlugin) OnHook(ctx context.Context, checkpoint string, payload *HookPayload) (*HookResult, error) {
	if h.onHookFn != nil {
		return h.onHookFn(ctx, checkpoint, payload)
	}
	return nil, nil
}

func TestHookBus_Modification(t *testing.T) {
	reg := NewRegistry()
	p := &hookMockPlugin{
		mockPlugin: mockPlugin{manifest: PluginManifest{Name: "modifier", Hooks: []string{"post-render-html"}}},
		onHookFn: func(ctx context.Context, checkpoint string, payload *HookPayload) (*HookResult, error) {
			return &HookResult{
				Modified: true,
				Payload:  &HookPayload{HTML: []byte("<modified>")},
			}, nil
		},
	}
	reg.Register(p)
	bus := NewHookBus(reg, logx.Nop())

	result, _, err := bus.Dispatch(context.Background(), "post-render-html", &HookPayload{HTML: []byte("<original>")})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.HTML) != "<modified>" {
		t.Fatalf("expected modified HTML, got %q", string(result.HTML))
	}
}

func TestHookBus_Error_LoggedNonFatal(t *testing.T) {
	reg := NewRegistry()
	p := &hookMockPlugin{
		mockPlugin: mockPlugin{manifest: PluginManifest{Name: "failing", Hooks: []string{"post-load"}}},
		onHookFn: func(ctx context.Context, checkpoint string, payload *HookPayload) (*HookResult, error) {
			return nil, errors.New("plugin crash")
		},
	}
	reg.Register(p)
	bus := NewHookBus(reg, logx.Nop())

	result, diags, err := bus.Dispatch(context.Background(), "post-load", &HookPayload{Documents: []DocumentPayload{{Name: "doc"}}})
	if err != nil {
		t.Fatalf("hook errors should not fail dispatch, got: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if result.Documents[0].Name != "doc" {
		t.Fatal("payload should pass through on error")
	}
}

func TestHookBus_PerPluginHookTimeout(t *testing.T) {
	deadlines := make(map[string]time.Duration)
	newHookPlugin := func(name string, hookTimeout time.Duration) *hookMockPlugin {
		return &hookMockPlugin{
			mockPlugin: mockPlugin{manifest: PluginManifest{
				Name:        name,
				Hooks:       []string{"post-load"},
				HookTimeout: hookTimeout,
			}},
			onHookFn: func(ctx context.Context, checkpoint string, payload *HookPayload) (*HookResult, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Errorf("plugin %s: hook context has no deadline", name)
					return &HookResult{}, nil
				}
				deadlines[name] = time.Until(deadline)
				return &HookResult{}, nil
			},
		}
	}

	reg := NewRegistry()
	reg.Register(newHookPlugin("pinned", 2*time.Minute))
	reg.Register(newHookPlugin("fallback", 0))
	bus := NewHookBus(reg, logx.Nop())

	if _, _, err := bus.Dispatch(context.Background(), "post-load", &HookPayload{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	// The bino.toml hook_timeout declaration must drive the hook deadline;
	// without it the 30s bus default applies.
	if got := deadlines["pinned"]; got <= 30*time.Second || got > 2*time.Minute {
		t.Errorf("pinned plugin deadline %v, want ~2m", got)
	}
	if got := deadlines["fallback"]; got <= 0 || got > 30*time.Second {
		t.Errorf("fallback plugin deadline %v, want ~30s", got)
	}
}
