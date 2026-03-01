import { LitElement, html, css, nothing } from 'lit';

var PAGE_SIZES = [25, 50, 100, 250];

class BinoDataExplorer extends LitElement {
  static properties = {
    _open: { state: true },
    _metadata: { state: true },
    _sql: { state: true },
    _result: { state: true },
    _summarizeResult: { state: true },
    _loading: { state: true },
    _error: { state: true },
    _page: { state: true },
    _pageSize: { state: true },
    _activeTab: { state: true },
    _expandedSource: { state: true },
    _refreshing: { state: true },
  };

  static styles = css`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.5);
      z-index: var(--bino-z-modal);
      display: flex;
      flex-direction: column;
    }
    .explorer {
      display: flex;
      flex-direction: column;
      width: 100%;
      height: 100%;
      background: var(--bino-surface);
    }
    .explorer-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: var(--bino-surface);
    }
    .explorer-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .header-actions {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
    }
    .refresh-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 4px 10px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-secondary);
    }
    .refresh-btn:hover {
      background: var(--bino-surface-hover);
      border-color: #9ca3af;
      color: var(--bino-text);
    }
    .refresh-btn.refreshing {
      opacity: 0.6;
      cursor: not-allowed;
    }
    .refresh-icon {
      display: inline-block;
      font-size: var(--bino-font-size-md);
      line-height: 1;
    }
    .refresh-btn.refreshing .refresh-icon {
      animation: spin 0.8s linear infinite;
    }
    @keyframes spin {
      to { transform: rotate(360deg); }
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
    .explorer-body {
      display: flex;
      flex: 1;
      min-height: 0;
      overflow: hidden;
    }
    .sidebar {
      width: 280px;
      min-width: 280px;
      border-right: 1px solid var(--bino-border);
      overflow-y: auto;
      background: #fafbfc;
      flex-shrink: 0;
    }
    .sidebar-section {
      padding: var(--bino-space-sm) 0;
    }
    .sidebar-title {
      padding: var(--bino-space-xs) var(--bino-space-md);
      font-size: var(--bino-font-size-xs);
      font-weight: 700;
      text-transform: uppercase;
      color: var(--bino-text-secondary);
      letter-spacing: 0.05em;
    }
    .sidebar-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 5px var(--bino-space-md);
      cursor: pointer;
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
      border-left: 3px solid transparent;
    }
    .sidebar-item:hover {
      background: var(--bino-surface-hover);
      border-left-color: var(--bino-border-light);
    }
    .sidebar-item-name {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-weight: 500;
    }
    .sidebar-item-badge {
      flex-shrink: 0;
      padding: 1px 5px;
      border-radius: 3px;
      font-size: 10px;
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge-source {
      background: #fee2e2;
      color: #991b1b;
    }
    .badge-dataset {
      background: #ffedd5;
      color: #9a3412;
    }
    .sidebar-info-btn {
      flex-shrink: 0;
      background: none;
      border: none;
      cursor: pointer;
      color: var(--bino-text-secondary);
      font-size: 14px;
      padding: 0 2px;
      line-height: 1;
    }
    .sidebar-info-btn:hover {
      color: var(--bino-primary);
    }
    .column-list {
      padding: 2px var(--bino-space-md) var(--bino-space-xs) calc(var(--bino-space-md) + 12px);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .column-entry {
      display: flex;
      justify-content: space-between;
      padding: 1px 0;
    }
    .column-name {
      font-weight: 500;
      color: var(--bino-text-muted);
    }
    .column-type {
      font-style: italic;
      color: var(--bino-text-secondary);
    }
    .main-panel {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-width: 0;
      overflow: hidden;
    }
    .editor-area {
      display: flex;
      flex-direction: column;
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .sql-editor {
      width: 100%;
      min-height: 100px;
      max-height: 200px;
      padding: var(--bino-space-sm) var(--bino-space-md);
      border: none;
      outline: none;
      resize: vertical;
      font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
      font-size: var(--bino-font-size-sm);
      line-height: 1.5;
      background: #1e1e2e;
      color: #cdd6f4;
      box-sizing: border-box;
    }
    .sql-editor::placeholder {
      color: #6c7086;
    }
    .editor-toolbar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      background: #f1f3f5;
      border-top: 1px solid var(--bino-border);
    }
    .editor-btn {
      padding: 4px 12px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-muted);
    }
    .editor-btn:hover {
      background: var(--bino-surface-hover);
      border-color: #9ca3af;
    }
    .editor-btn.primary {
      background: var(--bino-primary);
      border-color: var(--bino-primary);
      color: #fff;
    }
    .editor-btn.primary:hover {
      background: var(--bino-primary-hover);
    }
    .editor-btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .editor-shortcut {
      font-size: 10px;
      color: var(--bino-text-secondary);
      margin-left: auto;
    }
    .results-area {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-height: 0;
      overflow: hidden;
    }
    .tab-bar {
      display: flex;
      gap: 0;
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: #f9fafb;
    }
    .tab-btn {
      padding: var(--bino-space-xs) var(--bino-space-md);
      border: none;
      background: none;
      font-size: var(--bino-font-size-sm);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-secondary);
      cursor: pointer;
      border-bottom: 2px solid transparent;
      font-weight: 500;
    }
    .tab-btn:hover {
      color: var(--bino-text);
      background: var(--bino-surface-hover);
    }
    .tab-btn.active {
      color: var(--bino-primary);
      border-bottom-color: var(--bino-primary);
      font-weight: 600;
    }
    .table-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: var(--bino-font-size-sm);
    }
    thead {
      position: sticky;
      top: 0;
      z-index: 1;
    }
    th {
      background: #f1f3f5;
      padding: 6px 12px;
      text-align: left;
      font-weight: 600;
      color: var(--bino-text-muted);
      border-bottom: 2px solid var(--bino-border);
      white-space: nowrap;
      font-size: var(--bino-font-size-xs);
    }
    .col-type {
      font-weight: 400;
      color: var(--bino-text-secondary);
      font-style: italic;
      margin-left: 4px;
    }
    td {
      padding: 4px 12px;
      border-bottom: 1px solid var(--bino-border);
      max-width: 300px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      color: var(--bino-text);
    }
    tr:nth-child(even) {
      background: #fafbfc;
    }
    tr:hover {
      background: var(--bino-surface-hover);
    }
    .pagination {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: #f9fafb;
    }
    .pagination button {
      padding: 2px 8px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      cursor: pointer;
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
    }
    .pagination button:hover:not(:disabled) {
      background: var(--bino-surface-hover);
    }
    .pagination button:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }
    .pagination select {
      padding: 2px 4px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
      background: var(--bino-surface);
    }
    .status-bar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: #f9fafb;
      flex-shrink: 0;
    }
    .error-msg {
      padding: var(--bino-space-md);
      color: var(--bino-error);
      font-size: var(--bino-font-size-sm);
      white-space: pre-wrap;
      word-break: break-word;
    }
    .loading-msg {
      padding: var(--bino-space-lg);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
    }
    .empty-state {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
    .duration {
      color: var(--bino-text-secondary);
    }
  `;

