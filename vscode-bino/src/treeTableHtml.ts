import { TreeDocument, TreeNode } from './yamlModel';
import { FieldDef } from './schemaResolver';

/**
 * Generate the complete HTML for the tree-table webview.
 */
export function getTreeTableHtml(
    documents: TreeDocument[],
    kindFieldsMap: Map<string, FieldDef[]>,
    metadataFieldsMap: Map<string, FieldDef[]>
): string {
    const docSections = documents.map(doc => renderDocSection(doc, kindFieldsMap.get(doc.kind), metadataFieldsMap.get(doc.kind || '_default'))).join('\n');

    return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline';">
    <title>Tree Editor</title>
    <style>${getStyles()}</style>
</head>
<body>
    <div id="tree-table">
        ${documents.length === 0
            ? '<div class="empty-state">No YAML documents found</div>'
            : docSections}
    </div>
    <div id="completion-dropdown" class="completion-dropdown" style="display:none;"></div>
    <script>${getScript()}</script>
</body>
</html>`;
}

/** Generate the error-state HTML */
export function getErrorHtml(message: string): string {
    return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline';">
    <title>Tree Editor</title>
    <style>${getStyles()}</style>
</head>
<body>
    <div class="error-banner">${escapeHtml(message)}</div>
    <script>${getScript()}</script>
</body>
</html>`;
}

