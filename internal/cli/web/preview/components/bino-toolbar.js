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
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    select {
      padding: 0.375rem 0.625rem;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: #f9fafb;
      font-size: var(--bino-font-size-md);
      color: var(--bino-text-muted);
      cursor: pointer;
      min-width: var(--bino-search-width);
    }
    select:hover {
      border-color: #9ca3af;
    }
    select:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
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
      background: #fef3c7;
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
    .assets-btn:hover, .graph-btn:hover, .explorer-btn:hover {
      background: var(--bino-surface-hover);
      border-color: #9ca3af;
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
      background: #22c55e;
      border: 1px solid #16a34a;
      color: #fff;
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .present-btn:hover {
      background: #16a34a;
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
      background: var(--bino-primary);
      border-radius: 1px;
      animation: progress-slide 1.2s ease-in-out infinite;
    }
    @keyframes progress-slide {
      0% { transform: translateX(-100%); }
      100% { transform: translateX(350%); }
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
    this._panelDismissed = false;
    this._boundOnErrorsChanged = this._onErrorsChanged.bind(this);
    this._boundOnPanelDismissed = this._onPanelDismissed.bind(this);
    this._boundOnRefreshing = this._onRefreshing.bind(this);
    this._boundOnRefreshDone = this._onRefreshDone.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-errors-changed', this._boundOnErrorsChanged);
    document.addEventListener('bino-panel-dismissed', this._boundOnPanelDismissed);
    document.addEventListener('bn-preview:refreshing', this._boundOnRefreshing);
    document.addEventListener('bn-preview:refresh-done', this._boundOnRefreshDone);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-errors-changed', this._boundOnErrorsChanged);
    document.removeEventListener('bino-panel-dismissed', this._boundOnPanelDismissed);
    document.removeEventListener('bn-preview:refreshing', this._boundOnRefreshing);
    document.removeEventListener('bn-preview:refresh-done', this._boundOnRefreshDone);
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
      <span class="title">bino preview</span>
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
      ${this.graph ? html`
        <button class="graph-btn" title="Dependency graph" @click=${this._onGraphClick}>
          <span class="graph-icon">\u229E</span>
          <span>Graph</span>
        </button>
      ` : ''}
      <button class="explorer-btn" title="Data Explorer" @click=${this._onExplorerClick}>
        <span class="explorer-icon">\u2636</span>
        <span>Explorer</span>
      </button>
      ${presURL ? html`
        <button class="present-btn" title="Open presentation" @click=${function() { window.open(presURL, '_blank'); }}>
          <span class="present-icon">\u25B6</span>
          <span>Present</span>
        </button>
      ` : ''}
      <span class="spacer"></span>
      <slot></slot>
      <div class="progress-bar ${this._refreshing ? 'active' : ''}"></div>
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
    this._refreshing = true;
  }

  _onRefreshDone() {
    this._refreshing = false;
  }
}

customElements.define('bino-toolbar', BinoToolbar);
