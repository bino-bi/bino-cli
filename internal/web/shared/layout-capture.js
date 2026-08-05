/**
 * Capture of the template engine's layout state.
 *
 * `bn-context.getLayoutState()` (engine >= v1.0.0-next.24) reports what
 * actually reached the screen. Two things it cannot report are added here from
 * the DOM: which manifest document a component came from (the engine only has
 * generated ids like "bn-table[0]"), and the declared measure of a component
 * (ComponentMetadata declares measureUnit but no component populates it).
 * Both come from attributes this CLI's renderer writes.
 */

/**
 * Every visual bn-* component, mirroring VISUAL_SELECTOR in bn-context.
 * The list and its order decide the generated ids, so it must stay in step
 * with the engine or the source join silently misaligns.
 */
export const VISUAL_SELECTOR =
  'bn-body, bn-card, bn-chart-bubble, bn-chart-bullet, bn-chart-scatter, ' +
  'bn-chart-structure, bn-chart-time, bn-footer, bn-grid, bn-image, ' +
  'bn-layout-card, bn-layout-page, bn-message, bn-page, bn-table, ' +
  'bn-template, bn-text, bn-title, bn-tree';

/** @returns {Element|null} the report's bn-context host. */
export function findContext() {
  return document.querySelector('bn-context');
}

/**
 * Whether the loaded engine exposes layout state. Feature-detected rather
 * than version-gated: the CLI's supported engine range is far wider than
 * layout-state support.
 * @returns {boolean}
 */
export function supportsLayoutState() {
  var ctx = findContext();
  return !!ctx && typeof ctx.getLayoutState === 'function';
}

/** Resolves after ms milliseconds. */
function delay(ms) {
  return new Promise(function (resolve) {
    setTimeout(resolve, ms);
  });
}

/**
 * A cheap fingerprint of the report's geometry: every visual host's rounded
 * box. Two identical samples a moment apart mean the layout stopped moving.
 * @param {Element} ctx
 * @returns {string}
 */
function layoutSignature(ctx) {
  var parts = [];
  Array.prototype.forEach.call(ctx.querySelectorAll(VISUAL_SELECTOR), function (el) {
    var rect = el.getBoundingClientRect();
    parts.push(Math.round(rect.width) + 'x' + Math.round(rect.height));
  });
  return parts.join(',');
}

/** How long the geometry must stay unchanged before a capture is trusted. */
const QUIET_MS = 250;

/**
 * Default settle budget. Generous because a heavy report keeps re-measuring
 * for several seconds — charts render collapsed and without a canvas until
 * their auto-fit pass completes, and a capture taken in that window reports
 * zero bars for a chart that is about to render fine.
 */
const SETTLE_TIMEOUT_MS = 8000;

/**
 * Wait until the rendered report stops moving.
 *
 * `componentRegisterIsRenderedResult` is the engine's documented trigger, but
 * it is a 500 ms debounce on component status — it goes true while charts are
 * still auto-fitting, and a capture taken then reports collapsed boxes and
 * zero bars for a chart that is about to render fine. So the flag only opens
 * the gate; the capture waits for two identical geometry samples after it.
 *
 * @param {number} [timeoutMs] give up and capture anyway after this long.
 * @returns {Promise<boolean>} true if the report settled, false on timeout.
 */
export async function waitForSettle(timeoutMs) {
  var deadline = Date.now() + (timeoutMs || SETTLE_TIMEOUT_MS);
  var ctx = findContext();

  while (window.componentRegisterIsRenderedResult !== true) {
    if (Date.now() >= deadline) return false;
    await delay(100);
  }
  if (!ctx) return true;

  var previous = null;
  while (Date.now() < deadline) {
    var signature = layoutSignature(ctx);
    if (signature === previous) return true;
    previous = signature;
    await delay(QUIET_MS);
  }
  return false;
}

/**
 * Resolve the engine's component id for each visual host, replicating the
 * engine's rule: explicit `id` attribute, then `name`, otherwise a per-tag
 * document-order `tag[index]`.
 * @param {Element} ctx
 * @returns {{id: string, element: Element}[]} hosts in document order.
 */
export function identifyHosts(ctx) {
  var counters = {};
  return Array.prototype.map.call(ctx.querySelectorAll(VISUAL_SELECTOR), function (el) {
    var tag = el.localName;
    var index = counters[tag] || 0;
    counters[tag] = index + 1;
    var explicit = el.getAttribute('id') || el.getAttribute('name');
    return { id: explicit || tag + '[' + index + ']', element: el };
  });
}

/**
 * Build the source map the analyzer needs: manifest identity from the
 * data-bino-* attributes writeSourceAttrs puts on the slot wrapper, and the
 * declared measure from the component's own attributes.
 * @param {{id: string, element: Element}[]} hosts
 * @returns {Object<string, object>} keyed by engine component id.
 */
export function collectSources(hosts) {
  var sources = {};
  hosts.forEach(function (host) {
    var el = host.element;
    var owner = el.closest ? el.closest('[data-bino-kind]') : null;
    var source = {};
    if (owner) {
      source.kind = owner.getAttribute('data-bino-kind') || '';
      source.name = owner.getAttribute('data-bino-name') || '';
      source.ref = owner.getAttribute('data-bino-ref') || '';
    }
    var unit = el.getAttribute('measure-unit');
    var scale = el.getAttribute('measure-scale');
    if (unit) source.measureUnit = unit;
    if (scale) source.measureScale = scale;

    // An entry with nothing known would only bloat the payload.
    if (source.kind || source.name || source.ref || source.measureUnit) {
      sources[host.id] = source;
    }
  });
  return sources;
}

/**
 * Capture a summary snapshot of the rendered report.
 *
 * `elements` maps component id to the live DOM node so callers can highlight,
 * scroll to, or request per-component full detail without re-deriving the
 * mapping. It is deliberately not part of the posted payload.
 *
 * `settled` false means the report was still moving when the budget ran out.
 * Such a snapshot describes a real moment but not the finished report, so
 * callers must not derive findings from it — an unfinished chart reports zero
 * bars and would be flagged as "rendered empty".
 *
 * @param {{settleTimeoutMs?: number}} [options]
 * @returns {Promise<{state: object, sources: object, elements: Map<string, Element>, settled: boolean}|null>}
 *   null when the engine has no layout-state support.
 */
export async function captureLayoutState(options) {
  var ctx = findContext();
  if (!ctx || typeof ctx.getLayoutState !== 'function') {
    return null;
  }

  var settled = await waitForSettle(options && options.settleTimeoutMs);
  var state = await ctx.getLayoutState();
  var hosts = identifyHosts(ctx);

  var elements = new Map();
  hosts.forEach(function (host) {
    elements.set(host.id, host.element);
  });

  return { state: state, sources: collectSources(hosts), elements: elements, settled: settled };
}

/**
 * Full per-element detail for a single component. Never request full detail
 * from bn-context: every cell of a large table would run to megabytes.
 * @param {Element} element a rich component host.
 * @returns {Promise<object|null>}
 */
export async function captureComponentDetail(element) {
  if (!element || typeof element.getLayoutState !== 'function') {
    return null;
  }
  return element.getLayoutState({ detail: 'full' });
}
