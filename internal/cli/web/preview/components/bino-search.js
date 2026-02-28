import { LitElement, html, css } from 'lit';

class BinoSearch extends LitElement {
  static properties = {
    _results: { state: true },
    _activeIndex: { state: true },
    _open: { state: true },
  };

  static styles = css`
    :host {
      position: relative;
      display: inline-block;
      font-family: var(--bino-font-sans);
    }
    .search-wrap {
      position: relative;
      display: flex;
      align-items: center;
    }
    .search-icon {
      position: absolute;
      left: var(--bino-space-sm);
      pointer-events: none;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
      line-height: 1;
    }
    input {
      width: var(--bino-search-width);
      padding: 0.375rem 0.625rem 0.375rem 1.75rem;
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-base);
      font-family: inherit;
      color: var(--bino-text);
      background: #f9fafb;
      transition: width var(--bino-transition-normal), border-color var(--bino-transition-fast), box-shadow var(--bino-transition-fast);
    }
    input:focus {
      width: var(--bino-search-width-focus);
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
      background: var(--bino-surface);
    }
    input::placeholder {
      color: var(--bino-text-secondary);
    }
    .dropdown {
      display: none;
      position: absolute;
      top: 100%;
      right: 0;
      margin-top: 4px;
      min-width: 320px;
      max-height: 400px;
      overflow-y: auto;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      box-shadow: var(--bino-shadow-dropdown);
      z-index: var(--bino-z-panel);
    }
    .dropdown.open {
      display: block;
    }
    .result {
      display: flex;
      flex-direction: column;
      gap: 2px;
      padding: var(--bino-space-sm) 0.75rem;
      cursor: pointer;
      border-bottom: 1px solid var(--bino-surface-hover);
      font-size: var(--bino-font-size-base);
      transition: background 0.1s;
    }
    .result:last-child {
      border-bottom: none;
    }
    .result:hover, .result.active {
      background: #f0f4ff;
    }
    .result-name {
      font-weight: 500;
      color: var(--bino-text);
    }
    .result-kind {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      text-transform: uppercase;
      letter-spacing: 0.03em;
    }
    .result-context {
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .no-results {
      padding: 0.75rem;
      text-align: center;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text-secondary);
    }
    mark {
      background: #fef08a;
      color: inherit;
      border-radius: 2px;
      padding: 0 1px;
    }
  `;

  constructor() {
    super();
    this._results = [];
    this._activeIndex = -1;
    this._open = false;
    this._debounceTimer = null;
    this._query = '';
  }

  connectedCallback() {
    super.connectedCallback();
    var self = this;

    // Close dropdown when clicking outside
    this._boundOutsideClick = function(e) {
      if (!self.contains(e.target) && !self.renderRoot.contains(e.target)) {
        self._close();
      }
    };
    document.addEventListener('click', this._boundOutsideClick);

    // Re-index when content updates
    this._boundContentUpdated = function() {
      if (self._query) {
        self._search(self._query);
      }
    };
    document.addEventListener('bn-preview:content-updated', this._boundContentUpdated);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('click', this._boundOutsideClick);
    document.removeEventListener('bn-preview:content-updated', this._boundContentUpdated);
  }

  render() {
    var self = this;
    return html`
      <div class="search-wrap">
        <span class="search-icon">\u2315</span>
        <input type="text" placeholder="Search elements..." autocomplete="off" spellcheck="false"
          @input=${this._onInput}
          @keydown=${this._onKeydown}
          @focus=${this._onFocus}>
      </div>
      <div class="dropdown ${this._open ? 'open' : ''}">
        ${this._results.length === 0 && this._open
          ? html`<div class="no-results">No results found</div>`
          : this._results.map(function(result, index) {
              return html`
                <div class="result ${index === self._activeIndex ? 'active' : ''}"
                  @click=${() => self._selectResult(index)}>
                  <span class="result-kind">${result.kind}</span>
                  <span class="result-name">${self._highlightMatch(result, self._query)}</span>
                </div>
              `;
            })
        }
      </div>
    `;
  }

  _highlightMatch(result, query) {
    if (!query) return result.name;
    var lowerQuery = query.toLowerCase();
    var nameIdx = result.name.toLowerCase().indexOf(lowerQuery);
    if (nameIdx === -1) return result.name;

    var before = result.name.substring(0, nameIdx);
    var match = result.name.substring(nameIdx, nameIdx + query.length);
    var after = result.name.substring(nameIdx + query.length);
    return html`${before}<mark>${match}</mark>${after}`;
  }

  _onInput(e) {
    var self = this;
    var value = e.target.value.trim();
    clearTimeout(this._debounceTimer);
    this._debounceTimer = setTimeout(function() {
      self._query = value;
      self._search(value);
    }, 150);
  }

