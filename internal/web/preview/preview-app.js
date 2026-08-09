import { normalizePath, appBase, viewPath, waitForEngine, swapContext } from '../shared/dom-utils.js';
import './components/bino-toolbar.js';
import './components/bino-error-panel.js';
import './components/bino-search.js';
import './components/bino-assets-modal.js';
import './components/bino-graph-modal.js';
import './components/bino-data-explorer.js';
import './components/bino-inspector.js';

if (!window.EventSource || window.__bnPreviewRuntime) {
  // Prevent double-initialization
} else {
  window.__bnPreviewRuntime = true;

  // Bumped on each user-visible runtime change so a quick devtools check
  // confirms whether the page is on the latest preview-app.js. Increment
  // when fixing a hot-reload bug here.
  console.info('bn preview runtime v12 (doc routes live-reload)');

  var parser = new DOMParser();
  var basePrefix = appBase();
  var normalizedPath = viewPath();
  var source = new EventSource(basePrefix + '/__preview/events');
  var sseReady = false;
  var engineReady = false;

  function tryFetchContext() {
    if (sseReady && engineReady) {
      fetchAndSwapContext('initial');
    }
  }

  waitForEngine().then(function () {
    engineReady = true;
    tryFetchContext();
  });

  source.addEventListener('ready', function () {
    sseReady = true;
    document.dispatchEvent(new CustomEvent('bn-preview:refresh-done'));
    tryFetchContext();
  });

  source.addEventListener('refreshing', function (event) {
    try {
      var payload = JSON.parse(event.data || '{}');
      document.dispatchEvent(new CustomEvent('bn-preview:refreshing', { detail: payload }));
    } catch (err) {
      document.dispatchEvent(new CustomEvent('bn-preview:refreshing', { detail: {} }));
    }
  });

  source.addEventListener('refresh-done', function (event) {
    var detail = {};
    try {
      detail = JSON.parse(event.data || '{}') || {};
    } catch (err) {
      detail = {};
    }
    var paths = Array.isArray(detail.paths) ? detail.paths : [];
    var matched = false;
    if (paths.length > 0) {
      for (var i = 0; i < paths.length; i++) {
        if (normalizePath(paths[i]) === normalizedPath) {
          matched = true;
          break;
        }
      }
      // Server completed a refresh but didn't broadcast for this view.
      // On selective refreshes that's expected (nothing this view depends
      // on changed); genuine render failures arrive as refresh-error
      // events. Log for debugging only — no toolbar pill.
      if (!matched) {
        console.warn('bn preview: refresh did not include this view', normalizedPath, 'broadcast paths:', paths);
        document.dispatchEvent(new CustomEvent('bn-preview:no-payload', { detail: { path: normalizedPath, broadcastPaths: paths } }));
      }
    }
    // Initial-load retry: if our path was just broadcast (so the cache is
    // now populated for this path) but we never successfully swapped yet
    // (typical when the page opened before the first server-side refresh
    // completed), fetch and swap now. Without this, the bn-loading
    // placeholder stays forever.
    if (matched && !contextLoaded) {
      fetchAndSwapContext('initial-retry');
    }
    document.dispatchEvent(new CustomEvent('bn-preview:refresh-done', { detail: detail }));
  });

  // Track the most recent in-flight context fetch so a fast burst of
  // path-changed events doesn't morph stale HTML on top of fresh HTML.
  var contextFetchSeq = 0;
  // Set true after the first successful swap; refresh-done uses this to
  // retry the initial fetch when it 404'd because the cache hadn't been
  // populated yet (e.g. server still doing its first refresh when the page
  // loaded).
  var contextLoaded = false;

  function fetchAndSwapContext(reason) {
    var seq = ++contextFetchSeq;
    fetch(basePrefix + '/__preview/context?path=' + encodeURIComponent(normalizedPath))
      .then(function (resp) {
        if (!resp.ok) {
          console.warn('bn preview: context fetch failed', resp.status, reason);
          return null;
        }
        return resp.text();
      })
      .then(function (html) {
        if (!html || seq !== contextFetchSeq) return;
        var ok = swapContext(html, parser);
        if (ok) {
          contextLoaded = true;
          try {
            document.dispatchEvent(new CustomEvent('bn-preview:content-updated', { detail: { path: normalizedPath } }));
          } catch (eventErr) {
            console.debug('bn preview: custom event skipped', eventErr);
          }
        } else {
          console.warn('bn preview: swapContext returned false; DOM not updated');
        }
      })
      .catch(function (err) {
        console.error('bn preview: context fetch errored', err);
      });
  }

  source.addEventListener('path-changed', function (event) {
    var payload = {};
    try {
      payload = JSON.parse(event.data || '{}') || {};
    } catch (err) {
      return;
    }
    if (!payload.path || normalizePath(payload.path) !== normalizedPath) {
      // The change applies to a different artefact's view; ignore it.
      return;
    }
    fetchAndSwapContext('path-changed');
  });

  source.addEventListener('refresh-error', function (event) {
    var payload = {};
    try {
      payload = JSON.parse(event.data || '{}');
    } catch (err) {
      // Ignore malformed payload.
    }
    // Path-scoped errors only apply to clients viewing that path; ignore
    // errors for other artefacts to keep the toolbar from flashing.
    if (payload && payload.path && normalizePath(payload.path) !== normalizedPath) {
      return;
    }
    console.error('bn preview: refresh failed', payload && payload.message);
    document.dispatchEvent(new CustomEvent('bn-preview:refresh-error', { detail: payload }));
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

      if (meta.artifacts && meta.artifacts.length > 0) {
        var alabel = document.createElement('span');
        alabel.className = 'bn-page-info-label';
        alabel.textContent = 'used in:';
        overlay.appendChild(alabel);
        meta.artifacts.forEach(function (a) {
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
