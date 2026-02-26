import { escapeHtml } from '../../shared/dom-utils.js';

var template = document.createElement('template');
template.innerHTML = `
<style>
  :host {
    position: relative;
    display: inline-block;
    font-family: var(--bino-font-sans, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif);
  }
  .search-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }
  .search-icon {
    position: absolute;
    left: 0.5rem;
    pointer-events: none;
    color: var(--bino-text-secondary, #6b7280);
    font-size: 0.8125rem;
    line-height: 1;
  }
  input {
    width: 200px;
    padding: 0.375rem 0.625rem 0.375rem 1.75rem;
    border: 1px solid var(--bino-border-light, #d1d5db);
    border-radius: var(--bino-radius, 6px);
    font-size: 0.8125rem;
    font-family: inherit;
    color: var(--bino-text, #111827);
    background: #f9fafb;
    transition: width 0.2s ease, border-color 0.15s, box-shadow 0.15s;
  }
  input:focus {
    width: 300px;
    outline: none;
    border-color: var(--bino-primary, #3b82f6);
    box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
    background: var(--bino-surface, #ffffff);
  }
  input::placeholder {
    color: var(--bino-text-secondary, #6b7280);
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
    background: var(--bino-surface, #ffffff);
    border: 1px solid var(--bino-border, #e5e7eb);
    border-radius: var(--bino-radius, 6px);
    box-shadow: 0 10px 25px rgba(0,0,0,0.1);
    z-index: 10001;
  }
  .dropdown.open {
    display: block;
  }
  .result {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 0.5rem 0.75rem;
    cursor: pointer;
    border-bottom: 1px solid #f3f4f6;
    font-size: 0.8125rem;
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
    color: var(--bino-text, #111827);
  }
  .result-kind {
    font-size: 0.6875rem;
    color: var(--bino-text-secondary, #6b7280);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .result-context {
    font-size: 0.75rem;
    color: var(--bino-text-secondary, #6b7280);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .no-results {
    padding: 0.75rem;
    text-align: center;
    font-size: 0.8125rem;
    color: var(--bino-text-secondary, #6b7280);
  }
  mark {
    background: #fef08a;
    color: inherit;
    border-radius: 2px;
    padding: 0 1px;
  }
</style>
<div class='search-wrap'>
  <span class='search-icon'>\u2315</span>
  <input type='text' placeholder='Search elements...' autocomplete='off' spellcheck='false'>
</div>
<div class='dropdown' id='dropdown'></div>
`;

