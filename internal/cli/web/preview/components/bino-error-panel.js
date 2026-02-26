import { escapeHtml } from '../../shared/dom-utils.js';

const template = document.createElement('template');
template.innerHTML = `
<style>
  :host {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    max-height: 200px;
    overflow-y: auto;
    background: var(--bino-warning-bg, #fffbeb);
    border-top: 2px solid var(--bino-warning, #f59e0b);
    font-family: var(--bino-font-sans, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif);
    font-size: 13px;
    z-index: 10001;
    box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.1);
    display: none;
  }
  :host(.visible) {
    display: block;
  }
  .header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    background: #fef3c7;
    border-bottom: 1px solid var(--bino-warning-border, #fcd34d);
    font-weight: 600;
    color: var(--bino-warning-text, #92400e);
  }
  .close-btn {
    background: none;
    border: none;
    font-size: 18px;
    cursor: pointer;
    color: var(--bino-warning-text, #92400e);
    padding: 0 4px;
  }
  .close-btn:hover {
    color: #78350f;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    padding: 8px 12px;
    border-bottom: 1px solid #fde68a;
    cursor: pointer;
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }
  li:hover {
    background: #fef3c7;
  }
  li.highlighted {
    background: #fde68a;
    border-left: 3px solid var(--bino-warning, #f59e0b);
  }
  li:last-child {
    border-bottom: none;
  }
  .badge {
    flex-shrink: 0;
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
  }
  .badge.warning {
    background: var(--bino-warning-border, #fcd34d);
    color: #78350f;
  }
  .badge.error {
    background: #fca5a5;
    color: #7f1d1d;
  }
  .message {
    color: #78350f;
  }
</style>
<div class='header'>
  <span id='count'></span>
  <button class='close-btn' title='Close'>&times;</button>
</div>
<ul id='list'></ul>
`;

