import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { AGG_FUNCTIONS } from '../ibcs';
import { esc, jsonAttr, widgetShell } from './widgetHtml';

/**
 * The aggregate-expression widget family: the four Table fields that are an
 * ordered array of small objects — `thereof`, `partof`, `columnthereof`,
 * `attributes`. One widget renders the row shape for whichever field claimed it.
 * Emits the array-of-objects (or the raw-JSON tab the edit re-validates). Array
 * order is column order; rows append/replace today — reorder lands with brief 05b.
 */

/** The Table fields this widget owns, each with its row shape. */
const AGGREGATE_FIELDS = new Set(['thereof', 'partof', 'columnthereof', 'attributes']);

export const aggregateWidget: DesignerWidget<unknown> = {
    id: 'aggregate',
    match: ({ kind, field }) => kind === 'Table' && AGGREGATE_FIELDS.has(field.key),
    render: (ctx: WidgetContext<unknown>): WidgetHandle => {
        const field = ctx.field.key;
        const value = ctx.value;
        // columnthereof additionally accepts an explicit null (distinct from absent).
        const isNull = field === 'columnthereof' && value === null;
        const items = Array.isArray(value) ? (value as Record<string, unknown>[]) : [];
        const columns = ctx.binding?.columns ?? [];

        const rows = items.map(item => renderRow(field, item, columns)).join('');
        const nullToggle = field === 'columnthereof'
            ? `<label class="dw-radio"><input type="checkbox" data-dw-role="null" ${isNull ? 'checked' : ''}> explicit null</label>`
            : '';

        const structured = `
            ${nullToggle}
            <div class="dw-rows" data-dw-role="rows" data-field="${esc(field)}" data-columns="${jsonAttr(columns)}"${isNull ? ' hidden' : ''}>${rows}</div>
            <button type="button" class="dw-add" data-dw-role="add-row"${isNull ? ' hidden' : ''}>+ ${esc(rowNoun(field))}</button>`;

        return { html: widgetShell({ kind: 'aggregate', value, structuredHtml: structured }) };
    },
};

/** Human noun for the add button per field. */
function rowNoun(field: string): string {
    switch (field) {
        case 'attributes': return 'attribute';
        case 'columnthereof': return 'column';
        default: return 'drilldown';
    }
}

/** Render one row of the field's object shape from an existing item. */
function renderRow(field: string, item: Record<string, unknown>, columns: readonly string[]): string {
    if (field === 'attributes') {
        return renderAttributeRow(item, columns);
    }
    if (field === 'columnthereof') {
        const scenario = strOf(item.scenario);
        const name = strOf(item.name);
        const subGroups = Array.isArray(item.subGroups) ? (item.subGroups as unknown[]).map(String).join(', ') : '';
        return `<div class="dw-row dw-row-wrap" data-dw-role="row">
            ${textCell('scenario', 'scenario', scenario)}
            ${textCell('name', 'name', name)}
            ${textCell('subGroups', 'subGroups (comma-separated)', subGroups)}
            ${delBtn()}
        </div>`;
    }
    // thereof / partof: text cells for the present keys.
    const rowGroup = strOf(item.rowGroup);
    const category = strOf(item.category);
    const subCategory = field === 'thereof' ? strOf(item.subCategory) : undefined;
    return `<div class="dw-row dw-row-wrap" data-dw-role="row">
        ${textCell('rowGroup', 'rowGroup', rowGroup)}
        ${textCell('category', 'category', category)}
        ${subCategory !== undefined ? textCell('subCategory', 'subCategory', subCategory) : ''}
        ${delBtn()}
    </div>`;
}

/**
 * An attributes row: a label text + an expression builder (function ▾ + field, or
 * `lit(…)`). The field accepts a bound column or a custom `_`-prefixed name, so it
 * is free-entry with the columns offered as a datalist.
 */
function renderAttributeRow(item: Record<string, unknown>, columns: readonly string[]): string {
    const label = strOf(item.label);
    const expr = strOf(item.expression);
    const parsed = parseExpression(expr);
    const fn = parsed?.fn ?? 'set';
    const arg = parsed?.arg ?? '';
    const isLit = parsed?.isLit ?? false;
    const listId = `dwcols-${Math.random().toString(36).slice(2, 8)}`;

    return `<div class="dw-row dw-row-attr" data-dw-role="row">
        <input class="dw-input dw-sm" type="text" data-dw-role="label" placeholder="label" value="${esc(label)}">
        <select class="dw-select dw-sm" data-dw-role="fn">
            ${['lit', ...AGG_FUNCTIONS].map(f => {
                const sel = (isLit ? f === 'lit' : f === fn) ? ' selected' : '';
                return `<option value="${esc(f)}"${sel}>${esc(f)}</option>`;
            }).join('')}
        </select>
        <input class="dw-input dw-sm" type="text" data-dw-role="arg" list="${esc(listId)}" placeholder="${isLit ? 'constant' : 'field or _custom'}" value="${esc(arg)}">
        <datalist id="${esc(listId)}">${columns.map(c => `<option value="${esc(c)}">`).join('')}</datalist>
        ${delBtn()}
    </div>`;
}

/** A labeled text input cell carrying its object key. */
function textCell(role: string, placeholder: string, value: string): string {
    return `<input class="dw-input dw-sm" type="text" data-dw-role="${esc(role)}" placeholder="${esc(placeholder)}" value="${esc(value)}">`;
}

function delBtn(): string {
    return `<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">×</button>`;
}

function strOf(v: unknown): string {
    return typeof v === 'string' ? v : '';
}

/** Decompose an aggregate expression into {fn, arg, isLit}, or undefined. */
function parseExpression(expr: string): { fn: string; arg: string; isLit: boolean } | undefined {
    const m = expr.match(/^\s*([A-Za-z]+)\((.*)\)\s*$/);
    if (!m) {
        return undefined;
    }
    const fn = m[1];
    const arg = m[2].trim();
    if (fn === 'lit') {
        return { fn: 'lit', arg, isLit: true };
    }
    if (AGG_FUNCTIONS.includes(fn)) {
        return { fn, arg, isLit: false };
    }
    return undefined;
}
