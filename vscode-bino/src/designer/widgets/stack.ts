import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { STACK_BY, STACK_MODE, STACK_ORDER } from '../ibcs';
import { esc, selectHtml, widgetShell } from './widgetHtml';

/** A chart `stack` value: a map with a required `by` and optional mode/order. */
export interface StackValue {
    by: 'scenarios' | 'dimensions';
    mode?: 'absolute' | 'relative' | 'absolute-relative';
    order?: 'asc' | 'desc' | 'dataset';
}

const STACK_KINDS = new Set(['ChartTime', 'ChartStructure']);

/**
 * The chart-stack widget: a `by` radio (required) plus optional `mode`/`order`
 * dropdowns. Emits the `stack` map. Degrades to the raw-JSON tab.
 */
export const stackWidget: DesignerWidget<StackValue | undefined> = {
    id: 'stack',
    match: ({ kind, field }) => STACK_KINDS.has(kind) && field.key === 'stack',
    render: (ctx: WidgetContext<StackValue | undefined>): WidgetHandle => {
        const value = ctx.value && typeof ctx.value === 'object' ? ctx.value : undefined;
        const by = value?.by ?? 'scenarios';
        const mode = value?.mode ?? '';
        const order = value?.order ?? '';

        const byRadios = STACK_BY.map(
            b => `<label class="dw-radio"><input type="radio" data-dw-role="by" value="${esc(b)}"${b === by ? ' checked' : ''}> ${esc(b)}</label>`
        ).join('');

        const structured = `
            <div class="dw-field"><span class="dw-flabel">by</span><div class="dw-mode">${byRadios}</div></div>
            <div class="dw-field"><span class="dw-flabel">mode</span>
                ${selectHtml({ cls: 'dw-select', role: 'mode', options: STACK_MODE, selected: mode, includeEmpty: true, emptyLabel: '(default: absolute)' })}
            </div>
            <div class="dw-field"><span class="dw-flabel">order</span>
                ${selectHtml({ cls: 'dw-select', role: 'order', options: STACK_ORDER, selected: order, includeEmpty: true, emptyLabel: '(default: dataset)' })}
            </div>`;

        return { html: widgetShell({ kind: 'stack', value, structuredHtml: structured }) };
    },
};
