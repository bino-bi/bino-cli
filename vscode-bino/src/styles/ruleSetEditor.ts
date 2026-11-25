import { WorkspaceIndexer } from '../indexer';
import { BinoPreviewManager } from '../preview';
import { ContentDoc, ContentEditorProvider, EditorMessage, EditorSession } from './contentEditor';
import { readStyleContent } from './styleEditor';
import { DEFAULT_STYLE, getIn, isObject, mergeStyles, SYSTEM_STYLE } from './styleModel';
import {
    effectiveRule, LOOKS, Rule, sortedPrefixes, sortIndexChanges, systemRule, varianceOrder,
} from './ruleSetModel';
import { getRuleSetEditorHtml } from './ruleSetEditorHtml';

/** The keys of a rule a scenario row edits. */
const RULE_KEYS = ['name', 'colorIndex', 'sortIndex'];

/**
 * The RuleSet editor: one table row per scenario prefix plus the fallback.
 * colorIndex is shown as the four looks, drawn in the colours of a chosen
 * ComponentStyle. Rows are ordered by drag and drop, which writes sortIndex in
 * steps of 100.
 */
export class RuleSetEditorProvider extends ContentEditorProvider {
    static readonly viewType = 'bino.ruleSetEditor';

    constructor(indexer: WorkspaceIndexer, preview: BinoPreviewManager) {
        super(indexer, preview, 'RuleSet', RuleSetEditorProvider.viewType);
    }

    protected html(nonce: string): string {
        return getRuleSetEditorHtml(nonce);
    }

    protected async buildState(s: EditorSession, doc: ContentDoc): Promise<Record<string, unknown>> {
        const content = typeof doc.content === 'string' ? safeJson(doc.content) : doc.content;
        const scenarios = getIn(content, ['scenarios']);
        const fallback = getIn(content, ['fallback']);

        const styles = this.indexer.getDocuments(['ComponentStyle']).map(d => d.name).sort();
        if (!s.choice || (s.choice !== '_system' && !styles.includes(s.choice))) {
            s.choice = styles.includes(DEFAULT_STYLE) ? DEFAULT_STYLE : '_system';
        }
        const layers: unknown[] = [SYSTEM_STYLE];
        if (s.choice !== '_system') {
            if (s.choice !== DEFAULT_STYLE) { layers.push(await readStyleContent(this.indexer, DEFAULT_STYLE)); }
            layers.push(await readStyleContent(this.indexer, s.choice));
        }
        const bars = getIn(mergeStyles(...layers), ['bn-chart-time', 'barStyles']);

        return {
            name: doc.name,
            styles: ['_system', ...styles],
            style: s.choice,
            looks: LOOKS.map(l => ({ ...l, style: isObject(bars) ? bars[l.slot] ?? {} : {} })),
            rows: sortedPrefixes(scenarios).map(prefix => {
                const effective = effectiveRule(scenarios, prefix);
                return {
                    prefix,
                    own: pick(getIn(scenarios, [prefix])),
                    system: systemRule(prefix),
                    effective,
                    variance: effective.sortIndex === undefined ? undefined : varianceOrder(effective.sortIndex),
                };
            }),
            fallback: { own: pick(fallback, [...RULE_KEYS, 'group']), system: systemRule() },
            usedBy: await this.usedBy(s, doc),
        };
    }

    protected async handleOwnMessage(s: EditorSession, msg: EditorMessage): Promise<boolean> {
        if (msg.type === 'pickStyle') {
            s.choice = String(msg.name);
            await this.render(s);
            return true;
        }
        if (msg.type === 'reorder' && Array.isArray(msg.order)) {
            const doc = this.current(s);
            if (!doc || typeof doc.content === 'string') { return true; }
            const scenarios = getIn(doc.content, ['scenarios']);
            const { set, remove } = sortIndexChanges(scenarios, msg.order.map(String));
            const patch: Record<string, unknown> = {};
            for (const [prefix, value] of Object.entries(set)) { patch[`scenarios.${prefix}.sortIndex`] = value; }
            if (Object.keys(patch).length > 0 && !(await this.editContent(s, patch))) { return true; }
            if (remove.length > 0) {
                // read again: the edit above moved the text
                await this.removeContent(s, remove.map(prefix => ['scenarios', prefix, 'sortIndex']));
            }
            return true;
        }
        return false;
    }
}

/** The rule keys of a content object, without anything else. */
function pick(value: unknown, keys: string[] = RULE_KEYS): Rule {
    const out: Record<string, unknown> = {};
    if (isObject(value)) {
        for (const k of keys) { if (value[k] !== undefined) { out[k] = value[k]; } }
    }
    return out as Rule;
}

function safeJson(text: string): unknown {
    try { return JSON.parse(text); } catch { return undefined; }
}
