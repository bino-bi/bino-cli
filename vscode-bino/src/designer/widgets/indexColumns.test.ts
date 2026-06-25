import { describe, it, expect } from 'vitest';
import { indexColumnsWidget } from './indexColumns';
import { FieldDef } from '../../schemaResolver';
import { WidgetContext, Binding } from './registry';

function field(key: string): FieldDef {
    return { key, path: [key], type: 'array', required: false };
}

function ctx(value: unknown, binding?: Binding): WidgetContext<unknown> {
    return { field: field('indexColumns'), value, binding, onChange: () => undefined };
}

const binding: Binding = {
    dataset: 'sales',
    columns: ['category', 'revenue'],
    standardColumns: [
        { name: 'category', kind: 'dim', group: 'dimension', pair: 'categoryIndex' },
        // a pair whose source dimension is NOT in columns → must be excluded.
        { name: 'rowGroup', kind: 'dim', group: 'dimension', pair: 'rowGroupIndex' },
    ],
};

describe('indexColumnsWidget.match', () => {
    it('claims DataSet.indexColumns', () => {
        expect(indexColumnsWidget.match({ kind: 'DataSet', field: field('indexColumns') })).toBe(true);
    });
    it('rejects other kinds and other keys', () => {
        expect(indexColumnsWidget.match({ kind: 'DataSet', field: field('groupBy') })).toBe(false);
        expect(indexColumnsWidget.match({ kind: 'Tree', field: field('indexColumns') })).toBe(false);
    });
});

describe('indexColumnsWidget.render', () => {
    it('wraps in the indexColumns widgetShell', () => {
        const html = indexColumnsWidget.render(ctx(undefined)).html;
        expect(html).toContain('data-widget-kind="indexColumns"');
    });

    it('renders a row per item with the right mode selected', () => {
        const value = [
            { column: 'categoryIndex', fn: 'hash', of: 'category' },
            { column: 'rn', fn: 'rowNumber', over: 'ts' },
            { column: 'custom', expr: 'rank() over (order by x)' },
        ];
        const html = indexColumnsWidget.render(ctx(value, binding)).html;
        expect(html).toMatch(/data-dw-role="mode">[\s\S]*?<option value="hash" selected>/);
        expect(html).toMatch(/<option value="rowNumber" selected>/);
        expect(html).toMatch(/<option value="expr" selected>/);
        // hash row seeds the of input.
        expect(html).toContain('value="category"');
        // expr row seeds the expr input.
        expect(html).toContain('value="rank() over (order by x)"');
    });

    it('hides args blocks not matching the row mode', () => {
        const value = [{ column: 'h', fn: 'hash', of: 'category' }];
        const html = indexColumnsWidget.render(ctx(value, binding)).html;
        // hash mode: window + expr arg blocks hidden, hash block visible.
        expect(html).toMatch(/data-dw-role="args-window" hidden/);
        expect(html).toMatch(/data-dw-role="args-expr" hidden/);
        expect(html).toMatch(/data-dw-role="args-hash"(?! hidden)/);
    });

    it('bakes the dimension→index pairs filtered to present columns', () => {
        const html = indexColumnsWidget.render(ctx(undefined, binding)).html;
        expect(html).toContain('data-index-pairs="');
        // category is in columns → its pair is offered.
        expect(html).toContain('categoryIndex');
        expect(html).toContain('category');
        // rowGroup is NOT in columns → its pair must be excluded.
        expect(html).not.toContain('rowGroupIndex');
    });

    it('empty value renders no rows and no crash', () => {
        const html = indexColumnsWidget.render(ctx(undefined)).html;
        expect(html).not.toContain('data-dw-role="mode"');
    });
});