/** Generate the placeholder HTML for non-YAML editors */
export function getPlaceholderHtml(): string {
    return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline';">
    <title>Tree Editor</title>
    <style>${getStyles()}</style>
</head>
<body>
    <div class="empty-state">Open a Bino YAML file to view its structure</div>
    <script>${getScript()}</script>
</body>
</html>`;
}

function renderDocSection(doc: TreeDocument, specFields?: FieldDef[], metadataFields?: FieldDef[]): string {
    const kindBadge = doc.kind ? `<span class="kind-badge">${escapeHtml(doc.kind)}</span>` : '';
    const nameLabel = doc.name ? `<span class="doc-name">"${escapeHtml(doc.name)}"</span>` : '';

    return `
    <div class="doc-section" data-doc-index="${doc.docIndex}">
        <div class="doc-header" data-collapsed="false" onclick="toggleDocSection(this)">
            <span class="disclosure">&#9660;</span>
            ${kindBadge}
            ${nameLabel}
            <span class="doc-line">line ${doc.startLine + 1}</span>
        </div>
        <div class="doc-body">
            <table class="tree-table">
                <colgroup>
                    <col class="col-key">
                    <col class="col-type">
                    <col class="col-value">
                </colgroup>
                <thead>
                    <tr><th>Key</th><th>Type</th><th>Value</th></tr>
                </thead>
                <tbody>
                    ${renderDocNodes(doc, specFields, metadataFields)}
                </tbody>
            </table>
        </div>
    </div>`;
}

/** Render all nodes for a document, merging schema fields for spec/metadata */
function renderDocNodes(doc: TreeDocument, specFields?: FieldDef[], metadataFields?: FieldDef[]): string {
    let html = '';
    for (const node of doc.nodes) {
        if (node.key === 'spec' && specFields) {
            // Render the spec container row, then merge children with schema
            html += renderContainerRow(node, 0, doc.docIndex, true);
            html += renderMergedChildren(node.children || [], specFields, 1, doc.docIndex, ['spec']);
        } else if (node.key === 'metadata' && metadataFields) {
            html += renderContainerRow(node, 0, doc.docIndex, true);
            html += renderMergedChildren(node.children || [], metadataFields, 1, doc.docIndex, ['metadata']);
        } else {
            html += renderNode(node, 0, doc.docIndex, undefined, false);
            if (node.children) {
                html += renderChildNodes(node.children, 1, doc.docIndex, undefined);
            }
        }
    }
    return html;
}

/** Render a container row (spec, metadata, or any object/array) */
function renderContainerRow(node: TreeNode, depth: number, docIndex: number, isRequired: boolean): string {
    const indent = depth * 16;
    const pathAttr = `data-path='${escapeHtml(JSON.stringify(node.path))}'`;
    const docAttr = `data-doc-index="${docIndex}"`;
    const lineAttr = `data-line="${node.line}"`;
    const keyClass = isRequired ? 'key-name key-required' : 'key-name';

    return `<tr class="tree-row expandable" ${pathAttr} ${docAttr} ${lineAttr}>
        <td class="cell-key">
            <div class="cell-key-inner" style="padding-left: ${indent + 4}px;">
                <span class="disclosure clickable" onclick="toggleRow(this)">&#9660;</span>
                <span class="${keyClass}" onclick="goToLine(${docIndex}, ${node.line})">${escapeHtml(node.key)}</span>
            </div>
        </td>
        <td class="cell-type"><span class="type-badge type-${node.type}">${escapeHtml(node.type)}</span></td>
        <td class="cell-value"><span class="value-summary">${escapeHtml(node.displayValue)}</span></td>
    </tr>`;
}

/**
 * Merge YAML children with schema fields.
 * Shows all schema fields: present ones with values, absent ones as ghost rows.
 * Also includes any YAML fields not in the schema (unknown fields).
 */
function renderMergedChildren(
    yamlChildren: TreeNode[],
    schemaFields: FieldDef[],
    depth: number,
    docIndex: number,
    parentPath: string[]
): string {
    const yamlMap = new Map<string, TreeNode>();
    for (const child of yamlChildren) {
        yamlMap.set(child.key, child);
    }

    let html = '';

    // 1. Render schema fields in schema order (present + ghost)
    for (const field of schemaFields) {
        const yamlNode = yamlMap.get(field.key);
        if (yamlNode) {
            // Present in YAML — render actual node with schema info
            html += renderNode(yamlNode, depth, docIndex, field, false);
            // Recurse into children
            if (yamlNode.type === 'array' && yamlNode.children && field.children) {
                // Array with item schema — render each item, merging object items with item schema
                html += renderArrayItems(yamlNode.children, field.children, depth + 1, docIndex);
            } else if (yamlNode.children && field.children) {
                html += renderMergedChildren(yamlNode.children, field.children, depth + 1, docIndex, [...parentPath, field.key]);
            } else if (yamlNode.children) {
                html += renderChildNodes(yamlNode.children, depth + 1, docIndex, field);
            }
            yamlMap.delete(field.key);
        } else {
            // Absent from YAML — render ghost row
            html += renderGhostRow(field, depth, docIndex, parentPath);
        }
    }

    // 2. Render any remaining YAML fields not in schema (unknown fields)
    for (const [, yamlNode] of yamlMap) {
        html += renderNode(yamlNode, depth, docIndex, undefined, false);
        if (yamlNode.children) {
            html += renderChildNodes(yamlNode.children, depth + 1, docIndex, undefined);
        }
    }

    return html;
}

/** Render child nodes without schema merging (for unknown nodes or arrays) */
function renderChildNodes(children: TreeNode[], depth: number, docIndex: number, parentFieldDef?: FieldDef): string {
    let html = '';
    for (const child of children) {
        html += renderNode(child, depth, docIndex, parentFieldDef?.children?.find(f => f.key === child.key), false);
        if (child.children) {
            const childFieldDef = parentFieldDef?.children?.find(f => f.key === child.key);
            if (child.type === 'array' && childFieldDef?.children) {
                // Array with item schema
                html += renderArrayItems(child.children, childFieldDef.children, depth + 1, docIndex);
            } else if (child.type === 'object' && childFieldDef?.children) {
                html += renderMergedChildren(child.children, childFieldDef.children, depth + 1, docIndex, child.path);
            } else {
                html += renderChildNodes(child.children, depth + 1, docIndex, childFieldDef);
            }
        }
    }
    return html;
}

/**
 * Render array items, merging each object item's children with the item schema.
 * Each [0], [1], etc. is rendered as a row, and if it's an object its children
 * are merged with itemSchema to show all possible fields.
 */
function renderArrayItems(
    items: TreeNode[],
    itemSchema: FieldDef[],
    depth: number,
    docIndex: number
): string {
    let html = '';
    for (const item of items) {
        // Render the item row itself ([0], [1], etc.)
        html += renderNode(item, depth, docIndex, undefined, false);

        if (item.type === 'object') {
            // Merge the item's actual children with the item schema
            html += renderMergedChildren(item.children || [], itemSchema, depth + 1, docIndex, item.path);
        } else if (item.children) {
            html += renderChildNodes(item.children, depth + 1, docIndex, undefined);
        }
    }
    return html;
}

/** Render a ghost row for a schema field not present in YAML */
function renderGhostRow(field: FieldDef, depth: number, docIndex: number, parentPath: string[]): string {
    const indent = depth * 16;
    const fullPath = [...parentPath, field.key];
    const pathAttr = `data-path='${escapeHtml(JSON.stringify(fullPath))}'`;
    const docAttr = `data-doc-index="${docIndex}"`;
    const escapedPath = escapeHtml(JSON.stringify(JSON.stringify(fullPath)));

    const keyClass = field.required ? 'key-name key-required ghost-key' : 'key-name ghost-key';
    const typeBadge = renderTypeBadge(field.type, field);
    const desc = field.description
        ? `<span class="info-icon" title="${escapeHtml(field.description)}">&#9432;</span>`
        : '';

    // Ghost value: type-specific activation controls
    let valueCell: string;
    if (field.type === 'object') {
        valueCell = `<button class="ghost-btn ghost-btn-map" onclick="activateField(${docIndex}, ${escapedPath}, 'object', null)" title="Create object"><span class="ghost-btn-icon">{&thinsp;}</span> Add</button>`;
    } else if (field.type === 'array') {
        const itemKindField = field.children?.find(f => f.key === 'kind' && f.required && f.enumValues);
        if (itemKindField) {
            // Typed array (children) — use kind picker flow
            const kindEnumJson = escapeHtml(JSON.stringify(JSON.stringify(itemKindField.enumValues)));
            valueCell = `<button class="ghost-btn ghost-btn-array" onclick="addTypedArrayItem(${docIndex}, ${escapedPath}, ${kindEnumJson})" title="Add child component"><span class="ghost-btn-icon">[&thinsp;]</span> Add</button>`;
        } else {
            // If items are objects (have children fields), create [{}]; otherwise ['']
            const itemsAreObjects = field.children && field.children.length > 0;
            const arrayDefault = itemsAreObjects ? 'array-object' : 'array';
            valueCell = `<button class="ghost-btn ghost-btn-array" onclick="activateField(${docIndex}, ${escapedPath}, '${arrayDefault}', null)" title="Create array with first item"><span class="ghost-btn-icon">[&thinsp;]</span> Add</button>`;
        }
    } else {
        const defaultForType = field.enumValues ? field.enumValues[0] : getDefaultPlaceholder(field.type);
        valueCell = `<span class="ghost-value" onclick="activateField(${docIndex}, ${escapedPath}, '${escapeHtml(field.type)}', ${escapeHtml(JSON.stringify(JSON.stringify(field.enumValues?.[0] ?? null)))})">${escapeHtml(defaultForType)}</span>`;
    }

    return `<tr class="tree-row ghost-row" ${pathAttr} ${docAttr} data-line="0">
        <td class="cell-key">
            <div class="cell-key-inner" style="padding-left: ${indent + 4}px;">
                <span class="disclosure leaf"></span>
                <span class="${keyClass}">${escapeHtml(field.key)}</span>
                ${desc}
            </div>
        </td>
        <td class="cell-type">${typeBadge}</td>
        <td class="cell-value">${valueCell}</td>
    </tr>`;
}