class BinoErrorPanel extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.shadowRoot.appendChild(template.content.cloneNode(true));
    this._countEl = this.shadowRoot.getElementById('count');
    this._listEl = this.shadowRoot.getElementById('list');
    this._closeBtn = this.shadowRoot.querySelector('.close-btn');
    this._errors = [];
    this._scanTimer = null;
    this._observer = null;
    this._badges = [];
    this._highlightTimer = null;
  }

  connectedCallback() {
    this._closeBtn.addEventListener('click', this._onClose.bind(this));

    // Listen for "show errors" event from toolbar
    document.addEventListener('bino-show-errors', this._onShowErrors.bind(this));

    // Listen for content updates to rescan
    document.addEventListener('bn-preview:content-updated', this._debouncedScan.bind(this));

    // Start observing
    this._startObserver();

    // Initial scan
    this._debouncedScan();
  }

  disconnectedCallback() {
    if (this._observer) {
      this._observer.disconnect();
      this._observer = null;
    }
    this._removeBadges();
  }

  _startObserver() {
    this._observer = new MutationObserver(this._onMutation.bind(this));
    this._observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['has-error', 'has-errors']
    });
  }

  _onMutation(mutations) {
    var shouldScan = false;
    mutations.forEach(function(m) {
      if (m.type === 'attributes' && (m.attributeName === 'has-error' || m.attributeName === 'has-errors')) {
        shouldScan = true;
      }
      if (m.type === 'childList' && m.addedNodes.length > 0) {
        m.addedNodes.forEach(function(node) {
          if (node.nodeType === 1 && node.hasAttribute && (node.hasAttribute('has-error') || node.hasAttribute('has-errors'))) {
            shouldScan = true;
          }
          if (node.nodeType === 1 && node.querySelector && node.querySelector('[has-error], [has-errors]')) {
            shouldScan = true;
          }
        });
      }
    });
    if (shouldScan) {
      this._debouncedScan();
    }
  }

  _debouncedScan() {
    var self = this;
    if (this._scanTimer) {
      clearTimeout(this._scanTimer);
    }
    this._scanTimer = setTimeout(function() {
      self._scanForErrors();
    }, 100);
  }

  _parseErrors(attrValue) {
    if (!attrValue) return [];
    try {
      var parsed = JSON.parse(attrValue);
      return Array.isArray(parsed) ? parsed : [];
    } catch (e) {
      return [];
    }
  }

  _scanForErrors() {
    var results = [];
    var elements = document.querySelectorAll('[has-error], [has-errors]');
    var self = this;
    elements.forEach(function(el) {
      var attrValue = el.getAttribute('has-error') || el.getAttribute('has-errors');
      var errors = self._parseErrors(attrValue);
      errors.forEach(function(err) {
        results.push({ element: el, error: err });
      });
    });

    this._errors = results;

    // Dispatch count change event for toolbar badge
    document.dispatchEvent(new CustomEvent('bino-errors-changed', {
      detail: { count: results.length }
    }));

    if (results.length > 0) {
      this._showPanel(results);
      this._injectBadges(results);
    } else {
      this._hidePanel();
      this._removeBadges();
    }
  }

  _showPanel(errors) {
    this._countEl.textContent = errors.length + ' warning' + (errors.length !== 1 ? 's' : '') + ' found';
    this._listEl.innerHTML = '';
    var self = this;
    errors.forEach(function(item, idx) {
      var li = document.createElement('li');
      li._errorElement = item.element;
      li.innerHTML = '<span class="badge ' + (item.error.type || 'warning') + '">' +
        (item.error.type || 'warning') + '</span><span class="message">' +
        escapeHtml(item.error.message || item.error.id || 'Unknown error') + '</span>';
      li.addEventListener('click', function() {
        self._scrollToElement(item.element);
      });
      self._listEl.appendChild(li);
    });
    this.classList.add('visible');
  }

  // Highlight all list items that belong to a given source element
  _highlightForElement(el) {
    var self = this;
    var items = this._listEl.querySelectorAll('li');
    var firstMatch = null;
    items.forEach(function(li) {
      li.classList.remove('highlighted');
      if (li._errorElement === el) {
        li.classList.add('highlighted');
        if (!firstMatch) firstMatch = li;
      }
    });
    // Scroll the first matching item into view
    if (firstMatch) {
      firstMatch.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
    // Auto-clear highlight after 4 seconds
    if (this._highlightTimer) clearTimeout(this._highlightTimer);
    this._highlightTimer = setTimeout(function() {
      items.forEach(function(li) { li.classList.remove('highlighted'); });
    }, 4000);
  }

  _hidePanel() {
    this.classList.remove('visible');
  }

  _onClose() {
    this._hidePanel();
    document.dispatchEvent(new CustomEvent('bino-panel-dismissed'));
  }

  _onShowErrors() {
    if (this._errors.length > 0) {
      this._showPanel(this._errors);
    }
  }

  _scrollToElement(el) {
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.classList.remove('bn-error-highlight');
    void el.offsetWidth;
    el.classList.add('bn-error-highlight');
    setTimeout(function() {
      el.classList.remove('bn-error-highlight');
    }, 700);
  }

  // Inject clickable badge overlays next to elements with has-error/has-errors.
  // Badges are inserted as siblings (not children) because the error elements are
  // Web Components with Shadow DOM that won't render light DOM children.
  _injectBadges(errors) {
    this._removeBadges();
    var self = this;
    // Group errors by element
    var elementMap = new Map();
    errors.forEach(function(item, idx) {
      if (!elementMap.has(item.element)) {
        elementMap.set(item.element, []);
      }
      elementMap.get(item.element).push({ error: item.error, index: idx });
    });

    elementMap.forEach(function(items, el) {
      var badge = document.createElement('div');
      badge.className = 'bn-error-indicator-badge';
      badge.style.cssText = 'position:absolute;top:2px;right:2px;width:18px;height:18px;' +
        'background:#f59e0b;color:#fff;font-size:12px;border-radius:50%;display:flex;' +
        'align-items:center;justify-content:center;z-index:10000;cursor:pointer;' +
        'user-select:none;line-height:1;';
      badge.textContent = '\u26A0';
      badge.title = items.map(function(i) { return i.error.message || i.error.id || 'Error'; }).join('\n');
      badge.addEventListener('click', function(e) {
        e.stopPropagation();
        // Show panel and highlight errors belonging to this element
        self._showPanel(self._errors);
        self._highlightForElement(el);
      });

      // Insert badge as a sibling after the element, positioned absolutely
      // within the element's parent (which must be position:relative).
      var parent = el.parentNode;
      if (parent) {
        var computed = window.getComputedStyle(parent);
        if (computed.position === 'static') {
          parent.style.position = 'relative';
        }
        // Insert right after the element so it overlaps its top-right corner
        el.insertAdjacentElement('afterend', badge);
      }
      self._badges.push(badge);
    });
  }

  _removeBadges() {
    this._badges.forEach(function(badge) {
      if (badge.parentNode) {
        badge.parentNode.removeChild(badge);
      }
    });
    this._badges = [];
  }
}

customElements.define('bino-error-panel', BinoErrorPanel);
