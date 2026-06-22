import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { availableSlots, INHERITED_LITERALS, isInheritedLiteral } from '../ibcs';
import { esc, widgetShell } from './widgetHtml';

/** A scenario field: a string literal (inherited/auto) or an ordered slot array. */
export type ScenarioValue = string | string[];

/**
 * Kinds carrying a cross-component `scenarios` / `variances` field. The scenario
 * and variance widgets claim it on any of these (Table + both chart kinds).
 */
const SCENARIO_KINDS = new Set(['Table', 'ChartStructure', 'ChartTime']);

/** True for the kinds whose `scenarios` field this family of widgets owns. */
export function isScenarioKind(kind: string): boolean {
    return SCENARIO_KINDS.has(kind);
}

/**
 * The scenario widget: an ordered multi-select over the 16 IBCS slots filtered to
 * the bound dataset's columns, plus the `inherited-*`/`auto` string forms. Emits
 * a string (the literal form) or an ordered array of slot names. Degrades to a
 * raw-JSON tab the authoring edit re-validates.
 */
export const scenarioWidget: DesignerWidget<ScenarioValue> = {
    id: 'scenario',
    match: ({ kind, field }) => isScenarioKind(kind) && field.key === 'scenarios',
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
