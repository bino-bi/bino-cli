import { LitElement, html, css } from 'lit';

class BinoErrorPanel extends LitElement {
  static properties = {
    _errors: { state: true },
    _visible: { state: true },
  };

  static styles = css`
    :host {
      position: fixed;
      bottom: 0;
      left: 0;
      right: 0;
      max-height: var(--bino-panel-max-height);
      overflow-y: auto;
      background: var(--bino-warning-bg);
      border-top: 2px solid var(--bino-warning);
      font-family: var(--bino-font-sans);
      font-size: 13px;
      z-index: var(--bino-z-panel);
      box-shadow: var(--bino-shadow-panel);
      display: none;
    }
    :host([visible]) {
      display: block;
    }
    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 8px 12px;
      background: #fef3c7;
      border-bottom: 1px solid var(--bino-warning-border);
      font-weight: 600;
      color: var(--bino-warning-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 18px;
      cursor: pointer;
      color: var(--bino-warning-text);
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
      border-left: 3px solid var(--bino-warning);
    }
    li:last-child {
      border-bottom: none;
    }
    .badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge.warning {
      background: var(--bino-warning-border);
      color: #78350f;
    }
    .badge.error {
      background: #fca5a5;
      color: #7f1d1d;
    }
    .message {
      color: #78350f;
    }
  `;

  constructor() {
    super();
    this._errors = [];
    this._visible = false;
    this._scanTimer = null;
    this._observer = null;
    this._badges = [];
    this._highlightTimer = null;
    this._boundOnShowErrors = this._onShowErrors.bind(this);
    this._boundOnContentUpdated = this._debouncedScan.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-show-errors', this._boundOnShowErrors);
    document.addEventListener('bn-preview:content-updated', this._boundOnContentUpdated);
    this._startObserver();
    this._debouncedScan();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-show-errors', this._boundOnShowErrors);
    document.removeEventListener('bn-preview:content-updated', this._boundOnContentUpdated);
    if (this._observer) {
      this._observer.disconnect();
      this._observer = null;
    }
    this._removeBadges();
  }

  updated(changedProperties) {
    if (changedProperties.has('_visible')) {
      if (this._visible) {
        this.setAttribute('visible', '');
      } else {
        this.removeAttribute('visible');
      }
    }
  }

  render() {
    if (!this._visible || this._errors.length === 0) {
      return html``;
    }

    var self = this;
    var count = this._errors.length;
    return html`
      <div class="header">
        <span>${count} warning${count !== 1 ? 's' : ''} found</span>
        <button class="close-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <ul>
        ${this._errors.map(function(item, idx) {
          return html`
            <li @click=${() => self._scrollToElement(item.element)}>
              <span class="badge ${item.error.type || 'warning'}">${item.error.type || 'warning'}</span>
              <span class="message">${item.error.message || item.error.id || 'Unknown error'}</span>
            </li>
          `;
        })}
      </ul>
    `;
  }

  _startObserver() {
    var self = this;
    this._observer = new MutationObserver(function(mutations) {
      self._onMutation(mutations);
    });
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
      this._visible = true;
      this._injectBadges(results);
    } else {
      this._visible = false;
      this._removeBadges();
    }
  }

  // Highlight all list items that belong to a given source element
  _highlightForElement(el) {
    var self = this;
    var items = this.renderRoot.querySelectorAll('li');
    var firstMatch = null;
    items.forEach(function(li, idx) {
      li.classList.remove('highlighted');
      if (self._errors[idx] && self._errors[idx].element === el) {
        li.classList.add('highlighted');
        if (!firstMatch) firstMatch = li;
      }
    });
    if (firstMatch) {
      firstMatch.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
    if (this._highlightTimer) clearTimeout(this._highlightTimer);
    this._highlightTimer = setTimeout(function() {
      items.forEach(function(li) { li.classList.remove('highlighted'); });
    }, 4000);
  }

  _onClose() {
    this._visible = false;
    document.dispatchEvent(new CustomEvent('bino-panel-dismissed'));
  }

  _onShowErrors() {
    if (this._errors.length > 0) {
      this._visible = true;
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
  _injectBadges(errors) {
    this._removeBadges();
    var self = this;
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
        self._visible = true;
        self._highlightForElement(el);
      });

      var parent = el.parentNode;
      if (parent) {
        var computed = window.getComputedStyle(parent);
        if (computed.position === 'static') {
          parent.style.position = 'relative';
        }
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
