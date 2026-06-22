import { StandardColumn } from './widgets/registry';

/** A field rendered into the form: its metadata + the value-control HTML. */
export interface FormRow {
    /** Dotted path within the document, e.g. ["spec", "title"]. */
    path: string[];
    /** Display label (the field key). */
    label: string;
    /** Optional field description (shown as a hint). */
    description?: string;
    /** Whether the field is required by the schema. */
    required: boolean;
    /** Value type, used by the webview to coerce edited values. */
    valueType: string;
    /** The value control HTML for a leaf field (generic control or widget fragment). */
    controlHtml?: string;
    /** True when a custom DesignerWidget rendered this row (for styling/debug). */
    widget?: string;
    /**
     * Nested rows for an expandable object field. When present the row renders as
     * a collapsible group (its scalar children edit through their own dotted
     * paths); `controlHtml` is then a short summary shown on the group header.
     */
    children?: FormRow[];
}

/** host → webview: replace the form for the shown component. */
export interface SetFormMessage {
    type: 'setForm';
    kind: string;
    name: string;
    /** The data-binding control HTML (dataset picker), or '' if the kind has none. */
    bindingHtml: string;
    rows: FormRow[];
}

/** host → webview: deliver columns for the bound dataset to column-aware widgets. */
export interface ColumnsMessage {
    type: 'columns';
    dataset: string;
    columns: string[];
    standardColumns: StandardColumn[];
}

/** webview → host: a field's value changed. */
export interface EditFieldMessage {
    type: 'editField';
    docIndex: number;
    path: string[];
    value: unknown;
}

/** webview → host: the user wants to (re)bind the dataset. */
export interface PickDatasetMessage {
    type: 'pickDataset';
    current?: string;
}

export type ToWebview = SetFormMessage | ColumnsMessage;
export type ToHost = EditFieldMessage | PickDatasetMessage;

