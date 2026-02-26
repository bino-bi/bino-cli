import { decodeBase64, swapContext } from '../shared/dom-utils.js';
import './components/bino-control-panel.js';

var config = window.__binoServeConfig || {};
var routes = config.routes || {};
var queryParams = config.queryParams || [];
var missingParams = config.missingParams || [];
var currentPath = config.currentPath || '/';
var currentURL = config.currentURL || '/';
var initialContextBase64 = config.initialContextBase64 || '';

var parser = new DOMParser();
var engineReady = false;
var controlPanel = null;

function initEngine() {
  controlPanel = document.querySelector('bino-control-panel');

  // Set initial config on the control panel
  if (controlPanel) {
    controlPanel.updateConfig({
      routes: routes,
      queryParams: queryParams,
      missingParams: missingParams,
      currentPath: currentPath
    });
  }

  // If we have missing params, show the missing params message (no report to render)
  if (missingParams && missingParams.length > 0) {
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', showMissingParamsMessage);
    } else {
      showMissingParamsMessage();
    }
    return;
  }

  // Wait for template engine to become ready, then inject initial content
  import('../shared/dom-utils.js').then(function(mod) {
    mod.waitForEngine().then(function() {
      engineReady = true;
      injectInitialContent();
    });
  });
}

function injectInitialContent() {
  if (!initialContextBase64) return;
  var html = decodeBase64(initialContextBase64);
  swapContext(html, parser);
  initialContextBase64 = null;
}

function showMissingParamsMessage() {
  var contentArea = document.getElementById('bino-content-area');
  if (contentArea) {
    contentArea.innerHTML =
      '<div class="bino-missing-params-banner">' +
      '<div class="bino-missing-icon">\u26A0</div>' +
      '<div class="bino-missing-text">' +
      '<strong>Required parameters missing</strong>' +
      '<p>Please fill in the required fields marked with <span class="required">*</span> to view the report.</p>' +
      '</div></div>';
  }
}

// Start engine init immediately
initEngine();

// Listen for control panel events
document.addEventListener('bino-apply-params', function(e) {
  var params = e.detail.params;
  var newURL = currentPath;
  var queryString = params.toString();
  if (queryString) {
    newURL += '?' + queryString;
  }
  navigateTo(newURL);
});

document.addEventListener('bino-navigate', function(e) {
  var path = e.detail.path;
  navigateTo(path);
});

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
    navigateTo(path + url.search);
  }
});

// Handle browser back/forward
window.addEventListener('popstate', function(e) {
  if (e.state && e.state.url) {
    loadContent(e.state.url);
  }
});

function navigateTo(url) {
  history.pushState({ url: url }, '', url);
  loadContent(url);
}

function loadContent(url) {
  var context = document.querySelector('bn-context');
  if (context) {
    context.style.opacity = '0.5';
  }

  if (controlPanel) {
    controlPanel.setLoading(true);
  }

  fetch(url, {
    headers: { 'X-Requested-With': 'bino-serve' }
  })
  .then(function(response) {
    if (!response.ok) {
      throw new Error('HTTP ' + response.status);
    }
    return response.text();
  })
  .then(function(html) {
    var doc = parser.parseFromString(html, 'text/html');

    // Extract the config script data from the new page
    var configEl = doc.getElementById('bino-serve-config');
    if (!configEl) {
      console.error('bino: no config script found in response');
      if (controlPanel) controlPanel.setLoading(false);
      return;
    }

    var configText = configEl.textContent;

    // Extract the config object from window.__binoServeConfig = {...};
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

    var contentArea = document.getElementById('bino-content-area');
    var currentContext = document.querySelector('bn-context');

    if (newContextBase64) {
      var newContextHtml = decodeBase64(newContextBase64);
      var contextDoc = parser.parseFromString(newContextHtml, 'text/html');
      var newContext = contextDoc.querySelector('bn-context');
      if (newContext) {
        if (currentContext) {
          currentContext.replaceWith(newContext);
        } else if (contentArea) {
          contentArea.innerHTML = '';
          contentArea.appendChild(newContext);
        }
      }
    } else if (missingParams.length > 0 && contentArea) {
      if (currentContext) {
        currentContext.remove();
      }
      showMissingParamsMessage();
    }

    var newTitle = doc.querySelector('title');
    if (newTitle) {
      document.title = newTitle.textContent;
    }

    // Update the control panel with new config
    if (controlPanel) {
      controlPanel.updateConfig({
        routes: routes,
        queryParams: queryParams,
        missingParams: missingParams,
        currentPath: currentPath
      });
    }
  })
  .catch(function(err) {
    console.error('bino: navigation failed', err);
    if (controlPanel) controlPanel.setLoading(false);
    if (context) {
      context.style.opacity = '1';
    }
    alert('Failed to load: ' + err.message);
  });
}

// Set initial state with full URL
if (!history.state) {
  history.replaceState({ url: currentURL }, '', currentURL);
}
