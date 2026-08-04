import { LitElement, html, css, nothing } from 'lit';
import { appBase } from '../../shared/dom-utils.js';
import {
  captureComponentDetail,
  captureLayoutState,
  supportsLayoutState,
} from '../../shared/layout-capture.js';

/** How many times an unsettled capture is retried before giving up. */
const MAX_RETRIES = 3;

/**
 * Report inspector: what the engine actually rendered, joined back to the
 * manifest.
 *
 * The DOM alone already gives geometry, so this deliberately does not compete
 * with browser devtools. It shows what only the engine knows — resolved
 * auto-scaling, the font auto-fit factor, data reachability and diagnostics —
 * and the findings the CLI derives from them.
 */
class BinoInspector extends LitElement {
  static properties = {
    _open: { state: true },
    _supported: { state: true },
    _state: { state: true },
    _findings: { state: true },
    _elements: { state: true },
    _selectedId: { state: true },
    _detail: { state: true },
    _showBoxes: { state: true },
    _provisional: { state: true },
    _busy: { state: true },
    _error: { state: true },
  };

  static styles = css`
    :host {
      position: fixed;
      top: var(--bino-toolbar-height);
      right: 0;
      bottom: 0;
      width: var(--bino-sidebar-width);
      max-width: 100vw;
      z-index: var(--bino-z-panel);
      background: var(--bino-surface);
      border-left: 1px solid var(--bino-border);
      box-shadow: var(--bino-shadow-dropdown);
      font-family: var(--bino-font-sans);
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
      display: none;
      flex-direction: column;
    }
    :host([open]) {
      display: flex;
    }
    .header {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-bottom: 1px solid var(--bino-border);
      font-weight: 600;
      color: var(--bino-text-muted);
      flex-shrink: 0;
    }
    .header .spacer {
      flex: 1;
    }
    .icon-btn {
      background: none;
      border: none;
      cursor: pointer;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-md);
      padding: 2px 4px;
      border-radius: var(--bino-radius);
      font-family: inherit;
    }
    .icon-btn:hover {
      background: var(--bino-surface-hover);
      color: var(--bino-text);
    }
    .icon-btn[aria-pressed='true'] {
      background: var(--bino-surface-active);
      color: var(--bino-active-text);
    }
    .body {
      overflow-y: auto;
      flex: 1;
    }
    .notice {
      padding: var(--bino-space-md);
      color: var(--bino-text-secondary);
      line-height: 1.5;
    }
    .notice.provisional {
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      margin: 0;
      padding: var(--bino-space-sm) var(--bino-space-md);
    }
    .notice code {
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      background: var(--bino-surface-inset);
      padding: 1px 4px;
      border-radius: 4px;
    }
    .section-title {
      padding: var(--bino-space-sm) var(--bino-space-md) var(--bino-space-xs);
      font-size: var(--bino-font-size-xs);
      text-transform: uppercase;
      letter-spacing: 0.04em;
      color: var(--bino-text-secondary);
      font-weight: 600;
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    .finding {
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-left: 3px solid var(--bino-warning);
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      cursor: pointer;
      line-height: 1.4;
    }
    .finding.error {
      border-left-color: var(--bino-error);
      background: var(--bino-error-bg);
      border-bottom-color: var(--bino-error-border);
    }
    .finding-label {
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    .finding-hint {
      display: block;
      margin-top: 2px;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
    .row {
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-bottom: 1px solid var(--bino-border);
      cursor: pointer;
    }
    .row:hover {
      background: var(--bino-surface-hover);
    }
    .row.selected {
      background: var(--bino-surface-active);
    }
    .row-label {
      font-weight: 600;
      display: flex;
      align-items: baseline;
      gap: var(--bino-space-xs);
    }
    .row-tag {
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      font-weight: 400;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs);
      margin-top: var(--bino-space-xs);
    }
    .chip {
      padding: 1px 6px;
      border-radius: var(--bino-radius-pill);
      font-size: var(--bino-font-size-xs);
      background: var(--bino-surface-inset);
      color: var(--bino-text-secondary);
      white-space: nowrap;
    }
    .chip.scale {
      background: var(--bino-celeste-100);
      color: var(--bino-celeste-800);
    }
    .chip.warn {
      background: var(--bino-warning-bg);
      color: var(--bino-warning-text);
    }
    .chip.bad {
      background: var(--bino-error-bg);
      color: var(--bino-bad);
    }
    .detail {
      padding: var(--bino-space-sm) var(--bino-space-md);
      background: var(--bino-surface-subtle);
      border-bottom: 1px solid var(--bino-border);
      line-height: 1.5;
    }
    .detail dl {
      margin: 0;
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 2px var(--bino-space-sm);
    }
    .detail dt {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
    .detail dd {
      margin: 0;
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      overflow-wrap: anywhere;
    }
    .detail-actions {
      margin-top: var(--bino-space-sm);
      display: flex;
      gap: var(--bino-space-xs);
    }
    .text-btn {
      background: var(--bino-surface);
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius-pill);
      padding: 2px 10px;
      font-size: var(--bino-font-size-xs);
      font-family: inherit;
      color: var(--bino-text-secondary);
      cursor: pointer;
    }
    .text-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
  `;

