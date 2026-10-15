package markdown

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/yuin/goldmark"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
)

type fakePluginRenderer struct {
	html     string
	warnings []string
	err      error

	mu   sync.Mutex
	reqs []render.PluginRenderRequest
}

func (f *fakePluginRenderer) RenderComponent(_ context.Context, req render.PluginRenderRequest) (string, []string, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	return f.html, f.warnings, f.err
}

// warnLogger records Warnf lines.
type warnLogger struct {
	logx.Logger
	mu    sync.Mutex
	warns []string
}

func (l *warnLogger) Warnf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}

func pluginRefDocs() []config.Document {
	return []config.Document{
		{
			Kind: "Gauge",
			Name: "g1",
			Raw:  json.RawMessage(`{"kind":"Gauge","metadata":{"name":"g1"},"spec":{"max":10}}`),
		},
		{
			Kind: "Tree",
			Name: "t1",
			Raw:  json.RawMessage(`{"kind":"Tree","metadata":{"name":"t1"},"spec":{}}`),
		},
	}
}

func convertRef(t *testing.T, rc *RenderContext, src string) string {
	t.Helper()
	md := goldmark.New(goldmark.WithExtensions(NewRefExtensionWithContext(rc)))
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		t.Fatalf("convert: %v", err)
	}
	return buf.String()
}

func TestRenderRefPluginKind(t *testing.T) {
	t.Parallel()

	newRC := func(r render.PluginComponentRenderer, mode string) (*RenderContext, *warnLogger) {
		log := &warnLogger{Logger: logx.Nop()}
		rc := NewRenderContext(pluginRefDocs(), nil, nil, "v1.0.0")
		rc.Ctx = logx.WithLogger(context.Background(), log)
		rc.PluginRenderer = r
		rc.RenderMode = mode
		return rc, log
	}

	t.Run("renders plugin html in a ref container", func(t *testing.T) {
		t.Parallel()
		fake := &fakePluginRenderer{html: "<x-gauge></x-gauge>", warnings: []string{"plugin demo: Gauge \"g1\": low max"}}
		rc, log := newRC(fake, "preview")

		got := convertRef(t, rc, `:ref[Gauge:g1]{caption="Fill"}`)
		want := "<figure class='bn-figure'><div class='bn-ref-container' data-ref-kind='Gauge' data-ref-name='g1' data-bino-kind='Gauge' data-bino-name='g1'><x-gauge></x-gauge></div><figcaption>Fill</figcaption></figure>"
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
		if len(fake.reqs) != 1 {
			t.Fatalf("renderer calls = %d, want 1", len(fake.reqs))
		}
		req := fake.reqs[0]
		if req.Kind != "Gauge" || req.Name != "g1" || req.RenderMode != "preview" || string(req.Spec) != `{"max":10}` {
			t.Errorf("request = %+v (spec %s)", req, req.Spec)
		}
		if len(log.warns) != 1 || log.warns[0] != "plugin demo: Gauge \"g1\": low max" {
			t.Errorf("warnings = %q, want the plugin warning", log.warns)
		}
	})

	t.Run("empty render mode is sent as build", func(t *testing.T) {
		t.Parallel()
		fake := &fakePluginRenderer{html: "<x-gauge></x-gauge>"}
		rc, _ := newRC(fake, "")
		convertRef(t, rc, ":ref[Gauge:g1]")
		if len(fake.reqs) != 1 || fake.reqs[0].RenderMode != "build" {
			t.Errorf("requests = %+v, want one with RenderMode build", fake.reqs)
		}
	})

	t.Run("no renderer falls back to placeholder", func(t *testing.T) {
		t.Parallel()
		rc, log := newRC(nil, "build")
		got := convertRef(t, rc, ":ref[Gauge:g1]")
		if !strings.Contains(got, "<bn-ref kind='Gauge' name='g1'></bn-ref>") {
			t.Errorf("placeholder missing:\n%s", got)
		}
		if len(log.warns) != 0 {
			t.Errorf("warnings = %q, want none", log.warns)
		}
	})

	t.Run("renderer error logs and falls back", func(t *testing.T) {
		t.Parallel()
		fake := &fakePluginRenderer{err: errors.New("boom")}
		rc, log := newRC(fake, "build")
		got := convertRef(t, rc, ":ref[Gauge:g1]")
		if !strings.Contains(got, "<bn-ref kind='Gauge' name='g1'></bn-ref>") {
			t.Errorf("placeholder missing:\n%s", got)
		}
		if strings.Contains(got, "bn-ref-container") {
			t.Errorf("failed render must not emit a container:\n%s", got)
		}
		if len(log.warns) != 1 || !strings.Contains(log.warns[0], `Gauge "g1"`) || !strings.Contains(log.warns[0], "boom") {
			t.Errorf("warnings = %q, want one naming Gauge \"g1\" and the error", log.warns)
		}
	})

	t.Run("built-in Tree keeps the silent placeholder", func(t *testing.T) {
		t.Parallel()
		fake := &fakePluginRenderer{html: "<x-gauge></x-gauge>"}
		rc, log := newRC(fake, "build")
		got := convertRef(t, rc, ":ref[Tree:t1]")
		if !strings.Contains(got, "<bn-ref kind='Tree' name='t1'></bn-ref>") {
			t.Errorf("placeholder missing:\n%s", got)
		}
		if len(fake.reqs) != 0 {
			t.Errorf("renderer called for a built-in kind: %+v", fake.reqs)
		}
		if len(log.warns) != 0 {
			t.Errorf("warnings = %q, want none", log.warns)
		}
	})
}

func TestWrapDocumentWithContextPluginHead(t *testing.T) {
	t.Parallel()

	const head = "<script type='module' src='/plugins/demo/gauge.js'></script>"
	rc := NewRenderContext(nil, nil, nil, "v1.0.0")
	rc.PluginHead = head
	out, _ := WrapDocumentWithContext([]byte("<p>x</p>"), FullDocumentOptions{
		DocumentOptions: DocumentOptions{Title: "T", Format: "a4"},
		RenderContext:   rc,
	})
	got := string(out)
	engine := strings.Index(got, "<script nomodule src='/cdn/bn-template-engine/")
	plugin := strings.Index(got, head)
	headEnd := strings.Index(got, "</head>")
	if engine < 0 || plugin < 0 || plugin < engine || plugin > headEnd {
		t.Errorf("plugin head must follow the engine scripts inside <head>:\n%s", got[:headEnd])
	}
}