function getDefaultPlaceholder(type: string): string {
    switch (type) {
        case 'string': return '(click to set)';
        case 'number': case 'integer': return '(click to set)';
        case 'boolean': return 'false';
        case 'array': return '(click to add)';
        case 'object': return '(click to add)';
        default: return '(click to set)';
    }
}

/** Render a node that is present in the YAML */
function renderNode(
    node: TreeNode,
    depth: number,
    docIndex: number,
    fieldDef: FieldDef | undefined,
    _isGhost: boolean
): string {
    const hasChildren = node.children && node.children.length > 0;
    const indent = depth * 16;
    const disclosureClass = hasChildren ? 'disclosure clickable' : 'disclosure leaf';
    const disclosureChar = hasChildren ? '&#9660;' : '';
    const pathAttr = `data-path='${escapeHtml(JSON.stringify(node.path))}'`;
    const docAttr = `data-doc-index="${docIndex}"`;
    const lineAttr = `data-line="${node.line}"`;

    const isRequired = fieldDef?.required ?? isTopLevelRequired(node.key);
    const isArrayItem = /^\[\d+\]$/.test(node.key);
    const keyClass = isRequired ? 'key-name key-required' : 'key-name';

    // Description info icon
    const desc = fieldDef?.description
        ? `<span class="info-icon" title="${escapeHtml(fieldDef.description)}">&#9432;</span>`
        : '';

    // Type badge
    const typeBadge = renderTypeBadge(node.type, fieldDef);

    // Value cell with clear button
    const valueCell = renderValueCell(node, docIndex, fieldDef, isRequired);

    // Inline action buttons in the key cell
    let actionBtns = '';
    if (node.type === 'array') {
        const itemKindField = fieldDef?.children?.find(f => f.key === 'kind' && f.required && f.enumValues);
        if (itemKindField) {
            // Typed array (children) — use kind picker flow
            const kindEnumJson = escapeHtml(JSON.stringify(JSON.stringify(itemKindField.enumValues)));
            actionBtns = `<button class="add-btn" onclick="event.stopPropagation(); addTypedArrayItem(${docIndex}, ${escapeHtml(JSON.stringify(JSON.stringify(node.path)))}, ${kindEnumJson})" title="Add child component">+</button>`;
        } else {
            const itemsAreObjects = fieldDef?.children && fieldDef.children.length > 0;
            actionBtns = `<button class="add-btn" onclick="event.stopPropagation(); addArrayItem(${docIndex}, ${escapeHtml(JSON.stringify(JSON.stringify(node.path)))}, ${!!itemsAreObjects})" title="Add item">+</button>`;
        }
    } else if (node.type === 'object' && fieldDef?.children && fieldDef.children.length > 0) {
        // + button to add field to this object (via add-field menu)
        const existingKeys = new Set((node.children || []).map(c => c.key));
        const addable = fieldDef.children
            .filter(f => !existingKeys.has(f.key))
            .map(f => ({ key: f.key, type: f.type, desc: f.description || '' }));
        if (addable.length > 0) {
            actionBtns = `<button class="add-btn" onclick="event.stopPropagation(); showAddFieldMenu(this, ${docIndex}, ${escapeHtml(JSON.stringify(JSON.stringify(node.path)))})" data-addable-fields='${escapeHtml(JSON.stringify(addable))}' title="Add field">+</button>`;
        }
    }
    if (isArrayItem) {
        // - button to remove this array item
        actionBtns = `<button class="remove-item-btn" onclick="event.stopPropagation(); removeField(${docIndex}, ${escapeHtml(JSON.stringify(JSON.stringify(node.path)))})" title="Remove item">&minus;</button>`;
    }

    return `<tr class="tree-row${hasChildren ? ' expandable' : ''}${isArrayItem ? ' array-item-row' : ''}" ${pathAttr} ${docAttr} ${lineAttr}>
        <td class="cell-key">
            <div class="cell-key-inner" style="padding-left: ${indent + 4}px;">
                <span class="${disclosureClass}" onclick="toggleRow(this)">${disclosureChar}</span>
                <span class="${keyClass}" onclick="goToLine(${docIndex}, ${node.line})">${escapeHtml(node.key)}</span>
                ${desc}
                ${actionBtns}
            </div>
        </td>
        <td class="cell-type">${typeBadge}</td>
        <td class="cell-value">${valueCell}</td>
    </tr>`;
}