  constructor() {
    super();
    this._open = false;
    this._supported = true;
    this._state = null;
    this._findings = [];
    this._elements = new Map();
    this._selectedId = '';
    this._detail = null;
    this._showBoxes = false;
    this._provisional = false;
    this._retries = 0;
    this._busy = false;
    this._error = '';
    this._captureTimer = null;
    this._overlays = [];
    this._boundOnOpen = this._onOpen.bind(this);
    this._boundOnContentUpdated = this._onContentUpdated.bind(this);
    this._boundOnKeydown = this._onKeydown.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-open-inspector', this._boundOnOpen);
    document.addEventListener('bn-preview:content-updated', this._boundOnContentUpdated);
    document.addEventListener('keydown', this._boundOnKeydown);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-open-inspector', this._boundOnOpen);
    document.removeEventListener('bn-preview:content-updated', this._boundOnContentUpdated);
    document.removeEventListener('keydown', this._boundOnKeydown);
    if (this._captureTimer) clearTimeout(this._captureTimer);
    this._clearOverlays();
  }

  updated(changed) {
    if (changed.has('_open')) {
      if (this._open) {
        this.setAttribute('open', '');
      } else {
        this.removeAttribute('open');
      }
    }
  }

  render() {
    return html`
      <div class="header">
        <span>Inspector</span>
        <span class="spacer"></span>
        ${this._supported
          ? html`
              <button class="icon-btn" title="Outline every component"
                aria-pressed=${this._showBoxes ? 'true' : 'false'} @click=${this._onToggleBoxes}>□</button>
              <button class="icon-btn" title="Copy the snapshot as JSON" @click=${this._onCopy}>⎘</button>
              <button class="icon-btn" title="Re-capture" @click=${this._onRefresh}>↻</button>
            `
          : nothing}
        <button class="icon-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <div class="body">${this._renderBody()}</div>
    `;
  }

  _renderBody() {
    if (!this._supported) {
      return html`
        <p class="notice">
          This template engine does not expose layout state. The inspector needs
          <code>bn-template-engine v1.0.0-next.24</code> or newer — pin it with
          <code>engine-version</code> in <code>bino.toml</code>.
        </p>
      `;
    }
    if (this._error) {
      return html`<p class="notice">${this._error}</p>`;
    }
    if (this._busy && !this._state) {
      return html`<p class="notice">Capturing…</p>`;
    }
    if (!this._state) {
      return html`<p class="notice">No snapshot yet.</p>`;
    }

    var components = this._state.components || [];
    return html`
      ${this._provisional
        ? html`
            <p class="notice provisional">
              The report is still rendering, so these boxes are provisional and
              no checks were run. Re-capturing…
            </p>
          `
        : nothing}
      ${this._findings.length > 0
        ? html`
            <div class="section-title">${this._findings.length} finding${this._findings.length === 1 ? '' : 's'}</div>
            <ul>${this._findings.map((f) => this._renderFinding(f))}</ul>
          `
        : nothing}
      <div class="section-title">${components.length} component${components.length === 1 ? '' : 's'}</div>
      <ul>${components.map((c) => this._renderComponent(c))}</ul>
    `;
  }

