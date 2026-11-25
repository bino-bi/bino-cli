import engineDefaults from './engineDefaults.json';

/** The sections of a ComponentStyle, in tab order. */
export const STYLE_SECTIONS = [
    'global', 'bn-text', 'bn-table', 'bn-chart-time', 'bn-chart-structure', 'bn-chart-scatter',
    'bn-chart-bubble', 'bn-chart-bullet', 'bn-tree', 'bn-grid', 'bn-layout-card', 'bn-annotation',
];

/** The name of the style every other style is merged over. */
export const DEFAULT_STYLE = '_default';

/** The engine's built-in style, the bottom layer of every style. */
export const SYSTEM_STYLE = engineDefaults.style as Record<string, unknown>;

/** The engine release the built-in values were read from. */
export const ENGINE_DEFAULTS_VERSION = engineDefaults.engineVersion;

/** Where an inherited value comes from. */
export type InheritSource = '_default' | '_system';

export interface Inherited {
    value: unknown;
    source: InheritSource;
}

export type JsonObject = Record<string, unknown>;

export function isObject(value: unknown): value is JsonObject {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
}

/** The value at a key path, or undefined. */
export function getIn(root: unknown, path: string[]): unknown {
    let node = root;
    for (const key of path) {
        if (!isObject(node) || !Object.prototype.hasOwnProperty.call(node, key)) { return undefined; }
        node = node[key];
    }
    return node;
}

/**
 * The value a key of the style `styleName` takes when the style does not set
 * it. The engine merges `_system`, then `_default`, then the named style, so a
 * named style inherits from `_default` first. `_default` inherits from
 * `_system` only.
 */
export function inheritedValue(path: string[], styleName: string, defaultContent: unknown): Inherited | undefined {
    const system = getIn(SYSTEM_STYLE, path);
    if (styleName !== DEFAULT_STYLE) {
        const value = getIn(defaultContent, path);
        // an object of _default is merged over the engine's, as the engine does
        if (isObject(value)) { return { value: mergeStyles(system, value), source: '_default' }; }
        if (value !== undefined) { return { value, source: '_default' }; }
    }
    return system === undefined ? undefined : { value: system, source: '_system' };
}

/**
 * The key path to remove when the field at `path` is cleared. A parent object
 * that would be left empty goes too, so a cleared style writes no empty
 * sections. The top level (the content itself) is never removed.
 */
export function removalPath(content: unknown, path: string[]): string[] {
    let cut = path.length;
    while (cut > 1) {
        const parent = getIn(content, path.slice(0, cut - 1));
        if (!isObject(parent) || Object.keys(parent).length !== 1) { break; }
        cut--;
    }
    return path.slice(0, cut);
}

/** A deep merge of style layers, later layers win. Arrays are replaced, not merged. */
export function mergeStyles(...layers: unknown[]): JsonObject {
    const out: JsonObject = {};
    for (const layer of layers) {
        if (!isObject(layer)) { continue; }
        for (const [key, value] of Object.entries(layer)) {
            out[key] = isObject(value) && isObject(out[key]) ? mergeStyles(out[key], value) : value;
        }
    }
    return out;
}