function isTopLevelRequired(key: string): boolean {
    return ['apiVersion', 'kind', 'metadata', 'spec'].includes(key);
}

function renderTypeBadge(type: string, fieldDef?: FieldDef): string {
    const displayType = fieldDef?.enumValues ? 'enum' : type;
    return `<span class="type-badge type-${displayType}">${escapeHtml(displayType)}</span>`;
}

function renderValueCell(node: TreeNode, docIndex: number, fieldDef: FieldDef | undefined, isRequired: boolean): string {
    const pathJson = JSON.stringify(node.path);
    const escapedPath = escapeHtml(JSON.stringify(pathJson));

    // Clear button — shown for non-required, non-top-level fields that have a value
    const isArrayItem = /^\[\d+\]$/.test(node.key);
    const canClear = !isRequired && !isTopLevelRequired(node.key) && !isArrayItem;
    const clearBtn = canClear
        ? `<button class="clear-btn" onclick="event.stopPropagation(); removeField(${docIndex}, ${escapedPath})" title="Remove value">&#10005;</button>`
        : '';

    // Objects and arrays show summary + clear button
    if (node.type === 'object') {
        return `<div class="value-cell-wrap"><span class="value-summary">${escapeHtml(node.displayValue)}</span>${clearBtn}</div>`;
    }
    if (node.type === 'array') {
        return `<div class="value-cell-wrap"><span class="value-summary">${escapeHtml(node.displayValue)}</span>${clearBtn}</div>`;
    }
    if (node.type === 'multiline') {
        return `<div class="value-cell-wrap"><span class="value-multiline" title="${escapeHtml(String(node.value))}">${escapeHtml(node.displayValue)}</span>${clearBtn}</div>`;
    }

    // Enum fields — render as select
    if (fieldDef?.enumValues) {
        const options = fieldDef.enumValues
            .map(v => `<option value="${escapeHtml(v)}"${v === String(node.value) ? ' selected' : ''}>${escapeHtml(v)}</option>`)
            .join('');
        return `<div class="value-cell-wrap"><select class="value-select" onchange="editValue(${docIndex}, ${escapedPath}, this.value)">${options}</select>${clearBtn}</div>`;
    }

    // Boolean fields — checkbox
    if (node.type === 'boolean') {
        const checked = node.value ? ' checked' : '';
        return `<div class="value-cell-wrap"><label class="value-checkbox"><input type="checkbox"${checked} onchange="editValue(${docIndex}, ${escapedPath}, this.checked)"><span>${node.value ? 'true' : 'false'}</span></label>${clearBtn}</div>`;
    }

    // Null
    if (node.type === 'null') {
        return `<div class="value-cell-wrap"><span class="value-null">null</span>${clearBtn}</div>`;
    }

    // String / number — editable input
    const inputType = node.type === 'number' ? 'number' : 'text';
    const valueStr = node.value === null || node.value === undefined ? '' : String(node.value);

    // Check if this field is completion-eligible
    const completionField = getCompletionFieldType(node, fieldDef);
    const completionAttr = completionField ? ` data-completion-field="${completionField}"` : '';

    return `<div class="value-cell-wrap"><input class="value-input" type="${inputType}" value="${escapeHtml(valueStr)}"${completionAttr}
        onfocus="onInputFocus(this)" onblur="onInputBlur(this, ${docIndex}, ${escapedPath})"
        onkeydown="onInputKeydown(event, this, ${docIndex}, ${escapedPath})">${clearBtn}</div>`;
}

/** Determine if a field should trigger completion */
function getCompletionFieldType(node: TreeNode, fieldDef?: FieldDef): string | undefined {
    const path = node.path;
    if (!path || path.length === 0) { return undefined; }

    // kind field (top-level, path length 1)
    if (path.length === 1 && path[0] === 'kind') { return 'kind'; }

    if (path.length < 2) { return undefined; }

    if (path[0] === 'spec' && path[path.length - 1] === 'dataset') { return 'dataset'; }
    if (path[0] === 'spec' && path[path.length - 1] === 'signingProfile') { return 'signingProfile'; }
    if (path[0] === 'spec' && path[path.length - 1] === 'source' && fieldDef?.type === 'string') { return 'source'; }
    if (path[path.length - 1] === 'ref') { return 'ref'; }
    if ((path[path.length - 2] === 'scenarios' || path[path.length - 2] === 'variances') && /^\d+$/.test(path[path.length - 1])) { return 'column'; }
    if (path[path.length - 2] === 'layoutPages' && /^\d+$/.test(path[path.length - 1])) { return 'layoutPage'; }

    return undefined;
}

