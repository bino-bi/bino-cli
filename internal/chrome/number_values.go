package chrome

import (
	"context"
	"fmt"

	"github.com/chromedp/chromedp"

	"bino.bi/bino/internal/web"
)

// captureNumberValues asks every table and chart of the rendered page for its
// full layout detail and keeps what each shows: the cells, the value labels,
// the bars and the marks, each with its data-point key. Geometry is dropped
// in the page, so a large table stays small on the way back.
//
// Returns nil (no error) when the engine has no getLayoutState().
func captureNumberValues(ctx context.Context) ([]byte, error) {
	script, err := web.LayoutCaptureScript()
	if err != nil {
		return nil, fmt.Errorf("read layout capture script: %w", err)
	}
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](string(script))); err != nil {
		return nil, fmt.Errorf("evaluate layout capture: %w", err)
	}
	raw, err := chromedp.Run(ctx, chromedp.Evaluate[[]byte](numberValuesExpression, chromedp.EvalAwaitPromise))
	if err != nil {
		return nil, fmt.Errorf("evaluate number values capture: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	return raw, nil
}

// numberValuesExpression uses the ids and sources of the layout capture
// bundle, so a component here has the same id as in the layout state file.
const numberValuesExpression = `(async () => {
  const lc = binoLayoutCapture;
  const ctx = lc.findContext();
  if (!ctx || typeof ctx.getLayoutState !== 'function') return null;
  await lc.waitForSettle(15000);
  const hosts = lc.identifyHosts(ctx);
  const sources = lc.collectSources(hosts);
  const pages = Array.from(ctx.querySelectorAll('bn-layout-page'));
  const components = [];
  for (const host of hosts) {
    const el = host.element;
    if (el.localName !== 'bn-table' && !el.localName.startsWith('bn-chart-')) continue;
    if (typeof el.getLayoutState !== 'function') continue;
    const source = sources[host.id] || {};
    const page = el.closest('bn-layout-page');
    const entry = {
      id: host.id,
      tag: el.localName,
      kind: source.kind || '',
      name: source.name || '',
      ref: source.ref || '',
      page: page ? pages.indexOf(page) + 1 : 0,
      pageName: page ? (page.getAttribute('data-bino-page') || '') : '',
      datasets: el.getAttribute('datasets') || '',
      filter: el.getAttribute('filter') || '',
      dateInterval: el.getAttribute(el.localName === 'bn-table' ? 'interval' : 'date-interval') || '',
      measureScale: el.getAttribute('measure-scale') || '',
      measureUnit: el.getAttribute('measure-unit') || '',
      items: [],
    };
    let state = null;
    try {
      state = await lc.captureComponentDetail(el);
    } catch (e) {
      entry.error = String(e && e.message ? e.message : e);
    }
    if (state) {
      const meta = state.metadata || {};
      if (!entry.measureScale && meta.measureScale) entry.measureScale = meta.measureScale;
      if (!entry.measureUnit && meta.measureUnit) entry.measureUnit = meta.measureUnit;
      if (!entry.dateInterval && el.localName === 'bn-chart-time') {
        // a blank date-interval is detected by the engine, which says so in a warning
        for (const d of state.diagnostics || []) {
          const m = /auto-detected: (\w+)/.exec(d.message || '');
          if (m) entry.dateInterval = m[1];
        }
      }
      if (state.error) entry.error = state.error;
      for (const item of state.elements || []) {
        const keep = { kind: item.kind };
        if (item.point) keep.point = item.point;
        if (item.kind === 'cell') {
          if (item.key != null) keep.key = item.key;
          if (item.type != null) keep.type = item.type;
          if (item.value != null) keep.value = item.value;
          if (item.text != null) keep.text = item.text;
        } else if (item.kind === 'bar') {
          // only an element with a data-point key belongs to a value; axis text and ticks have none
          if (!item.point) continue;
          if (item.value != null) keep.value = item.value;
          if (item.segment != null) keep.segment = item.segment;
          if (item.scenario != null) keep.scenario = item.scenario;
        } else if (item.kind === 'label') {
          if (!item.point) continue;
          keep.role = item.role;
          keep.text = item.text;
        } else if (item.kind === 'point') {
          if (!item.point) continue;
          if (item.text != null) keep.text = item.text;
          if (item.series != null) keep.series = item.series;
        } else {
          continue;
        }
        entry.items.push(keep);
      }
    }
    components.push(entry);
  }
  return { components: components };
})()`
