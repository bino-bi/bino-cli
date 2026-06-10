import { LitElement, html, css } from 'lit';

class BinoControlPanel extends LitElement {
  static properties = {
    routes: { type: Object },
    queryParams: { type: Array },
    missingParams: { type: Array },
    currentPath: { type: String, attribute: 'current-path' },
    _loading: { state: true },
  };

  static styles = css`
    :host {
      width: var(--bino-sidebar-width);
      min-width: var(--bino-sidebar-width);
      background: var(--bino-surface);
      border-right: 1px solid var(--bino-border);
      padding: var(--bino-space-md);
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-md);
      overflow-y: auto;
      max-height: 100vh;
      position: sticky;
      top: 0;
      font-family: var(--bino-font-sans);
    }
    :host([hidden]) {
      display: none;
    }
    h3 {
      margin: 0;
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--bino-text-secondary);
    }
    .sitemap {
      border-bottom: 1px solid var(--bino-border);
      padding-bottom: var(--bino-space-md);
      margin-bottom: var(--bino-space-sm);
    }
    .route-list {
      list-style: none;
      margin: var(--bino-space-sm) 0 0 0;
      padding: 0;
      display: flex;
      flex-direction: column;
      gap: var(--bino-space-xs);
    }
    .route-list li a {
      display: block;
      padding: var(--bino-space-sm) 0.75rem;
      border-radius: var(--bino-radius);
      text-decoration: none;
      color: var(--bino-text-muted);
      font-size: var(--bino-font-size-md);
      transition: background var(--bino-transition-fast);
    }
    .route-list li a:hover {
      background: var(--bino-surface-hover);
    }
    .route-list li.active a {
      background: var(--bino-surface-active);
      color: var(--bino-active-text);
      font-weight: 500;
    }
    .param-group {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
    }
    .param-group.missing {
      background: var(--bino-error-bg);
      border: 1px solid #fecaca;
      border-radius: 8px;
      padding: 0.75rem;
      margin: -0.25rem;
    }
    .param-group.missing .param-label {
      color: var(--bino-error);
    }
    .param-label {
      font-size: var(--bino-font-size-base);
      font-weight: 500;
      color: var(--bino-text-muted);
    }
    .param-label .required {
      color: var(--bino-error);
      margin-left: 2px;
    }
    .param-desc {
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      margin: 0;
    }
    .param-input {
      padding: var(--bino-space-sm) 0.75rem;
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-md);
      font-family: inherit;
      transition: border-color var(--bino-transition-fast), box-shadow var(--bino-transition-fast);
    }
    .param-input:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-primary-ring);
    }
    .param-input.invalid {
      border-color: var(--bino-error);
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
      gap: var(--bino-space-sm);
    }
    .range-values {
      display: flex;
      justify-content: space-between;
      align-items: center;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text-muted);
      font-weight: 500;
    }
    .range-value {
      background: var(--bino-surface-hover);
      padding: var(--bino-space-xs) var(--bino-space-sm);
      border-radius: 4px;
      min-width: 3rem;
      text-align: center;
    }
    .range-sep {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-md);
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
      background: var(--bino-border);
      border-radius: 3px;
    }
    .range-slider::-webkit-slider-thumb {
      -webkit-appearance: none;
      appearance: none;
      width: 18px;
      height: 18px;
      background: var(--bino-surface);
      border: 2px solid var(--bino-primary);
      border-radius: 50%;
      cursor: pointer;
      pointer-events: auto;
      margin-top: -6px;
      box-shadow: var(--bino-shadow-header);
    }
    .range-slider::-moz-range-track {
      width: 100%;
      height: 6px;
      background: var(--bino-border);
      border-radius: 3px;
    }
    .range-slider::-moz-range-thumb {
      width: 14px;
      height: 14px;
      background: var(--bino-surface);
      border: 2px solid var(--bino-primary);
      border-radius: 50%;
      cursor: pointer;
      pointer-events: auto;
      box-shadow: var(--bino-shadow-header);
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
      padding: 0.625rem var(--bino-space-md);
      background: var(--bino-primary);
      color: var(--bino-surface);
      border: none;
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-md);
      font-weight: 500;
      cursor: pointer;
      transition: background var(--bino-transition-fast);
    }
    .apply-btn:hover {
      background: var(--bino-primary-hover);
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
  `;

  constructor() {
    super();
    this.routes = {};
    this.queryParams = [];
    this.missingParams = [];
    this.currentPath = '/';
    this._loading = false;
  }

  // Public methods for JS updates (used by serve-app.js)
  updateConfig(config) {
    if (config.routes !== undefined) this.routes = config.routes;
    if (config.queryParams !== undefined) this.queryParams = config.queryParams;
    if (config.missingParams !== undefined) this.missingParams = config.missingParams;
    if (config.currentPath !== undefined) this.currentPath = config.currentPath;
  }

  setLoading(loading) {
    this._loading = loading;
  }

  render() {
    var routeKeys = Object.keys(this.routes).sort();
    var hasRoutes = routeKeys.length > 0;
    var hasParams = this.queryParams.length > 0;

    // Hide the panel when there is nothing to show
    this.hidden = !hasRoutes && !hasParams;

    var urlParams = new URLSearchParams(window.location.search);

    return html`
      ${hasRoutes ? this._renderNavigation(routeKeys) : ''}
      ${hasParams ? this._renderParams(urlParams) : ''}
    `;
  }