  _renderFinding(finding) {
    var label = finding.name
      ? (finding.kind ? finding.kind + ' ' + finding.name : finding.name)
      : finding.componentId;
    return html`
      <li class="finding ${finding.severity === 'error' ? 'error' : ''}"
        @click=${() => this._select(finding.componentId)}>
        <span class="finding-label">${label}</span> — ${finding.message}
        ${finding.hint ? html`<span class="finding-hint">${finding.hint}</span>` : nothing}
      </li>
    `;
  }

  _renderComponent(component) {
    var source = (this._state.sources && this._state.sources[component.id]) || {};
    var label = source.name || source.ref || component.id;
    var selected = component.id === this._selectedId;
    return html`
      <li>
        <div class="row ${selected ? 'selected' : ''}"
          @click=${(e) => this._onRowClick(e, component.id)}
          @mouseenter=${() => this._outline(component.id)}
          @mouseleave=${() => this._clearHoverOutline()}>
          <div class="row-label">
            <span>${source.kind ? source.kind + ' ' + label : label}</span>
            <span class="row-tag">${component.tag}</span>
          </div>
          <div class="chips">
            <span class="chip">${this._formatBox(component)}</span>
            ${this._countChips(component)}
            ${this._scaleChip(component)}
            ${this._diagChips(component)}
          </div>
        </div>
        ${selected ? this._renderDetail(component) : nothing}
      </li>
    `;
  }

  _renderDetail(component) {
    var rows = [];
    var em = component.em || {};
    rows.push(['font', this._round(em.fontSizePx) + 'px']);
    if (em.appliedScaleFactor != null && em.appliedScaleFactor !== 1) {
      rows.push(['auto-fit', Math.round(em.appliedScaleFactor * 100) + '%']);
    }
    var scaling = component.scaling || {};
    if (scaling.unitsPerEm != null) {
      rows.push(['units/em', this._round(scaling.unitsPerEm) + ' (' + (scaling.unitMode || 'auto') + ')']);
    }
    if (scaling.percentagePointsPerEm != null) {
      rows.push(['pp/em', this._round(scaling.percentagePointsPerEm) + ' (' + (scaling.percentageMode || 'auto') + ')']);
    }
    (component.regions || []).forEach(function (region) {
      rows.push([region.id, Math.round(region.rect.component.width) + '×' + Math.round(region.rect.component.height)]);
    });
    var meta = component.metadata || {};
    if (meta.scenarios && meta.scenarios.length > 0) {
      rows.push(['scenarios', meta.scenarios.join(', ')]);
    }
    if (meta.variances && meta.variances.length > 0) {
      rows.push(['variances', meta.variances.join(', ')]);
    }
    (component.diagnostics || []).forEach(function (d) {
      rows.push([d.id || d.type || 'diagnostic', d.message || '']);
    });

    return html`
      <div class="detail">
        <dl>${rows.map((r) => html`<dt>${r[0]}</dt><dd>${r[1]}</dd>`)}</dl>
        ${this._detail ? this._renderElementDetail() : nothing}
        <div class="detail-actions">
          ${this._detail
            ? nothing
            : html`<button class="text-btn" @click=${() => this._loadDetail(component.id)}>Element detail</button>`}
          <button class="text-btn" @click=${() => this._revealSource(component.id)}>Reveal source</button>
        </div>
      </div>
    `;
  }

  _renderElementDetail() {
    var byKind = {};
    (this._detail.elements || []).forEach(function (el) {
      byKind[el.kind] = (byKind[el.kind] || 0) + 1;
    });
    var kinds = Object.keys(byKind).sort();
    var columns = (this._detail.table && this._detail.table.columns) || [];

    return html`
      <dl>
        ${kinds.map((k) => html`<dt>${k}</dt><dd>${byKind[k]}</dd>`)}
        ${columns.map(
          (c) => html`<dt>col ${c.index}</dt><dd>${c.key}${c.bucket ? ' · ' + c.bucket : ''}</dd>`
        )}
      </dl>
    `;
  }

