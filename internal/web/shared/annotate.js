/**
 * Annotate mode of the preview: a plain click on a component posts the data
 * point under the cursor to the host (the VS Code extension), which opens a
 * prefilled annotation form. Cmd/Ctrl+click keeps revealing the source.
 *
 * The full preview imports this module; the embedding route injects the IIFE
 * build of the same source, so both run one implementation.
 *
 * Neither data-bino-* (this CLI's renderer) nor data-bn-* (the engine) is a
 * stable interface. A missing attribute means "no data point", never an
 * error: the click then names the whole component.
 */

/** Point fields and the attribute the engine writes them to. */
export const KEY_ATTRIBUTES = [
  ['rowGroup', 'data-bn-row-group'],
  ['category', 'data-bn-category'],
  ['subCategory', 'data-bn-sub-category'],
  ['date', 'data-bn-date'],
  ['scenario', 'data-bn-scenario'],
  ['row', 'data-bn-row'],
];

/** Kinds that have no data points: a mark goes on the whole component. */
export const NO_POINT_KINDS = ['Text', 'Tree', 'Grid', 'SmallMultiple'];

/** The engine's component hosts that report a layout state with points. */
export const POINT_HOSTS = ['bn-chart-time', 'bn-chart-structure', 'bn-chart-scatter', 'bn-chart-bubble', 'bn-chart-bullet', 'bn-table'];

const isElement = (node) => !!node && node.nodeType === 1;

/** The point of a data-point element, or null when it carries no part. */
export function readPoint(el) {
  const part = el.getAttribute('data-bn-part');
  if (!part) return null;
  const point = {};
  for (const [field, attribute] of KEY_ATTRIBUTES) {
    const value = el.getAttribute(attribute);
    if (value !== null && value !== '') point[field] = value;
  }
  // element is the default part: an annotation does not need to say it
  if (part !== 'element') point.part = part;
  return point;
}

/**
 * The first data point on a path, inside out. An empty table cell has no
 * attributes of its own and its row has: the walk stops at a cell, so the
 * cell does not get the key of its row.
 */
export function pointOnPath(inner) {
  for (const el of inner) {
    const point = readPoint(el);
    if (point) return { point, stopped: false };
    if (el.localName === 'td' || el.localName === 'th') return { point: null, stopped: true };
  }
  return { point: null, stopped: false };
}

const isSlot = (el) => el.hasAttribute('data-bino-kind') && !!(el.getAttribute('data-bino-ref') || el.getAttribute('data-bino-name'));
// a Grid or Tree holds components that cannot be targets themselves
const isHolder = (el) => el.localName === 'bn-grid' || el.localName === 'bn-tree';

/**
 * The component a path is in, as an annotation can name it. A Grid child or
 * Tree node says so itself (data-bino-target-kind). Anything else inside a
 * Grid or Tree, a card in a cell for example, cannot be a target: the click
 * then names the Grid or Tree, or the SmallMultiple whose panel it is in.
 * Elements inside the component come first in `inner`, with the engine host
 * of the component among them.
 */
export function locate(path) {
  const els = path.filter(isElement);
  let at = els.findIndex((el) => el.hasAttribute('data-bino-target-kind'));
  if (at < 0) {
    at = els.findIndex(isSlot);
    if (at < 0) return null;
    const holder = els.findIndex((el, i) => i > at && isHolder(el));
    if (holder >= 0) {
      const outer = els.findIndex((el, i) => i > holder && isSlot(el));
      if (outer >= 0) at = outer;
    }
  }
  const slot = els[at];
  const byTarget = slot.hasAttribute('data-bino-target-kind');
  const kind = byTarget ? slot.getAttribute('data-bino-target-kind') : slot.getAttribute('data-bino-kind');
  const name = byTarget ? slot.getAttribute('data-bino-target-name') : (slot.getAttribute('data-bino-ref') || slot.getAttribute('data-bino-name'));
  const inner = els.slice(0, at);
  const found = { kind, name, slot, inner, host: [...inner].reverse().find((el) => POINT_HOSTS.includes(el.localName)) };
  if (kind === 'SmallMultiple') {
    const cell = inner.find((el) => el.hasAttribute('data-bino-panel'));
    if (cell) found.panel = cell.getAttribute('data-bino-panel');
  }
  // the source location is on the slot of the manifest, which can be further out than a Grid child
  const source = els.slice(at).find((el) => el.hasAttribute('data-bino-source-line'));
  const line = source ? parseInt(source.getAttribute('data-bino-source-line') || '', 10) : 0;
  if (line > 0) {
    found.sourceFile = source.getAttribute('data-bino-source-file') || '';
    found.line = line;
  }
  return found;
}

/**
 * The smallest point of a layout state whose box holds a click, or null.
 * The fallback for marks that cover the elements with the keys, such as the
 * overlay line of a time chart over its bars.
 */
