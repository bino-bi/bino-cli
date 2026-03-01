import { decodeBase64, normalizePath, waitForEngine, swapContext } from '../shared/dom-utils.js';
import './components/bino-toolbar.js';
import './components/bino-error-panel.js';
import './components/bino-search.js';
import './components/bino-assets-modal.js';
import './components/bino-graph-modal.js';
import './components/bino-data-explorer.js';

if (!window.EventSource || window.__bnPreviewRuntime) {
  // Prevent double-initialization
} else {
  window.__bnPreviewRuntime = true;

  var parser = new DOMParser();
  var normalizedPath = normalizePath(window.location.pathname || '/');
  var source = new EventSource('/__preview/events');
  var sseReady = false;
  var engineReady = false;

  function swapContextWithEvent(html) {
    if (swapContext(html, parser)) {
      try {
        document.dispatchEvent(new CustomEvent('bn-preview:content-updated', { detail: { path: normalizedPath } }));
      } catch (eventErr) {
        console.debug('bn preview: custom event skipped', eventErr);
      }
    }
  }

  function fetchInitialContext() {
    fetch('/__preview/context?path=' + encodeURIComponent(normalizedPath))
      .then(function (resp) {
        if (!resp.ok) {
          console.debug('bn preview: context not available yet');
          return null;
        }
        return resp.text();
      })
      .then(function (html) {
        if (html) {
          swapContextWithEvent(html);
        }
      })
      .catch(function (err) {
        console.error('bn preview: fetch context failed', err);
      });
  }

  function tryFetchContext() {
    if (sseReady && engineReady) {
      fetchInitialContext();
    }
  }

  waitForEngine().then(function () {
    engineReady = true;
    tryFetchContext();
  });

  source.addEventListener('ready', function () {
    sseReady = true;
    tryFetchContext();
  });

  source.addEventListener('content', function (event) {
    try {
      var payload = JSON.parse(event.data || '{}');
      if (!payload || normalizePath(payload.path) !== normalizedPath) {
        return;
      }
      var html = decodeBase64(payload.htmlBase64);
      swapContextWithEvent(html);
    } catch (err) {
      console.error('bn preview: apply failed', err);
    }
  });

  window.addEventListener('beforeunload', function () {
    source.close();
  });

  // Click-to-source: Cmd/Ctrl+click on a [data-bino-kind] element
  document.addEventListener('click', function (e) {
    if (!e.metaKey && !e.ctrlKey) {
      return;
    }
    var el = e.target.closest('[data-bino-kind]');
    if (!el) {
      return;
    }
    var msg = {
      type: 'bino:revealSource',
      kind: el.getAttribute('data-bino-kind'),
      name: el.getAttribute('data-bino-name') || '',
      ref: el.getAttribute('data-bino-ref') || ''
    };
    if (window.parent && window.parent !== window) {
      window.parent.postMessage(msg, '*');
    }
    e.preventDefault();
    e.stopPropagation();
  });

  // Page info overlays for "All Pages" view
  function applyPageInfoOverlays() {
    if (normalizedPath !== '/') return;

    document.querySelectorAll('.bn-page-info').forEach(function (el) {
      el.remove();
    });

    var bnContext = document.querySelector('bn-context');
    if (!bnContext) return;
    var metaJSON = bnContext.getAttribute('data-page-meta');
    if (!metaJSON) return;
    var pageMeta;
    try {
      pageMeta = JSON.parse(metaJSON);
    } catch (e) {
      return;
    }
    if (!Array.isArray(pageMeta)) return;

    var metaByName = {};
    pageMeta.forEach(function (m) {
      metaByName[m.name] = m;
    });

    var searchRoot = bnContext.shadowRoot || bnContext;
    var pages = searchRoot.querySelectorAll('bn-layout-page[data-bino-page]');
    if (pages.length === 0) {
      pages = document.querySelectorAll('bn-layout-page[data-bino-page]');
    }
    pages.forEach(function (pageEl) {
      var pageName = pageEl.getAttribute('data-bino-page');
      if (!pageName) return;
      var baseName = pageName.split('#')[0];
      var meta = metaByName[baseName] || metaByName[pageName];
      if (!meta) return;

      var overlay = document.createElement('div');
      overlay.className = 'bn-page-info';

      var nameSpan = document.createElement('span');
      nameSpan.className = 'bn-page-info-name';
      nameSpan.textContent = pageName;
      overlay.appendChild(nameSpan);

      if (meta.constraints && meta.constraints.length > 0) {
        var clabel = document.createElement('span');
        clabel.className = 'bn-page-info-label';
        clabel.textContent = 'constraints:';
        overlay.appendChild(clabel);
        meta.constraints.forEach(function (c) {
          var pill = document.createElement('span');
          pill.className = 'bn-page-info-pill constraint';
          pill.textContent = c;
          overlay.appendChild(pill);
        });
      }

      if (meta.artefacts && meta.artefacts.length > 0) {
        var alabel = document.createElement('span');
        alabel.className = 'bn-page-info-label';
        alabel.textContent = 'used in:';
        overlay.appendChild(alabel);
        meta.artefacts.forEach(function (a) {
          var pill = document.createElement('span');
          pill.className = 'bn-page-info-pill artefact';
          pill.textContent = a;
          overlay.appendChild(pill);
        });
      }

      pageEl.parentNode.insertBefore(overlay, pageEl);
    });
  }

  document.addEventListener('bn-preview:content-updated', function () {
    applyPageInfoOverlays();
  });

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', applyPageInfoOverlays);
  } else {
    applyPageInfoOverlays();
  }
}