  _formatBox(component) {
    var rect = (component.rect && component.rect.component) || { width: 0, height: 0 };
    return Math.round(rect.width) + '×' + Math.round(rect.height);
  }

  _countChips(component) {
    var meta = component.metadata || {};
    var counts = [
      ['bars', meta.barCount],
      ['points', meta.pointCount],
      ['rows', meta.rowCount],
      ['nodes', meta.nodeCount],
    ];
    return counts
      .filter(function (c) {
        return c[1] != null;
      })
      .map(function (c) {
        return html`<span class="chip ${c[1] === 0 ? 'warn' : ''}">${c[1]} ${c[0]}</span>`;
      });
  }

  _scaleChip(component) {
    var scaling = component.scaling || {};
    if (scaling.unitsPerEm == null) return nothing;
    return html`<span class="chip scale">${scaling.unitMode || 'auto'} ${this._round(scaling.unitsPerEm)}/em</span>`;
  }

  _diagChips(component) {
    return (component.diagnostics || []).map(function (d) {
      return html`<span class="chip ${d.type === 'error' ? 'bad' : 'warn'}">${d.id || d.type}</span>`;
    });
  }

  _round(value) {
    if (value == null) return '';
    return Math.round(value * 100) / 100;
  }

  // --- capture -------------------------------------------------------------

  _onOpen() {
    this._open = true;
    this._retries = 0;
    this._capture();
  }

  _onClose() {
    this._open = false;
    this._showBoxes = false;
    this._clearOverlays();
  }

  _onKeydown(event) {
    if (event.key === 'Escape' && this._open) {
      this._onClose();
    }
  }

  _onContentUpdated() {
    if (!this._open) return;
    // The report re-renders and auto-fits after a hot reload; capturing on the
    // trailing edge avoids snapshotting an intermediate layout.
    if (this._captureTimer) clearTimeout(this._captureTimer);
    var self = this;
    this._captureTimer = setTimeout(function () {
      self._retries = 0;
      self._capture();
    }, 250);
  }

  _onRefresh() {
    this._retries = 0;
    this._capture();
  }

  /**
   * Retry after an unsettled capture. Bounded: a report that never settles
   * would otherwise re-capture forever, and each capture forces a full layout.
   */
  _scheduleRetry() {
    if (this._retries >= MAX_RETRIES) return;
    this._retries++;
    var self = this;
    if (this._captureTimer) clearTimeout(this._captureTimer);
    this._captureTimer = setTimeout(function () {
      self._capture();
    }, 1000);
  }

  async _capture() {
    this._supported = supportsLayoutState();
    if (!this._supported) {
      this._state = null;
      return;
    }

    this._busy = true;
    this._error = '';
    try {
      var capture = await captureLayoutState({ settleTimeoutMs: 5000 });
      if (!capture) {
        this._supported = false;
        return;
      }
      // Selection and element detail belong to the previous DOM.
      var previousId = this._selectedId;
      this._elements = capture.elements;
      this._detail = null;
      this._state = Object.assign({}, capture.state, { sources: capture.sources });
      this._selectedId = capture.elements.has(previousId) ? previousId : '';
      this._provisional = !capture.settled;

      // Findings from an unsettled report are wrong in the worst way: a chart
      // that has not finished rendering reports zero bars and would be called
      // empty. Show the geometry, withhold the verdict, and try again.
      if (this._provisional) {
        this._findings = [];
        this._scheduleRetry();
      } else {
        this._findings = await this._analyze(capture);
      }
      if (this._showBoxes) this._drawAllOutlines();
    } catch (err) {
      console.error('bino inspector: capture failed', err);
      this._error = 'Capture failed: ' + (err && err.message ? err.message : err);
    } finally {
      this._busy = false;
    }
  }

