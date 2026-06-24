import { DesignerWidget, WidgetContext, WidgetHandle } from './registry';
import { SENTIMENTS, parseVarianceToken, varianceMeaning } from '../ibcs';
import { esc, jsonAttr, widgetShell } from './widgetHtml';
import { isScenarioKind } from './scenario';

/** A variance field: an inherited literal string, or an array of variance tokens. */
export type VarianceValue = string | string[];

/** Read the chosen scenario slots from the sibling `spec.scenarios` value. */
function chosenScenarios(spec: Record<string, unknown> | undefined): string[] {
    const s = spec?.scenarios;
    if (Array.isArray(s)) {
        return s.filter((v): v is string => typeof v === 'string');
    }
    return [];
}

/**
 * The variance widget: a row builder over the grammar `d|dr_B_A_sentiment`. Each
 * row picks the prefix (absolute/relative), a base and compare slot constrained
 * to the component's chosen `scenarios`, and a sentiment — with a live preview of
 * the token and its decoded meaning. Emits an array of tokens (or the raw-JSON
 * tab, which the authoring edit re-validates).
 */
export const varianceWidget: DesignerWidget<VarianceValue> = {
    id: 'variance',
    match: ({ kind, field }) => isScenarioKind(kind) && field.key === 'variances',
    render: (ctx: WidgetContext<VarianceValue>): WidgetHandle => {
        const value = ctx.value;
        const tokens = Array.isArray(value) ? value : [];
        const slots = chosenScenarios(ctx.spec);
        // Fall back to the bound dataset's measure slots if scenarios isn't an
        // array yet (e.g. inherited), so the row builder still offers choices.
        const slotChoices = slots.length > 0 ? slots : (ctx.binding?.columns ?? []).filter(c => /^(ac|pp|fc|pl)\d$/.test(c));

        const rows = tokens.map(tok => renderRow(tok, slotChoices)).join('');
        const structured = `
            <div class="dw-rows" data-dw-role="rows" data-slots="${jsonAttr(slotChoices)}" data-sentiments="${jsonAttr(SENTIMENTS)}">${rows}</div>
            <button type="button" class="dw-add" data-dw-role="add-row">+ variance</button>
            ${slots.length === 0 ? '<div class="dw-warn">No scenario slots chosen — pick scenarios first, or use JSON.</div>' : ''}`;

        return { html: widgetShell({ kind: 'variance', value, structuredHtml: structured }) };
    },
};

/** Render one variance row from a token string (defaults when it doesn't parse). */
function renderRow(token: string, slots: readonly string[]): string {
    const t = parseVarianceToken(token);
    const prefix = t?.prefix ?? 'd';
    const base = t?.base ?? slots[0] ?? '';
    const compare = t?.compare ?? slots[1] ?? slots[0] ?? '';
    const sentiment = t?.sentiment ?? 'pos';
    const meaning = varianceMeaning(token) || varianceMeaning(`${prefix}_${base}_${compare}_${sentiment}`);

    const slotOpts = (sel: string) =>
        slots.map(s => `<option value="${esc(s)}"${s === sel ? ' selected' : ''}>${esc(s)}</option>`).join('') ||
        `<option value="${esc(sel)}" selected>${esc(sel || '—')}</option>`;
    const sentOpts = SENTIMENTS.map(s => `<option value="${esc(s)}"${s === sentiment ? ' selected' : ''}>${esc(s)}</option>`).join('');

    return `<div class="dw-row" data-dw-role="row">
        <select class="dw-select dw-sm" data-dw-role="prefix">
            <option value="d"${prefix === 'd' ? ' selected' : ''}>d</option>
            <option value="dr"${prefix === 'dr' ? ' selected' : ''}>dr</option>
        </select>
        <select class="dw-select dw-sm" data-dw-role="base">${slotOpts(base)}</select>
        <span class="dw-vs">vs</span>
        <select class="dw-select dw-sm" data-dw-role="compare">${slotOpts(compare)}</select>
        <select class="dw-select dw-sm" data-dw-role="sentiment">${sentOpts}</select>
        <button type="button" class="dw-del" data-dw-role="del-row" title="Remove">×</button>
        <div class="dw-preview" data-dw-role="preview">${esc(meaning)}</div>
    </div>`;
}