  constructor() {
    super();
    this._open = false;
    this._metadata = null;
    this._sql = '';
    this._result = null;
    this._summarizeResult = null;
    this._loading = false;
    this._error = '';
    this._page = 0;
    this._pageSize = 50;
    this._activeTab = 'results';
    this._expandedSource = null;
    this._refreshing = false;
    this._boundOnOpen = this._onOpen.bind(this);
    this._boundOnKeydown = this._onKeydown.bind(this);
    this._boundOnDocsChanged = this._onDocsChanged.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-open-explorer', this._boundOnOpen);
    document.addEventListener('keydown', this._boundOnKeydown);
    document.addEventListener('bino-documents-changed', this._boundOnDocsChanged);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-open-explorer', this._boundOnOpen);
    document.removeEventListener('keydown', this._boundOnKeydown);
    document.removeEventListener('bino-documents-changed', this._boundOnDocsChanged);
  }

  render() {
    if (!this._open) return nothing;

    var self = this;
    return html`
      <div class="backdrop">
        <div class="explorer">
          <div class="explorer-header">
            <h2>Data Explorer</h2>
            <div class="header-actions">
              <button class="refresh-btn ${this._refreshing ? 'refreshing' : ''}"
                title="Refresh data sources and datasets"
                ?disabled=${this._refreshing}
                @click=${this._onRefresh}>
                <span class="refresh-icon">\u21BB</span>
                <span>${this._refreshing ? 'Refreshing...' : 'Refresh'}</span>
              </button>
              <button class="close-btn" title="Close (Esc)" @click=${this._close}>&times;</button>
            </div>
          </div>
          <div class="explorer-body">
            ${this._renderSidebar()}
            ${this._renderMainPanel()}
          </div>
        </div>
      </div>
    `;
  }