  _renderNavigation(routeKeys) {
    return html`
      <div class="sitemap">
        <h3>Navigation</h3>
        <ul class="route-list">
          ${routeKeys.map(path => {
            var title = this.routes[path] || path;
            var isActive = path === this.currentPath;
            return html`
              <li class=${isActive ? 'active' : ''}>
                <a href=${path} @click=${(e) => this._onRouteClick(e, path)}>${title}</a>
              </li>
            `;
          })}
        </ul>
      </div>
    `;
  }

  _renderParams(urlParams) {
    return html`
      <h3>Parameters</h3>
      ${this.queryParams.map(param => this._renderParamGroup(param, urlParams))}
      <button type="button" class="apply-btn"
        ?disabled=${this._loading}
        @click=${this._onApply}>
        ${this._loading ? 'Loading...' : 'Apply'}
      </button>
    `;
  }

  _renderParamGroup(param, urlParams) {
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
    var isMissing = this.missingParams.indexOf(param.name) !== -1;

    return html`
      <div class="param-group ${isMissing ? 'missing' : ''}">
        <label class="param-label" for="param-${param.name}">
          ${param.name}${param.required ? html`<span class="required">*</span>` : ''}
        </label>
        ${param.description ? html`<p class="param-desc">${param.description}</p>` : ''}
        ${this._buildInput(param, value, value2, isMissing)}
      </div>
    `;
  }

  _buildInput(param, value, value2, isMissing) {
    var type = param.type || 'string';
    var opts = param.options || {};
    var invalidClass = isMissing ? ' invalid' : '';

    switch (type) {
      case 'number':
        return html`<input type="number"
          class="param-input${invalidClass}" id="param-${param.name}"
          name=${param.name} .value=${value}
          data-required=${param.required}
          placeholder=${param.default != null ? String(param.default) : ''}
          min=${opts.min ?? ''} max=${opts.max ?? ''} step=${opts.step ?? ''}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;

      case 'number_range': {
        var minVal = opts.min !== undefined ? opts.min : 0;
        var maxVal = opts.max !== undefined ? opts.max : 100;
        var stepVal = opts.step !== undefined ? opts.step : 1;
        var curMin = value !== '' ? parseFloat(value) : minVal;
        var curMax = value2 !== '' ? parseFloat(value2) : maxVal;
        return html`
          <div class="range-slider-container">
            <div class="range-values">
              <span class="range-value" id="range-min-${param.name}">${curMin}</span>
              <span class="range-sep">\u2013</span>
              <span class="range-value" id="range-max-${param.name}">${curMax}</span>
            </div>
            <div class="dual-range">
              <input type="range" class="param-input range-slider range-min${invalidClass}"
                name=${param.name} .value=${String(curMin)}
                min=${minVal} max=${maxVal} step=${stepVal}
                data-required=${param.required}
                @input=${this._onRangeInput}>
              <input type="range" class="param-input range-slider range-max"
                name="${param.name}_max" .value=${String(curMax)}
                min=${minVal} max=${maxVal} step=${stepVal}
                data-required="false"
                @input=${this._onRangeInput}>
            </div>
          </div>`;
      }

      case 'select':
        return html`
          <select class="param-input param-select${invalidClass}"
            name=${param.name} data-required=${param.required}
            @keypress=${this._onKeypress} @input=${this._onInputChange}>
            ${!param.required ? html`<option value="">-- Select --</option>` : ''}
            ${(opts.items || []).map(item => html`
              <option value=${item.value} ?selected=${value === item.value}>
                ${item.label || item.value}
              </option>
            `)}
          </select>`;

      case 'date':
        return html`<input type="date" class="param-input${invalidClass}"
          name=${param.name} .value=${value}
          data-required=${param.required}
          placeholder=${param.default != null ? String(param.default) : ''}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;

      case 'date_time':
        return html`<input type="datetime-local" class="param-input${invalidClass}"
          name=${param.name} .value=${value}
          data-required=${param.required}
          placeholder=${param.default != null ? String(param.default) : ''}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;

      case 'string':
      default:
        return html`<input type="text" class="param-input${invalidClass}"
          name=${param.name} .value=${value}
          data-required=${param.required}
          placeholder=${param.default != null ? String(param.default) : ''}
          @keypress=${this._onKeypress} @input=${this._onInputChange}>`;
    }
  }

  updated() {
    this._setupRangeSliders();
  }

  _setupRangeSliders() {
    var self = this;
    this.renderRoot.querySelectorAll('.dual-range').forEach(function(container) {
      var minSlider = container.querySelector('.range-min');
      var maxSlider = container.querySelector('.range-max');
      if (!minSlider || !maxSlider) return;

      // Only attach listeners once
      if (minSlider._rangeSetup) return;
      minSlider._rangeSetup = true;

      var minDisplay = self.renderRoot.getElementById('range-min-' + minSlider.name);
      var maxDisplay = self.renderRoot.getElementById('range-max-' + maxSlider.name);

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

  _onRouteClick(e, path) {
    e.preventDefault();
    this.dispatchEvent(new CustomEvent('bino-navigate', {
      detail: { path: path },
      bubbles: true,
      composed: true
    }));
  }

  _onKeypress(e) {
    if (e.key === 'Enter') this._onApply();
  }

  _onInputChange(e) {
    var input = e.target;
    input.classList.remove('invalid');
    var group = input.closest('.param-group');
    if (group) group.classList.remove('missing');
  }

  _onRangeInput(e) {
    this._onInputChange(e);
  }

  _onApply() {
    var inputs = this.renderRoot.querySelectorAll('.param-input');
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
