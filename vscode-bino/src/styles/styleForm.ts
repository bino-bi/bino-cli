import type { FieldDef } from '../schemaResolver';
import { getIn, inheritedValue, InheritSource, STYLE_SECTIONS } from './styleModel';

/** The keys of an SVG style, in column order. */
export const SVG_STYLE_KEYS = ['stroke', 'fill', 'width', 'dasharray', 'fillpattern', 'negativeFill', 'outlineFill', 'outlineStroke'];

/** What the page draws for one field of a style. Paths are relative to `spec.content`. */
export type StyleNode =
    | { type: 'leaf'; label: string; path: string[]; input: 'color' | 'number' | 'text' | 'enum' | 'bool'; options?: string[]; own: unknown; inherited?: unknown; source?: InheritSource; description?: string }
    | { type: 'group'; label: string; path: string[]; children: StyleNode[]; description?: string }
    | { type: 'slots'; label: string; path: string[]; slots: string[]; keys: string[]; patterns: string[]; own: Record<string, unknown>; inherited: Record<string, unknown>; source?: InheritSource; description?: string }
    | { type: 'list'; label: string; path: string[]; colors: boolean; own?: unknown[]; inherited?: unknown[]; source?: InheritSource; description?: string }
    | { type: 'map'; label: string; path: string[]; own?: Record<string, unknown>; inherited?: Record<string, unknown>; source?: InheritSource; description?: string };

export interface StyleSection {
    key: string;
    nodes: StyleNode[];
}

const SLOT_GROUPS: Record<string, string[]> = {
    svgStyleGroup: ['ac', 'fc', 'pp', 'pl'],
    svgVariancesStyleGroup: ['pos', 'neg', 'neu'],
};

/** A key that holds a colour: the SVG paint keys and anything named ...Color, ...Background. */
export function isColorKey(key: string): boolean {
    return /(color|background|^stroke$|^fill$|fill$|stroke$)/i.test(key);
}

/**
 * The tabs of the style editor: one per section of the schema's styleContent,
 * in the order of STYLE_SECTIONS, each field with its own value and the value
 * it inherits.
 */
export function buildStyleSections(fields: FieldDef[], content: unknown, styleName: string, defaultContent: unknown): StyleSection[] {
    const byKey = new Map(fields.map(f => [f.key, f]));
    const order = [...STYLE_SECTIONS, ...fields.map(f => f.key).filter(k => !STYLE_SECTIONS.includes(k))];
    return order
        .filter(key => byKey.has(key))
        .map(key => {
            const field = byKey.get(key)!;
            const children = field.children ?? [];
            return { key, nodes: children.map(child => buildNode(child, [key, child.key], content, styleName, defaultContent)) };
        });
}

function buildNode(field: FieldDef, path: string[], content: unknown, styleName: string, defaultContent: unknown): StyleNode {
    const own = getIn(content, path);
    const inherit = inheritedValue(path, styleName, defaultContent);
    const base = { label: field.key, path, description: field.description };

    const slots = field.refName ? SLOT_GROUPS[field.refName] : undefined;
    if (slots) {
        const svg = (field.children ?? []).find(c => c.refName === 'svgStyle')?.children ?? [];
        const patterns = svg.find(c => c.key === 'fillpattern')?.enumValues?.filter(v => v !== 'fc') ?? ['solid', 'hatched', 'outlined'];
        return {
            type: 'slots', ...base, slots,
            keys: SVG_STYLE_KEYS.filter(k => svg.length === 0 || svg.some(c => c.key === k)),
            patterns,
            own: objectOr(own), inherited: objectOr(inherit?.value), source: inherit?.source,
        };
    }
    if (field.type === 'object' && field.children && field.children.length > 0) {
        return { type: 'group', ...base, children: field.children.map(c => buildNode(c, [...path, c.key], content, styleName, defaultContent)) };
    }
    if (field.type === 'object') {
        return { type: 'map', ...base, own: own === undefined ? undefined : objectOr(own), inherited: inherit ? objectOr(inherit.value) : undefined, source: inherit?.source };
    }
    if (field.type === 'array') {
        return {
            type: 'list', ...base, colors: /(Colors|Fills)$/.test(field.key),
            own: Array.isArray(own) ? own : undefined,
            inherited: Array.isArray(inherit?.value) ? inherit!.value as unknown[] : undefined,
            source: inherit?.source,
        };
    }
    const input = field.enumValues ? 'enum'
        : field.type === 'boolean' ? 'bool'
            : field.type === 'number' || field.type === 'integer' ? 'number'
                : isColorKey(field.key) ? 'color' : 'text';
    return { type: 'leaf', ...base, input, options: field.enumValues, own, inherited: inherit?.value, source: inherit?.source };
}

function objectOr(value: unknown): Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
}
