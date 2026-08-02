import { LitElement, html, css } from 'lit';
import { decodeBase64, swapContext } from '../shared/dom-utils.js';
import { createFitScaler } from './fit-scale.js';
import './components/bino-control-panel.js';

var config = window.__binoServeConfig || {};
var routes = config.routes || {};
var queryParams = config.queryParams || [];
var missingParams = config.missingParams || [];
var currentPath = config.currentPath || '/';
var currentURL = config.currentURL || '/';
var initialContextBase64 = config.initialContextBase64 || '';

/**
 * bino-serve-shell: top-level layout that renders the sidebar + outlet.
 *
 * The shell owns the responsive state: >=1024px the panel is pinned in-flow
 * (collapsible), below that it becomes an off-canvas drawer with a scrim.
 * The document never scrolls — #outlet owns all report scrolling.
 */
class BinoServeShell extends LitElement {
  static properties = {
    narrow: { type: Boolean, reflect: true },
    sidebarOpen: { type: Boolean, reflect: true, attribute: 'sidebar-open' },
    chromeless: { type: Boolean, reflect: true },
  };

  static styles = css`
    :host {
      display: flex;
      width: 100%;
      height: 100%;
    }
    #outlet {
      flex: 1;
      min-width: 0;
      overflow: auto;
      -webkit-overflow-scrolling: touch;
      overscroll-behavior: contain;
    }
    :host([narrow][sidebar-open]) #outlet {
      overflow: hidden;
    }
    #sidebar-toggle {
      position: fixed;
      top: calc(var(--bino-space-sm) + env(safe-area-inset-top, 0px));
      left: calc(var(--bino-space-sm) + env(safe-area-inset-left, 0px));
      width: 44px;
      height: 44px;
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 0;
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      background: var(--bino-surface);
      color: var(--bino-text-muted);
      box-shadow: var(--bino-shadow-header);
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;
    }
    #sidebar-toggle:hover {
      background: var(--bino-surface-hover);
    }
    :host([chromeless]) #sidebar-toggle {
      display: none;
    }
    #scrim {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-panel);
      opacity: 0;
      pointer-events: none;
      transition: opacity var(--bino-transition-normal);
    }
    :host([narrow][sidebar-open]) #scrim {
      opacity: 1;
      pointer-events: auto;
    }
    @media print {
      #sidebar-toggle,
      #scrim {
        display: none !important;
      }
    }
  `;

  constructor() {
    super();
    this.narrow = false;
    this.sidebarOpen = true;
    this.chromeless = false;
  }

  render() {
    return html`
      <bino-control-panel id="panel"></bino-control-panel>
      <div id="outlet"><slot></slot></div>
      <div id="scrim" @click=${this._closeSidebar}></div>
      <button id="sidebar-toggle" type="button"
        aria-controls="panel"
        aria-expanded=${this.sidebarOpen ? 'true' : 'false'}
        aria-label=${this.sidebarOpen ? 'Close sidebar' : 'Open sidebar'}
        @click=${this._toggleSidebar}>
        ${this.sidebarOpen
          ? html`<svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M5 5l10 10M15 5L5 15" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"/>
            </svg>`
          : html`<svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M3.5 5.5h13M3.5 10h13M3.5 14.5h13" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"/>
            </svg>`}
      </button>
    `;
  }

  firstUpdated() {
    this._controlPanel = this.renderRoot.querySelector('bino-control-panel');
    this._outlet = this.renderRoot.getElementById('outlet');
    this._fit = createFitScaler(this._outlet);

    // Set initial config on the control panel
    if (this._controlPanel) {
      this._controlPanel.updateConfig({
        routes: routes,
        queryParams: queryParams,
        missingParams: missingParams,
        currentPath: currentPath
      });
    }
    this._syncChrome();

    // Responsive mode: pinned sidebar >=1024px, off-canvas drawer below
    this._mq = window.matchMedia('(min-width: 1024px)');
    this._mq.addEventListener('change', this._syncMode.bind(this));
    this._syncMode();

    document.addEventListener('keydown', this._onKeydown.bind(this));

    // Listen for control panel events
    this.addEventListener('bino-apply-params', this._onApplyParams.bind(this));
    this.addEventListener('bino-navigate', this._onNavigate.bind(this));

    // Handle initial content
    this._initContent();
  }

