import { LitElement, html, css } from 'lit';
import { Router } from '@vaadin/router';
import { decodeBase64, swapContext } from '../shared/dom-utils.js';
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
 */
class BinoServeShell extends LitElement {
  static styles = css`
    :host {
      display: flex;
      width: 100%;
      min-height: 100vh;
    }
    #outlet {
      flex: 1;
      min-width: 0;
    }
  `;

  render() {
    return html`
      <bino-control-panel></bino-control-panel>
      <div id="outlet"><slot></slot></div>
    `;
  }

  firstUpdated() {
    this._controlPanel = this.renderRoot.querySelector('bino-control-panel');
    this._outlet = this.renderRoot.getElementById('outlet');

    // Set initial config on the control panel
    if (this._controlPanel) {
      this._controlPanel.updateConfig({
        routes: routes,
        queryParams: queryParams,
        missingParams: missingParams,
        currentPath: currentPath
      });
    }

    // Set up Vaadin Router on the outlet
    this._router = new Router(this._outlet);
    var routeKeys = Object.keys(routes);
    var self = this;
    var vaadinRoutes = routeKeys.map(function(path) {
      return {
        path: path,
        action: function(context, commands) {
          // Instead of a Vaadin component, we handle navigation ourselves
          self._handleRouteChange(path, context);
          // Return empty — content is managed by fetch + swapContext
          return undefined;
        }
      };
    });

    // Add catch-all fallback
    vaadinRoutes.push({
      path: '(.*)',
      action: function(context) {
        // For unknown paths, try to navigate
        self._handleRouteChange(context.pathname, context);
        return undefined;
      }
    });

    this._router.setRoutes(vaadinRoutes);

    // Listen for control panel events
    this.addEventListener('bino-apply-params', this._onApplyParams.bind(this));
    this.addEventListener('bino-navigate', this._onNavigate.bind(this));

    // Handle initial content
    this._initContent();
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
  }

  _showMissingParamsMessage() {
    var contentArea = this._outlet;
    if (!contentArea) return;
    // Find existing slotted content or the outlet itself
    var bnContext = contentArea.querySelector('bn-context') || document.querySelector('bn-context');
    if (bnContext) {
      bnContext.remove();
    }
    // Create a banner in the outlet
    var banner = document.createElement('div');
    banner.className = 'bino-missing-params-banner';
    banner.innerHTML =
      '<div class="bino-missing-icon">\u26A0</div>' +
      '<div class="bino-missing-text">' +
      '<strong>Required parameters missing</strong>' +
      '<p>Please fill in the required fields marked with <span class="required">*</span> to view the report.</p>' +
      '</div>';
    // Insert in light DOM (slotted into outlet)
    var existingBanner = document.querySelector('.bino-missing-params-banner');
    if (existingBanner) existingBanner.remove();
    document.body.appendChild(banner);
  }

  _handleRouteChange(path, context) {
    // Only handle if this is a user-initiated navigation (not the initial route)
    // The initial content is handled by _initContent
    if (path === currentPath && !this._hasNavigated) {
      this._hasNavigated = true;
      return;
    }
    this._hasNavigated = true;

    var url = path;
    if (context && context.search) {
      url += context.search;
    }
    this._loadContent(url);
  }

  _onApplyParams(e) {
    var params = e.detail.params;
    var newURL = currentPath;
    var queryString = params.toString();
    if (queryString) {
      newURL += '?' + queryString;
    }
    this._navigateTo(newURL);
  }

  _onNavigate(e) {
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
            // Append to body as slotted content
            var existingBanner = document.querySelector('.bino-missing-params-banner');
            if (existingBanner) existingBanner.remove();
            document.body.appendChild(newContext);
          }
        }
      } else if (missingParams.length > 0) {
        if (currentContext) {
          currentContext.remove();
        }
        self._showMissingParamsMessage();
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
      }
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
    history.pushState({ url: path + url.search }, '', path + url.search);
    var shell = document.querySelector('bino-serve-shell');
    if (shell) {
      shell._loadContent(path + url.search);
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
