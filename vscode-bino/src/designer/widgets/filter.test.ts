import { describe, it, expect } from 'vitest';
import { filterWidget, coerceFilterValue } from './filter';
import { FieldDef } from '../../schemaResolver';
import { WidgetContext, Binding } from './registry';

function field(key: string): FieldDef {
    return { key, path: [key], type: 'object', required: false };
}

function ctx(value: unknown, binding?: Binding): WidgetContext<unknown> {
    return { field: field('filter'), value, binding, onChange: () => undefined };
}

const binding: Binding = {
    dataset: 'sales',
    columns: ['category', 'revenue'],
    standardColumns: [{ name: 'category', kind: 'dim', group: 'dimension', pair: 'categoryIndex' }],
};

describe('filterWidget.match', () => {
    it('claims DataSet.filter', () => {
        expect(filterWidget.match({ kind: 'DataSet', field: field('filter') })).toBe(true);
    });
    it('rejects other kinds and other keys', () => {
        expect(filterWidget.match({ kind: 'Table', field: field('filter') })).toBe(false);
        expect(filterWidget.match({ kind: 'DataSet', field: field('groupBy') })).toBe(false);
    });
});

describe('filterWidget.render', () => {
    it('wraps in the filter widgetShell', () => {
        const html = filterWidget.render(ctx(undefined)).html;
        expect(html).toContain('data-widget-kind="filter"');
    });

    it('empty value renders no rows and an empty JSON textarea', () => {
        const html = filterWidget.render(ctx(undefined)).html;
        expect(html).not.toContain('data-row-kind="leaf"');
        expect(html).toContain('<textarea class="dw-raw"');
        // No seeded JSON content between the textarea tags.
        expect(html).toMatch(/spellcheck="false"><\/textarea>/);
    });

    it('seeds the group op and a leaf row per condition', () => {
        const value = { op: 'or', conditions: [{ column: 'category', op: 'equal', value: 'A' }] };
        const html = filterWidget.render(ctx(value, binding)).html;
        // group-op select shows "or" selected.
        expect(html).toMatch(/data-dw-role="group-op"[\s\S]*?<option value="or" selected>/);
        // one leaf row with the column value seeded.
        expect(html).toContain('data-row-kind="leaf"');
        expect(html).toContain('value="category"');
        expect(html).toMatch(/data-dw-role="op"[\s\S]*?<option value="equal" selected>/);
        expect(html).toContain('value="A"');
    });

    it('renders a nested group as a read-only summary, not inputs', () => {
        const value = { op: 'and', conditions: [{ op: 'or', conditions: [{ column: 'x', op: 'gt', value: 1 }] }] };
        const html = filterWidget.render(ctx(value)).html;
        expect(html).toContain('data-row-kind="group"');
        expect(html).toContain('group(or, 1)');
        expect(html).toContain('data-group-json=');
        // The nested group is not rendered as an editable leaf.
        expect(html).not.toContain('data-row-kind="leaf"');
    });

    it('bakes the binding columns into data-columns', () => {
        const html = filterWidget.render(ctx(undefined, binding)).html;
        expect(html).toContain('data-columns="');
        expect(html).toContain('category');
        expect(html).toContain('revenue');
    });

    it('seeds the null toggle and disables the value box for a null leaf', () => {
        const value = { conditions: [{ column: 'category', op: 'equal', value: null }] };
        const html = filterWidget.render(ctx(value)).html;
        expect(html).toMatch(/data-dw-role="value-null" checked/);
        expect(html).toMatch(/data-dw-role="value"[^>]*disabled/);
    });
});

describe('coerceFilterValue', () => {
    it('splits in/notIn on commas into an array', () => {
        expect(coerceFilterValue('a, b ,c', 'in')).toEqual(['a', 'b', 'c']);
        expect(coerceFilterValue('a,,b', 'notIn')).toEqual(['a', 'b']);
    });
    it('keeps regex as a verbatim string', () => {
        expect(coerceFilterValue('^foo.*$', 'regex')).toBe('^foo.*$');
    });
    it('parses numeric-looking values to numbers', () => {
        expect(coerceFilterValue('42', 'equal')).toBe(42);
        expect(coerceFilterValue('3.14', 'gt')).toBe(3.14);
    });
    it('parses true/false to booleans', () => {
        expect(coerceFilterValue('true', 'equal')).toBe(true);
        expect(coerceFilterValue('false', 'equal')).toBe(false);
    });
    it('returns undefined for an empty box (omit value → IS NULL)', () => {
        expect(coerceFilterValue('', 'equal')).toBeUndefined();
        expect(coerceFilterValue('   ', 'notEqual')).toBeUndefined();
    });
    it('keeps a non-numeric string as a string', () => {
        expect(coerceFilterValue('north', 'equal')).toBe('north');
    });
});
