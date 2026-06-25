import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { INDEX_FUNCTIONS } from '../ibcs';
import { esc, jsonAttr, widgetShell } from './widgetHtml';

/**
 * The DataSet `indexColumns` widget. The value is an array of `dataSetIndexColumn`
 * ({ column, EXACTLY ONE OF fn | expr, of?, over?, overDesc?, partition? }). Each
 * row edits one computed index column: an output-name input, a mode select (the
 * predefined functions plus an `expr` raw-SQL escape hatch), and the args the mode
 * needs (hash → of; window fns → over/overDesc/partition; expr → expr). Emits the
 * whole array at `spec.indexColumns`; the mode drives fn-XOR-expr so the schema
 * `oneOf` holds by construction.
 *
 * The dimension→index pairs are baked into `data-index-pairs` from
 * `binding.standardColumns[].pair` (the StandardColumn substrate, NOT the dormant
 * `__binoOnColumns` live channel): when a row's output column is a known `*Index`
 * target, the webview auto-suggests/defaults its source dimension for `of`/`over`,
 * which is the supported way to satisfy the category→categoryIndex rule.
 */
export const indexColumnsWidget: DesignerWidget<unknown> = {
    id: 'indexColumns',
    match: ({ kind, field }) => kind === 'DataSet' && field.key === 'indexColumns',
    render: (ctx: WidgetContext<unknown>): WidgetHandle => {
        const items = Array.isArray(ctx.value) ? (ctx.value as Record<string, unknown>[]) : [];
        const columns = ctx.binding?.columns ?? [];
        // dimension → paired *Index target, restricted to dims actually present.
        const colSet = new Set(columns);
        const pairs = (ctx.binding?.standardColumns ?? [])
            .filter(s => typeof s.pair === 'string' && s.pair && colSet.has(s.name))
            .map(s => ({ source: s.name, index: s.pair as string }));
        // Output-column suggestions: the available *Index targets.
        const indexTargets = pairs.map(p => p.index);

        const rows = items.map(item => renderRow(item, columns, indexTargets)).join('');
        const structured = `
            <div class="dw-rows" data-dw-role="rows" data-columns="${jsonAttr(columns)}" data-index-pairs="${jsonAttr(pairs)}">${rows}</div>
            <button type="button" class="dw-add" data-dw-role="add-row">+ index column</button>`;

        return { html: widgetShell({ kind: 'indexColumns', value: ctx.value, structuredHtml: structured }) };
    },
};

/** One index-column row from an existing item. */
function renderRow(item: Record<string, unknown>, columns: readonly string[], indexTargets: readonly string[]): string {
    const column = strOf(item.column);
    const expr = strOf(item.expr);
    const isExpr = expr !== '' || (item.fn === undefined && item.expr !== undefined);
    const mode = isExpr ? 'expr' : (typeof item.fn === 'string' ? item.fn : 'rowNumber');
    const of = strOf(item.of);
    const over = strOf(item.over);
    const overDesc = item.overDesc === true;
    const partition = Array.isArray(item.partition) ? (item.partition as unknown[]).map(String).join(', ') : '';

    const colListId = `dwidx-${Math.random().toString(36).slice(2, 8)}`;
    const ofListId = `dwcols-${Math.random().toString(36).slice(2, 8)}`;
    const overListId = `dwcols-${Math.random().toString(36).slice(2, 8)}`;

    const modeOpts = ['expr', ...INDEX_FUNCTIONS]
        .map(m => `<option value="${esc(m)}"${m === mode ? ' selected' : ''}>${esc(m)}</option>`)
        .join('');
    const isHash = mode === 'hash';
    const isWindow = mode === 'rowNumber' || mode === 'rank' || mode === 'denseRank';
    const isExprMode = mode === 'expr';

    return `<div class="dw-row dw-row-wrap" data-dw-role="row">
        <input class="dw-input dw-sm" type="text" data-dw-role="column" list="${esc(colListId)}" placeholder="output column" value="${esc(column)}">
        <datalist id="${esc(colListId)}">${indexTargets.map(t => `<option value="${esc(t)}">`).join('')}</datalist>
        <select class="dw-select dw-sm" data-dw-role="mode">${modeOpts}</select>
        <span data-dw-role="args-hash"${isHash ? '' : ' hidden'}>
            <input class="dw-input dw-sm" type="text" data-dw-role="of" list="${esc(ofListId)}" placeholder="of (column)" value="${esc(of)}">
            <datalist id="${esc(ofListId)}">${columns.map(c => `<option value="${esc(c)}">`).join('')}</datalist>
        </span>
        <span data-dw-role="args-window"${isWindow ? '' : ' hidden'}>
            <input class="dw-input dw-sm" type="text" data-dw-role="over" list="${esc(overListId)}" placeholder="over (column)" value="${esc(over)}">
            <datalist id="${esc(overListId)}">${columns.map(c => `<option value="${esc(c)}">`).join('')}</datalist>
            <label class="dw-radio"><input type="checkbox" data-dw-role="over-desc" ${overDesc ? 'checked' : ''}> desc</label>
            <input class="dw-input dw-sm" type="text" data-dw-role="partition" placeholder="partition (comma-separated)" value="${esc(partition)}">
        </span>
        <span data-dw-role="args-expr"${isExprMode ? '' : ' hidden'}>
            <input class="dw-input dw-sm" type="text" data-dw-role="expr" placeholder="raw SQL expression" value="${esc(expr)}">
        </span>
        ${delBtn()}
    </div>`;
}

function delBtn(): string {
    return `<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">×</button>`;
}

function strOf(v: unknown): string {
    return typeof v === 'string' ? v : '';
}