class BinoSearch extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.shadowRoot.appendChild(template.content.cloneNode(true));
    this._input = this.shadowRoot.querySelector('input');
    this._dropdown = this.shadowRoot.getElementById('dropdown');
    this._results = [];
    this._activeIndex = -1;
    this._debounceTimer = null;
  }

  connectedCallback() {
    var self = this;

    this._input.addEventListener('input', function() {
      clearTimeout(self._debounceTimer);
      self._debounceTimer = setTimeout(function() {
        self._search(self._input.value.trim());
      }, 150);
    });

    this._input.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') {
        self._close();
        self._input.blur();
        return;
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        self._moveActive(1);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        self._moveActive(-1);
        return;
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        if (self._activeIndex >= 0 && self._activeIndex < self._results.length) {
          self._selectResult(self._activeIndex);
        } else if (self._results.length > 0) {
          self._selectResult(0);
        }
        return;
      }
    });

    this._input.addEventListener('focus', function() {
      if (self._input.value.trim() && self._results.length > 0) {
        self._dropdown.classList.add('open');
      }
    });

    // Close dropdown when clicking outside
    document.addEventListener('click', function(e) {
      if (!self.contains(e.target) && !self.shadowRoot.contains(e.target)) {
        self._close();
      }
    });

    // Re-index when content updates
    document.addEventListener('bn-preview:content-updated', function() {
      // Clear cached results so next search re-scans
      if (self._input.value.trim()) {
        self._search(self._input.value.trim());
      }
    });
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

    // Search text content within layout pages (limit to avoid performance issues)
    if (results.length < 50) {
      pages.forEach(function(pageEl) {
        var pageName = pageEl.getAttribute('data-bino-page') || '';
        var searchRoot = pageEl.shadowRoot || pageEl;
        var textEls = searchRoot.querySelectorAll('*');

        for (var i = 0; i < textEls.length && results.length < 50; i++) {
          var textEl = textEls[i];
          // Only check direct text nodes, skip script/style
          if (textEl.tagName === 'SCRIPT' || textEl.tagName === 'STYLE') continue;

          var childNodes = textEl.childNodes;
          for (var j = 0; j < childNodes.length; j++) {
            var node = childNodes[j];
            if (node.nodeType !== 3) continue; // Text node only
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
              break; // One match per element
            }
          }
        }
      });
    }

    this._results = results;
    this._renderResults(query);
  }

  _renderResults(query) {
    if (this._results.length === 0) {
      this._dropdown.innerHTML = '<div class="no-results">No results found</div>';
      this._dropdown.classList.add('open');
      return;
    }

    var self = this;
    var lowerQuery = query.toLowerCase();
    var html = '';

    this._results.forEach(function(result, index) {
      var activeClass = index === self._activeIndex ? ' active' : '';
      var displayName = result.type === 'text' ? result.name : escapeHtml(result.name);

      // Highlight match in name
      if (result.type !== 'text') {
        var nameIdx = result.name.toLowerCase().indexOf(lowerQuery);
        if (nameIdx !== -1) {
          displayName = escapeHtml(result.name.substring(0, nameIdx)) +
                        '<mark>' + escapeHtml(result.name.substring(nameIdx, nameIdx + query.length)) + '</mark>' +
                        escapeHtml(result.name.substring(nameIdx + query.length));
        }
      } else {
        // For text results, highlight the matched portion
        var textIdx = result.name.toLowerCase().indexOf(lowerQuery);
        if (textIdx !== -1) {
          displayName = escapeHtml(result.name.substring(0, textIdx)) +
                        '<mark>' + escapeHtml(result.name.substring(textIdx, textIdx + query.length)) + '</mark>' +
                        escapeHtml(result.name.substring(textIdx + query.length));
        } else {
          displayName = escapeHtml(result.name);
        }
      }

      html += '<div class="result' + activeClass + '" data-index="' + index + '">';
      html += '<span class="result-kind">' + escapeHtml(result.kind) + '</span>';
      html += '<span class="result-name">' + displayName + '</span>';
      html += '</div>';
    });

    this._dropdown.innerHTML = html;
    this._dropdown.classList.add('open');

    // Bind click events
    var resultEls = this._dropdown.querySelectorAll('.result');
    resultEls.forEach(function(el) {
      el.addEventListener('click', function() {
        var idx = parseInt(el.getAttribute('data-index'), 10);
        self._selectResult(idx);
      });
    });
  }

  _moveActive(delta) {
    if (this._results.length === 0) return;

    var newIndex = this._activeIndex + delta;
    if (newIndex < 0) newIndex = this._results.length - 1;
    if (newIndex >= this._results.length) newIndex = 0;
    this._activeIndex = newIndex;

    // Update visual state
    var items = this._dropdown.querySelectorAll('.result');
    items.forEach(function(el, i) {
      if (i === newIndex) {
        el.classList.add('active');
        el.scrollIntoView({ block: 'nearest' });
      } else {
        el.classList.remove('active');
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
    el.style.outline = '2px solid ' + getComputedStyle(document.documentElement).getPropertyValue('--bino-primary').trim() || '#3b82f6';
    el.style.outlineOffset = '2px';

    setTimeout(function() {
      el.style.outline = originalOutline;
      el.style.outlineOffset = originalOutlineOffset;
    }, 3000);
  }

  _close() {
    this._dropdown.classList.remove('open');
    this._activeIndex = -1;
  }
}

customElements.define('bino-search', BinoSearch);
