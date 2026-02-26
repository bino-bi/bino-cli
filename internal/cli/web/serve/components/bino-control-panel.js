import { escapeHtml } from '../../shared/dom-utils.js';

const template = document.createElement('template');
template.innerHTML = `
<style>
  :host {
    width: 280px;
    min-width: 280px;
    background: var(--bino-surface, #ffffff);
    border-right: 1px solid var(--bino-border, #e5e7eb);
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    overflow-y: auto;
    max-height: 100vh;
    position: sticky;
    top: 0;
    font-family: var(--bino-font-sans, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif);
  }
  :host(:empty), :host([hidden]) {
    display: none;
  }
  h3 {
    margin: 0;
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--bino-text-secondary, #6b7280);
  }
  .sitemap {
    border-bottom: 1px solid var(--bino-border, #e5e7eb);
    padding-bottom: 1rem;
    margin-bottom: 0.5rem;
  }
  .route-list {
    list-style: none;
    margin: 0.5rem 0 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .route-list li a {
    display: block;
    padding: 0.5rem 0.75rem;
    border-radius: var(--bino-radius, 6px);
    text-decoration: none;
    color: var(--bino-text-muted, #374151);
    font-size: 0.875rem;
    transition: background 0.15s;
  }
  .route-list li a:hover {
    background: #f3f4f6;
  }
  .route-list li.active a {
    background: #eff6ff;
    color: #1d4ed8;
    font-weight: 500;
  }
  .param-group {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
  }
  .param-group.missing {
    background: var(--bino-error-bg, #fef2f2);
    border: 1px solid #fecaca;
    border-radius: 8px;
    padding: 0.75rem;
    margin: -0.25rem;
  }
  .param-group.missing .param-label {
    color: var(--bino-error, #dc2626);
  }
  .param-label {
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--bino-text-muted, #374151);
  }
  .param-label .required {
    color: var(--bino-error, #dc2626);
    margin-left: 2px;
  }
  .param-desc {
    font-size: 0.75rem;
    color: var(--bino-text-secondary, #6b7280);
    margin: 0;
  }
  .param-input {
    padding: 0.5rem 0.75rem;
    border: 1px solid var(--bino-border-light, #d1d5db);
    border-radius: var(--bino-radius, 6px);
    font-size: 0.875rem;
    font-family: inherit;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .param-input:focus {
    outline: none;
    border-color: var(--bino-primary, #3b82f6);
    box-shadow: 0 0 0 3px var(--bino-primary-ring, rgba(59, 130, 246, 0.1));
  }
  .param-input.invalid {
    border-color: var(--bino-error, #dc2626);
  }
  .param-select {
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%236b7280' d='M3 5l3 3 3-3'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 0.75rem center;
    padding-right: 2rem;
    cursor: pointer;
  }
  .range-slider-container {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .range-values {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.8125rem;
    color: var(--bino-text-muted, #374151);
    font-weight: 500;
  }
  .range-value {
    background: #f3f4f6;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
    min-width: 3rem;
    text-align: center;
  }
  .range-sep {
    color: var(--bino-text-secondary, #6b7280);
    font-size: 0.875rem;
  }
  .dual-range {
    position: relative;
    height: 1.5rem;
  }
  .range-slider {
    position: absolute;
    width: 100%;
    height: 6px;
    top: 50%;
    transform: translateY(-50%);
    -webkit-appearance: none;
    appearance: none;
    background: transparent;
    pointer-events: none;
    padding: 0;
    border: none;
    margin: 0;
  }
  .range-min { z-index: 1; }
  .range-max { z-index: 2; }
  .range-slider::-webkit-slider-runnable-track {
    width: 100%;
    height: 6px;
    background: var(--bino-border, #e5e7eb);
    border-radius: 3px;
  }
  .range-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 18px;
    height: 18px;
    background: var(--bino-surface, #ffffff);
    border: 2px solid var(--bino-primary, #3b82f6);
    border-radius: 50%;
    cursor: pointer;
    pointer-events: auto;
    margin-top: -6px;
    box-shadow: var(--bino-shadow-header, 0 1px 3px rgba(0,0,0,0.05));
  }
  .range-slider::-moz-range-track {
    width: 100%;
    height: 6px;
    background: var(--bino-border, #e5e7eb);
    border-radius: 3px;
  }
  .range-slider::-moz-range-thumb {
    width: 14px;
    height: 14px;
    background: var(--bino-surface, #ffffff);
    border: 2px solid var(--bino-primary, #3b82f6);
    border-radius: 50%;
    cursor: pointer;
    pointer-events: auto;
    box-shadow: var(--bino-shadow-header, 0 1px 3px rgba(0,0,0,0.05));
  }
  input[type="date"].param-input,
  input[type="datetime-local"].param-input {
    cursor: pointer;
  }
  input[type="number"].param-input {
    -moz-appearance: textfield;
  }
  input[type="number"].param-input::-webkit-outer-spin-button,
  input[type="number"].param-input::-webkit-inner-spin-button {
    -webkit-appearance: none;
    margin: 0;
  }
  .apply-btn {
    padding: 0.625rem 1rem;
    background: var(--bino-primary, #3b82f6);
    color: var(--bino-surface, #ffffff);
    border: none;
    border-radius: var(--bino-radius, 6px);
    font-size: 0.875rem;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s;
  }
  .apply-btn:hover {
    background: var(--bino-primary-hover, #2563eb);
  }
  .apply-btn:disabled {
    background: #9ca3af;
    cursor: not-allowed;
  }
  @media print {
    :host {
      display: none !important;
    }
  }
</style>
<div id='content'></div>
`;