function escapeHtml(str: string): string {
    return str
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

function getStyles(): string {
    return `
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            color: var(--vscode-foreground);
            background: var(--vscode-editor-background);
            overflow-x: auto;
        }

        .empty-state {
            display: flex;
            align-items: center;
            justify-content: center;
            height: 100vh;
            color: var(--vscode-descriptionForeground);
            font-size: 1.1em;
        }

        .error-banner {
            padding: 12px 16px;
            background: var(--vscode-inputValidation-errorBackground);
            border: 1px solid var(--vscode-inputValidation-errorBorder);
            color: var(--vscode-errorForeground);
            margin: 8px;
            border-radius: 4px;
        }

        /* Document sections */
        .doc-section {
            border-bottom: 1px solid var(--vscode-panel-border);
        }
        .doc-header {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 8px 12px;
            background: var(--vscode-sideBar-background, var(--vscode-editor-background));
            cursor: pointer;
            user-select: none;
            position: sticky;
            top: 0;
            z-index: 10;
            border-bottom: 1px solid var(--vscode-panel-border);
        }
        .doc-header:hover {
            background: var(--vscode-list-hoverBackground);
        }
        .doc-header .disclosure {
            font-size: 0.7em;
            transition: transform 0.15s;
        }
        .doc-header[data-collapsed="true"] .disclosure {
            transform: rotate(-90deg);
        }
        .doc-body {
            overflow: hidden;
        }
        .doc-header[data-collapsed="true"] + .doc-body {
            display: none;
        }

        .kind-badge {
            background: var(--vscode-badge-background);
            color: var(--vscode-badge-foreground);
            padding: 1px 8px;
            border-radius: 10px;
            font-size: 0.85em;
            font-weight: 500;
        }
        .doc-name {
            font-weight: 500;
        }
        .doc-line {
            color: var(--vscode-descriptionForeground);
            font-size: 0.85em;
            margin-left: auto;
        }

        /* Tree table */
        .tree-table {
            width: 100%;
            border-collapse: collapse;
            table-layout: fixed;
        }
        .col-key { width: 40%; }
        .col-type { width: 60px; }
        .col-value { width: auto; }

        .tree-table thead th {
            padding: 4px 8px;
            text-align: left;
            font-weight: 600;
            font-size: 0.8em;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            color: var(--vscode-descriptionForeground);
            border-bottom: 1px solid var(--vscode-panel-border);
            background: var(--vscode-editor-background);
        }

        .tree-row {
            border-bottom: 1px solid var(--vscode-panel-border, transparent);
        }
        .tree-row:hover {
            background: var(--vscode-list-hoverBackground);
        }
        .tree-row:hover .add-btn {
            opacity: 1;
        }
        .tree-row:hover .clear-btn {
            opacity: 0.6;
        }

        .cell-key {
            padding: 3px 4px;
            overflow: hidden;
        }
        .cell-key-inner {
            display: flex;
            align-items: center;
            gap: 2px;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        .cell-type {
            padding: 3px 4px;
        }
        .cell-value {
            padding: 3px 8px;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        /* Value cell wrapper — flex for input + clear button */
        .value-cell-wrap {
            display: flex;
            align-items: center;
            gap: 4px;
        }
        .value-cell-wrap .value-input,
        .value-cell-wrap .value-select {
            flex: 1;
            min-width: 0;
        }

        /* Disclosure triangles */
        .disclosure {
            display: inline-block;
            width: 14px;
            text-align: center;
            font-size: 0.65em;
            flex-shrink: 0;
            transition: transform 0.15s;
        }
        .disclosure.clickable {
            cursor: pointer;
        }
        .disclosure.clickable:hover {
            color: var(--vscode-textLink-foreground);
        }
        .disclosure.leaf {
            visibility: hidden;
        }
        .disclosure.collapsed {
            transform: rotate(-90deg);
        }

        /* Key names */
        .key-name {
            cursor: pointer;
            flex-shrink: 1;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        .key-name:hover {
            color: var(--vscode-textLink-foreground);
            text-decoration: underline;
        }
        .key-required {
            font-weight: 600;
        }

        /* Info icon */
        .info-icon {
            flex-shrink: 0;
            font-size: 0.85em;
            color: var(--vscode-descriptionForeground);
            opacity: 0.5;
            cursor: help;
            margin-left: 2px;
        }
        .tree-row:hover .info-icon {
            opacity: 0.9;
        }

        /* Ghost rows (schema fields not in YAML) */
        .ghost-row {
            opacity: 0.4;
        }
        .ghost-row:hover {
            opacity: 0.75;
        }
        .ghost-key {
            color: var(--vscode-descriptionForeground);
        }
        .ghost-value {
            color: var(--vscode-descriptionForeground);
            font-style: italic;
            cursor: pointer;
            font-size: 0.9em;
        }
        .ghost-value:hover {
            color: var(--vscode-textLink-foreground);
            text-decoration: underline;
        }

        /* Type badges */
        .type-badge {
            font-size: 0.75em;
            padding: 1px 5px;
            border-radius: 3px;
            background: var(--vscode-badge-background);
            color: var(--vscode-badge-foreground);
            opacity: 0.7;
        }
        .type-string { background: #2d6b3f30; color: #4ec96b; }
        .type-number { background: #6b4e2d30; color: #c9964e; }
        .type-boolean { background: #2d4e6b30; color: #4e96c9; }
        .type-enum { background: #6b2d5e30; color: #c94eb0; }
        .type-object, .type-array { opacity: 0.5; }
        .type-null { opacity: 0.4; }

        /* Value cells */
        .value-input {
            width: 100%;
            background: transparent;
            border: 1px solid transparent;
            color: var(--vscode-input-foreground, var(--vscode-foreground));
            font-family: var(--vscode-editor-font-family, monospace);
            font-size: var(--vscode-editor-font-size, 13px);
            padding: 1px 4px;
            border-radius: 2px;
            outline: none;
        }
        .value-input:focus {
            background: var(--vscode-input-background);
            border-color: var(--vscode-focusBorder);
        }
        .value-input[type="number"] {
            -moz-appearance: textfield;
        }

        .value-select {
            background: var(--vscode-dropdown-background);
            color: var(--vscode-dropdown-foreground);
            border: 1px solid var(--vscode-dropdown-border);
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            padding: 1px 4px;
            border-radius: 2px;
            outline: none;
            max-width: 100%;
        }
        .value-select:focus {
            border-color: var(--vscode-focusBorder);
        }

        .value-checkbox {
            display: flex;
            align-items: center;
            gap: 6px;
            cursor: pointer;
        }
        .value-checkbox input {
            accent-color: var(--vscode-checkbox-background);
        }

        .value-null {
            color: var(--vscode-descriptionForeground);
            font-style: italic;
        }
        .value-summary {
            color: var(--vscode-descriptionForeground);
            font-size: 0.9em;
        }
        .value-multiline {
            color: var(--vscode-descriptionForeground);
            font-style: italic;
            cursor: help;
        }

        /* Ghost activate buttons for object/array */
        .ghost-btn {
            border: 1px solid var(--vscode-button-secondaryBackground, #444);
            background: transparent;
            color: var(--vscode-descriptionForeground);
            cursor: pointer;
            font-size: 0.85em;
            padding: 1px 8px;
            border-radius: 3px;
            display: inline-flex;
            align-items: center;
            gap: 4px;
            transition: all 0.15s;
        }
        .ghost-btn:hover {
            background: var(--vscode-button-secondaryBackground, #333);
            color: var(--vscode-button-secondaryForeground, #ccc);
            border-color: var(--vscode-button-secondaryHoverBackground, #555);
        }
        .ghost-btn-icon {
            font-family: var(--vscode-editor-font-family, monospace);
            font-weight: 600;
            font-size: 1.1em;
        }

        /* Array item row — remove button */
        .remove-item-btn {
            border: none;
            background: transparent;
            color: var(--vscode-descriptionForeground);
            cursor: pointer;
            font-size: 1.1em;
            font-weight: 600;
            line-height: 1;
            padding: 0 4px;
            border-radius: 3px;
            opacity: 0;
            transition: opacity 0.15s;
        }
        .tree-row:hover .remove-item-btn {
            opacity: 0.6;
        }
        .remove-item-btn:hover {
            background: var(--vscode-inputValidation-errorBackground);
            color: var(--vscode-errorForeground);
            opacity: 1 !important;
        }

        /* Clear (x) button — right-aligned in value column */
        .clear-btn {
            flex-shrink: 0;
            border: none;
            background: transparent;
            color: var(--vscode-descriptionForeground);
            cursor: pointer;
            font-size: 0.85em;
            line-height: 1;
            padding: 2px 4px;
            border-radius: 3px;
            opacity: 0;
            transition: opacity 0.15s;
        }
        .clear-btn:hover {
            background: var(--vscode-inputValidation-errorBackground);
            color: var(--vscode-errorForeground);
            opacity: 1 !important;
        }

        /* Add button */
        .add-btn {
            border: none;
            background: transparent;
            cursor: pointer;
            font-size: 1em;
            line-height: 1;
            padding: 0 4px;
            border-radius: 3px;
            opacity: 0;
            transition: opacity 0.15s;
        }
        .add-btn:hover {
            background: var(--vscode-button-background);
            color: var(--vscode-button-foreground);
            opacity: 1;
        }

        /* Completion dropdown */
        .completion-dropdown {
            position: fixed;
            background: var(--vscode-editorSuggestWidget-background, var(--vscode-dropdown-background));
            border: 1px solid var(--vscode-editorSuggestWidget-border, var(--vscode-dropdown-border));
            border-radius: 4px;
            max-height: 200px;
            overflow-y: auto;
            z-index: 100;
            box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            min-width: 180px;
        }
        .completion-item {
            padding: 4px 10px;
            cursor: pointer;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
            font-size: 0.9em;
        }
        .completion-item:hover, .completion-item.selected {
            background: var(--vscode-editorSuggestWidget-selectedBackground, var(--vscode-list-activeSelectionBackground));
            color: var(--vscode-editorSuggestWidget-selectedForeground, var(--vscode-list-activeSelectionForeground));
        }

        /* Add field menu */
        .add-field-menu {
            position: fixed;
            background: var(--vscode-menu-background, var(--vscode-dropdown-background));
            border: 1px solid var(--vscode-menu-border, var(--vscode-dropdown-border));
            border-radius: 4px;
            max-height: 300px;
            overflow-y: auto;
            z-index: 100;
            box-shadow: 0 4px 12px rgba(0,0,0,0.3);
            min-width: 200px;
        }
        .add-field-item {
            padding: 4px 12px;
            cursor: pointer;
            display: flex;
            flex-direction: column;
            gap: 1px;
        }
        .add-field-item:hover {
            background: var(--vscode-menu-selectionBackground, var(--vscode-list-activeSelectionBackground));
            color: var(--vscode-menu-selectionForeground, var(--vscode-list-activeSelectionForeground));
        }
        .add-field-item .field-key {
            font-weight: 500;
        }
        .add-field-item .field-desc {
            font-size: 0.8em;
            color: var(--vscode-descriptionForeground);
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            max-width: 300px;
        }
        .add-field-item:hover .field-desc {
            color: inherit;
            opacity: 0.8;
        }

        /* Scrollbar styling */
        ::-webkit-scrollbar { width: 8px; height: 8px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb {
            background: var(--vscode-scrollbarSlider-background);
            border-radius: 4px;
        }
        ::-webkit-scrollbar-thumb:hover {
            background: var(--vscode-scrollbarSlider-hoverBackground);
        }
    `;
}

function getScript(): string {
    return `
    const vscode = acquireVsCodeApi();
    let activeAddMenu = null;
    let activeCompletionDropdown = null;
    let completionSelectedIndex = -1;

    // ---- Document section collapse/expand ----
    function toggleDocSection(header) {
        const collapsed = header.getAttribute('data-collapsed') === 'true';
        header.setAttribute('data-collapsed', collapsed ? 'false' : 'true');
    }

    // ---- Row expand/collapse ----
    function toggleRow(disclosure) {
        const row = disclosure.closest('tr');
        if (!row) return;
        const isCollapsed = disclosure.classList.contains('collapsed');

        if (isCollapsed) {
            disclosure.classList.remove('collapsed');
            showChildRows(row, true);
        } else {
            disclosure.classList.add('collapsed');
            showChildRows(row, false);
        }
    }

    function showChildRows(parentRow, show) {
        const parentPath = JSON.parse(parentRow.getAttribute('data-path') || '[]');
        const parentDepth = parentPath.length;
        let sibling = parentRow.nextElementSibling;

        while (sibling && sibling.classList.contains('tree-row')) {
            const sibPath = JSON.parse(sibling.getAttribute('data-path') || '[]');
            if (sibPath.length <= parentDepth) break;

            sibling.style.display = show ? '' : 'none';

            if (show) {
                const childDisclosure = sibling.querySelector('.disclosure.clickable');
                if (childDisclosure && childDisclosure.classList.contains('collapsed')) {
                    const childDepth = sibPath.length;
                    let grandchild = sibling.nextElementSibling;
                    while (grandchild && grandchild.classList.contains('tree-row')) {
                        const gcPath = JSON.parse(grandchild.getAttribute('data-path') || '[]');
                        if (gcPath.length <= childDepth) break;
                        grandchild.style.display = 'none';
                        sibling = grandchild;
                        grandchild = grandchild.nextElementSibling;
                    }
                }
            }

            sibling = sibling.nextElementSibling;
        }
    }

    // ---- Click key to go to line ----
    function goToLine(docIndex, line) {
        vscode.postMessage({ type: 'goToLine', docIndex, line });
    }

    // ---- Edit value ----
    function editValue(docIndex, pathJson, newValue) {
        const path = JSON.parse(pathJson);
        vscode.postMessage({ type: 'editValue', docIndex, path, newValue });
    }

    function onInputFocus(input) {
        input._originalValue = input.value;
        const completionField = input.getAttribute('data-completion-field');
        if (completionField) {
            requestCompletions(input, completionField);
        }
    }

    function onInputBlur(input, docIndex, pathJson) {
        setTimeout(() => {
            hideCompletionDropdown();
            if (input.value !== input._originalValue) {
                const path = JSON.parse(pathJson);
                let value = input.value;
                if (input.type === 'number') { value = Number(value); }
                vscode.postMessage({ type: 'editValue', docIndex, path, newValue: value });
            }
        }, 150);
    }

    function onInputKeydown(event, input, docIndex, pathJson) {
        if (activeCompletionDropdown) {
            if (event.key === 'ArrowDown') {
                event.preventDefault();
                moveCompletionSelection(1);
                return;
            }
            if (event.key === 'ArrowUp') {
                event.preventDefault();
                moveCompletionSelection(-1);
                return;
            }
            if (event.key === 'Enter' || event.key === 'Tab') {
                const selected = activeCompletionDropdown.querySelector('.completion-item.selected');
                if (selected) {
                    event.preventDefault();
                    input.value = selected.textContent;
                    hideCompletionDropdown();
                    input.blur();
                    return;
                }
            }
            if (event.key === 'Escape') {
                event.preventDefault();
                hideCompletionDropdown();
                return;
            }
        }

        if (event.key === 'Enter') {
            event.preventDefault();
            input.blur();
        }
        if (event.key === 'Escape') {
            event.preventDefault();
            input.value = input._originalValue || '';
            input.blur();
        }
    }

    // ---- Completions ----
    function requestCompletions(input, fieldType) {
        const row = input.closest('tr');
        const docIndex = parseInt(row.getAttribute('data-doc-index'));
        const path = JSON.parse(row.getAttribute('data-path') || '[]');
        vscode.postMessage({ type: 'requestCompletions', docIndex, path, fieldType, currentValue: input.value });
        window._completionInput = input;
    }

    function showCompletions(items) {
        const input = window._completionInput;
        if (!input || !document.body.contains(input)) return;

        hideCompletionDropdown();
        if (!items || items.length === 0) return;

        const dropdown = document.getElementById('completion-dropdown');
        dropdown.innerHTML = items.map((item, i) =>
            '<div class="completion-item" onmousedown="selectCompletion(this)">' +
            escapeHtml(item) + '</div>'
        ).join('');

        const rect = input.getBoundingClientRect();
        dropdown.style.left = rect.left + 'px';
        dropdown.style.top = (rect.bottom + 2) + 'px';
        dropdown.style.display = 'block';
        dropdown.style.minWidth = rect.width + 'px';
        activeCompletionDropdown = dropdown;
        completionSelectedIndex = -1;
    }

    function selectCompletion(item) {
        const input = window._completionInput;
        if (input) {
            input.value = item.textContent;
            input.dispatchEvent(new Event('change'));
            setTimeout(() => input.blur(), 10);
        }
        hideCompletionDropdown();
    }

    function moveCompletionSelection(delta) {
        if (!activeCompletionDropdown) return;
        const items = activeCompletionDropdown.querySelectorAll('.completion-item');
        if (items.length === 0) return;

        if (completionSelectedIndex >= 0) {
            items[completionSelectedIndex].classList.remove('selected');
        }
        completionSelectedIndex += delta;
        if (completionSelectedIndex < 0) completionSelectedIndex = items.length - 1;
        if (completionSelectedIndex >= items.length) completionSelectedIndex = 0;
        items[completionSelectedIndex].classList.add('selected');
        items[completionSelectedIndex].scrollIntoView({ block: 'nearest' });
    }

    function hideCompletionDropdown() {
        const dropdown = document.getElementById('completion-dropdown');
        if (dropdown) {
            dropdown.style.display = 'none';
            dropdown.innerHTML = '';
        }
        activeCompletionDropdown = null;
        completionSelectedIndex = -1;
    }

    // ---- Remove field (clear value) ----
    function removeField(docIndex, pathJson) {
        const path = JSON.parse(pathJson);
        vscode.postMessage({ type: 'removeField', docIndex, path });
    }

    // ---- Activate ghost field (add to YAML) ----
    function activateField(docIndex, pathJson, fieldType, defaultEnumValue) {
        const path = JSON.parse(pathJson);
        let value;
        if (defaultEnumValue !== null && defaultEnumValue !== undefined) {
            value = defaultEnumValue;
        } else {
            switch (fieldType) {
                case 'string': value = ''; break;
                case 'number': case 'integer': value = 0; break;
                case 'boolean': value = false; break;
                case 'array': value = ['']; break;
                case 'array-object': value = [{}]; break;
                case 'object': value = {}; break;
                default: value = ''; break;
            }
        }
        const key = path[path.length - 1];
        const parentPath = path.slice(0, -1);
        vscode.postMessage({ type: 'addField', docIndex, parentPath, key, fieldType, defaultValue: value });
    }

    // ---- Add array item ----
    function addArrayItem(docIndex, pathJson, itemIsObject) {
        const path = JSON.parse(pathJson);
        vscode.postMessage({ type: 'addArrayItem', docIndex, path, itemIsObject: !!itemIsObject });
    }

    // ---- Add typed array item (children with kind picker) ----
    function addTypedArrayItem(docIndex, pathJson, kindEnumJson) {
        const path = JSON.parse(pathJson);
        const kindEnum = JSON.parse(kindEnumJson);
        vscode.postMessage({ type: 'addTypedArrayItem', docIndex, path, kindEnum });
    }

    // ---- Add field menu ----
    function showAddFieldMenu(button, docIndex, parentPathJson) {
        closeAddMenu();
        const addableRaw = button.getAttribute('data-addable-fields');
        if (!addableRaw) return;

        let fields;
        try { fields = JSON.parse(addableRaw); } catch { return; }
        if (!fields || fields.length === 0) return;

        const menu = document.createElement('div');
        menu.className = 'add-field-menu';

        for (const f of fields) {
            const item = document.createElement('div');
            item.className = 'add-field-item';
            item.innerHTML = '<span class="field-key">' + escapeHtml(f.key) + '</span>' +
                (f.desc ? '<span class="field-desc">' + escapeHtml(f.desc.substring(0, 80)) + '</span>' : '');
            item.onclick = () => {
                closeAddMenu();
                const parentPath = JSON.parse(parentPathJson);
                vscode.postMessage({ type: 'addField', docIndex, parentPath, key: f.key, fieldType: f.type });
            };
            menu.appendChild(item);
        }

        const rect = button.getBoundingClientRect();
        menu.style.left = rect.left + 'px';
        menu.style.top = (rect.bottom + 2) + 'px';
        document.body.appendChild(menu);
        activeAddMenu = menu;

        setTimeout(() => {
            document.addEventListener('click', closeAddMenuHandler, { once: true });
        }, 10);
    }

    function closeAddMenu() {
        if (activeAddMenu) {
            activeAddMenu.remove();
            activeAddMenu = null;
        }
    }

    function closeAddMenuHandler(e) {
        if (activeAddMenu && !activeAddMenu.contains(e.target)) {
            closeAddMenu();
        }
    }

    // ---- Message handling from extension ----
    window.addEventListener('message', event => {
        const msg = event.data;
        switch (msg.type) {
            case 'setTree':
                document.getElementById('tree-table').innerHTML = msg.html;
                break;
            case 'completions':
                showCompletions(msg.items);
                break;
        }
    });

    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            closeAddMenu();
            hideCompletionDropdown();
        }
    });

    document.addEventListener('scroll', () => {
        closeAddMenu();
        hideCompletionDropdown();
    }, true);

    function escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
    `;
}
