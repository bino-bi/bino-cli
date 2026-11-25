//go:build integration

package refresh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"bino.bi/bino/internal/chrome"
	"bino.bi/bino/internal/web"
)

// The annotate mode runs inside the preview frame and posts to its parent, so
// each test page is an outer page that records the messages of an iframe.
const annotateOuter = `<!doctype html><html><body style="margin:0">
<iframe id="f" src="/frame" style="width:1200px;height:900px;border:0"></iframe>
<script>window.msgs=[];addEventListener('message',e=>window.msgs.push(e.data));
window.annotate=on=>document.getElementById('f').contentWindow.postMessage({type:'bino:annotateMode',on:on},'*');</script>
</body></html>`

// synthetic components with the attributes the engine and the renderer write
const syntheticFrame = `<!doctype html><html><head>%ANNOTATE%</head><body style="margin:0">
<div data-bino-kind="ChartTime" data-bino-ref="sales" data-bino-source-file="/p/charts.yaml" data-bino-source-line="7">
  <bn-chart-time id="chart" style="display:block;width:300px;height:100px"></bn-chart-time>
</div>
<div data-bino-kind="Table" data-bino-name="detail">
  <table><tr data-bn-row-group="Europe" data-bn-category="Software" data-bn-part="element">
    <td id="cell" data-bn-row-group="Europe" data-bn-category="Software" data-bn-scenario="ac1" data-bn-part="element">12</td>
    <td id="empty"></td></tr></table>
</div>
<div data-bino-kind="SmallMultiple" data-bino-name="by_city">
  <div data-bino-panel="Berlin"><div id="panel" data-bn-date="2024-01-01" data-bn-scenario="pl1" data-bn-part="label">panel</div></div>
</div>
<div data-bino-kind="Grid" data-bino-name="grid"><bn-grid>
  <div data-bino-target-kind="Text" data-bino-target-name="note"><span id="note">note</span></div>
  <div><div data-bino-kind="Table" data-bino-name="in_card"><span id="incard" data-bn-category="x" data-bn-part="element">card</span></div></div>
</bn-grid></div>
<button id="chrome">toolbar</button>
<script>
// a chart with one keyed bar in its shadow root and a line on top that has no key
customElements.define('bn-chart-time', class extends HTMLElement {
  connectedCallback() {
    const root = this.attachShadow({mode:'open'});
    root.innerHTML = '<svg width="300" height="100">' +
      '<rect id="bar" x="10" y="10" width="40" height="80" data-bn-date="2024-01-01" data-bn-scenario="ac1" data-bn-part="element"></rect>' +
      '<rect id="plain" x="100" y="10" width="40" height="80"></rect>' +
      '<path id="line" d="M0 50 L300 50" stroke="black" stroke-width="20"></path></svg>';
  }
  async getLayoutState() {
    return { elements: [
      { point: { date: '2024-01-01', scenario: 'ac1', part: 'element' }, rect: { component: { x: 10, y: 10, width: 40, height: 80 } } },
      { point: { date: '2025-01-01', scenario: 'ac1', part: 'element' }, rect: { component: { x: 100, y: 10, width: 40, height: 80 } } },
    ] };
  }
});
</script></body></html>`

// annotateSetup serves the outer page and the frame, and returns a browser tab on the outer page.
func annotateSetup(t *testing.T, frame string, extra map[string]http.HandlerFunc) (context.Context, func()) {
	t.Helper()
	mgr, err := chrome.NewManager()
	if err != nil {
		t.Fatalf("chrome manager: %v", err)
	}
	chromePath, err := mgr.ResolveExecPath()
	if err != nil {
		t.Fatalf("chrome-headless-shell not installed: %v", err)
	}
	script, err := web.AnnotateScript()
	if err != nil || len(script) == 0 {
		t.Fatalf("annotate script: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, annotateOuter)
	})
	mux.HandleFunc("/frame", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		page := withRevealSource([]byte(strings.Replace(frame, "%ANNOTATE%", "", 1)))
		_, _ = w.Write(page)
	})
	for path, h := range extra {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)

	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chromePath), chromedp.Flag("no-sandbox", true), chromedp.WindowSize(1300, 1000))
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	ctx, timeout := context.WithTimeout(ctx, 2*time.Minute)
	if err := chromedp.Do(ctx, chromedp.Navigate(srv.URL)); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	return ctx, func() { timeout(); cancel(); allocCancel(); srv.Close() }
}

