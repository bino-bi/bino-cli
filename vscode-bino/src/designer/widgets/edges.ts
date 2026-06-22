import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { EDGE_OPERATORS } from '../ibcs';
import { esc, widgetShell } from './widgetHtml';

/** An `edges` value: a JSON string, or an array of edge objects. */
export interface EdgeStyle {
    color?: string;
    width?: number;
    dasharray?: string;
}
export interface Edge {
    from: string;
    to: string;
    operator?: '*' | '/' | '+' | '-' | 'x' | '÷' | 'none';
    label?: string;
    style?: EdgeStyle;
}
export type EdgesValue = string | Edge[];

/**
 * The tree-edges widget: a from/to row builder with an operator dropdown, an
 * optional label, and a collapsible style sub-form (color, width, dasharray).
 * `from`/`to` are required per row. Emits the array of edge objects (or the
 * raw-JSON tab the edit re-validates).
 */
export const edgesWidget: DesignerWidget<EdgesValue> = {
    id: 'edges',
    match: ({ kind, field }) => kind === 'Tree' && field.key === 'edges',
    render: (ctx: WidgetContext<EdgesValue>): WidgetHandle => {
        const value = ctx.value;
        const edges = Array.isArray(value) ? value : [];
        const rows = edges.map(renderRow).join('');
        const structured = `
            <div class="dw-rows" data-dw-role="rows">${rows}</div>
            <button type="button" class="dw-add" data-dw-role="add-row">+ edge</button>`;
        return { html: widgetShell({ kind: 'edges', value, structuredHtml: structured }) };
    },
};

/** Render one edge row from an existing edge object. */
function renderRow(edge: Edge): string {
    const from = strOf(edge.from);
    const to = strOf(edge.to);
    const operator = strOf(edge.operator);
    const label = strOf(edge.label);
    const color = strOf(edge.style?.color);
    const width = typeof edge.style?.width === 'number' ? String(edge.style.width) : '';
    const dasharray = strOf(edge.style?.dasharray);

    const opOpts = [
        `<option value=""></option>`,
        ...EDGE_OPERATORS.map(o => `<option value="${esc(o)}"${o === operator ? ' selected' : ''}>${esc(o)}</option>`),
    ].join('');

    return `<div class="dw-row dw-row-edge" data-dw-role="row">
        <div class="dw-edge-main">
            <input class="dw-input dw-sm" type="text" data-dw-role="from" placeholder="from" value="${esc(from)}">
            <span class="dw-arrow">→</span>
            <input class="dw-input dw-sm" type="text" data-dw-role="to" placeholder="to" value="${esc(to)}">
            <select class="dw-select dw-sm" data-dw-role="operator">${opOpts}</select>
            <input class="dw-input dw-sm" type="text" data-dw-role="label" placeholder="label" value="${esc(label)}">
            ${delBtn()}
        </div>
        <details class="dw-style"${color || width || dasharray ? ' open' : ''}>
            <summary>style</summary>
            <div class="dw-style-body">
                <label class="dw-flabel">color <input class="dw-color" type="color" data-dw-role="style-color" value="${esc(color || '#000000')}"></label>
                <label class="dw-flabel">width <input class="dw-input dw-xs" type="number" min="0" step="0.5" data-dw-role="style-width" value="${esc(width)}"></label>
                <label class="dw-flabel">dash <input class="dw-input dw-xs" type="text" data-dw-role="style-dasharray" placeholder="4 2" value="${esc(dasharray)}"></label>
                <label class="dw-radio"><input type="checkbox" data-dw-role="style-on" ${color || width || dasharray ? 'checked' : ''}> apply style</label>
            </div>
        </details>
    </div>`;
}

function delBtn(): string {
    return `<button type="button" class="dw-del" data-dw-role="del-row" title="Remove">×</button>`;
}

function strOf(v: unknown): string {
    return typeof v === 'string' ? v : '';
}