  _renderSidebar() {
    var self = this;
    var meta = this._metadata;
    if (!meta) {
      return html`<div class="sidebar"><div class="loading-msg">Loading...</div></div>`;
    }

    return html`
      <div class="sidebar">
        ${meta.sources && meta.sources.length > 0 ? html`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSources (${meta.sources.length})</div>
            ${meta.sources.map(function(src) {
              var isExpanded = self._expandedSource === src.name;
              return html`
                <div>
                  <div class="sidebar-item" @click=${function() { self._selectSource(src.name); }}>
                    <span class="sidebar-item-name">${src.name}</span>
                    ${src.type ? html`<span class="sidebar-item-badge badge-source">${src.type}</span>` : nothing}
                    <button class="sidebar-info-btn" title="Show columns"
                      @click=${function(e) { e.stopPropagation(); self._toggleSourceInfo(src.name); }}>
                      ${isExpanded ? '\u25B4' : '\u25BE'}
                    </button>
                  </div>
                  ${isExpanded && src.columns && src.columns.length > 0 ? html`
                    <div class="column-list">
                      ${src.columns.map(function(col) {
                        return html`
                          <div class="column-entry">
                            <span class="column-name">${col.name}</span>
                            <span class="column-type">${col.type}</span>
                          </div>
                        `;
                      })}
                    </div>
                  ` : nothing}
                </div>
              `;
            })}
          </div>
        ` : nothing}
        ${meta.datasets && meta.datasets.length > 0 ? html`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSets (${meta.datasets.length})</div>
            ${meta.datasets.map(function(ds) {
              return html`
                <div class="sidebar-item" @click=${function() { self._selectDataset(ds.name); }}>
                  <span class="sidebar-item-name">${ds.name}</span>
                  <span class="sidebar-item-badge badge-dataset">set</span>
                </div>
              `;
            })}
          </div>
        ` : nothing}
        ${(!meta.sources || meta.sources.length === 0) && (!meta.datasets || meta.datasets.length === 0)
          ? html`<div class="empty-state">No data sources found</div>`
          : nothing}
      </div>
    `;
  }