export function pointAtCursor(state, host, x, y) {
  // the engine measures its boxes from the host's screen box, so they are screen px too
  const origin = host.getBoundingClientRect();
  const lx = x - origin.left;
  const ly = y - origin.top;
  let found = null;
  let smallest = Infinity;
  for (const entry of (state && state.elements) || []) {
    const box = entry.rect && entry.rect.component;
    if (!entry.point || !box) continue;
    if (lx < box.x || lx > box.x + box.width || ly < box.y || ly > box.y + box.height) continue;
    const area = box.width * box.height;
    if (area < smallest) {
      smallest = area;
      found = entry.point;
    }
  }
  if (!found) return null;
  const point = {};
  for (const [field] of KEY_ATTRIBUTES) {
    if (found[field] !== undefined && found[field] !== null && found[field] !== '') point[field] = String(found[field]);
  }
  if (found.part && found.part !== 'element') point.part = found.part;
  return point;
}

/** A component that failed to hydrate never answers. */
function readState(host, ms) {
  if (!host || typeof host.getLayoutState !== 'function') return Promise.resolve(null);
  let timer;
  return Promise.race([
    host.getLayoutState({ detail: 'full' }),
    new Promise((resolve) => { timer = setTimeout(() => resolve(null), ms); }),
  ]).catch(() => null).finally(() => clearTimeout(timer));
}

/**
 * What a click picks: the component, its panel, and the data point when the
 * component has data points. null when the click is on no named component.
 */
export async function readPick(path, x, y) {
  const found = locate(path);
  if (!found) return null;
  const pick = { kind: found.kind, name: found.name };
  if (found.panel) pick.panel = found.panel;
  if (found.sourceFile !== undefined) {
    pick.sourceFile = found.sourceFile;
    pick.line = found.line;
  }
  // a SmallMultiple has points only in one panel
  const pointKind = found.kind === 'SmallMultiple' && found.panel ? 'panel' : found.kind;
  if (NO_POINT_KINDS.includes(pointKind)) return pick;
  const onPath = pointOnPath(found.inner);
  if (onPath.stopped) return pick;
  let point = onPath.point;
  if (!point && found.host) {
    const state = await readState(found.host, 1000);
    point = pointAtCursor(state, found.host, x, y);
  }
  if (point && Object.keys(point).length) pick.point = point;
  return pick;
}

/**
 * The annotation a click on a mark or a list entry belongs to: its name, and
 * its source location from the items of the layer or list that drew it. null
 * when the path has no `data-bino-annotation` (no mark, or an engine before
 * v1.0.0-next.88).
 */
export function annotationOnPath(path, x, y) {
  const els = path.filter(isElement);
  let el = els.find((e) => e.hasAttribute('data-bino-annotation'));
  if (!el) return null;
  // The wide hit line of an arrow is invisible and can lie over another mark:
  // the visible mark under the cursor wins.
  if (els[0] && els[0].classList && els[0].classList.contains('hit') && x !== undefined) {
    const root = el.getRootNode();
    const under = root && root.elementsFromPoint ? root.elementsFromPoint(x, y) : [];
    const visible = under.find((e) => !e.classList.contains('hit') && e.closest && e.closest('[data-bino-annotation]'));
    if (visible) el = visible.closest('[data-bino-annotation]');
  }
  const name = el.getAttribute('data-bino-annotation');
  const found = { name };
  const root = el.getRootNode && el.getRootNode();
  const host = root && root.host;
  try {
    const items = JSON.parse((host && host.getAttribute('items')) || '[]');
    const item = items.find((i) => i && i.name === name);
    if (item && item.line > 0) {
      found.sourceFile = item.sourceFile || '';
      found.line = item.line;
    }
  } catch (e) { /* items are the engine's business: no source location then */ }
  return found;
}

/**
 * The annotations of each page: their number and whether the layer placed a
 * mark. A mark the engine did not draw has no element with its name.
 */
export function annotationReport(doc) {
  const pages = [];
  doc.querySelectorAll('bn-layout-page').forEach((page) => {
    const layer = page.querySelector('bn-annotation-layer');
    if (!layer) return;
    let items = [];
    try { items = JSON.parse(layer.getAttribute('items') || '[]'); } catch (e) { return; }
    const root = layer.shadowRoot;
    const seen = new Set();
    const out = [];
    for (const item of items) {
      if (!item || !item.name || seen.has(item.name)) continue;
      seen.add(item.name);
      const placed = !!(root && root.querySelector('[data-bino-annotation="' + CSS.escape(item.name) + '"]'));
      out.push({ name: item.name, label: item.label || '', placed });
    }
    pages.push({ page: page.getAttribute('data-bino-page') || '', items: out });
  });
  return pages;
}

