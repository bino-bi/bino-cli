/**
 * Shared DOM utilities for bino preview and serve modes.
 */
import { Idiomorph } from 'idiomorph';

/**
 * Decode a base64-encoded string. Returns empty string on failure.
 * @param {string} input
 * @returns {string}
 */
export function decodeBase64(input) {
  if (!input) return '';
  try {
    return atob(input);
  } catch (err) {
    console.error('bino: decode failed', err);
    return '';
  }
}

/**
 * Escape a string for safe HTML insertion.
 * @param {string} str
 * @returns {string}
 */
export function escapeHtml(str) {
  var div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

/**
 * Normalize a URL path to always start with "/".
 * @param {string} value
 * @returns {string}
 */
export function normalizePath(value) {
  if (!value) return '/';
  return value.charAt(0) === '/' ? value : '/' + value;
}

/**
 * Path prefix the app is served under, derived from an injected <base> tag.
 * Reverse proxies (e.g. bino-cloud) mount the preview under a path prefix
 * and inject <base href="{prefix}/">; direct local serving has no <base>
 * tag. Returns '' or a prefix without a trailing slash.
 * @returns {string}
 */
export function appBase() {
  var baseEl = document.querySelector('base');
  if (!baseEl || !baseEl.href) return '';
  var path = new URL(baseEl.href, window.location.href).pathname;
  return path === '/' ? '' : path.replace(/\/+$/, '');
}

/**
 * The current view path as bino's server routes it: the browser location
 * with any appBase() proxy prefix stripped.
 * @returns {string}
 */
export function viewPath() {
  var base = appBase();
  var path = window.location.pathname || '/';
  if (base && (path === base || path.indexOf(base + '/') === 0)) {
    path = path.slice(base.length);
  }
  return normalizePath(path);
}

/**
 * Wait for the bn-context custom element to be defined.
 * Resolves immediately if already defined.
 * @returns {Promise<void>}
 */
export function waitForEngine() {
  if (customElements.get('bn-context')) {
    return Promise.resolve();
  }
  return customElements.whenDefined('bn-context');
}

/**
 * Morph the current <bn-context> element's children to match the given HTML string.
 * Uses idiomorph for in-place DOM diffing which naturally preserves scroll position,
 * focus state, and avoids unnecessary re-rendering of unchanged nodes.
 * @param {string} html - Full or partial HTML containing a <bn-context> element.
 * @param {DOMParser} [parser] - Optional DOMParser instance (reuse for perf).
 * @returns {boolean} true if the swap succeeded.
 */
export function swapContext(html, parser) {
  if (!html) {
    console.debug('bino: swapContext skipped — empty html');
    return false;
  }
  if (!parser) parser = new DOMParser();
  var doc = parser.parseFromString(html, 'text/html');
  var nextCtx = doc.querySelector('bn-context');
  var currentCtx = document.querySelector('bn-context');
  if (!nextCtx) {
    console.debug('bino: swapContext skipped — incoming HTML has no <bn-context>');
    return false;
  }
  if (!currentCtx) {
    console.debug('bino: swapContext skipped — live DOM has no <bn-context>');
    return false;
  }

  // Sync attributes on the <bn-context> element itself (e.g. data-page-meta, locale)
  syncAttributes(currentCtx, nextCtx);

  // Morph light DOM children in-place — preserves scroll, focus, and unchanged nodes.
  // Pass innerHTML string so idiomorph sees the children, not nextCtx itself as a child.
  Idiomorph.morph(currentCtx, nextCtx.innerHTML, {
    morphStyle: 'innerHTML',
    callbacks: {
      beforeAttributeUpdated: function (name, el, action) {
        // Stencil adds "hydrated" to the class of every upgraded custom element.
        // The server HTML never contains it, so idiomorph would strip it on morph,
        // making every component invisible. Preserve class on custom elements.
        if (name === 'class' && el.tagName && el.tagName.includes('-')) {
          return false;
        }
      }
    }
  });

  return true;
}

/**
 * Copy attributes from src to dst, removing any that no longer exist.
 * @param {Element} dst
 * @param {Element} src
 */
function syncAttributes(dst, src) {
  // Skip 'class' — the live element has runtime classes (e.g. "hydrated" from Stencil)
  // that the parsed HTML won't have; removing them hides the component.
  // Add/update attributes from src
  for (var i = 0; i < src.attributes.length; i++) {
    var attr = src.attributes[i];
    if (attr.name === 'class') continue;
    if (dst.getAttribute(attr.name) !== attr.value) {
      dst.setAttribute(attr.name, attr.value);
    }
  }
  // Remove attributes not in src
  for (var j = dst.attributes.length - 1; j >= 0; j--) {
    var name = dst.attributes[j].name;
    if (name === 'class') continue;
    if (!src.hasAttribute(name)) {
      dst.removeAttribute(name);
    }
  }
}
