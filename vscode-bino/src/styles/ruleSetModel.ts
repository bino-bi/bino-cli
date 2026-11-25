import engineDefaults from './engineDefaults.json';
import { isObject, JsonObject } from './styleModel';

/** One rule of a RuleSet: a scenario prefix or the fallback. */
export interface Rule {
    name?: string;
    group?: string;
    colorIndex?: number;
    sortIndex?: number;
}

/** The four looks a colorIndex picks, with the style slot each one draws in. */
export const LOOKS: { colorIndex: number; slot: string; label: string }[] = [
    { colorIndex: 10, slot: 'ac', label: 'solid' },
    { colorIndex: 20, slot: 'pp', label: 'light' },
    { colorIndex: 30, slot: 'fc', label: 'hatched' },
    { colorIndex: 50, slot: 'pl', label: 'outlined' },
];

/** The scenario prefixes with data columns, in the order the docs list them. */
export const BASE_PREFIXES = ['ac', 'fc', 'pl', 'pp'];

/** The step between two sortIndex values the editor writes. */
export const SORT_STEP = 100;

const SYSTEM = engineDefaults.ruleSet as { scenarios: Record<string, Rule>; fallback: Rule };

/** The engine's rule for a prefix or for the fallback (`undefined` prefix). */
export function systemRule(prefix?: string): Rule {
    return prefix === undefined ? SYSTEM.fallback : SYSTEM.scenarios[prefix] ?? {};
}

/** A variance prefix (`dac`, `drfc`, ...): only its sortIndex is read, and it follows its base. */
export function isVariancePrefix(prefix: string): boolean {
    const base = prefix.startsWith('dr') ? prefix.slice(2) : prefix.startsWith('d') ? prefix.slice(1) : '';
    return base !== '' && Object.prototype.hasOwnProperty.call(SYSTEM.scenarios, base);
}

/** The scenario rows the table shows: the base prefixes, then other non-variance prefixes of the content. */
export function scenarioPrefixes(scenarios: unknown): string[] {
    const custom = isObject(scenarios)
        ? Object.keys(scenarios).filter(p => !BASE_PREFIXES.includes(p) && !isVariancePrefix(p))
        : [];
    return [...BASE_PREFIXES, ...custom];
}

/** The effective rule of a prefix: the content's keys over the engine's. */
export function effectiveRule(scenarios: unknown, prefix: string): Rule {
    const own = isObject(scenarios) && isObject(scenarios[prefix]) ? scenarios[prefix] as Rule : {};
    return { ...systemRule(prefix), ...own };
}

/** The variance order the engine derives from a base sortIndex: absolute +1000, relative +1005. */
export function varianceOrder(sortIndex: number): { absolute: number; relative: number } {
    return { absolute: sortIndex + 1000, relative: sortIndex + 1005 };
}

/** Rows sorted by their effective sortIndex, the column order of a report. Ties keep the row order. */
export function sortedPrefixes(scenarios: unknown): string[] {
    const rows = scenarioPrefixes(scenarios);
    return rows
        .map((prefix, i) => ({ prefix, i, sort: effectiveRule(scenarios, prefix).sortIndex ?? Number.MAX_SAFE_INTEGER }))
        .sort((a, b) => a.sort - b.sort || a.i - b.i)
        .map(r => r.prefix);
}

/**
 * The writes for a new row order: sortIndex 100, 200, ... in that order. A
 * value equal to the engine's is not written, and an own value that becomes
 * equal to it is removed, so only real overrides stay in the YAML.
 */
export function sortIndexChanges(scenarios: unknown, order: string[]): { set: Record<string, number>; remove: string[] } {
    const set: Record<string, number> = {};
    const remove: string[] = [];
    order.forEach((prefix, i) => {
        const wanted = (i + 1) * SORT_STEP;
        const own = isObject(scenarios) && isObject(scenarios[prefix]) ? (scenarios[prefix] as JsonObject).sortIndex : undefined;
        if (wanted === systemRule(prefix).sortIndex) {
            if (own !== undefined) { remove.push(prefix); }
        } else if (own !== wanted) {
            set[prefix] = wanted;
        }
    });
    return { set, remove };
}
