package plugin

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/render"
)

type renderMockPlugin struct {
	*mockPlugin
	res *RenderResult
	err error
	got render.PluginRenderRequest
}

func (p *renderMockPlugin) RenderComponent(_ context.Context, req render.PluginRenderRequest) (*RenderResult, error) {
	p.got = req
	return p.res, p.err
}

func newRenderRegistry(p *renderMockPlugin) ComponentRenderer {
	reg := NewRegistry()
	reg.Register(p)
	return NewComponentRenderer(reg)
}

func TestComponentRenderer_Diagnostics(t *testing.T) {
	p := &renderMockPlugin{
		mockPlugin: newMock("gauges", withKinds(KindRegistration{KindName: "Gauge", Category: KindCategoryComponent})),
		res: &RenderResult{
			HTML: "<bn-gauge></bn-gauge>",
			Diagnostics: []Diagnostic{
				{Message: "no data", Severity: SeverityWarning},
				{Source: "gauges-core", Message: "bad axis", Severity: SeverityError},
				{Message: "rendered 3 points", Severity: SeverityInfo},
			},
		},
	}
	buf := &bytes.Buffer{}
	ctx := logx.WithLogger(context.Background(), logx.NewTerminalWithColor(buf, buf, true, true))
	req := render.PluginRenderRequest{Kind: "Gauge", Name: "rev", Spec: []byte(`{}`), RenderMode: "build"}

	html, warnings, err := newRenderRegistry(p).RenderComponent(ctx, req)
	if err != nil {
		t.Fatalf("RenderComponent: %v", err)
	}
	if html != "<bn-gauge></bn-gauge>" {
		t.Errorf("html = %q", html)
	}
	if p.got.Kind != req.Kind || p.got.Name != req.Name || p.got.RenderMode != req.RenderMode {
		t.Errorf("plugin got request %+v, want %+v", p.got, req)
	}
	want := []string{
		`plugin gauges: Gauge "rev": no data`,
		`plugin gauges-core: Gauge "rev": error: bad axis`,
	}
	if !slices.Equal(warnings, want) {
		t.Errorf("warnings = %q, want %q", warnings, want)
	}
	if !strings.Contains(debugLines(buf.String()), `plugin gauges: Gauge "rev": rendered 3 points`) {
		t.Errorf("info diagnostic not logged at debug level; logged: %q", buf.String())
	}
}

func TestComponentRenderer_PluginError(t *testing.T) {
	boom := errors.New("boom")
	p := &renderMockPlugin{
		mockPlugin: newMock("gauges", withKinds(KindRegistration{KindName: "Gauge", Category: KindCategoryComponent})),
		err:        boom,
	}
	_, _, err := newRenderRegistry(p).RenderComponent(context.Background(), render.PluginRenderRequest{Kind: "Gauge"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// debugLines keeps the lines the terminal logger wrote at debug level.
func debugLines(logged string) string {
	var out []string
	for _, l := range strings.Split(logged, "\n") {
		if strings.Contains(l, "DBG") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
