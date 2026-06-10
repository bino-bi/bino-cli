import { LitElement, html, css, nothing } from 'lit';

class BinoAssetsModal extends LitElement {
  static properties = {
    _documents: { state: true },
    _open: { state: true },
    _selectedDoc: { state: true },
    _filterKind: { state: true },
  };

  static styles = css`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.35);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: 12px;
      box-shadow: 0 20px 60px rgba(0, 0, 0, 0.2);
      width: 640px;
      max-width: calc(100vw - 2rem);
      max-height: 80vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover {
      color: var(--bino-text);
    }
    .kind-tabs {
      display: flex;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-wrap: wrap;
      flex-shrink: 0;
    }
    .kind-tab {
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      cursor: pointer;
      font-family: var(--bino-font-sans);
      user-select: none;
    }
    .kind-tab:hover {
      background: var(--bino-surface-hover);
    }
    .kind-tab.active {
      background: var(--bino-surface-active);
      border-color: var(--bino-primary);
      color: var(--bino-active-text);
      font-weight: 600;
    }
    .doc-list {
      flex: 1;
      overflow-y: auto;
      min-height: 0;
    }
    .doc-row {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      cursor: pointer;
    }
    .doc-row:last-child {
      border-bottom: none;
    }
    .doc-row:hover {
      background: var(--bino-surface-hover);
    }
    .doc-row.selected {
      background: var(--bino-surface-active);
    }
    .kind-badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
      background: #e0e7ff;
      color: #3730a3;
      white-space: nowrap;
    }
    .doc-info {
      min-width: 0;
      flex: 1;
    }
    .doc-name {
      font-weight: 600;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .doc-file {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .detail-panel {
      border-top: 2px solid var(--bino-border);
      padding: var(--bino-space-md) var(--bino-space-lg);
      flex-shrink: 0;
      background: #f9fafb;
    }
    .detail-row {
      display: flex;
      gap: var(--bino-space-sm);
      margin-bottom: var(--bino-space-xs);
      font-size: var(--bino-font-size-sm);
      align-items: baseline;
    }
    .detail-row:last-child {
      margin-bottom: 0;
    }
    .detail-label {
      color: var(--bino-text-secondary);
      font-weight: 600;
      flex-shrink: 0;
      min-width: 80px;
    }
    .detail-value {
      color: var(--bino-text);
      min-width: 0;
      word-break: break-word;
    }
    .pills {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs);
    }
    .pill {
      padding: 1px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      background: #e5e7eb;
      color: var(--bino-text-muted);
    }
    .pill.label-pill {
      background: #dbeafe;
      color: #1e40af;
    }
    .pill.constraint-pill {
      background: #fef3c7;
      color: #92400e;
    }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;

  constructor() {
    super();
    this._documents = [];
    this._open = false;
    this._selectedDoc = null;
    this._filterKind = '';
    this._boundOnOpen = this._onOpen.bind(this);
    this._boundOnChanged = this._onChanged.bind(this);
    this._boundOnKeydown = this._onKeydown.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-open-assets', this._boundOnOpen);
    document.addEventListener('bino-documents-changed', this._boundOnChanged);
    document.addEventListener('keydown', this._boundOnKeydown);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-open-assets', this._boundOnOpen);
    document.removeEventListener('bino-documents-changed', this._boundOnChanged);
    document.removeEventListener('keydown', this._boundOnKeydown);
  }

  render() {
    if (!this._open) return nothing;

    var self = this;
    var docs = this._filteredDocs();
    var kinds = this._uniqueKinds();

    return html`
      <div class="backdrop" @click=${this._onBackdropClick}>
        <div class="modal" @click=${this._stopPropagation}>
          <div class="modal-header">
            <h2>Manifest Documents</h2>
            <button class="close-btn" title="Close" @click=${this._close}>&times;</button>
          </div>
          <div class="kind-tabs">
            <button class="kind-tab ${this._filterKind === '' ? 'active' : ''}"
              @click=${function() { self._filterKind = ''; self._selectedDoc = null; }}>
              All (${this._documents.length})
            </button>
            ${kinds.map(function(k) {
              var count = self._countKind(k);
              return html`
                <button class="kind-tab ${self._filterKind === k ? 'active' : ''}"
                  @click=${function() { self._filterKind = k; self._selectedDoc = null; }}>
                  ${k} (${count})
                </button>
              `;
            })}
          </div>
          <div class="doc-list">
            ${docs.length === 0
              ? html`<div class="empty">No documents found</div>`
              : docs.map(function(doc) {
                  var isSelected = self._selectedDoc === doc;
                  return html`
                    <div class="doc-row ${isSelected ? 'selected' : ''}"
                      @click=${function() { self._selectedDoc = isSelected ? null : doc; }}>
                      <span class="kind-badge">${doc.kind}</span>
                      <div class="doc-info">
                        <div class="doc-name">${doc.name}</div>
                        <div class="doc-file">${doc.file}</div>
                      </div>
                    </div>
                  `;
                })
            }
          </div>
          ${this._selectedDoc ? this._renderDetail(this._selectedDoc) : nothing}
        </div>
      </div>
    `;
  }

  _renderDetail(doc) {
    var labels = doc.labels || {};
    var labelKeys = Object.keys(labels);
    var constraints = doc.constraints || [];

    return html`
      <div class="detail-panel">
        <div class="detail-row">
          <span class="detail-label">Kind</span>
          <span class="detail-value">${doc.kind}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">Name</span>
          <span class="detail-value">${doc.name}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">File</span>
          <span class="detail-value">${doc.file}</span>
        </div>
        ${labelKeys.length > 0 ? html`
          <div class="detail-row">
            <span class="detail-label">Labels</span>
            <div class="pills">
              ${labelKeys.map(function(k) {
                return html`<span class="pill label-pill">${k}: ${labels[k]}</span>`;
              })}
            </div>
          </div>
        ` : nothing}
        ${constraints.length > 0 ? html`
          <div class="detail-row">
            <span class="detail-label">Constraints</span>
            <div class="pills">
              ${constraints.map(function(c) {
                return html`<span class="pill constraint-pill">${c}</span>`;
              })}
            </div>
          </div>
        ` : nothing}
      </div>
    `;
  }

  _filteredDocs() {
    if (!this._filterKind) return this._documents;
    var kind = this._filterKind;
    return this._documents.filter(function(d) { return d.kind === kind; });
  }

  _uniqueKinds() {
    var seen = {};
    var result = [];
    this._documents.forEach(function(d) {
      if (!seen[d.kind]) {
        seen[d.kind] = true;
        result.push(d.kind);
      }
    });
    return result;
  }

  _countKind(kind) {
    var count = 0;
    this._documents.forEach(function(d) {
      if (d.kind === kind) count++;
    });
    return count;
  }

  _onOpen(e) {
    this._documents = (e.detail && e.detail.documents) || [];
    this._open = true;
    this._selectedDoc = null;
    this._filterKind = '';
  }

  _onChanged(e) {
    if (!this._open) return;
    this._documents = (e.detail && e.detail.documents) || [];
    // If selected doc was removed, deselect
    if (this._selectedDoc) {
      var sel = this._selectedDoc;
      var stillExists = this._documents.some(function(d) {
        return d.kind === sel.kind && d.name === sel.name && d.file === sel.file;
      });
      if (!stillExists) this._selectedDoc = null;
    }
  }

  _onKeydown(e) {
    if (this._open && e.key === 'Escape') {
      this._close();
    }
  }

  _onBackdropClick() {
    this._close();
  }

  _stopPropagation(e) {
    e.stopPropagation();
  }

  _close() {
    this._open = false;
    this._selectedDoc = null;
  }
}

customElements.define('bino-assets-modal', BinoAssetsModal);
