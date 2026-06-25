/**
 * IBCS vocabulary shared by the bespoke designer widgets — mirrored from the
 * engine's `bn-template-engine/src/utils/constants.ts` so the GUI offers exactly
 * the slots/enums the renderer understands. Kept here (not re-fetched per widget)
 * so scenario, variance, and stack controls cannot drift from one another.
 */

/** The 16 IBCS scenario slots: ac1-4, pp1-4, fc1-4, pl1-4 (engine `Scenarios`). */
export const SCENARIO_SLOTS: readonly string[] = [
    'ac1', 'ac2', 'ac3', 'ac4',
    'pp1', 'pp2', 'pp3', 'pp4',
    'fc1', 'fc2', 'fc3', 'fc4',
    'pl1', 'pl2', 'pl3', 'pl4',
];

/** Variance sentiment suffixes (engine `Sentiments`). */
export const SENTIMENTS: readonly string[] = ['pos', 'neg', 'neu'];

/** `stack.by` choices (engine `StackBy`). */
export const STACK_BY: readonly string[] = ['scenarios', 'dimensions'];

/** `stack.mode` choices (engine `StackMode`). */
export const STACK_MODE: readonly string[] = ['absolute', 'relative', 'absolute-relative'];

/** `stack.order` choices (engine `StackOrder`). */
export const STACK_ORDER: readonly string[] = ['asc', 'desc', 'dataset'];

/** Tree-edge operator choices (schema `edges[].operator`). */
export const EDGE_OPERATORS: readonly string[] = ['*', '/', '+', '-', 'x', '÷', 'none'];

/** Aggregate functions for `attributes[].expression` (schema pattern). */
export const AGG_FUNCTIONS: readonly string[] = ['set', 'first', 'last', 'min', 'max', 'avg', 'sum'];

/**
 * The non-array string forms `scenarios`/`variances` accept: inherit from the
 * nearest ancestor card/page, or from the page only. (`auto` is offered for
 * scenarios as the engine's implicit-derive literal.)
 */
export const INHERITED_LITERALS: readonly string[] = ['inherited-closest', 'inherited-page'];

/** True when a value is one of the inherited string literals. */
export function isInheritedLiteral(value: unknown): value is string {
    return typeof value === 'string' && INHERITED_LITERALS.includes(value);
}

/** Restrict a slot list to those a bound dataset actually emits (columns ∩ slots). */
export function availableSlots(columns: readonly string[] | undefined): string[] {
    if (!columns || columns.length === 0) {
        // No binding yet: offer every slot rather than an empty palette.
        return [...SCENARIO_SLOTS];
    }
    const set = new Set(columns);
    return SCENARIO_SLOTS.filter(slot => set.has(slot));
}

/** A decoded variance token: prefix (absolute/relative) + base/compare slots + sentiment. */
export interface VarianceToken {
    /** `d` = absolute, `dr` = relative (%). */
    prefix: 'd' | 'dr';
    /** Base scenario slot (the `B` in d_B_A_sent). */
    base: string;
    /** Compare scenario slot (the `A`). */
    compare: string;
    /** Sentiment: pos | neg | neu. */
    sentiment: string;
}

/** Build the canonical variance literal `d|dr_B_A_sent` from its parts. */
export function formatVarianceToken(t: VarianceToken): string {
    return `${t.prefix}_${t.base}_${t.compare}_${t.sentiment}`;
}

/**
 * Parse a variance token of the grammar `d|dr_B_A_sentiment`, or undefined when
 * it does not match (mirrors the Go `varianceMeaning` parser).
 */
export function parseVarianceToken(token: string): VarianceToken | undefined {
    let rest = token;
    let prefix: 'd' | 'dr';
    if (rest.startsWith('dr')) {
        prefix = 'dr';
        rest = rest.slice(2);
    } else if (rest.startsWith('d')) {
        prefix = 'd';
        rest = rest.slice(1);
    } else {
        return undefined;
    }
    const parts = rest.replace(/^_/, '').split('_');
    if (parts.length !== 3) {
        return undefined;
    }
    const [base, compare, sentiment] = parts;
    if (!base || !compare || !sentiment) {
        return undefined;
    }
    return { prefix, base, compare, sentiment };
}

const SENTIMENT_PHRASE: Record<string, string> = {
    pos: 'positive sentiment — more is better',
    neg: 'negative sentiment — more is worse',
    neu: 'neutral sentiment',
};

/**
 * Decode a variance token into prose, matching the Go `varianceMeaning` phrasing
 * ("absolute variance of A vs B; …"). Returns '' when the token does not parse.
 */
export function varianceMeaning(token: string): string {
    const t = parseVarianceToken(token);
    if (!t) {
        return '';
    }
    const kind = t.prefix === 'dr' ? 'relative (%)' : 'absolute';
    let out = `${kind} variance of ${t.compare} vs ${t.base}`;
    const phrase = SENTIMENT_PHRASE[t.sentiment];
    if (phrase) {
        out += `; ${phrase}`;
    }
    return out;
}
