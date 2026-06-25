import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { GROUPBY_AGG_FUNCTIONS } from '../ibcs';
import { esc, jsonAttr, selectHtml, widgetShell } from './widgetHtml';

/**
 * The DataSet `groupBy` widget. The value is a `dataSetGroupBy`
 * ({ columns: [string, …] (≥1), aggregates?: [{column, fn, as, orderBy?,
 * orderDesc?}, …] }). The structured tab has two sections: a chip multi-select of
 * the group-by columns (bound dataset columns, plus free-entry), and a repeatable
 * aggregate row list (column / fn / as, with a collapsible order sub-form for
 * first/last determinism). Emits the whole object at `spec.groupBy`. Degrades to
 * the raw-JSON tab the authoring edit re-validates.
 */
export const groupByWidget: DesignerWidget<unknown> = {
    id: 'groupBy',
    match: ({ kind, field }) => kind === 'DataSet' && field.key === 'groupBy',
    render: (ctx: WidgetContext<unknown>): WidgetHandle => {
        const v = ctx.value && typeof ctx.value === 'object' && !Array.isArray(ctx.value)
            ? (ctx.value as Record<string, unknown>)
            : undefined;
        const groupCols = Array.isArray(v?.columns) ? (v!.columns as unknown[]).map(String) : [];
        const aggregates = Array.isArray(v?.aggregates) ? (v!.aggregates as Record<string, unknown>[]) : [];
        const columns = ctx.binding?.columns ?? [];

        // Chips: chosen columns first (DOM order = emitted order), then the rest.
        const chosenSet = new Set(groupCols);
        const ordered = [...groupCols, ...columns.filter(c => !chosenSet.has(c))];
        const chips = ordered
            .map(col => {
                const on = chosenSet.has(col);
                return `<button type="button" class="dw-chip${on ? ' dw-chip-on' : ''}" data-dw-role="col-chip" data-slot="${esc(col)}">${esc(col)}</button>`;
            })
            .join('');
        const listId = `dwcols-${Math.random().toString(36).slice(2, 8)}`;
        const aggRows = aggregates.map(a => renderAggRow(a, columns)).join('');

        const structured = `
            <div class="dw-field"><span class="dw-flabel">group by</span></div>
            <div class="dw-chips" data-dw-role="columns" data-columns="${jsonAttr(columns)}">${chips}</div>
            <input class="dw-input dw-sm" type="text" data-dw-role="col-add" list="${esc(listId)}" placeholder="+ column">
            <datalist id="${esc(listId)}">${columns.map(c => `<option value="${esc(c)}">`).join('')}</datalist>
            <div class="dw-field"><span class="dw-flabel">aggregates</span></div>
            <div class="dw-rows" data-dw-role="agg-rows" data-columns="${jsonAttr(columns)}">${aggRows}</div>
            <button type="button" class="dw-add" data-dw-role="add-row">+ aggregate</button>`;

        return { html: widgetShell({ kind: 'groupBy', value: ctx.value, structuredHtml: structured }) };
    },
};

/** One aggregate row from an existing aggregate object. */
function renderAggRow(a: Record<string, unknown>, columns: readonly string[]): string {
    const column = strOf(a.column);
    const fn = typeof a.fn === 'string' ? a.fn : 'sum';
    const as = strOf(a.as);
    const orderBy = strOf(a.orderBy);
    const orderDesc = a.orderDesc === true;
    const listId = `dwcols-${Math.random().toString(36).slice(2, 8)}`;

    return `<div class="dw-row dw-row-wrap" data-dw-role="row">
        <input class="dw-input dw-sm" type="text" data-dw-role="agg-column" list="${esc(listId)}" placeholder="column or *" value="${esc(column)}">
        <datalist id="${esc(listId)}">${columns.map(c => `<option value="${esc(c)}">`).join('')}</datalist>
        ${selectHtml({ cls: 'dw-select dw-sm', role: 'agg-fn', options: GROUPBY_AGG_FUNCTIONS, selected: fn })}
        <input class="dw-input dw-sm" type="text" data-dw-role="agg-as" placeholder="as" value="${esc(as)}">
        <details class="dw-style"${orderBy ? ' open' : ''}>
            <summary>order</summary>
            <div class="dw-style-body">
                <label class="dw-flabel">orderBy <input class="dw-input dw-sm" type="text" data-dw-role="agg-orderby" placeholder="column" value="${esc(orderBy)}"></label>
                <label class="dw-radio"><input type="checkbox" data-dw-role="agg-orderdesc" ${orderDesc ? 'checked' : ''}> desc</label>
            </div>
        </details>
        ${delBtn()}
    </div>`;
}

function delBtn(): string {
    return `<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">×</button>`;
}

function strOf(v: unknown): string {
    return typeof v === 'string' ? v : '';
}
