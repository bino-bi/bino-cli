import { escapeHtml } from '../../shared/dom-utils.js';

const template = document.createElement('template');
template.innerHTML = `
<style>
  :host {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 10000;
    display: flex;
    align-items: center;
    gap: 1rem;
    background: var(--bino-surface, #ffffff);
    border-bottom: 1px solid var(--bino-border, #e5e7eb);
    padding: 0.5rem 1rem;
    font-size: 0.875rem;
    font-family: var(--bino-font-sans, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif);
    box-shadow: var(--bino-shadow-header, 0 1px 3px rgba(0,0,0,0.05));
  }
  .title {
    font-weight: 600;
    color: var(--bino-text-muted, #374151);
  }
  select {
    padding: 0.375rem 0.625rem;
    border-radius: var(--bino-radius, 6px);
    border: 1px solid var(--bino-border-light, #d1d5db);
    background: #f9fafb;
    font-size: 0.875rem;
    color: var(--bino-text-muted, #374151);
    cursor: pointer;
    min-width: 200px;
  }
  select:hover {
    border-color: #9ca3af;
  }
  select:focus {
    outline: none;
    border-color: var(--bino-primary, #3b82f6);
    box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
  }
  .warning-badge {
    display: none;
    align-items: center;
    gap: 0.25rem;
    padding: 0.25rem 0.625rem;
    border-radius: 999px;
    background: var(--bino-warning-bg, #fffbeb);
    border: 1px solid var(--bino-warning-border, #fcd34d);
    color: var(--bino-warning-text, #92400e);
    font-size: 0.75rem;
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
    font-size: 0.875rem;
  }
  .spacer {
    flex: 1;
  }
  ::slotted(*) {
    margin-left: auto;
  }
</style>
<span class='title'>bino preview</span>
<select id='artefact-select'></select>
<span class='warning-badge' id='warning-badge' title='Show warnings'>
  <span class='warning-icon'>\u26A0</span>
  <span id='warning-count'>0</span>
</span>
<span class='spacer'></span>
<slot></slot>
`;

class BinoToolbar extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.shadowRoot.appendChild(template.content.cloneNode(true));
    this._select = this.shadowRoot.getElementById('artefact-select');
    this._badge = this.shadowRoot.getElementById('warning-badge');
    this._countEl = this.shadowRoot.getElementById('warning-count');
    this._errorCount = 0;
    this._panelDismissed = false;
  }

  connectedCallback() {
    this._renderOptions();
    this._select.addEventListener('change', this._onSelectChange.bind(this));
    this._badge.addEventListener('click', this._onBadgeClick.bind(this));

    // Listen for error count changes from the error panel
    document.addEventListener('bino-errors-changed', this._onErrorsChanged.bind(this));
    document.addEventListener('bino-panel-dismissed', this._onPanelDismissed.bind(this));
  }

  static get observedAttributes() {
    return ['artefacts', 'current-path'];
  }

  attributeChangedCallback() {
    this._renderOptions();
  }

  _renderOptions() {
    var artefactsAttr = this.getAttribute('artefacts');
    var currentPath = this.getAttribute('current-path') || '/';
    var artefacts = [];
    try {
      artefacts = JSON.parse(artefactsAttr || '[]');
    } catch (e) {
      artefacts = [];
    }

    var html = '';

    // "All Pages" option
    var allSelected = currentPath === '/' ? ' selected' : '';
    html += '<option value="/"' + allSelected + '>All Pages</option>';

    // Separate ReportArtefacts and DocumentArtefacts
    var reportArts = [];
    var docArts = [];
    artefacts.forEach(function(art) {
      if (art.isDoc) {
        docArts.push(art);
      } else {
        reportArts.push(art);
      }
    });

    if (reportArts.length > 0) {
      html += '<optgroup label="Report Artefacts">';
      reportArts.forEach(function(art) {
        var path = '/' + art.name;
        var selected = path === currentPath ? ' selected' : '';
        var label = art.name;
        if (art.title) {
          label = art.title + ' (' + art.name + ')';
        }
        html += '<option value="' + escapeHtml(path) + '"' + selected + '>' + escapeHtml(label) + '</option>';
      });
      html += '</optgroup>';
    }

    if (docArts.length > 0) {
      html += '<optgroup label="Document Artefacts">';
      docArts.forEach(function(art) {
        var path = '/doc/' + art.name;
        var selected = path === currentPath ? ' selected' : '';
        var label = art.name;
        if (art.title) {
          label = art.title + ' (' + art.name + ')';
        }
        html += '<option value="' + escapeHtml(path) + '"' + selected + '>' + escapeHtml(label) + '</option>';
      });
      html += '</optgroup>';
    }

    this._select.innerHTML = html;
  }

  _onSelectChange(e) {
    var newPath = e.target.value;
    if (newPath) {
      window.location.href = newPath;
    }
  }

  _onBadgeClick() {
    this._panelDismissed = false;
    this._badge.classList.remove('visible');
    document.dispatchEvent(new CustomEvent('bino-show-errors'));
  }

  _onErrorsChanged(e) {
    this._errorCount = (e.detail && e.detail.count) || 0;
    this._countEl.textContent = this._errorCount;
    // Only show badge when panel was dismissed and there are errors
    if (this._panelDismissed && this._errorCount > 0) {
      this._badge.classList.add('visible');
    } else {
      this._badge.classList.remove('visible');
    }
  }

  _onPanelDismissed() {
    this._panelDismissed = true;
    if (this._errorCount > 0) {
      this._badge.classList.add('visible');
    }
  }
}

customElements.define('bino-toolbar', BinoToolbar);
