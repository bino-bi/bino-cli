/**
 * Shared DOM utilities for bino preview and serve modes.
 */

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
 * Replace the current <bn-context> element with one parsed from the given HTML string.
 * @param {string} html - Full or partial HTML containing a <bn-context> element.
 * @param {DOMParser} [parser] - Optional DOMParser instance (reuse for perf).
 * @returns {boolean} true if the swap succeeded.
 */
export function swapContext(html, parser) {
  if (!html) return false;
  if (!parser) parser = new DOMParser();
  var doc = parser.parseFromString(html, 'text/html');
  var nextCtx = doc.querySelector('bn-context');
  var currentCtx = document.querySelector('bn-context');
  if (!nextCtx || !currentCtx) return false;
  currentCtx.replaceWith(nextCtx);
  return true;
}
