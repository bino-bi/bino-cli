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