/** Whether the engine's annotation layer names its marks and can take clicks. */
export function marksSupported(win) {
  const ctor = win.customElements && win.customElements.get('bn-annotation-layer');
  return !!ctor && 'interactive' in ctor.prototype;
}

/**
 * Start listening: `bino:annotateMode {on}` from the host turns the mode on
 * and off. The page tells the host it can do this with `bino:capabilities`.
 */
export function install(win) {
  const w = win || window;
  if (w.__binoAnnotate || w.parent === w) return;
  w.__binoAnnotate = true;
  const doc = w.document;
  let on = false;
  let latest = 0;
  let box = null;

  const outline = () => {
    if (!box) {
      box = doc.createElement('div');
      box.setAttribute('data-bino-annotate-outline', '');
      box.style.cssText = 'position:fixed;pointer-events:none;z-index:2147483647;outline:2px dashed #0070c0;outline-offset:1px;display:none';
      doc.body.appendChild(box);
    }
    return box;
  };
  const hide = () => { if (box) box.style.display = 'none'; };
  const show = (el) => {
    const r = el.getBoundingClientRect();
    const b = outline();
    b.style.left = r.left + 'px';
    b.style.top = r.top + 'px';
    b.style.width = r.width + 'px';
    b.style.height = r.height + 'px';
    b.style.display = 'block';
  };

  // in the mode the marks take clicks; a preview that swaps its content gets new layers
  const setInteractive = () => {
    doc.querySelectorAll('bn-annotation-layer').forEach((layer) => {
      if (on) layer.setAttribute('interactive', ''); else layer.removeAttribute('interactive');
    });
  };
  let lastReport = '';
  const pending = new Set();
  const sendReport = () => {
    const pages = annotationReport(doc);
    const text = JSON.stringify(pages);
    if (text === lastReport) return;
    lastReport = text;
    // an engine before the mark names draws marks without them: placed is unknown then
    w.parent.postMessage({ type: 'bino:annotations', pages, marks: marksSupported(w) ? 1 : 0 }, '*');
  };
  // The page changes all the time (toolbar, inspector), so this is a throttle, not
  // a debounce. A layer places its marks in its shadow root after the charts drew,
  // which no observer here sees: a later second look catches them.
  const report = () => {
    for (const ms of [800, 3000, 8000]) {
      if (pending.has(ms)) continue;
      pending.add(ms);
      setTimeout(() => { pending.delete(ms); sendReport(); }, ms);
    }
  };
  new w.MutationObserver(() => { if (on) setInteractive(); report(); })
    .observe(doc.documentElement, { childList: true, subtree: true });

  w.addEventListener('message', (e) => {
    if (!e.data || e.data.type !== 'bino:annotateMode') return;
    on = !!e.data.on;
    doc.documentElement.style.cursor = on ? 'crosshair' : '';
    setInteractive();
    if (!on) hide();
  });

  doc.addEventListener('mousemove', (e) => {
    if (!on) return;
    const path = e.composedPath();
    const found = locate(path);
    if (!found) { hide(); return; }
    // the data point a click would pick if there is one, else the component
    let mark = null;
    for (const el of found.inner) {
      if (el.hasAttribute('data-bn-part')) { mark = el; break; }
      if (el.localName === 'td' || el.localName === 'th') break;
    }
    show(mark || found.slot);
  }, true);

  doc.addEventListener('click', (e) => {
    if (!on || e.button !== 0) return;
    // a mark or list entry opens its own annotation; with Cmd/Ctrl its source
    const note = annotationOnPath(e.composedPath(), e.clientX, e.clientY);
    if (note) {
      e.preventDefault();
      e.stopImmediatePropagation();
      w.parent.postMessage(Object.assign({ type: 'bino:revealAnnotation', source: e.metaKey || e.ctrlKey }, note), '*');
      return;
    }
    if (e.metaKey || e.ctrlKey) return;
    // the preview's own toolbar and anything outside a component keep their clicks
    if (!locate(e.composedPath())) return;
    e.preventDefault();
    e.stopPropagation();
    const id = ++latest;
    readPick(e.composedPath(), e.clientX, e.clientY).then((pick) => {
      if (id !== latest || !pick) return;
      w.parent.postMessage(Object.assign({ type: 'bino:pickPoint' }, pick), '*');
    });
  }, true);

  // the engine may load after this script: ask once it has defined its elements
  const announce = () => w.parent.postMessage({ type: 'bino:capabilities', annotate: 1, marks: marksSupported(w) ? 1 : 0 }, '*');
  announce();
  if (w.customElements && !marksSupported(w)) {
    w.customElements.whenDefined('bn-annotation-layer').then(() => { if (marksSupported(w)) announce(); });
  }
  report();
}
