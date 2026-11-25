/**
 * The page shell of the ComponentStyle and RuleSet editors: a nonce CSP, the
 * common CSS, and helpers both page scripts use. Pages build their DOM with
 * `h()` and textContent, never from HTML strings, and bind events with
 * addEventListener: no inline handlers.
 */
export function editorPage(nonce: string, title: string, css: string, script: string): string {
    return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';">
    <title>${title}</title>
    <style>${COMMON_CSS}${css}</style>
</head>
<body>
    <div id="top"></div>
    <div id="main"><div class="muted">Loading...</div></div>
    <script nonce="${nonce}">${COMMON_SCRIPT}${script}</script>
</body>
</html>`;
}

const COMMON_CSS = `
    body { font-family: var(--vscode-font-family); font-size: var(--vscode-font-size); color: var(--vscode-foreground); padding: 8px 14px; }
    .muted { color: var(--vscode-descriptionForeground); }
    .header { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 8px; }
    .header h2 { margin: 0; font-size: 1.15em; }
    .badge { font-size: 0.8em; padding: 1px 6px; border-radius: 8px; background: var(--vscode-badge-background); color: var(--vscode-badge-foreground); }
    .banner { padding: 8px 10px; margin: 8px 0; border-left: 3px solid var(--vscode-editorWarning-foreground); background: var(--vscode-textBlockQuote-background); }
    button { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 3px 10px; cursor: pointer; border-radius: 2px; }
    button:hover { background: var(--vscode-button-hoverBackground); }
    button.link { background: transparent; color: var(--vscode-textLink-foreground); padding: 0 4px; }
    button.icon { background: transparent; color: var(--vscode-descriptionForeground); padding: 0 4px; }
    button.icon:hover { color: var(--vscode-foreground); }
    input, select { background: var(--vscode-input-background); color: var(--vscode-input-foreground, var(--vscode-foreground)); border: 1px solid var(--vscode-input-border, transparent); padding: 2px 4px; font-family: inherit; font-size: inherit; }
    input::placeholder { color: var(--vscode-input-placeholderForeground); font-style: italic; }
    input[type=number] { width: 80px; }
    input.color-text { width: 90px; }
    input[type=color].unset { opacity: 0.15; }
    input[type=color] { width: 26px; height: 22px; padding: 0; border: none; background: none; vertical-align: middle; cursor: pointer; }
    .src { font-size: 0.8em; color: var(--vscode-descriptionForeground); margin-left: 4px; }
    .usedby { margin: 10px 0; padding: 6px 0; border-top: 1px solid var(--vscode-panel-border); }
    .usedby ul { margin: 4px 0; padding-left: 18px; }
    .usedby li { margin: 2px 0; }
    .docpicker { margin-bottom: 6px; }
`;

const COMMON_SCRIPT = `
    const vscode = acquireVsCodeApi();
    const saved = vscode.getState() || {};
    function h(tag, attrs, children) {
        const el = document.createElement(tag);
        for (const [k, v] of Object.entries(attrs || {})) {
            if (v === undefined || v === null || v === false) continue;
            if (k === 'text') el.textContent = String(v);
            else if (k === 'class') el.className = v;
            else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
            else el.setAttribute(k, v === true ? '' : String(v));
        }
        for (const c of [].concat(children || [])) {
            if (c === null || c === undefined || c === false) continue;
            el.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
        }
        return el;
    }
    function post(msg) { vscode.postMessage(msg); }
    function show(value) {
        if (value === undefined || value === null) return '';
        return typeof value === 'object' ? JSON.stringify(value) : String(value);
    }
    /** Re-render and give the focus back to the control with the same data-key. */
    function keepFocus(render) {
        const active = document.activeElement;
        const key = active && active.getAttribute && active.getAttribute('data-key');
        const scroll = window.scrollY;
        render();
        if (key) {
            const again = document.querySelector('[data-key="' + CSS.escape(key) + '"]');
            if (again) again.focus();
        }
        window.scrollTo(0, scroll);
    }
    function docPicker(state) {
        if (!state.docs || state.docs.length < 2) return null;
        const sel = h('select', { onchange: () => post({ type: 'select', name: sel.value }) },
            state.docs.map(n => h('option', { value: n, selected: n === state.current, text: n })));
        return h('div', { class: 'docpicker' }, [h('span', { class: 'muted', text: 'Document ' }), sel]);
    }
    function jsonBanner(state) {
        if (!state.jsonString) return null;
        return h('div', { class: 'banner' }, [
            h('div', { text: 'spec.content is a JSON string. The editor can only change a YAML object.' }),
            h('button', { onclick: () => post({ type: 'convert' }), text: 'Convert to YAML object' }),
        ]);
    }
    function usedByList(state, allText) {
        const items = (state.usedBy || []).map(e => h('li', {}, [
            h('button', { class: 'link', onclick: () => post({ type: 'openUsedBy', entry: e }), title: 'Open in the embedded preview', text: e.kind + ' ' + e.name }),
        ]));
        return h('div', { class: 'usedby' }, [
            h('strong', { text: 'Used by' }),
            allText ? h('div', { class: 'muted', text: allText }) : null,
            items.length ? h('ul', {}, items) : (allText ? null : h('div', { class: 'muted', text: 'No manifest names it.' })),
            items.length ? h('div', { class: 'muted', text: 'Click one to open it in the embedded preview. It follows each edit.' }) : null,
        ]);
    }
    /** A text input plus a colour swatch, bound to one value. */
    function colorInput(key, value, placeholder, onvalue) {
        const text = h('input', { class: 'color-text', 'data-key': key, value: show(value), placeholder: show(placeholder) });
        const hex = /^#[0-9a-fA-F]{6}$/;
        const base = hex.test(show(value)) ? value : (hex.test(show(placeholder)) ? placeholder : '#000000');
        const unset = !hex.test(show(value)) && !hex.test(show(placeholder));
        const swatch = h('input', { type: 'color', value: base, title: unset ? 'Not set: pick a colour' : 'Pick a colour', class: unset ? 'unset' : '' });
        text.addEventListener('change', () => onvalue(text.value.trim()));
        swatch.addEventListener('change', () => { text.value = swatch.value; onvalue(swatch.value); });
        return h('span', {}, [swatch, text]);
    }
`;
