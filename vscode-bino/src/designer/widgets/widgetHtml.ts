/**
 * Shared HTML helpers for the bespoke IBCS widgets. Each widget renders a pure
 * HTML fragment (no inline script — the designer webview's static script wires
 * interactions by `data-widget-kind`, so the CSP needs no `unsafe-eval`). These
 * helpers keep the fragments terse and the structured/raw-JSON tab layout uniform.
 */

/** Escape a string for use in HTML text or a double-quoted attribute. */
export function esc(str: string): string {
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

/** Encode a value as a JSON string safe to embed in a double-quoted attribute. */
export function jsonAttr(value: unknown): string {
    return esc(JSON.stringify(value ?? null));
}

/** Render a native <select> with the given options and current selection. */
export function selectHtml(opts: {
    cls: string;
    role: string;
    options: readonly string[];
    selected?: string;
    includeEmpty?: boolean;
    emptyLabel?: string;
}): string {
    const items = [
        ...(opts.includeEmpty ? [`<option value="">${esc(opts.emptyLabel ?? '')}</option>`] : []),
        ...opts.options.map(
            o => `<option value="${esc(o)}"${o === opts.selected ? ' selected' : ''}>${esc(o)}</option>`
        ),
    ].join('');
    return `<select class="${esc(opts.cls)}" data-dw-role="${esc(opts.role)}">${items}</select>`;
}

/**
 * Wrap a widget in the standard structured/raw-JSON tab shell. The structured
 * pane holds the widget's bespoke controls; the raw pane is a JSON textarea that
 * round-trips the same value (the schema permits a JSON string for these fields,
 * and the authoring edit re-validates it before writing). `kind` drives the
 * webview's interaction wiring; `value` seeds the raw pane.
 */
export function widgetShell(opts: {
    kind: string;
    value: unknown;
    structuredHtml: string;
    /** Hide the structured tab (e.g. the value is an opaque JSON string already). */
    rawOnly?: boolean;
}): string {
    const rawText = opts.value === undefined || opts.value === null
        ? ''
        : typeof opts.value === 'string'
            ? opts.value
            : JSON.stringify(opts.value, null, 2);
    const tabs = opts.rawOnly
        ? ''
        : `<div class="dw-tabs">
            <button type="button" class="dw-tab dw-tab-active" data-dw-tab="structured">Form</button>
            <button type="button" class="dw-tab" data-dw-tab="raw">JSON</button>
          </div>`;
    const structuredPane = opts.rawOnly
        ? ''
        : `<div class="dw-pane" data-dw-pane="structured">${opts.structuredHtml}</div>`;
    const rawActive = opts.rawOnly ? ' dw-pane-active' : '';
    return `<div class="dw" data-widget-kind="${esc(opts.kind)}">
        ${tabs}
        ${structuredPane}
        <div class="dw-pane dw-pane-raw${rawActive}" data-dw-pane="raw">
            <textarea class="dw-raw" data-dw-role="raw" rows="4" spellcheck="false">${esc(rawText)}</textarea>
            <div class="dw-raw-hint">JSON for this field — saved as-is and re-validated on write.</div>
        </div>
    </div>`;
}
