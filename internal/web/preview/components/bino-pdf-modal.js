import { LitElement, html, css, nothing } from 'lit';
import { appBase } from '../../shared/dom-utils.js';

// insideVSCode reports whether the preview runs in the VS Code webview. Its
// iframe sandbox blocks downloads and has no PDF viewer.
function insideVSCode() {
  var origins = window.location.ancestorOrigins;
  if (!origins) return false;
  for (var i = 0; i < origins.length; i++) {
    if (origins[i].indexOf('vscode-webview:') === 0) return true;
  }
  return false;
}

function pdfBlob(base64) {
  var raw = atob(base64);
  var bytes = new Uint8Array(raw.length);
  for (var i = 0; i < raw.length; i++) {
    bytes[i] = raw.charCodeAt(i);
  }
  return new Blob([bytes], { type: 'application/pdf' });
}

class BinoPdfModal extends LitElement {
  static properties = {
    _open: { state: true },
    _state: { state: true }, // 'building' | 'ready' | 'error' | 'embedded'
    _url: { state: true },
    _error: { state: true },
    _warnings: { state: true },
  };

  static styles = css`
    :host { font-family: var(--bino-font-sans); }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: var(--bino-radius-lg);
      box-shadow: var(--bino-shadow-page);
      width: 90vw;
      max-width: 1200px;
      height: 85vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      gap: var(--bino-space-md);
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      flex: 1;
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .download-btn {
      padding: 4px 12px;
      border-radius: var(--bino-radius);
      background: var(--bino-accent);
      border: 1px solid var(--bino-accent-strong);
      color: var(--bino-on-accent);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      text-decoration: none;
    }
    .download-btn:hover { background: var(--bino-accent-strong); }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover { color: var(--bino-text); }
    .warnings {
      flex-shrink: 0;
      max-height: 30%;
      overflow: auto;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      font-size: var(--bino-font-size-sm);
    }
    .warnings summary { cursor: pointer; font-weight: 600; }
    .warnings li { white-space: pre-wrap; overflow-wrap: anywhere; }
    .viewer {
      flex: 1;
      min-height: 0;
      border: 0;
      background: var(--bino-surface-subtle);
    }
    .message {
      flex: 1;
      min-height: 0;
      overflow: auto;
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
    .message a { color: var(--bino-primary); overflow-wrap: anywhere; }
    .error {
      margin: 0;
      padding: var(--bino-space-md);
      border: 1px solid var(--bino-error-border);
      border-radius: var(--bino-radius);
      background: var(--bino-error-bg);
      color: var(--bino-text);
      font: var(--bino-font-size-sm) var(--bino-font-mono);
      text-align: left;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    .footer {
      flex-shrink: 0;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
  `;

  constructor() {
    super();
    this._open = false;
    this._state = 'building';
    this._url = '';
    this._error = '';
    this._warnings = [];
    this._name = '';
    this._controller = null;
    this._boundOnOpen = this._onOpen.bind(this);
    this._boundOnKeydown = this._onKeydown.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-open-pdf', this._boundOnOpen);
    document.addEventListener('keydown', this._boundOnKeydown);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-open-pdf', this._boundOnOpen);
    document.removeEventListener('keydown', this._boundOnKeydown);
    this._reset();
  }

  render() {
    if (!this._open) return nothing;

    return html`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>PDF preview: ${this._name}</h2>
            ${this._state === 'ready' ? html`
              <a class='download-btn' href=${this._url}
                download=${this._name.replace(/[@/]/g, '_') + '-preview.pdf'}>Download</a>
            ` : ''}
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          ${this._renderWarnings()}
          ${this._renderBody()}
          ${this._state === 'ready' ? html`
            <div class='footer'>
              Every page is marked PREVIEW. For the final PDF run: bino build --artefact ${this._name}
            </div>
          ` : ''}
        </div>
      </div>
    `;
  }

  _renderWarnings() {
    var n = this._warnings.length;
    if (n === 0) return '';
    return html`
      <details class='warnings' ?open=${this._state === 'error'}>
        <summary>${n} warning${n !== 1 ? 's' : ''}. The PDF may be incomplete.</summary>
        <ul>${this._warnings.map(function(w) { return html`<li>${w}</li>`; })}</ul>
      </details>
    `;
  }

  _renderBody() {
    if (this._state === 'embedded') {
      return html`
        <div class='message'>
          <p>The PDF preview does not work inside VS Code. Open the preview in a browser:</p>
          <p><a href=${window.location.href} target='_blank' rel='noopener'>${window.location.href}</a></p>
        </div>
      `;
    }
    if (this._state === 'error') {
      return html`
        <div class='message'>
          <p>The PDF could not be built.</p>
          <pre class='error'>${this._error}</pre>
        </div>
      `;
    }
    if (this._state === 'building') {
      return html`<div class='message'>Building the PDF… This can take a minute.</div>`;
    }
    if (navigator.pdfViewerEnabled === false) {
      return html`<div class='message'>Your browser does not show PDFs inline. Use Download.</div>`;
    }
    return html`<iframe class='viewer' title='PDF preview' src=${this._url}></iframe>`;
  }

  _onOpen(e) {
    if (this._open) return;
    this._reset();
    this._name = e.detail.name;
    this._open = true;
    if (insideVSCode()) {
      this._state = 'embedded';
      return;
    }
    this._build(e.detail.kind);
  }

  _build(kind) {
    var self = this;
    var controller = new AbortController();
    this._controller = controller;
    this._state = 'building';

    var url = appBase() + '/__preview/pdf?kind=' + encodeURIComponent(kind) +
      '&name=' + encodeURIComponent(this._name);
    fetch(url, { method: 'POST', signal: controller.signal })
      .then(function(resp) {
        // A proxy error page is not JSON; fall back to the status.
        return resp.json().catch(function() { return {}; }).then(function(data) {
          if (controller !== self._controller) return;
          self._warnings = data.warnings || [];
          if (!data.pdf) {
            throw new Error(data.error || 'HTTP ' + resp.status);
          }
          self._url = URL.createObjectURL(pdfBlob(data.pdf));
          self._state = 'ready';
        });
      })
      .catch(function(err) {
        // Closing the modal aborts the request and clears the controller.
        if (controller !== self._controller) return;
        self._error = err.message || String(err);
        self._state = 'error';
      });
  }

  // _reset stops a running build and frees the PDF.
  _reset() {
    if (this._controller) {
      this._controller.abort();
      this._controller = null;
    }
    if (this._url) {
      URL.revokeObjectURL(this._url);
      this._url = '';
    }
    this._error = '';
    this._warnings = [];
  }

  _onKeydown(e) {
    if (this._open && e.key === 'Escape') {
      this._close();
    }
  }

  _close() {
    this._reset();
    this._open = false;
  }

  _stop(e) {
    e.stopPropagation();
  }
}

customElements.define('bino-pdf-modal', BinoPdfModal);
