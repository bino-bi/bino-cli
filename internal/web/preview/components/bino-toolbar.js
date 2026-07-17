import { LitElement, html, css } from 'lit';

class BinoToolbar extends LitElement {
  static properties = {
    artifacts: { type: Array },
    documents: { type: Array },
    graph: { type: Object },
    currentPath: { type: String, attribute: 'current-path' },
    _errorCount: { state: true },
    _badgeVisible: { state: true },
    _refreshing: { state: true },
    _refreshError: { state: true },
  };

  static styles = css`
    :host {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      z-index: var(--bino-z-toolbar);
      display: flex;
      align-items: center;
      gap: var(--bino-space-md);
      background: var(--bino-surface);
      border-bottom: 1px solid var(--bino-border);
      padding: var(--bino-space-sm) var(--bino-space-md);
      font-size: var(--bino-font-size-md);
      font-family: var(--bino-font-sans);
      box-shadow: var(--bino-shadow-header);
    }
    .title {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-sm);
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    .mark {
      height: 20px;
      width: auto;
      display: block;
    }
    select {
      padding: 0.375rem 0.625rem;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface-subtle);
      font-size: var(--bino-font-size-md);
      color: var(--bino-text-muted);
      cursor: pointer;
      min-width: var(--bino-search-width);
    }
    select:hover {
      border-color: var(--bino-border-hover);
    }
    select:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-focus-ring);
    }
    .warning-badge {
      display: none;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-warning-bg);
      border: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      cursor: pointer;
      user-select: none;
    }
    .warning-badge:hover {
      background: var(--bino-yellow-300);
    }
    .warning-badge.visible {
      display: inline-flex;
    }
    .warning-icon {
      font-size: var(--bino-font-size-md);
    }
    .assets-btn, .graph-btn, .explorer-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border-light);
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .assets-btn:hover, .graph-btn:hover:not(:disabled), .explorer-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
    .graph-btn:disabled, .present-btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .present-btn:disabled {
      background: var(--bino-surface);
      border-color: var(--bino-border-light);
      color: var(--bino-text-secondary);
    }
    .assets-icon, .graph-icon, .explorer-icon {
      font-size: var(--bino-font-size-md);
    }
    .present-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-accent);
      border: 1px solid var(--bino-accent-strong);
      color: var(--bino-on-accent);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .present-btn:hover:not(:disabled) {
      background: var(--bino-accent-strong);
    }
    .present-icon {
      font-size: var(--bino-font-size-md);
    }
    .spacer {
      flex: 1;
    }
    ::slotted(*) {
      margin-left: auto;
    }
    .progress-bar {
      position: absolute;
      bottom: 0;
      left: 0;
      right: 0;
      height: 2px;
      overflow: hidden;
      opacity: 0;
      transition: opacity 0.15s ease;
    }
    .progress-bar.active {
      opacity: 1;
    }
    .progress-bar::after {
      content: '';
      display: block;
      height: 100%;
      width: 40%;
      background: var(--bino-accent);
      border-radius: 1px;
      animation: progress-slide 1.2s ease-in-out infinite;
    }
    .progress-bar.error {
      opacity: 1;
      height: 3px;
    }
    .progress-bar.error::after {
      width: 100%;
      background: var(--bino-error);
      animation: none;
    }
    @keyframes progress-slide {
      0% { transform: translateX(-100%); }
      100% { transform: translateX(350%); }
    }
    @media (prefers-reduced-motion: reduce) {
      .progress-bar::after {
        animation: none;
        width: 100%;
      }
    }
    .refresh-error-msg {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-error-bg);
      border: 1px solid var(--bino-error-border);
      color: var(--bino-error);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      max-width: 32rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      cursor: help;
    }
  `;

  constructor() {
    super();
    this.artifacts = [];
    this.documents = [];
    this.graph = null;
    this.currentPath = '/';
    this._errorCount = 0;
    this._badgeVisible = false;
    this._refreshing = false;
    this._refreshError = '';
    this._panelDismissed = false;
    this._boundOnErrorsChanged = this._onErrorsChanged.bind(this);
    this._boundOnPanelDismissed = this._onPanelDismissed.bind(this);
    this._boundOnRefreshing = this._onRefreshing.bind(this);
    this._boundOnRefreshDone = this._onRefreshDone.bind(this);
    this._boundOnRefreshError = this._onRefreshError.bind(this);
    this._boundOnNoPayload = this._onNoPayload.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-errors-changed', this._boundOnErrorsChanged);
    document.addEventListener('bino-panel-dismissed', this._boundOnPanelDismissed);
    document.addEventListener('bn-preview:refreshing', this._boundOnRefreshing);
    document.addEventListener('bn-preview:refresh-done', this._boundOnRefreshDone);
    document.addEventListener('bn-preview:refresh-error', this._boundOnRefreshError);
    document.addEventListener('bn-preview:no-payload', this._boundOnNoPayload);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-errors-changed', this._boundOnErrorsChanged);
    document.removeEventListener('bino-panel-dismissed', this._boundOnPanelDismissed);
    document.removeEventListener('bn-preview:refreshing', this._boundOnRefreshing);
    document.removeEventListener('bn-preview:refresh-done', this._boundOnRefreshDone);
    document.removeEventListener('bn-preview:refresh-error', this._boundOnRefreshError);
    document.removeEventListener('bn-preview:no-payload', this._boundOnNoPayload);
  }