  _renderMainPanel() {
    var self = this;
    return html`
      <div class="main-panel">
        <div class="editor-area">
          <textarea class="sql-editor"
            placeholder="Enter SQL query... (Ctrl+Enter to run)"
            .value=${this._sql}
            @input=${function(e) { self._sql = e.target.value; }}
            @keydown=${this._onEditorKeydown.bind(this)}
            spellcheck="false"
          ></textarea>
          <div class="editor-toolbar">
            <button class="editor-btn primary" ?disabled=${this._loading} @click=${this._runQuery.bind(this)}>Run</button>
            <button class="editor-btn" ?disabled=${this._loading || !this._sql.trim()} @click=${this._runSummarize.bind(this)}>Summarize</button>
            <button class="editor-btn" @click=${this._clearEditor.bind(this)}>Clear</button>
            <span class="editor-shortcut">${navigator.platform.includes('Mac') ? '\u2318' : 'Ctrl'}+Enter to run</span>
          </div>
        </div>
        <div class="results-area">
          <div class="tab-bar">
            <button class="tab-btn ${this._activeTab === 'results' ? 'active' : ''}"
              @click=${function() { self._activeTab = 'results'; }}>Results</button>
            <button class="tab-btn ${this._activeTab === 'summarize' ? 'active' : ''}"
              @click=${function() { self._activeTab = 'summarize'; }}>Summarize</button>
          </div>
          ${this._activeTab === 'results' ? this._renderResults() : this._renderSummarizeTab()}
        </div>
      </div>
    `;
  }

  _renderResults() {
    if (this._loading) {
      return html`<div class="loading-msg">Executing query...</div>`;
    }
    if (this._error) {
      return html`<div class="error-msg">${this._error}</div>`;
    }
    if (!this._result) {
      return html`<div class="empty-state">Run a query to see results</div>`;
    }
    if (this._result.error) {
      return html`
        <div class="error-msg">${this._result.error}</div>
        ${this._result.durationMs != null ? html`<div class="status-bar"><span class="duration">${this._result.durationMs}ms</span></div>` : nothing}
      `;
    }

    var self = this;
    var cols = this._result.columns || [];
    var rows = this._result.rows || [];
    var totalRows = this._result.totalRows;
    var totalPages = typeof totalRows === 'number' ? Math.ceil(totalRows / this._pageSize) : null;

    return html`
      <div class="table-container">
        ${cols.length === 0
          ? html`<div class="empty-state">No columns returned</div>`
          : html`
            <table>
              <thead>
                <tr>
                  ${cols.map(function(col) {
                    return html`<th>${col.name}<span class="col-type">${col.type}</span></th>`;
                  })}
                </tr>
              </thead>
              <tbody>
                ${rows.map(function(row) {
                  return html`<tr>${row.map(function(cell) {
                    return html`<td title="${cell != null ? String(cell) : ''}">${cell != null ? String(cell) : ''}</td>`;
                  })}</tr>`;
                })}
              </tbody>
            </table>
          `}
      </div>
      <div class="pagination">
        <button ?disabled=${this._page === 0} @click=${function() { self._page--; self._rerunQuery(); }}>\u2190</button>
        <span>Page ${this._page + 1}${totalPages != null ? ' of ' + totalPages : ''}</span>
        <button ?disabled=${totalPages != null && this._page >= totalPages - 1} @click=${function() { self._page++; self._rerunQuery(); }}>\u2192</button>
        <span>|</span>
        <select .value=${String(this._pageSize)} @change=${function(e) { self._pageSize = parseInt(e.target.value, 10); self._page = 0; self._rerunQuery(); }}>
          ${PAGE_SIZES.map(function(s) {
            return html`<option value=${s} ?selected=${s === self._pageSize}>${s} rows</option>`;
          })}
        </select>
        <span class="duration">${this._result.durationMs != null ? this._result.durationMs + 'ms' : ''}</span>
        ${typeof totalRows === 'number' ? html`<span>${totalRows} total rows</span>` : nothing}
      </div>
    `;
  }

