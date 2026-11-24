import { FieldDef } from '../../schemaResolver';
import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { availableSlots, INHERITED_LITERALS, isInheritedLiteral } from '../ibcs';
import { esc, widgetShell } from './widgetHtml';

/** A scenario field: a string literal (inherited/auto) or an ordered slot array. */
export type ScenarioValue = string | string[];

/**
 * True for a top-level spec field in the IBCS list form (a string literal or an
 * array). The fields come from the kind's schema, so every kind that declares
 * `scenarios` / `variances` matches, plugin kinds included. An enum such as
 * ChartBullet's `variances: auto|none` is not a list and keeps the enum select.
 */
export function isScenarioListField(field: FieldDef): boolean {
    return field.path.length === 1 && !field.enumValues?.length;
}

/**
 * The scenario widget: an ordered multi-select over the 16 IBCS slots filtered to
 * the bound dataset's columns, plus the `inherited-*`/`auto` string forms. Emits
 * a string (the literal form) or an ordered array of slot names. Degrades to a
 * raw-JSON tab the authoring edit re-validates.
 */
export const scenarioWidget: DesignerWidget<ScenarioValue> = {
    id: 'scenario',
    match: ({ field }) => field.key === 'scenarios' && isScenarioListField(field),
    render: (ctx: WidgetContext<ScenarioValue>): WidgetHandle => {
        const slots = availableSlots(ctx.binding?.columns);
        const value = ctx.value;
        const isLiteral = typeof value === 'string';
        const selected = Array.isArray(value) ? value : [];
        const literals = ['auto', ...INHERITED_LITERALS];
        const currentLiteral = isInheritedLiteral(value) ? value : 'auto';

        // Chips render chosen slots first (preserving their order), then the
        // remaining available slots. DOM order of "on" chips is the emitted order.
        const chosenSet = new Set(selected);
        const ordered = [...selected, ...slots.filter(s => !chosenSet.has(s))];
        const chips = ordered
            .map(slot => {
                const on = chosenSet.has(slot);
                return `<button type="button" class="dw-chip${on ? ' dw-chip-on' : ''}" data-dw-role="slot" data-slot="${esc(slot)}">${esc(slot)}</button>`;
            })
            .join('');

        const structured = `
            <div class="dw-mode">
                <label class="dw-radio"><input type="radio" data-dw-role="mode" value="array"${isLiteral ? '' : ' checked'}> Slots</label>
                <label class="dw-radio"><input type="radio" data-dw-role="mode" value="literal"${isLiteral ? ' checked' : ''}> Inherit</label>
            </div>
            <div class="dw-mode-array"${isLiteral ? ' hidden' : ''}>
                <div class="dw-chips" data-dw-role="chips">${chips}</div>
                <div class="dw-hint">Click to toggle; left-to-right is the emitted order.</div>
            </div>
            <div class="dw-mode-literal"${isLiteral ? '' : ' hidden'}>
                <select class="dw-select" data-dw-role="literal">
                    ${literals.map(l => `<option value="${esc(l)}"${l === currentLiteral ? ' selected' : ''}>${esc(l)}</option>`).join('')}
                </select>
            </div>`;

        return { html: widgetShell({ kind: 'scenario', value, structuredHtml: structured }) };
    },
};