// messages returns the messages the frame posted.
func messages(t *testing.T, ctx context.Context) []map[string]any {
	t.Helper()
	raw, err := chromedp.Run(ctx, chromedp.Evaluate[[]byte](`JSON.stringify(window.msgs)`))
	if err != nil {
		t.Fatalf("read messages: %v", err)
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	var out []map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("parse messages %s: %v", s, err)
	}
	return out
}

// clickIn clicks the centre of the element the expression finds in the frame
// (plus dx, dy) and returns the pick it posts.
func clickIn(t *testing.T, ctx context.Context, find string, dx, dy float64, meta bool) map[string]any {
	t.Helper()
	before := len(messages(t, ctx))
	// find returns an element, or a promise of a point {x, y} in the frame
	expr := `(async () => { const d = document.getElementById('f').contentDocument; const el = await (` + find + `)(d);
		const f = document.getElementById('f').getBoundingClientRect();
		if (!el.getBoundingClientRect) return [f.left + el.x, f.top + el.y];
		const r = el.getBoundingClientRect();
		return [f.left + r.left + r.width / 2, f.top + r.top + r.height / 2]; })()`
	xy, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](expr, chromedp.EvalAwaitPromise))
	if err != nil {
		t.Fatalf("find %s: %v", find, err)
	}
	opts := []chromedp.MouseOption{}
	if meta {
		opts = append(opts, chromedp.ButtonModifiers(2)) // Ctrl
	}
	if err := chromedp.Do(ctx, chromedp.MouseClickXY(xy[0]+dx, xy[1]+dy, opts...)); err != nil {
		t.Fatalf("click: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msgs := messages(t, ctx)
		for _, m := range msgs[before:] {
			if m["type"] == "bino:pickPoint" || m["type"] == "bino:revealSource" {
				return m
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func TestIntegration_AnnotateModeSynthetic(t *testing.T) {
	ctx, done := annotateSetup(t, syntheticFrame, nil)
	defer done()

	// the frame says it can annotate as soon as it loads
	waitFor(t, func() bool {
		for _, m := range messages(t, ctx) {
			if m["type"] == "bino:capabilities" && m["annotate"] == float64(1) {
				return true
			}
		}
		return false
	})

	// off: a plain click posts nothing
	if pick := clickIn(t, ctx, `d => d.getElementById('cell')`, 0, 0, false); pick != nil {
		t.Fatalf("annotate mode off, got %v", pick)
	}
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`annotate(true)`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	bar := `d => d.getElementById('chart').shadowRoot.getElementById('bar')`
	tests := []struct {
		name string
		find string
		dy   float64
		want string
	}{
		{"bar in a shadow root, with the source location of the slot", bar, -30,
			`{"kind":"ChartTime","line":7,"name":"sales","point":{"date":"2024-01-01","scenario":"ac1"},"sourceFile":"/p/charts.yaml","type":"bino:pickPoint"}`},
		{"line over a bar falls back to the layout state", bar, 0,
			`{"kind":"ChartTime","line":7,"name":"sales","point":{"date":"2024-01-01","scenario":"ac1"},"sourceFile":"/p/charts.yaml","type":"bino:pickPoint"}`},
		{"line over an unkeyed shape: the layout state knows it", `d => d.getElementById('chart').shadowRoot.getElementById('plain')`, 0,
			`{"kind":"ChartTime","line":7,"name":"sales","point":{"date":"2025-01-01","scenario":"ac1"},"sourceFile":"/p/charts.yaml","type":"bino:pickPoint"}`},
		{"table cell", `d => d.getElementById('cell')`, 0,
			`{"kind":"Table","name":"detail","point":{"category":"Software","rowGroup":"Europe","scenario":"ac1"},"type":"bino:pickPoint"}`},
		{"empty cell does not take the key of its row", `d => d.getElementById('empty')`, 0,
			`{"kind":"Table","name":"detail","type":"bino:pickPoint"}`},
		{"panel of a SmallMultiple, with part", `d => d.getElementById('panel')`, 0,
			`{"kind":"SmallMultiple","name":"by_city","panel":"Berlin","point":{"date":"2024-01-01","part":"label","scenario":"pl1"},"type":"bino:pickPoint"}`},
		{"Grid child names its own target", `d => d.getElementById('note')`, 0,
			`{"kind":"Text","name":"note","type":"bino:pickPoint"}`},
		{"a card in a Grid cell is no target: the Grid is", `d => d.getElementById('incard')`, 0,
			`{"kind":"Grid","name":"grid","type":"bino:pickPoint"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pick := clickIn(t, ctx, tt.find, 0, tt.dy, false)
			got, _ := json.Marshal(pick)
			if string(got) != tt.want {
				t.Fatalf("pick = %s\nwant   %s", got, tt.want)
			}
		})
	}

	// a click outside every component keeps its meaning and picks nothing
	if pick := clickIn(t, ctx, `d => { d.getElementById('chrome').onclick = () => { d.defaultView.toolbarClicked = true; }; return d.getElementById('chrome'); }`, 0, 0, false); pick != nil {
		t.Fatalf("click on the toolbar posted %v", pick)
	}
	clicked, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.getElementById('f').contentWindow.toolbarClicked === true`))
	if err != nil || !clicked {
		t.Fatalf("the toolbar button did not get its click (err %v)", err)
	}

	// Ctrl+click keeps revealing the source, also in annotate mode, and picks nothing
	before := len(messages(t, ctx))
	if pick := clickIn(t, ctx, `d => d.getElementById('cell')`, 0, 0, true); pick == nil || pick["type"] != "bino:revealSource" {
		t.Fatalf("Ctrl+click = %v, want bino:revealSource", pick)
	}
	time.Sleep(300 * time.Millisecond)
	for _, m := range messages(t, ctx)[before:] {
		if m["type"] == "bino:pickPoint" {
			t.Fatalf("Ctrl+click also picked %v", m)
		}
	}
}

func TestIntegration_AnnotateModeEngine(t *testing.T) {
	pin := enginePin(t)
	engineDir := filepath.Join(os.Getenv("HOME"), ".bino", "cdn", "bn-template-engine", pin)
	if _, err := os.Stat(filepath.Join(engineDir, "bn-template-engine.esm.js")); err != nil {
		t.Skipf("engine %s is not in the local CDN cache %s: run a bino preview once", pin, engineDir)
	}
	row := `{"operation":"+","setname":"de","subCategory":"","subCategoryIndex":1,"rowGroup":"Europe","rowGroupIndex":1,`
	rows := `[` + row + `"date":"2023-01-01","category":"Software","categoryIndex":1,"ac1":40,"ac2":12,"pl1":35},` +
		row + `"date":"2024-01-01","category":"Hardware","categoryIndex":2,"ac1":55,"ac2":20,"pl1":60}]`
	frame := `<!doctype html><html><head><script type="module" src="/cdn/bn-template-engine.esm.js"></script>%ANNOTATE%</head><body style="margin:0">
<bn-context locale="en"><bn-datasource name="sales">` + rows + `</bn-datasource>
<div data-bino-kind="ChartTime" data-bino-ref="trend" style="position:relative;overflow:hidden;width:500px;height:300px">
  <bn-chart-time datasets="$sales" date-interval="year" scenarios="ac1,pl1" unit-scaling="auto"></bn-chart-time></div>
<div data-bino-kind="Table" data-bino-ref="detail" style="position:relative;overflow:hidden;width:500px">
  <bn-table datasets="$sales" type="sum" grouped="true" level="category" scenarios="ac1,pl1" unit-scaling="auto"></bn-table></div>
<div data-bino-kind="ChartScatter" data-bino-ref="spread" style="position:relative;overflow:hidden;width:500px;height:300px">
  <bn-chart-scatter datasets="$sales" point-level="category" x='{"measure":"ac1"}' y='{"measure":"ac2"}'></bn-chart-scatter></div>
</bn-context></body></html>`
	ctx, done := annotateSetup(t, frame, map[string]http.HandlerFunc{
		"/cdn/": func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(engineDir, strings.TrimPrefix(r.URL.Path, "/cdn/")))
		},
	})
	defer done()
	waitFor(t, func() bool {
		ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => { const w = document.getElementById('f').contentWindow;
			return w.componentRegisterIsRenderedResult === true; })()`))
		return err == nil && ok
	})
	time.Sleep(500 * time.Millisecond)
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`annotate(true)`)); err != nil {
		t.Fatal(err)
	}

	deep := `(selector) => (d) => { const all = []; const walk = (root) => { root.querySelectorAll(selector).forEach(e => all.push(e));
		root.querySelectorAll('*').forEach(e => e.shadowRoot && walk(e.shadowRoot)); }; walk(d); return all[0]; }`
	tests := []struct {
		name, selector, kind, want string
	}{
		{"ChartTime bar", `[data-bn-date="2024-01-01"][data-bn-scenario="ac1"][data-bn-part="element"]`, "ChartTime", `{"date":"2024-01-01","scenario":"ac1"}`},
		{"Table cell", `[data-bn-category="Hardware"][data-bn-scenario="pl1"][data-bn-part="element"]`, "Table", ``},
		// the dots of a scatter chart carry no attributes: the click is read from its layout state
		{"scatter dot", "", "ChartScatter", `{"category":"Software","rowGroup":"Europe"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := map[string]string{"ChartTime": "bn-chart-time", "Table": "bn-table", "ChartScatter": "bn-chart-scatter"}[tt.kind]
			find := `(d => (` + deep + `)('` + tt.selector + `')(d.querySelector('` + host + `').shadowRoot || d))`
			if tt.selector == "" {
				find = `(async d => { const host = d.querySelector('` + host + `'); const s = await host.getLayoutState({ detail: 'full' });
					const dot = s.elements.find(e => e.point && e.point.category === 'Software'); const r = host.getBoundingClientRect();
					return { x: r.left + dot.rect.component.x + dot.rect.component.width / 2, y: r.top + dot.rect.component.y + dot.rect.component.height / 2 }; })`
			}
			pick := clickIn(t, ctx, find, 0, 0, false)
			if pick == nil || pick["kind"] != tt.kind {
				t.Fatalf("pick = %v, want kind %s", pick, tt.kind)
			}
			point, _ := json.Marshal(pick["point"])
			if tt.want != "" && string(point) != tt.want {
				t.Fatalf("point = %s, want %s", point, tt.want)
			}
			if pick["point"] == nil {
				t.Fatalf("no point in %v", pick)
			}
			t.Logf("%s -> %s", tt.name, point)
		})
	}
}

var enginePinRe = regexp.MustCompile(`(?m)^ARG ENGINE_VERSION=(\S+)`)

func enginePin(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	m := enginePinRe.FindSubmatch(b)
	if m == nil {
		t.Fatal("no engine pin in Dockerfile")
	}
	return string(m[1])
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// a layer that names its marks like engine v1.0.0-next.88: an interactive
// prop and data-bino-annotation on each mark
const markFrame = `<!doctype html><html><head>%ANNOTATE%</head><body style="margin:0">
<bn-layout-page data-bino-page="overview">
  <div data-bino-kind="Table" data-bino-name="detail"><div style="width:400px;height:200px">table</div></div>
  <bn-annotation-layer slot="annotations" items='[{"name":"peak","label":"1","mark":"marker","target":"t1","sourceFile":"/p/notes.yaml","line":12},{"name":"lost","label":"2","mark":"frame","target":"t1"}]'></bn-annotation-layer>
</bn-layout-page>
<script>
customElements.define('bn-annotation-layer', class extends HTMLElement {
  get interactive() { return this.hasAttribute('interactive'); }
  connectedCallback() {
    const root = this.attachShadow({mode:'open'});
    root.innerHTML = '<style>:host{position:absolute;left:0;top:0;width:400px;height:200px;pointer-events:none}' +
      ':host([interactive]) .mark{pointer-events:auto}</style>' +
      '<svg width="400" height="200"><g class="mark marker" data-bino-annotation="peak"><circle id="peak" cx="50" cy="50" r="10"></circle></g>' +
      // an arrow drawn later, whose invisible hit line lies over the marker
      '<g class="mark arrow" data-bino-annotation="drop"><line class="hit" x1="0" y1="55" x2="200" y2="55" style="stroke:transparent;stroke-width:12;pointer-events:stroke"></line>' +
      '<line x1="0" y1="55" x2="200" y2="55" style="stroke:black;stroke-width:1.5"></line></g></svg>';
  }
});
</script></body></html>`

func TestIntegration_AnnotateModeMarks(t *testing.T) {
	ctx, done := annotateSetup(t, markFrame, nil)
	defer done()
	waitFor(t, func() bool {
		for _, m := range messages(t, ctx) {
			if m["type"] == "bino:annotations" {
				return true
			}
		}
		return false
	})
	var report, caps map[string]any
	for _, m := range messages(t, ctx) {
		switch m["type"] {
		case "bino:annotations":
			report = m
		case "bino:capabilities":
			caps = m
		}
	}
	if caps["marks"] != float64(1) {
		t.Fatalf("capabilities = %v, want marks 1 for an engine with mark names", caps)
	}
	got, _ := json.Marshal(report["pages"])
	if want := `[{"items":[{"label":"1","name":"peak","placed":true},{"label":"2","name":"lost","placed":false}],"page":"overview"}]`; string(got) != want {
		t.Fatalf("report = %s\nwant     %s", got, want)
	}

	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`annotate(true)`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	on, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.getElementById('f').contentDocument.querySelector('bn-annotation-layer').hasAttribute('interactive')`))
	if err != nil || !on {
		t.Fatalf("the layer is not interactive in annotate mode (err %v)", err)
	}
	mark := `d => d.querySelector('bn-annotation-layer').shadowRoot.getElementById('peak')`
	pick := clickIn(t, ctx, mark, 0, 0, false)
	if pick == nil {
		pick = map[string]any{}
	}
	for _, m := range messages(t, ctx) {
		if m["type"] == "bino:revealAnnotation" {
			pick = m
		}
	}
	gotPick, _ := json.Marshal(pick)
	if want := `{"line":12,"name":"peak","source":false,"sourceFile":"/p/notes.yaml","type":"bino:revealAnnotation"}`; string(gotPick) != want {
		t.Fatalf("click on a mark = %s\nwant              %s", gotPick, want)
	}

	// Ctrl+click on a mark asks for its source, not for the component's
	before := len(messages(t, ctx))
	clickIn(t, ctx, mark, 0, 0, true)
	time.Sleep(300 * time.Millisecond)
	var types []string
	for _, m := range messages(t, ctx)[before:] {
		types = append(types, m["type"].(string)+fmt.Sprint(m["source"]))
	}
	if strings.Join(types, ",") != "bino:revealAnnotationtrue" {
		t.Fatalf("Ctrl+click on a mark posted %v", types)
	}

	// off: the layer lets clicks through again
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`annotate(false)`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	off, _ := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.getElementById('f').contentDocument.querySelector('bn-annotation-layer').hasAttribute('interactive')`))
	if off {
		t.Fatal("the layer stays interactive after annotate mode is off")
	}
}