class BinoControlPanel extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.shadowRoot.appendChild(template.content.cloneNode(true));
    this._content = this.shadowRoot.getElementById('content');
    this._routes = {};
    this._queryParams = [];
    this._missingParams = [];
    this._currentPath = '/';
  }

  connectedCallback() {
    this._parseAttributes();
    this._render();
  }

  static get observedAttributes() {
    return ['routes', 'query-params', 'missing-params', 'current-path'];
  }

  attributeChangedCallback() {
    this._parseAttributes();
    this._render();
  }

  _parseAttributes() {
    try { this._routes = JSON.parse(this.getAttribute('routes') || '{}'); } catch (e) { this._routes = {}; }
    try { this._queryParams = JSON.parse(this.getAttribute('query-params') || '[]'); } catch (e) { this._queryParams = []; }
    try { this._missingParams = JSON.parse(this.getAttribute('missing-params') || '[]'); } catch (e) { this._missingParams = []; }
    this._currentPath = this.getAttribute('current-path') || '/';
  }

  // Public methods for JS updates (used by serve-app.js)
  updateConfig(config) {
    if (config.routes !== undefined) this._routes = config.routes;
    if (config.queryParams !== undefined) this._queryParams = config.queryParams;
    if (config.missingParams !== undefined) this._missingParams = config.missingParams;
    if (config.currentPath !== undefined) this._currentPath = config.currentPath;
    this._render();
  }

  setLoading(loading) {
    var btn = this.shadowRoot.querySelector('.apply-btn');
    if (btn) {
      btn.disabled = loading;
      btn.textContent = loading ? 'Loading...' : 'Apply';
    }
  }

  _render() {
    var html = '';

    // Navigation section
    var routeKeys = Object.keys(this._routes).sort();
    if (routeKeys.length > 1) {
      html += '<div class="sitemap">';
      html += '<h3>Navigation</h3>';
      html += '<ul class="route-list">';
      var self = this;
      routeKeys.forEach(function(path) {
        var title = self._routes[path] || path;
        var isActive = path === self._currentPath;
        var activeClass = isActive ? ' class="active"' : '';
        html += '<li' + activeClass + '><a href="' + escapeHtml(path) + '">' + escapeHtml(title) + '</a></li>';
      });
      html += '</ul>';
      html += '</div>';
    }

    // Parameters section
    if (this._queryParams.length > 0) {
      var urlParams = new URLSearchParams(window.location.search);
      var self = this;

      html += '<h3>Parameters</h3>';
      this._queryParams.forEach(function(param) {
        var value = urlParams.get(param.name);
        var value2 = null;
        if (param.type === 'number_range') {
          value2 = urlParams.get(param.name + '_max');
        }
        if (value === null && param.default !== undefined && param.default !== null) {
          value = param.default;
        }
        value = value || '';
        value2 = value2 || '';

        var isMissing = self._missingParams.indexOf(param.name) !== -1;
        var groupClass = isMissing ? ' missing' : '';

        html += '<div class="param-group' + groupClass + '">';
        html += '<label class="param-label" for="param-' + param.name + '">' +
                escapeHtml(param.name) +
                (param.required ? '<span class="required">*</span>' : '') + '</label>';
        if (param.description) {
          html += '<p class="param-desc">' + escapeHtml(param.description) + '</p>';
        }
        html += self._buildInput(param, value, value2, isMissing);
        html += '</div>';
      });

      html += '<button type="button" class="apply-btn">Apply</button>';
    }

    this._content.innerHTML = html;

    // Bind events
    var applyBtn = this.shadowRoot.querySelector('.apply-btn');
    if (applyBtn) {
      applyBtn.addEventListener('click', this._onApply.bind(this));
    }
    var self = this;
    this.shadowRoot.querySelectorAll('.param-input').forEach(function(input) {
      input.addEventListener('keypress', function(e) {
        if (e.key === 'Enter') self._onApply();
      });
      input.addEventListener('input', function() {
        input.classList.remove('invalid');
        var group = input.closest('.param-group');
        if (group) group.classList.remove('missing');
      });
    });

    // Setup range sliders
    this._setupRangeSliders();

    // Dispatch route clicks
    this.shadowRoot.querySelectorAll('.route-list a').forEach(function(link) {
      link.addEventListener('click', function(e) {
        e.preventDefault();
        var href = link.getAttribute('href');
        self.dispatchEvent(new CustomEvent('bino-navigate', {
          detail: { path: href },
          bubbles: true,
          composed: true
        }));
      });
    });
  }

  _buildInput(param, value, value2, isMissing) {
    var type = param.type || 'string';
    var opts = param.options || {};
    var placeholder = param.default !== undefined && param.default !== null ? 'placeholder="' + escapeHtml(String(param.default)) + '"' : '';
    var required = 'data-required="' + param.required + '"';
    var minAttr = opts.min !== undefined ? ' min="' + opts.min + '"' : '';
    var maxAttr = opts.max !== undefined ? ' max="' + opts.max + '"' : '';
    var stepAttr = opts.step !== undefined ? ' step="' + opts.step + '"' : '';
    var invalidClass = isMissing ? ' invalid' : '';

    switch (type) {
      case 'number':
        return '<input type="number" class="param-input' + invalidClass + '" id="param-' + param.name + '" ' +
               'name="' + param.name + '" value="' + escapeHtml(value) + '" ' + required + ' ' + placeholder + minAttr + maxAttr + stepAttr + '>';

      case 'number_range':
        var minVal = opts.min !== undefined ? opts.min : 0;
        var maxVal = opts.max !== undefined ? opts.max : 100;
        var stepVal = opts.step !== undefined ? opts.step : 1;
        var curMin = value !== '' ? parseFloat(value) : minVal;
        var curMax = value2 !== '' ? parseFloat(value2) : maxVal;
        return '<div class="range-slider-container"><div class="range-values">' +
               '<span class="range-value" id="range-min-' + param.name + '">' + curMin + '</span>' +
               '<span class="range-sep">\u2013</span>' +
               '<span class="range-value" id="range-max-' + param.name + '">' + curMax + '</span></div>' +
               '<div class="dual-range">' +
               '<input type="range" class="param-input range-slider range-min' + invalidClass + '" ' +
               'name="' + param.name + '" value="' + curMin + '" min="' + minVal + '" max="' + maxVal + '" step="' + stepVal + '" ' + required + '>' +
               '<input type="range" class="param-input range-slider range-max" ' +
               'name="' + param.name + '_max" value="' + curMax + '" min="' + minVal + '" max="' + maxVal + '" step="' + stepVal + '" data-required="false">' +
               '</div></div>';

      case 'select':
        var html = '<select class="param-input param-select' + invalidClass + '" name="' + param.name + '" ' + required + '>';
        if (!param.required) html += '<option value="">-- Select --</option>';
        if (opts.items && opts.items.length > 0) {
          opts.items.forEach(function(item) {
            var selected = value === item.value ? ' selected' : '';
            html += '<option value="' + escapeHtml(item.value) + '"' + selected + '>' + escapeHtml(item.label || item.value) + '</option>';
          });
        }
        html += '</select>';
        return html;

      case 'date':
        return '<input type="date" class="param-input' + invalidClass + '" name="' + param.name + '" value="' + escapeHtml(value) + '" ' + required + ' ' + placeholder + '>';

      case 'date_time':
        return '<input type="datetime-local" class="param-input' + invalidClass + '" name="' + param.name + '" value="' + escapeHtml(value) + '" ' + required + ' ' + placeholder + '>';

      case 'string':
      default:
        return '<input type="text" class="param-input' + invalidClass + '" name="' + param.name + '" value="' + escapeHtml(value) + '" ' + required + ' ' + placeholder + '>';
    }
  }

  _setupRangeSliders() {
    var self = this;
    this.shadowRoot.querySelectorAll('.dual-range').forEach(function(container) {
      var minSlider = container.querySelector('.range-min');
      var maxSlider = container.querySelector('.range-max');
      if (!minSlider || !maxSlider) return;

      var minDisplay = self.shadowRoot.getElementById('range-min-' + minSlider.name);
      var maxDisplay = self.shadowRoot.getElementById('range-max-' + maxSlider.name);

      function updateDisplay() {
        var minVal = parseFloat(minSlider.value);
        var maxVal = parseFloat(maxSlider.value);
        if (minVal > maxVal) {
          if (this === minSlider) { minSlider.value = maxVal; minVal = maxVal; }
          else { maxSlider.value = minVal; maxVal = minVal; }
        }
        if (minDisplay) minDisplay.textContent = minSlider.value;
        if (maxDisplay) maxDisplay.textContent = maxSlider.value;
      }
      minSlider.addEventListener('input', updateDisplay);
      maxSlider.addEventListener('input', updateDisplay);
    });
  }

  _onApply() {
    var inputs = this.shadowRoot.querySelectorAll('.param-input');
    var params = new URLSearchParams();
    var valid = true;

    inputs.forEach(function(input) {
      var name = input.name;
      var value = input.value.trim();
      var required = input.dataset.required === 'true';
      if (required && !value) {
        input.classList.add('invalid');
        valid = false;
      } else {
        input.classList.remove('invalid');
        if (value) params.set(name, value);
      }
    });

    if (!valid) return;

    this.dispatchEvent(new CustomEvent('bino-apply-params', {
      detail: { params: params },
      bubbles: true,
      composed: true
    }));
  }
}

customElements.define('bino-control-panel', BinoControlPanel);