  _syncMode() {
    var wide = this._mq.matches;
    this.narrow = !wide;
    // Breakpoint defaults: pinned starts open, drawer starts closed
    this.sidebarOpen = wide;
    if (this._controlPanel) {
      this._controlPanel.mode = wide ? 'pinned' : 'drawer';
      this._controlPanel.open = this.sidebarOpen;
    }
  }

  _syncChrome() {
    this.chromeless = Object.keys(routes).length === 0 && queryParams.length === 0;
  }

  _toggleSidebar() {
    this.sidebarOpen = !this.sidebarOpen;
    if (this._controlPanel) {
      this._controlPanel.open = this.sidebarOpen;
      if (this.narrow && this.sidebarOpen) {
        this._controlPanel.focusFirst();
      }
    }
  }

  _closeSidebar() {
    if (!this.sidebarOpen) return;
    this.sidebarOpen = false;
    if (this._controlPanel) {
      this._controlPanel.open = false;
    }
    var toggle = this.renderRoot.getElementById('sidebar-toggle');
    if (toggle) toggle.focus();
  }

  _onKeydown(e) {
    if (e.key === 'Escape' && this.narrow && this.sidebarOpen) {
      this._closeSidebar();
    }
  }

  _initContent() {
    // If we have missing params, show the missing params message (no report to render)
    if (missingParams && missingParams.length > 0) {
      if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', this._showMissingParamsMessage.bind(this));
      } else {
        this._showMissingParamsMessage();
      }
      return;
    }

