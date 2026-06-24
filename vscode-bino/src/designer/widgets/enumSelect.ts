import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';

/** Minimal HTML attribute/text escaper (the widget owns its own fragment). */
function esc(str: string): string {
    return str
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

/**
 * The reference DesignerWidget: renders any enum field as a native select. It
 * exists to prove the registry — the form renderer selects this over the generic
 * control whenever a field carries `enumValues` (including the string variant of
 * a oneOf). Brief 04's bespoke IBCS widgets register ahead of it and claim their
 * specific fields first.
 */
export const enumSelectWidget: DesignerWidget<string> = {
    id: 'enum-select',
    match: ({ field }) => Array.isArray(field.enumValues) && field.enumValues.length > 0,
    render: (ctx: WidgetContext<string>): WidgetHandle => {
        const current = ctx.value === null || ctx.value === undefined ? '' : String(ctx.value);
        const values = ctx.field.enumValues ?? [];
        // An "unset" sentinel only when the field is optional and currently empty,
        // so a required enum always carries a concrete value.
        const includeEmpty = !ctx.field.required && current === '';
        const options = [
            ...(includeEmpty ? ['<option value=""></option>'] : []),
            ...values.map(v =>
                `<option value="${esc(v)}"${v === current ? ' selected' : ''}>${esc(v)}</option>`
            ),
        ].join('');
        // `data-designer-edit` marks this control's change as a field edit; the
        // webview reads its value and posts editField, which the host routes to
        // WidgetContext.onChange (wired to AuthoringClient.edit).
        return {
            html: `<select class="designer-select" data-designer-edit data-value-type="string">${options}</select>`,
        };
    },
};
