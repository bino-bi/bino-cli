import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { FILTER_OPERATORS, FILTER_GROUP_OPS, coerceFilterValue } from '../ibcs';
import { esc, jsonAttr, selectHtml, widgetShell } from './widgetHtml';

/**
 * The DataSet `filter` widget. The whole `spec.filter` value is a single
 * recursive `dataSetFilterGroup` ({ op?, conditions: [leaf | group, …] }). The
 * structured tab edits ONE level: the top-level group's op plus a flat list of
 * leaf conditions (column / operator / value). Nested groups are rendered as
 * read-only summary chips — deep boolean trees are edited losslessly via the JSON
 * tab (the required escape hatch, supplied by widgetShell). Emits the whole group
 * object at `spec.filter` (replacement, like the aggregate/edges widgets).
 *
 * `coerceFilterValue` (ibcs.ts) is the canonical value-typing rule; `wireFilter`
 * in designerHtml.ts holds a byte-faithful copy for the webview.
 */
export const filterWidget: DesignerWidget<unknown> = {
    id: 'filter',
    match: ({ kind, field }) => kind === 'DataSet' && field.key === 'filter',
    render: (ctx: WidgetContext<unknown>): WidgetHandle => {
        const v = ctx.value && typeof ctx.value === 'object' && !Array.isArray(ctx.value)
            ? (ctx.value as Record<string, unknown>)
            : undefined;
        const groupOp = v?.op === 'or' ? 'or' : 'and';
        const conditions = Array.isArray(v?.conditions) ? (v!.conditions as Record<string, unknown>[]) : [];
        const columns = ctx.binding?.columns ?? [];

        const rows = conditions
            .map(c => (Array.isArray(c.conditions) ? renderNestedSummary(c) : renderLeafRow(c, columns)))
            .join('');

        const structured = `
            <div class="dw-field"><span class="dw-flabel">match</span>
                ${selectHtml({ cls: 'dw-select dw-sm', role: 'group-op', options: FILTER_GROUP_OPS, selected: groupOp })}
                <span class="dw-hint">of the following</span></div>
            <div class="dw-rows" data-dw-role="rows" data-columns="${jsonAttr(columns)}">${rows}</div>
            <button type="button" class="dw-add" data-dw-role="add-row">+ condition</button>
            <div class="dw-hint">Nested groups are shown read-only — edit them in the JSON tab.</div>`;

        return { html: widgetShell({ kind: 'filter', value: ctx.value, structuredHtml: structured }) };
    },
};

/** A single leaf condition row: column datalist + operator + value (+ null toggle). */
function renderLeafRow(c: Record<string, unknown>, columns: readonly string[]): string {
    const column = strOf(c.column);
    const op = typeof c.op === 'string' ? c.op : 'equal';
    const isNull = c.value === null;
    const value = valueText(c.value);
    const listId = `dwcols-${Math.random().toString(36).slice(2, 8)}`;

    return `<div class="dw-row dw-row-wrap" data-dw-role="row" data-row-kind="leaf">
        <input class="dw-input dw-sm" type="text" data-dw-role="column" list="${esc(listId)}" placeholder="column" value="${esc(column)}">
        <datalist id="${esc(listId)}">${columns.map(col => `<option value="${esc(col)}">`).join('')}</datalist>
        ${selectHtml({ cls: 'dw-select dw-sm', role: 'op', options: FILTER_OPERATORS, selected: op })}
        <input class="dw-input dw-sm" type="text" data-dw-role="value" placeholder="value" value="${esc(value)}"${isNull ? ' disabled' : ''}>
        <label class="dw-radio"><input type="checkbox" data-dw-role="value-null" ${isNull ? 'checked' : ''}> null</label>
        ${delBtn()}
    </div>`;
}

/** A read-only summary for a nested group; the raw JSON rides along for round-trip. */
function renderNestedSummary(c: Record<string, unknown>): string {
    const op = typeof c.op === 'string' ? c.op : 'and';
    const count = Array.isArray(c.conditions) ? c.conditions.length : 0;
    return `<div class="dw-row" data-dw-role="row" data-row-kind="group" data-group-json="${jsonAttr(c)}">
        <span class="dw-chip">group(${esc(op)}, ${count})</span>
        <span class="dw-hint">edit in JSON</span>
        ${delBtn()}
    </div>`;
}

/** Seed the value box from a leaf's current value (arrays → comma-joined). */
function valueText(value: unknown): string {
    if (value === null || value === undefined) {
        return '';
    }
    if (Array.isArray(value)) {
        return value.map(String).join(', ');
    }
    return String(value);
}

function delBtn(): string {
    return `<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">×</button>`;
}

function strOf(v: unknown): string {
    return typeof v === 'string' ? v : '';
}

// Re-export so tests can assert the canonical coercion alongside the widget.
export { coerceFilterValue };