    // Wait for template engine to become ready, then inject initial content
    var self = this;
    import('../shared/dom-utils.js').then(function(mod) {
      mod.waitForEngine().then(function() {
        self._injectInitialContent();
      });
    });
  }

  _injectInitialContent() {
    if (!initialContextBase64) return;
    var contextHtml = decodeBase64(initialContextBase64);
    var parser = new DOMParser();
    // swapContext operates on the document-level bn-context inside the outlet
    swapContext(contextHtml, parser);
    initialContextBase64 = null;
    this._fit.rebind();
  }

  _showMissingParamsMessage() {
    // Remove existing report content and banners
    var bnContext = document.querySelector('bn-context');
    if (bnContext) {
      bnContext.remove();
    }
    var existingBanner = this.querySelector('.bino-missing-params-banner');
    if (existingBanner) existingBanner.remove();

    // Create banner as light DOM child of the shell so it slots into the outlet
    var banner = document.createElement('div');
    banner.className = 'bino-missing-params-banner';
    banner.innerHTML =
      '<div class="bino-missing-icon">⚠</div>' +
      '<div class="bino-missing-text">' +
      '<strong>Required parameters missing</strong>' +
      '<p>Please fill in the required fields marked with <span class="required">*</span> to view the report.</p>' +
      '</div>';
    this.appendChild(banner);
  }

  _onApplyParams(e) {
    if (this.narrow) this._closeSidebar();
    var params = e.detail.params;
    var newURL = currentPath;
    var queryString = params.toString();
    if (queryString) {
      newURL += '?' + queryString;
    }
    this._navigateTo(newURL);
  }

  _onNavigate(e) {
    if (this.narrow) this._closeSidebar();
    var path = e.detail.path;
    this._navigateTo(path);
  }

  _navigateTo(url) {
    history.pushState({ url: url }, '', url);
    this._loadContent(url);
  }

  _loadContent(url) {
    var self = this;
    var context = document.querySelector('bn-context');
    if (context) {
      context.style.opacity = '0.5';
    }

    if (this._controlPanel) {
      this._controlPanel.setLoading(true);
    }

    var parser = new DOMParser();

    fetch(url, {
      headers: { 'X-Requested-With': 'bino-serve' }
    })
    .then(function(response) {
      if (!response.ok) {
        throw new Error('HTTP ' + response.status);
      }
      return response.text();
    })
    .then(function(responseHtml) {
      var doc = parser.parseFromString(responseHtml, 'text/html');

      // Extract the config script data from the new page
      var configEl = doc.getElementById('bino-serve-config');
      if (!configEl) {
        console.error('bino: no config script found in response');
        if (self._controlPanel) self._controlPanel.setLoading(false);
        return;
      }

      var configText = configEl.textContent;
      var configMatch = configText.match(/window\.__binoServeConfig\s*=\s*(\{[\s\S]*\})\s*;?\s*$/);
      var newConfig = {};
      if (configMatch && configMatch[1]) {
        try {
          newConfig = JSON.parse(configMatch[1]);
        } catch (e) {
          console.error('bino: failed to parse config', e);
        }
      }

      // Update local state from new config
      var newMissingParams = newConfig.missingParams || [];
      var newQueryParams = newConfig.queryParams || [];
      var newCurrentPath = newConfig.currentPath || currentPath;
      var newContextBase64 = newConfig.initialContextBase64 || '';

      missingParams = newMissingParams;
      queryParams = newQueryParams;
      currentPath = newCurrentPath;

      var currentContext = document.querySelector('bn-context');

      if (newContextBase64) {
        var newContextHtml = decodeBase64(newContextBase64);
        var contextDoc = parser.parseFromString(newContextHtml, 'text/html');
        var newContext = contextDoc.querySelector('bn-context');
        if (newContext) {
          if (currentContext) {
            currentContext.replaceWith(newContext);
          } else {
            // Append as light DOM child of the shell so it slots into the outlet
            var existingBanner = self.querySelector('.bino-missing-params-banner');
            if (existingBanner) existingBanner.remove();
            self.appendChild(newContext);
          }
          self._fit.rebind();
          self._outlet.scrollTop = 0;
        }
      } else if (missingParams.length > 0) {
        if (currentContext) {
          currentContext.remove();
        }
        self._showMissingParamsMessage();
        self._outlet.scrollTop = 0;
      }

      var newTitle = doc.querySelector('title');
      if (newTitle) {
        document.title = newTitle.textContent;
      }

      // Update the control panel with new config
      if (self._controlPanel) {
        self._controlPanel.updateConfig({
          routes: routes,
          queryParams: queryParams,
          missingParams: missingParams,
          currentPath: currentPath
        });
        self._controlPanel.setLoading(false);
      }
      self._syncChrome();
    })
    .catch(function(err) {
      console.error('bino: navigation failed', err);
      if (self._controlPanel) self._controlPanel.setLoading(false);
      if (context) {
        context.style.opacity = '1';
      }
      alert('Failed to load: ' + err.message);
    });
  }
}

customElements.define('bino-serve-shell', BinoServeShell);

// Intercept link clicks for seamless navigation
document.addEventListener('click', function(e) {
  var link = e.target.closest('a[href]');
  if (!link) return;

  var href = link.getAttribute('href');
  if (!href || href.startsWith('http') || href.startsWith('//') || href.startsWith('#')) return;

  var url = new URL(href, window.location.origin);
  var path = url.pathname;

  if (routes.hasOwnProperty(path)) {
    e.preventDefault();
    var shell = document.querySelector('bino-serve-shell');
    if (shell) {
      shell._navigateTo(path + url.search);
    }
  }
});

// Handle browser back/forward
window.addEventListener('popstate', function(e) {
  if (e.state && e.state.url) {
    var shell = document.querySelector('bino-serve-shell');
    if (shell) {
      shell._loadContent(e.state.url);
    }
  }
});

// Set initial state with full URL
if (!history.state) {
  history.replaceState({ url: currentURL }, '', currentURL);
}