  /**
   * Findings come from the CLI so the inspector, the build warnings and the
   * MCP tooling all report the same thing.
   */
  async _analyze(capture) {
    try {
      var resp = await fetch(appBase() + '/__bino/layout-state', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ state: capture.state, sources: capture.sources }),
      });
      if (!resp.ok) {
        console.warn('bino inspector: analysis failed', resp.status);
        return [];
      }
      var body = await resp.json();
      return body.findings || [];
    } catch (err) {
      console.warn('bino inspector: analysis request failed', err);
      return [];
    }
  }

  async _loadDetail(componentId) {
    var element = this._elements.get(componentId);
    try {
      this._detail = (await captureComponentDetail(element)) || { elements: [] };
    } catch (err) {
      console.warn('bino inspector: element detail failed', err);
      this._detail = { elements: [] };
    }
  }

  // --- interaction ---------------------------------------------------------

  _onRowClick(event, componentId) {
    if (event.metaKey || event.ctrlKey) {
      this._revealSource(componentId);
      return;
    }
    this._select(componentId);
  }

  _select(componentId) {
    if (this._selectedId === componentId) {
      this._selectedId = '';
      this._detail = null;
      return;
    }
    this._selectedId = componentId;
    this._detail = null;
    var element = this._elements.get(componentId);
    if (element) {
      element.scrollIntoView({ behavior: 'smooth', block: 'center' });
      this._outline(componentId, true);
    }
  }

  /** Jump to the manifest via the preview's existing click-to-source channel. */
  _revealSource(componentId) {
    var element = this._elements.get(componentId);
    var owner = element && element.closest ? element.closest('[data-bino-kind]') : null;
    if (!owner) return;
    var msg = {
      type: 'bino:revealSource',
      kind: owner.getAttribute('data-bino-kind'),
      name: owner.getAttribute('data-bino-name') || '',
      ref: owner.getAttribute('data-bino-ref') || '',
    };
    if (window.parent && window.parent !== window) {
      window.parent.postMessage(msg, '*');
    }
  }

  // --- outlines ------------------------------------------------------------

  // Outlines measure the live element rather than the snapshot: the snapshot's
  // viewport anchor goes stale as soon as the user scrolls.
  _outline(componentId, persist) {
    var element = this._elements.get(componentId);
    if (!element) return;
    if (!this._showBoxes) this._clearOverlays();
    this._overlays.push(this._makeOutline(element, persist ? 'selected' : 'hover'));
  }

  _clearHoverOutline() {
    if (this._showBoxes) return;
    this._clearOverlays();
  }

  _onToggleBoxes() {
    this._showBoxes = !this._showBoxes;
    this._clearOverlays();
    if (this._showBoxes) this._drawAllOutlines();
  }

  _drawAllOutlines() {
    var self = this;
    this._elements.forEach(function (element) {
      self._overlays.push(self._makeOutline(element, 'all'));
    });
  }

  _makeOutline(element, variant) {
    var rect = element.getBoundingClientRect();
    var box = document.createElement('div');
    box.className = 'bn-inspect-outline';
    // Light-DOM overlay: this node lives outside the component's shadow root,
    // so the geometry is inline and only the look comes from preview.css.
    box.style.cssText =
      'position:absolute;pointer-events:none;' +
      'left:' + (rect.left + window.scrollX) + 'px;' +
      'top:' + (rect.top + window.scrollY) + 'px;' +
      'width:' + rect.width + 'px;' +
      'height:' + rect.height + 'px;';
    if (variant === 'selected') box.classList.add('selected');
    document.body.appendChild(box);
    return box;
  }

  _clearOverlays() {
    this._overlays.forEach(function (box) {
      if (box.parentNode) box.parentNode.removeChild(box);
    });
    this._overlays = [];
  }

  async _onCopy() {
    if (!this._state) return;
    var payload = JSON.stringify({ state: this._state, findings: this._findings }, null, 2);
    try {
      await navigator.clipboard.writeText(payload);
    } catch (err) {
      console.warn('bino inspector: clipboard write failed', err);
    }
  }
}

customElements.define('bino-inspector', BinoInspector);