  _onKeydown(e) {
    if (e.key === 'Escape') {
      this._close();
      e.target.blur();
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      this._moveActive(1);
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      this._moveActive(-1);
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      if (this._activeIndex >= 0 && this._activeIndex < this._results.length) {
        this._selectResult(this._activeIndex);
      } else if (this._results.length > 0) {
        this._selectResult(0);
      }
      return;
    }
  }

  _onFocus() {
    if (this._query && this._results.length > 0) {
      this._open = true;
    }
  }

  _search(query) {
    this._activeIndex = -1;

    if (!query || query.length < 2) {
      this._results = [];
      this._close();
      return;
    }

    var lowerQuery = query.toLowerCase();
    var results = [];
    var seen = new Set();

    // Search [data-bino-kind] elements
    var kindEls = document.querySelectorAll('[data-bino-kind]');
    kindEls.forEach(function(el) {
      var kind = el.getAttribute('data-bino-kind') || '';
      var name = el.getAttribute('data-bino-name') || '';
      var key = kind + ':' + name;

      if (seen.has(key)) return;

      if (kind.toLowerCase().indexOf(lowerQuery) !== -1 ||
          name.toLowerCase().indexOf(lowerQuery) !== -1) {
        seen.add(key);
        results.push({
          type: 'element',
          kind: kind,
          name: name,
          el: el
        });
      }
    });

    // Search bn-layout-page elements by data-bino-page
    var pages = document.querySelectorAll('bn-layout-page[data-bino-page]');
    pages.forEach(function(el) {
      var pageName = el.getAttribute('data-bino-page') || '';
      var key = 'page:' + pageName;

      if (seen.has(key)) return;

      if (pageName.toLowerCase().indexOf(lowerQuery) !== -1) {
        seen.add(key);
        results.push({
          type: 'page',
          kind: 'LayoutPage',
          name: pageName,
          el: el
        });
      }
    });

    // Search text content within layout pages
    if (results.length < 50) {
      pages.forEach(function(pageEl) {
        var pageName = pageEl.getAttribute('data-bino-page') || '';
        var searchRoot = pageEl.shadowRoot || pageEl;
        var textEls = searchRoot.querySelectorAll('*');

        for (var i = 0; i < textEls.length && results.length < 50; i++) {
          var textEl = textEls[i];
          if (textEl.tagName === 'SCRIPT' || textEl.tagName === 'STYLE') continue;

          var childNodes = textEl.childNodes;
          for (var j = 0; j < childNodes.length; j++) {
            var node = childNodes[j];
            if (node.nodeType !== 3) continue;
            var text = node.textContent.trim();
            if (!text || text.length < 2) continue;

            var idx = text.toLowerCase().indexOf(lowerQuery);
            if (idx !== -1) {
              var snippet = text.substring(Math.max(0, idx - 30), Math.min(text.length, idx + query.length + 30));
              var key = 'text:' + pageName + ':' + text.substring(idx, idx + Math.min(40, text.length - idx));
              if (seen.has(key)) continue;
              seen.add(key);

              results.push({
                type: 'text',
                kind: 'text in ' + pageName,
                name: snippet,
                el: textEl,
                query: query
              });
              break;
            }
          }
        }
      });
    }

    this._results = results;
    this._open = true;
  }

  _moveActive(delta) {
    if (this._results.length === 0) return;

    var newIndex = this._activeIndex + delta;
    if (newIndex < 0) newIndex = this._results.length - 1;
    if (newIndex >= this._results.length) newIndex = 0;
    this._activeIndex = newIndex;

    // Scroll active item into view
    this.updateComplete.then(() => {
      var items = this.renderRoot.querySelectorAll('.result');
      if (items[newIndex]) {
        items[newIndex].scrollIntoView({ block: 'nearest' });
      }
    });
  }

  _selectResult(index) {
    var result = this._results[index];
    if (!result || !result.el) return;

    this._close();

    // Scroll to the element
    result.el.scrollIntoView({ behavior: 'smooth', block: 'center' });

    // Highlight with outline
    var el = result.el;
    var originalOutline = el.style.outline;
    var originalOutlineOffset = el.style.outlineOffset;
    el.style.outline = '2px solid ' + (getComputedStyle(document.documentElement).getPropertyValue('--bino-primary').trim() || '#3b82f6');
    el.style.outlineOffset = '2px';

    setTimeout(function() {
      el.style.outline = originalOutline;
      el.style.outlineOffset = originalOutlineOffset;
    }, 3000);
  }

  _close() {
    this._open = false;
    this._activeIndex = -1;
  }
}

customElements.define('bino-search', BinoSearch);