  _renderSummarizeTab() {
    if (this._loading) {
      return html`<div class="loading-msg">Running SUMMARIZE...</div>`;
    }
    if (!this._summarizeResult) {
      return html`<div class="empty-state">Click "Summarize" to see column statistics</div>`;
    }
    if (this._summarizeResult.error) {
      return html`<div class="error-msg">${this._summarizeResult.error}</div>`;
    }

    var cols = this._summarizeResult.columns || [];
    var rows = this._summarizeResult.rows || [];

    return html`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              ${cols.map(function(col) {
                return html`<th>${col.name}<span class="col-type">${col.type}</span></th>`;
              })}
            </tr>
          </thead>
          <tbody>
            ${rows.map(function(row) {
              return html`<tr>${row.map(function(cell) {
                return html`<td title="${cell != null ? String(cell) : ''}">${cell != null ? String(cell) : ''}</td>`;
              })}</tr>`;
            })}
          </tbody>
        </table>
      </div>
      <div class="status-bar">
        <span class="duration">${this._summarizeResult.durationMs != null ? this._summarizeResult.durationMs + 'ms' : ''}</span>
      </div>
    `;
  }

  _onOpen() {
    this._open = true;
    this._fetchMetadata();
  }

  _close() {
    this._open = false;
  }

  _onKeydown(e) {
    if (this._open && e.key === 'Escape') {
      this._close();
    }
  }

  _onDocsChanged() {
    if (this._open) {
      this._fetchMetadata();
    }
  }

  _onRefresh() {
    if (this._refreshing) return;
    this._refreshing = true;
    var self = this;
    this._fetchMetadata(function() {
      self._refreshing = false;
    });
  }

  _onEditorKeydown(e) {
    // Tab inserts spaces
    if (e.key === 'Tab') {
      e.preventDefault();
      var textarea = e.target;
      var start = textarea.selectionStart;
      var end = textarea.selectionEnd;
      this._sql = this._sql.substring(0, start) + '  ' + this._sql.substring(end);
      this.updateComplete.then(function() {
        textarea.selectionStart = textarea.selectionEnd = start + 2;
      });
      return;
    }
    // Ctrl/Cmd+Enter runs query
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      this._runQuery();
    }
  }

  _selectSource(name) {
    this._sql = 'SELECT * FROM "' + name + '"';
    this._page = 0;
    this._activeTab = 'results';
    this._runQuery();
  }

  _selectDataset(name) {
    this._sql = 'SELECT * FROM "' + name + '"';
    this._page = 0;
    this._activeTab = 'results';
    this._runQuery();
  }

  _toggleSourceInfo(name) {
    this._expandedSource = this._expandedSource === name ? null : name;
  }

  _clearEditor() {
    this._sql = '';
    this._result = null;
    this._summarizeResult = null;
    this._error = '';
    this._page = 0;
  }

  _rerunQuery() {
    if (this._sql.trim()) {
      this._runQuery();
    }
  }

  _runQuery() {
    var sqlText = this._sql.trim();
    if (!sqlText) return;

    var self = this;
    this._loading = true;
    this._error = '';
    this._activeTab = 'results';

    fetch('/__explorer/query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        sql: sqlText,
        limit: this._pageSize,
        offset: this._page * this._pageSize,
      }),
    })
      .then(function(resp) { return resp.json(); })
      .then(function(data) {
        self._result = data;
        self._loading = false;
      })
      .catch(function(err) {
        self._error = err.message || 'Query failed';
        self._loading = false;
      });
  }

  _runSummarize() {
    var sqlText = this._sql.trim();
    if (!sqlText) return;

    var self = this;
    this._loading = true;
    this._error = '';
    this._activeTab = 'summarize';

    fetch('/__explorer/summarize', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sql: sqlText }),
    })
      .then(function(resp) { return resp.json(); })
      .then(function(data) {
        self._summarizeResult = data;
        self._loading = false;
      })
      .catch(function(err) {
        self._error = err.message || 'Summarize failed';
        self._loading = false;
      });
  }

  _fetchMetadata(done) {
    var self = this;
    fetch('/__explorer/metadata')
      .then(function(resp) { return resp.json(); })
      .then(function(data) {
        self._metadata = data;
        if (done) done();
      })
      .catch(function(err) {
        console.error('explorer: fetch metadata failed', err);
        if (done) done();
      });
  }
}

customElements.define('bino-data-explorer', BinoDataExplorer);
