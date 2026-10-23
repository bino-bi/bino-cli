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
			HTML: "<gauges-gauge></gauges-gauge>",
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
	if html != "<gauges-gauge></gauges-gauge>" {
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

func TestComponentRenderer_FragmentWarnings(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{"plugin tag", `<highcharts-chart data='[]'></highcharts-chart>`, nil},
		{"bn root", `<bn-foo></bn-foo>`, []string{"plugin gauges: Gauge: root element <bn-foo> uses the reserved bn- prefix"}},
		{"bino root", "  \n<BINO-x></BINO-x>", []string{"plugin gauges: Gauge: root element <bino-x> uses the reserved bino- prefix"}},
		{"inline script", `<gauges-gauge><script>alert(1)</script></gauges-gauge>`, []string{"plugin gauges: Gauge: the HTML contains an inline <script>"}},
		{"module script", `<gauges-gauge><script type="module">x()</script></gauges-gauge>`, []string{"inline <script>"}},
		{"json script", `<gauges-gauge><script type="application/json">{"a":1}</script></gauges-gauge>`, nil},
		{"json script single quotes", `<gauges-gauge><script type='Application/JSON'>{}</script></gauges-gauge>`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &renderMockPlugin{
				mockPlugin: newMock("gauges", withKinds(KindRegistration{KindName: "Gauge", Category: KindCategoryComponent})),
				res:        &RenderResult{HTML: tt.html},
			}
			r := newRenderRegistry(p)
			req := render.PluginRenderRequest{Kind: "Gauge", Name: "rev"}
			html, warnings, err := r.RenderComponent(context.Background(), req)
			if err != nil {
				t.Fatalf("RenderComponent: %v", err)
			}
			if html != tt.html {
				t.Errorf("html changed: %q", html)
			}
			if len(warnings) != len(tt.want) {
				t.Fatalf("warnings = %q, want %d", warnings, len(tt.want))
			}
			for i, w := range tt.want {
				if !strings.Contains(warnings[i], w) {
					t.Errorf("warning %d = %q, want it to contain %q", i, warnings[i], w)
				}
			}
			if _, again, _ := r.RenderComponent(context.Background(), req); len(again) != 0 {
				t.Errorf("second render of the same kind warned again: %q", again)
			}
		})
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
