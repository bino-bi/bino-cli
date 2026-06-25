import { describe, it, expect } from 'vitest';
import { groupByWidget } from './groupBy';
import { FieldDef } from '../../schemaResolver';
import { WidgetContext, Binding } from './registry';

function field(key: string): FieldDef {
    return { key, path: [key], type: 'object', required: false };
}

function ctx(value: unknown, binding?: Binding): WidgetContext<unknown> {
    return { field: field('groupBy'), value, binding, onChange: () => undefined };
}

const binding: Binding = {
    dataset: 'sales',
    columns: ['region', 'product', 'revenue'],
    standardColumns: [],
};

describe('groupByWidget.match', () => {
    it('claims DataSet.groupBy', () => {
        expect(groupByWidget.match({ kind: 'DataSet', field: field('groupBy') })).toBe(true);
    });
    it('rejects other kinds and other keys', () => {
        expect(groupByWidget.match({ kind: 'DataSet', field: field('filter') })).toBe(false);
        expect(groupByWidget.match({ kind: 'Table', field: field('groupBy') })).toBe(false);
    });
});

describe('groupByWidget.render', () => {
    it('wraps in the groupBy widgetShell', () => {
        const html = groupByWidget.render(ctx(undefined)).html;
        expect(html).toContain('data-widget-kind="groupBy"');
    });

    it('marks chosen group-by columns as on-chips', () => {
        const value = { columns: ['region'] };
        const html = groupByWidget.render(ctx(value, binding)).html;
        // region is chosen → on; product is offered but off.
        expect(html).toMatch(/class="dw-chip dw-chip-on" data-dw-role="col-chip" data-slot="region"/);
        expect(html).toMatch(/class="dw-chip" data-dw-role="col-chip" data-slot="product"/);
    });

    it('renders an aggregate row per aggregate with fn and as seeded', () => {
        const value = { columns: ['region'], aggregates: [{ column: 'revenue', fn: 'sum', as: 'total' }] };
        const html = groupByWidget.render(ctx(value, binding)).html;
        expect(html).toContain('value="revenue"');
        expect(html).toMatch(/data-dw-role="agg-fn"[\s\S]*?<option value="sum" selected>/);
        expect(html).toContain('value="total"');
    });

    it('opens the order sub-form when orderBy is present', () => {
        const value = {
            columns: ['region'],
            aggregates: [{ column: 'revenue', fn: 'first', as: 'firstRev', orderBy: 'ts', orderDesc: true }],
        };
        const html = groupByWidget.render(ctx(value, binding)).html;
        expect(html).toMatch(/<details class="dw-style" open>/);
        expect(html).toContain('value="ts"');
        expect(html).toMatch(/data-dw-role="agg-orderdesc" checked/);
    });

    it('bakes the binding columns into data-columns', () => {
        const html = groupByWidget.render(ctx(undefined, binding)).html;
        expect(html).toContain('data-columns="');
        expect(html).toContain('product');
    });

    it('empty value renders no chips-on, no agg rows, no crash', () => {
        const html = groupByWidget.render(ctx(undefined)).html;
        expect(html).not.toContain('dw-chip-on');
        expect(html).not.toContain('data-dw-role="agg-column"');
    });
});