/** Build the complete designer webview HTML (CSP mirrors the tree editor). */
export function getDesignerHtml(): string {
    return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline';">
    <title>Component Designer</title>
    <style>${getStyles()}</style>
</head>
<body>
    <div id="header" class="header"></div>
    <div id="binding" class="binding"></div>
    <div id="form" class="form">
        <div class="empty-state">Open the designer on an embeddable component.</div>
    </div>
    <script>${getScript()}</script>
</body>
</html>`;
}

function getStyles(): string {
    return `
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            color: var(--vscode-foreground);
            background: var(--vscode-editor-background);
            padding: 0 0 16px 0;
        }
        .empty-state {
            display: flex;
            align-items: center;
            justify-content: center;
            height: 60vh;
            color: var(--vscode-descriptionForeground);
            text-align: center;
            padding: 0 24px;
        }
        .header {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 10px 14px;
            border-bottom: 1px solid var(--vscode-panel-border);
            position: sticky;
            top: 0;
            background: var(--vscode-sideBar-background, var(--vscode-editor-background));
            z-index: 10;
        }
        .header:empty { display: none; }
        .kind-badge {
            background: var(--vscode-badge-background);
            color: var(--vscode-badge-foreground);
            padding: 1px 8px;
            border-radius: 10px;
            font-size: 0.85em;
            font-weight: 500;
        }
        .doc-name { font-weight: 600; }
        .binding {
            padding: 8px 14px;
            border-bottom: 1px solid var(--vscode-panel-border);
        }
        .binding:empty { display: none; }
        .binding-row {
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .binding-label {
            color: var(--vscode-descriptionForeground);
            font-size: 0.85em;
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }
        .binding-value { font-weight: 500; }
        .binding-empty { color: var(--vscode-descriptionForeground); font-style: italic; }
        .link-btn {
            border: none;
            background: transparent;
            color: var(--vscode-textLink-foreground);
            cursor: pointer;
            font-size: 0.9em;
            padding: 2px 4px;
        }
        .link-btn:hover { text-decoration: underline; }

        .form { padding: 6px 0; }
        .field-row {
            display: grid;
            grid-template-columns: 38% auto;
            gap: 8px;
            align-items: start;
            padding: 6px 14px;
            border-bottom: 1px solid var(--vscode-panel-border, transparent);
        }
        .field-row:hover { background: var(--vscode-list-hoverBackground); }
        .field-label-wrap { overflow: hidden; }
        .field-label {
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        .field-required { font-weight: 600; }
        .field-desc {
            font-size: 0.8em;
            color: var(--vscode-descriptionForeground);
            margin-top: 2px;
            white-space: normal;
        }
        .field-control { min-width: 0; }

        .field-group { border-bottom: 1px solid var(--vscode-panel-border, transparent); }
        .field-group-summary {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 6px 14px;
            cursor: pointer;
            user-select: none;
            list-style: none;
        }
        .field-group-summary::-webkit-details-marker { display: none; }
        .field-group-summary::before {
            content: '\\25B6';
            font-size: 0.7em;
            transition: transform 0.15s;
        }
        .field-group[open] > .field-group-summary::before { transform: rotate(90deg); }
        .field-group-summary:hover { background: var(--vscode-list-hoverBackground); }

        .designer-input,
        .designer-select {
            width: 100%;
            background: var(--vscode-input-background);
            color: var(--vscode-input-foreground, var(--vscode-foreground));
            border: 1px solid var(--vscode-input-border, transparent);
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            padding: 3px 6px;
            border-radius: 2px;
            outline: none;
        }
        .designer-input:focus,
        .designer-select:focus { border-color: var(--vscode-focusBorder); }
        .designer-select {
            background: var(--vscode-dropdown-background);
            color: var(--vscode-dropdown-foreground);
            border-color: var(--vscode-dropdown-border);
        }
        .designer-checkbox { display: flex; align-items: center; gap: 6px; cursor: pointer; }
        .designer-checkbox input { accent-color: var(--vscode-checkbox-background); }
        .value-summary {
            color: var(--vscode-descriptionForeground);
            font-size: 0.9em;
            font-style: italic;
        }
        .widget-tag {
            display: inline-block;
            font-size: 0.7em;
            color: var(--vscode-descriptionForeground);
            opacity: 0.7;
            margin-left: 6px;
        }

        /* --- Bespoke IBCS widgets (brief 04) --- */
        .dw { display: flex; flex-direction: column; gap: 6px; }
        .dw-tabs { display: flex; gap: 4px; }
        .dw-tab {
            border: 1px solid var(--vscode-panel-border, transparent);
            background: transparent;
            color: var(--vscode-descriptionForeground);
            cursor: pointer;
            font-size: 0.8em;
            padding: 1px 8px;
            border-radius: 3px;
        }
        .dw-tab-active {
            color: var(--vscode-foreground);
            border-color: var(--vscode-focusBorder);
            background: var(--vscode-list-activeSelectionBackground, transparent);
        }
        .dw-pane { display: none; flex-direction: column; gap: 6px; }
        .dw-pane-active { display: flex; }
        .dw-mode { display: flex; gap: 12px; }
        .dw-radio { display: inline-flex; align-items: center; gap: 4px; cursor: pointer; font-size: 0.9em; }
        .dw-hint, .dw-raw-hint { font-size: 0.78em; color: var(--vscode-descriptionForeground); }
        .dw-warn { font-size: 0.8em; color: var(--vscode-editorWarning-foreground, var(--vscode-descriptionForeground)); }
        .dw-chips { display: flex; flex-wrap: wrap; gap: 4px; }
        .dw-chip {
            border: 1px solid var(--vscode-input-border, var(--vscode-panel-border));
            background: var(--vscode-input-background);
            color: var(--vscode-descriptionForeground);
            cursor: pointer;
            font-size: 0.85em;
            padding: 1px 8px;
            border-radius: 10px;
        }
        .dw-chip-on {
            background: var(--vscode-badge-background);
            color: var(--vscode-badge-foreground);
            border-color: var(--vscode-focusBorder);
            font-weight: 500;
        }
        .dw-rows { display: flex; flex-direction: column; gap: 6px; }
        .dw-row { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
        .dw-row-wrap, .dw-row-attr { gap: 4px; }
        .dw-row-edge { flex-direction: column; align-items: stretch; gap: 3px; border-bottom: 1px dashed var(--vscode-panel-border); padding-bottom: 4px; }
        .dw-edge-main { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
        .dw-field { display: flex; align-items: center; gap: 8px; }
        .dw-flabel { font-size: 0.8em; color: var(--vscode-descriptionForeground); min-width: 42px; display: inline-flex; align-items: center; gap: 4px; }
        .dw-vs, .dw-arrow { color: var(--vscode-descriptionForeground); font-size: 0.85em; }
        .dw-preview { flex-basis: 100%; font-size: 0.78em; color: var(--vscode-descriptionForeground); font-style: italic; }
        .dw-select, .dw-input {
            background: var(--vscode-input-background);
            color: var(--vscode-input-foreground, var(--vscode-foreground));
            border: 1px solid var(--vscode-input-border, transparent);
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            padding: 2px 5px;
            border-radius: 2px;
            outline: none;
        }
        .dw-select:focus, .dw-input:focus { border-color: var(--vscode-focusBorder); }
        .dw-sm { min-width: 64px; flex: 0 1 auto; }
        .dw-xs { width: 56px; }
        .dw-input { flex: 1 1 80px; }
        .dw-add {
            align-self: flex-start;
            border: 1px dashed var(--vscode-input-border, var(--vscode-panel-border));
            background: transparent;
            color: var(--vscode-textLink-foreground);
            cursor: pointer;
            font-size: 0.85em;
            padding: 2px 10px;
            border-radius: 3px;
        }
        .dw-del {
            border: none; background: transparent;
            color: var(--vscode-descriptionForeground);
            cursor: pointer; font-size: 1.1em; line-height: 1; padding: 0 4px;
        }
        .dw-del:hover { color: var(--vscode-errorForeground); }
        .dw-style summary { font-size: 0.8em; color: var(--vscode-descriptionForeground); cursor: pointer; }
        .dw-style-body { display: flex; flex-wrap: wrap; gap: 8px; padding: 4px 0 0 12px; }
        .dw-color { width: 28px; height: 20px; padding: 0; border: none; background: none; cursor: pointer; }
        .dw-raw {
            width: 100%;
            background: var(--vscode-input-background);
            color: var(--vscode-input-foreground, var(--vscode-foreground));
            border: 1px solid var(--vscode-input-border, transparent);
            font-family: var(--vscode-editor-font-family, monospace);
            font-size: 0.9em;
            padding: 4px 6px;
            border-radius: 2px;
            resize: vertical;
        }
        .dw-raw.dw-raw-error { border-color: var(--vscode-errorForeground); }

        ::-webkit-scrollbar { width: 8px; height: 8px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb { background: var(--vscode-scrollbarSlider-background); border-radius: 4px; }
        ::-webkit-scrollbar-thumb:hover { background: var(--vscode-scrollbarSlider-hoverBackground); }
    `;
}

function getScript(): string {
    return `
    const vscode = acquireVsCodeApi();
    let currentDocIndex = 0;

    // Coerce an edited control's string value to the field's value type.
    function coerce(raw, type) {
        if (type === 'number' || type === 'integer') {
            const n = Number(raw);
            return Number.isNaN(n) ? raw : n;
        }
        if (type === 'boolean') { return raw === true || raw === 'true'; }
        return raw;
    }

    // Post a field edit to the host (→ AuthoringClient.edit).
    function emitEdit(control) {
        const row = control.closest('.field-row');
        if (!row) { return; }
        let path;
        try { path = JSON.parse(row.getAttribute('data-path') || '[]'); } catch (e) { return; }
        const type = control.getAttribute('data-value-type') || row.getAttribute('data-value-type') || 'string';
        let raw;
        if (control.type === 'checkbox') { raw = control.checked; }
        else { raw = control.value; }
        vscode.postMessage({ type: 'editField', docIndex: currentDocIndex, path: path, value: coerce(raw, type) });
    }

    // Any control marked data-designer-edit is a field sink (generic or widget).
    function wireControls(root) {
        const controls = root.querySelectorAll('[data-designer-edit]');
        controls.forEach((control) => {
            const tag = control.tagName;
            if (tag === 'SELECT' || control.type === 'checkbox') {
                control.addEventListener('change', () => emitEdit(control));
            } else {
                // text/number: commit on change (blur/Enter) and on Enter key.
                control.addEventListener('change', () => emitEdit(control));
                control.addEventListener('keydown', (e) => {
                    if (e.key === 'Enter') { e.preventDefault(); control.blur(); }
                });
            }
        });
    }

    // ---- Bespoke IBCS widgets (brief 04) ----
    // Widgets render pure HTML tagged with data-widget-kind; this static script
    // (CSP-inline, no eval) wires their interactions and posts the assembled
    // structured value through editField at the row's path.

    // Post a field edit carrying a structured value verbatim (not string-coerced).
    function postEdit(el, value) {
        const row = el.closest('.field-row');
        if (!row) { return; }
        let path;
        try { path = JSON.parse(row.getAttribute('data-path') || '[]'); } catch (e) { return; }
        vscode.postMessage({ type: 'editField', docIndex: currentDocIndex, path: path, value: value });
    }

    function q(root, role) { return root.querySelector('[data-dw-role="' + role + '"]'); }
    function qa(root, role) { return Array.prototype.slice.call(root.querySelectorAll('[data-dw-role="' + role + '"]')); }
    // Roles nested inside a child row must not be picked up by the parent scope.
    function ownRows(rowsEl) {
        return Array.prototype.slice.call(rowsEl.children).filter((c) => c.getAttribute('data-dw-role') === 'row');
    }

    function wireWidgets(form) {
        form.querySelectorAll('.dw[data-widget-kind]').forEach((dw) => {
            wireTabs(dw);
            wireRaw(dw);
            const kind = dw.getAttribute('data-widget-kind');
            if (kind === 'scenario') { wireScenario(dw); }
            else if (kind === 'variance') { wireVariance(dw); }
            else if (kind === 'stack') { wireStack(dw); }
            else if (kind === 'aggregate') { wireAggregate(dw); }
            else if (kind === 'edges') { wireEdges(dw); }
        });
    }

    // Structured/raw tab switch (purely visual; each pane emits independently).
    function wireTabs(dw) {
        dw.querySelectorAll('.dw-tab').forEach((tab) => {
            tab.addEventListener('click', () => {
                const target = tab.getAttribute('data-dw-tab');
                dw.querySelectorAll('.dw-tab').forEach((t) => t.classList.toggle('dw-tab-active', t === tab));
                dw.querySelectorAll('.dw-pane').forEach((p) => {
                    p.classList.toggle('dw-pane-active', p.getAttribute('data-dw-pane') === target);
                });
            });
        });
        // Default the structured pane visible when present (raw-only widgets ship
        // their raw pane pre-activated server-side).
        const structured = dw.querySelector('.dw-pane[data-dw-pane="structured"]');
        if (structured && !dw.querySelector('.dw-pane-active')) {
            structured.classList.add('dw-pane-active');
        }
    }

    // Raw-JSON fallback: parse on change; emit the parsed value, or flag invalid
    // (the authoring edit re-validates the shape before writing either way).
    function wireRaw(dw) {
        const ta = dw.querySelector('.dw-raw');
        if (!ta) { return; }
        ta.addEventListener('change', () => {
            const text = ta.value.trim();
            // Empty is a no-op (use the form controls or delete to clear a field),
            // so blanking the box never fires a spurious schema rejection.
            if (text === '') { ta.classList.remove('dw-raw-error'); return; }
            let parsed;
            try { parsed = JSON.parse(text); }
            catch (e) { ta.classList.add('dw-raw-error'); return; }
            ta.classList.remove('dw-raw-error');
            postEdit(ta, parsed);
        });
    }

    function wireScenario(dw) {
        const modeRadios = qa(dw, 'mode');
        const arrayPane = dw.querySelector('.dw-mode-array');
        const literalPane = dw.querySelector('.dw-mode-literal');
        const literalSel = q(dw, 'literal');
        const chipsWrap = q(dw, 'chips');

        function emitArray() {
            const on = chipsWrap ? Array.prototype.slice.call(chipsWrap.querySelectorAll('.dw-chip-on')) : [];
            postEdit(dw, on.map((c) => c.getAttribute('data-slot')));
        }
        function emitLiteral() { if (literalSel) { postEdit(dw, literalSel.value); } }

        modeRadios.forEach((r) => r.addEventListener('change', () => {
            const isArray = r.value === 'array';
            if (arrayPane) { arrayPane.hidden = !isArray; }
            if (literalPane) { literalPane.hidden = isArray; }
            if (isArray) { emitArray(); } else { emitLiteral(); }
        }));
        if (chipsWrap) {
            chipsWrap.querySelectorAll('.dw-chip').forEach((chip) => chip.addEventListener('click', () => {
                chip.classList.toggle('dw-chip-on');
                emitArray();
            }));
        }
        if (literalSel) { literalSel.addEventListener('change', emitLiteral); }
    }

    function decodeVariance(token) {
        let rest = token, prefix;
        if (rest.indexOf('dr') === 0) { prefix = 'dr'; rest = rest.slice(2); }
        else if (rest.indexOf('d') === 0) { prefix = 'd'; rest = rest.slice(1); }
        else { return ''; }
        const parts = rest.replace(/^_/, '').split('_');
        if (parts.length !== 3) { return ''; }
        const kind = prefix === 'dr' ? 'relative (%)' : 'absolute';
        const phrase = { pos: 'positive sentiment — more is better', neg: 'negative sentiment — more is worse', neu: 'neutral sentiment' }[parts[2]] || '';
        return kind + ' variance of ' + parts[1] + ' vs ' + parts[0] + (phrase ? '; ' + phrase : '');
    }

    function wireVariance(dw) {
        const rowsEl = q(dw, 'rows');
        if (!rowsEl) { return; }
        const slots = JSON.parse(rowsEl.getAttribute('data-slots') || '[]');
        const sentiments = JSON.parse(rowsEl.getAttribute('data-sentiments') || '[]');

        function tokenOf(row) {
            return q(row, 'prefix').value + '_' + q(row, 'base').value + '_' + q(row, 'compare').value + '_' + q(row, 'sentiment').value;
        }
        function emit() { postEdit(dw, ownRows(rowsEl).map(tokenOf)); }
        function refresh(row) {
            const prev = q(row, 'preview');
            if (prev) { prev.textContent = decodeVariance(tokenOf(row)); }
        }
        function wireRow(row) {
            qa(row, 'prefix').concat(qa(row, 'base'), qa(row, 'compare'), qa(row, 'sentiment')).forEach((sel) => {
                if (sel.closest('[data-dw-role="row"]') !== row) { return; }
                sel.addEventListener('change', () => { refresh(row); emit(); });
            });
            const del = q(row, 'del-row');
            if (del) { del.addEventListener('click', () => { row.remove(); emit(); }); }
        }
        ownRows(rowsEl).forEach(wireRow);
        const add = q(dw, 'add-row');
        if (add) {
            add.addEventListener('click', () => {
                const opt = (sel, list) => list.map((s) => '<option value="' + s + '"' + (s === sel ? ' selected' : '') + '>' + s + '</option>').join('');
                const b = slots[0] || '', a = slots[1] || slots[0] || '';
                const row = document.createElement('div');
                row.className = 'dw-row'; row.setAttribute('data-dw-role', 'row');
                row.innerHTML =
                    '<select class="dw-select dw-sm" data-dw-role="prefix"><option value="d" selected>d</option><option value="dr">dr</option></select>' +
                    '<select class="dw-select dw-sm" data-dw-role="base">' + opt(b, slots) + '</select>' +
                    '<span class="dw-vs">vs</span>' +
                    '<select class="dw-select dw-sm" data-dw-role="compare">' + opt(a, slots) + '</select>' +
                    '<select class="dw-select dw-sm" data-dw-role="sentiment">' + opt('pos', sentiments) + '</select>' +
                    '<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">\\u00d7</button>' +
                    '<div class="dw-preview" data-dw-role="preview"></div>';
                rowsEl.appendChild(row);
                wireRow(row); refresh(row); emit();
            });
        }
    }

    function wireStack(dw) {
        function emit() {
            const by = (qa(dw, 'by').find((r) => r.checked) || {}).value || 'scenarios';
            const out = { by: by };
            const mode = q(dw, 'mode'); if (mode && mode.value) { out.mode = mode.value; }
            const order = q(dw, 'order'); if (order && order.value) { out.order = order.value; }
            postEdit(dw, out);
        }
        qa(dw, 'by').forEach((r) => r.addEventListener('change', emit));
        ['mode', 'order'].forEach((role) => { const el = q(dw, role); if (el) { el.addEventListener('change', emit); } });
    }

    function wireAggregate(dw) {
        const rowsEl = q(dw, 'rows');
        if (!rowsEl) { return; }
        const field = rowsEl.getAttribute('data-field');
        const columns = JSON.parse(rowsEl.getAttribute('data-columns') || '[]');
        const nullToggle = q(dw, 'null');
        const addBtn = q(dw, 'add-row');

        function objOf(row) {
            if (field === 'attributes') {
                const fn = q(row, 'fn').value, arg = q(row, 'arg').value.trim();
                return { label: q(row, 'label').value, expression: fn + '(' + arg + ')' };
            }
            if (field === 'columnthereof') {
                const o = {};
                const sc = q(row, 'scenario').value.trim(); if (sc) { o.scenario = sc; }
                const nm = q(row, 'name').value.trim(); if (nm) { o.name = nm; }
                const sg = q(row, 'subGroups').value.trim();
                if (sg) { o.subGroups = sg.split(',').map((s) => s.trim()).filter(Boolean); }
                return o;
            }
            const o = {};
            const rg = q(row, 'rowGroup').value.trim(); if (rg) { o.rowGroup = rg; }
            const cat = q(row, 'category').value.trim(); if (cat) { o.category = cat; }
            if (field === 'thereof') { const sub = q(row, 'subCategory').value.trim(); if (sub) { o.subCategory = sub; } }
            return o;
        }
        function emit() {
            if (nullToggle && nullToggle.checked) { postEdit(dw, null); return; }
            postEdit(dw, ownRows(rowsEl).map(objOf));
        }
        function wireRow(row) {
            row.querySelectorAll('input, select').forEach((c) => {
                if (c.closest('[data-dw-role="row"]') !== row) { return; }
                c.addEventListener('change', emit);
            });
            const del = q(row, 'del-row');
            if (del) { del.addEventListener('click', () => { row.remove(); emit(); }); }
        }
        ownRows(rowsEl).forEach(wireRow);
        if (nullToggle) {
            nullToggle.addEventListener('change', () => {
                const on = nullToggle.checked;
                rowsEl.hidden = on; if (addBtn) { addBtn.hidden = on; }
                emit();
            });
        }
        if (addBtn) {
            addBtn.addEventListener('click', () => {
                const row = document.createElement('div');
                row.setAttribute('data-dw-role', 'row');
                if (field === 'attributes') {
                    row.className = 'dw-row dw-row-attr';
                    const fns = ['lit', 'set', 'first', 'last', 'min', 'max', 'avg', 'sum'];
                    const listId = 'dwcols-' + Math.random().toString(36).slice(2, 8);
                    row.innerHTML =
                        '<input class="dw-input dw-sm" type="text" data-dw-role="label" placeholder="label">' +
                        '<select class="dw-select dw-sm" data-dw-role="fn">' + fns.map((f) => '<option value="' + f + '"' + (f === 'set' ? ' selected' : '') + '>' + f + '</option>').join('') + '</select>' +
                        '<input class="dw-input dw-sm" type="text" data-dw-role="arg" list="' + listId + '" placeholder="field or _custom">' +
                        '<datalist id="' + listId + '">' + columns.map((c) => '<option value="' + c + '">').join('') + '</datalist>' +
                        '<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">\\u00d7</button>';
                } else if (field === 'columnthereof') {
                    row.className = 'dw-row dw-row-wrap';
                    row.innerHTML =
                        '<input class="dw-input dw-sm" type="text" data-dw-role="scenario" placeholder="scenario">' +
                        '<input class="dw-input dw-sm" type="text" data-dw-role="name" placeholder="name">' +
                        '<input class="dw-input dw-sm" type="text" data-dw-role="subGroups" placeholder="subGroups (comma-separated)">' +
                        '<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">\\u00d7</button>';
                } else {
                    row.className = 'dw-row dw-row-wrap';
                    const sub = field === 'thereof' ? '<input class="dw-input dw-sm" type="text" data-dw-role="subCategory" placeholder="subCategory">' : '';
                    row.innerHTML =
                        '<input class="dw-input dw-sm" type="text" data-dw-role="rowGroup" placeholder="rowGroup">' +
                        '<input class="dw-input dw-sm" type="text" data-dw-role="category" placeholder="category">' + sub +
                        '<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">\\u00d7</button>';
                }
                rowsEl.appendChild(row);
                wireRow(row);
            });
        }
    }

    function wireEdges(dw) {
        const rowsEl = q(dw, 'rows');
        if (!rowsEl) { return; }
        function edgeOf(row) {
            const o = { from: q(row, 'from').value.trim(), to: q(row, 'to').value.trim() };
            const op = q(row, 'operator').value; if (op) { o.operator = op; }
            const label = q(row, 'label').value.trim(); if (label) { o.label = label; }
            const styleOn = q(row, 'style-on');
            if (styleOn && styleOn.checked) {
                const style = {};
                const color = q(row, 'style-color').value; if (color) { style.color = color; }
                const w = q(row, 'style-width').value; if (w !== '') { style.width = Number(w); }
                const dash = q(row, 'style-dasharray').value.trim(); if (dash) { style.dasharray = dash; }
                if (Object.keys(style).length > 0) { o.style = style; }
            }
            return o;
        }
        function emit() { postEdit(dw, ownRows(rowsEl).map(edgeOf)); }
        function wireRow(row) {
            row.querySelectorAll('input, select').forEach((c) => {
                if (c.closest('[data-dw-role="row"]') !== row) { return; }
                c.addEventListener('change', emit);
            });
            const del = q(row, 'del-row');
            if (del) { del.addEventListener('click', () => { row.remove(); emit(); }); }
        }
        ownRows(rowsEl).forEach(wireRow);
        const add = q(dw, 'add-row');
        if (add) {
            add.addEventListener('click', () => {
                const ops = ['', '*', '/', '+', '-', 'x', '\\u00f7', 'none'];
                const row = document.createElement('div');
                row.className = 'dw-row dw-row-edge'; row.setAttribute('data-dw-role', 'row');
                row.innerHTML =
                    '<div class="dw-edge-main">' +
                    '<input class="dw-input dw-sm" type="text" data-dw-role="from" placeholder="from">' +
                    '<span class="dw-arrow">\\u2192</span>' +
                    '<input class="dw-input dw-sm" type="text" data-dw-role="to" placeholder="to">' +
                    '<select class="dw-select dw-sm" data-dw-role="operator">' + ops.map((o) => '<option value="' + o + '">' + o + '</option>').join('') + '</select>' +
                    '<input class="dw-input dw-sm" type="text" data-dw-role="label" placeholder="label">' +
                    '<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">\\u00d7</button>' +
                    '</div>' +
                    '<details class="dw-style"><summary>style</summary><div class="dw-style-body">' +
                    '<label class="dw-flabel">color <input class="dw-color" type="color" data-dw-role="style-color" value="#000000"></label>' +
                    '<label class="dw-flabel">width <input class="dw-input dw-xs" type="number" min="0" step="0.5" data-dw-role="style-width"></label>' +
                    '<label class="dw-flabel">dash <input class="dw-input dw-xs" type="text" data-dw-role="style-dasharray" placeholder="4 2"></label>' +
                    '<label class="dw-radio"><input type="checkbox" data-dw-role="style-on"> apply style</label>' +
                    '</div></details>';
                rowsEl.appendChild(row);
                wireRow(row);
            });
        }
    }

    function renderHeader(kind, name) {
        const el = document.getElementById('header');
        el.innerHTML =
            '<span class="kind-badge">' + escapeHtml(kind) + '</span>' +
            (name ? '<span class="doc-name">' + escapeHtml(name) + '</span>' : '');
    }

    function renderBinding(html) {
        document.getElementById('binding').innerHTML = html || '';
        wireControls(document.getElementById('binding'));
    }

    function renderRow(r, depth) {
        const labelClass = r.required ? 'field-label field-required' : 'field-label';
        const desc = r.description ? '<div class="field-desc">' + escapeHtml(r.description) + '</div>' : '';
        const tag = r.widget ? '<span class="widget-tag">' + escapeHtml(r.widget) + '</span>' : '';
        const pathAttr = escapeAttr(JSON.stringify(r.path));
        const typeAttr = escapeAttr(r.valueType || 'string');
        const indent = 'style="padding-left:' + (14 + depth * 14) + 'px"';

        // Expandable object: render a collapsible group with nested rows.
        if (r.children && r.children.length > 0) {
            const body = r.children.map((c) => renderRow(c, depth + 1)).join('');
            const summary = r.controlHtml ? '<span class="value-summary">' + escapeHtml(r.controlHtml) + '</span>' : '';
            return '<details class="field-group" open>' +
                '<summary class="field-group-summary" ' + indent + '>' +
                '<span class="' + labelClass + '">' + escapeHtml(r.label) + tag + '</span>' + summary +
                '</summary>' + desc + body + '</details>';
        }

        return '<div class="field-row" data-path="' + pathAttr + '" data-value-type="' + typeAttr + '" ' + indent + '>' +
            '<div class="field-label-wrap"><div class="' + labelClass + '">' + escapeHtml(r.label) + tag + '</div>' + desc + '</div>' +
            '<div class="field-control">' + (r.controlHtml || '') + '</div>' +
            '</div>';
    }

    function renderForm(rows) {
        const form = document.getElementById('form');
        if (!rows || rows.length === 0) {
            form.innerHTML = '<div class="empty-state">This component has no editable fields.</div>';
            return;
        }
        form.innerHTML = rows.map((r) => renderRow(r, 0)).join('');
        wireControls(form);
        wireWidgets(form);
    }

    // Push live columns into any column-aware widget that registered a receiver.
    function deliverColumns(msg) {
        if (typeof window.__binoOnColumns === 'function') {
            try { window.__binoOnColumns(msg); } catch (e) { /* widget receiver error */ }
        }
    }

    window.addEventListener('message', (event) => {
        const msg = event.data;
        if (!msg) { return; }
        switch (msg.type) {
            case 'setForm':
                renderHeader(msg.kind, msg.name);
                renderBinding(msg.bindingHtml);
                renderForm(msg.rows);
                break;
            case 'columns':
                deliverColumns(msg);
                break;
        }
    });

    function pickDataset(current) {
        vscode.postMessage({ type: 'pickDataset', current: current || undefined });
    }
    window.pickDataset = pickDataset;

    function escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str == null ? '' : String(str);
        return div.innerHTML;
    }
    function escapeAttr(str) {
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/"/g, '&quot;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }
    `;
}