  render() {
    var self = this;
    var currentPath = this.currentPath || '/';
    var artifacts = this.artifacts || [];

    // Separate ReportArtefacts and DocumentArtefacts
    var reportArts = [];
    var docArts = [];
    artifacts.forEach(function(art) {
      if (art.isDoc) {
        docArts.push(art);
      } else {
        reportArts.push(art);
      }
    });

    // Show Present button only when a specific ReportArtefact is selected
    var isReportArt = currentPath !== '/' && !currentPath.startsWith('/doc/') && !currentPath.startsWith('/pres/');
    var presURL = isReportArt ? '/pres' + currentPath : null;

    return html`
      <span class="title">
        <img class="mark" src="/__bino/assets/bino-mark.png" alt="">
        <span>bino preview</span>
      </span>
      <select id="artefact-select" @change=${this._onSelectChange}>
        <option value="/" ?selected=${currentPath === '/'}>All Pages</option>
        ${reportArts.length > 0 ? html`
          <optgroup label="Report Artefacts">
            ${reportArts.map(function(art) {
              var path = '/' + art.name;
              var label = art.title ? art.title + ' (' + art.name + ')' : art.name;
              return html`<option value=${path} ?selected=${path === currentPath}>${label}</option>`;
            })}
          </optgroup>
        ` : ''}
        ${docArts.length > 0 ? html`
          <optgroup label="Document Artefacts">
            ${docArts.map(function(art) {
              var path = '/doc/' + art.name;
              var label = art.title ? art.title + ' (' + art.name + ')' : art.name;
              return html`<option value=${path} ?selected=${path === currentPath}>${label}</option>`;
            })}
          </optgroup>
        ` : ''}
      </select>
      <span class="warning-badge ${this._badgeVisible ? 'visible' : ''}"
        title="Show warnings" @click=${this._onBadgeClick}>
        <span class="warning-icon">\u26A0</span>
        <span>${this._errorCount}</span>
      </span>
      <button class="assets-btn" title="Manifest documents" @click=${this._onAssetsClick}>
        <span class="assets-icon">\u25A6</span>
        <span>Assets (${(this.documents || []).length})</span>
      </button>
      <button class="graph-btn" ?disabled=${!this.graph}
        title=${this.graph ? 'Dependency graph' : 'Dependency graph is only available for a single artefact'}
        @click=${this._onGraphClick}>
        <span class="graph-icon">\u229E</span>
        <span>Graph</span>
      </button>
      <button class="explorer-btn" title="Data Explorer" @click=${this._onExplorerClick}>
        <span class="explorer-icon">\u2636</span>
        <span>Explorer</span>
      </button>
      <button class="present-btn" ?disabled=${!presURL}
        title=${presURL ? 'Open presentation' : 'Presentation is only available for a report artefact'}
        @click=${function() { if (presURL) window.open(presURL, '_blank'); }}>
        <span class="present-icon">\u25B6</span>
        <span>Present</span>
      </button>
      <span class="spacer"></span>
      ${this._refreshError ? html`
        <span class="refresh-error-msg" title=${this._refreshError}>
          <span>⚠</span>
          <span>Refresh failed</span>
        </span>
      ` : ''}
      <slot></slot>
      <div class="progress-bar ${this._refreshError ? 'error' : (this._refreshing ? 'active' : '')}"></div>
    `;
  }

  updated(changedProperties) {
    if (changedProperties.has('documents')) {
      document.dispatchEvent(new CustomEvent('bino-documents-changed', {
        detail: { documents: this.documents || [] }
      }));
    }
  }

  _onAssetsClick() {
    document.dispatchEvent(new CustomEvent('bino-open-assets', {
      detail: { documents: this.documents || [] }
    }));
  }

  _onGraphClick() {
    document.dispatchEvent(new CustomEvent('bino-open-graph', {
      detail: { graph: this.graph }
    }));
  }

  _onExplorerClick() {
    document.dispatchEvent(new CustomEvent('bino-open-explorer'));
  }

  _onSelectChange(e) {
    var newPath = e.target.value;
    if (newPath) {
      window.location.href = newPath;
    }
  }

  _onBadgeClick() {
    this._panelDismissed = false;
    this._badgeVisible = false;
    document.dispatchEvent(new CustomEvent('bino-show-errors'));
  }

  _onErrorsChanged(e) {
    this._errorCount = (e.detail && e.detail.count) || 0;
    if (this._panelDismissed && this._errorCount > 0) {
      this._badgeVisible = true;
    } else {
      this._badgeVisible = false;
    }
  }

  _onPanelDismissed() {
    this._panelDismissed = true;
    if (this._errorCount > 0) {
      this._badgeVisible = true;
    }
  }

  _onRefreshing() {
    console.debug('bino-toolbar: refreshing → _refreshing=true');
    this._refreshing = true;
    // Clear any prior error as soon as a new refresh starts.
    this._refreshError = '';
  }

  _onRefreshDone() {
    console.debug('bino-toolbar: refresh-done → _refreshing=false');
    this._refreshing = false;
  }

  _onRefreshError(e) {
    var message = (e && e.detail && e.detail.message) || 'Refresh failed';
    console.debug('bino-toolbar: refresh-error', message);
    this._refreshError = String(message);
    this._refreshing = false;
  }

  _onNoPayload(e) {
    // Server completed a refresh but did NOT broadcast content for this
    // view. Most common cause: the artefact at this path failed to render
    // (see CLI log for "Render blocked"/"Render failed" messages) or the
    // route no longer exists. If a per-path refresh-error already arrived,
    // keep that message; otherwise show a generic hint.
    if (this._refreshError) return;
    var path = (e && e.detail && e.detail.path) || '';
    this._refreshError =
      'No content was broadcast for ' + path +
      '. Check the bino terminal for "Render blocked" or "Render failed" messages.';
    this._refreshing = false;
  }
}

customElements.define('bino-toolbar', BinoToolbar);
