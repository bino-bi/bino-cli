import * as assert from 'assert';
import type { FieldDef } from '../schemaResolver';
import { getIn, inheritedValue, mergeStyles, removalPath, SYSTEM_STYLE } from '../styles/styleModel';
import { buildStyleSections, StyleNode } from '../styles/styleForm';
import { effectiveRule, isVariancePrefix, sortedPrefixes, sortIndexChanges, varianceOrder } from '../styles/ruleSetModel';
import { contentDocs, topLevelDocAt } from '../styles/contentEditor';

function field(key: string, type: string, extra: Partial<FieldDef> = {}): FieldDef {
    return { key, path: [key], type, required: false, ...extra };
}

suite('style editor model', () => {
    test('a named style inherits from _default first, then from the engine', () => {
        const def = { global: { fontColor: '#111111' } };
        assert.deepStrictEqual(inheritedValue(['global', 'fontColor'], 'corporate', def), { value: '#111111', source: '_default' });
        assert.deepStrictEqual(inheritedValue(['global', 'fontFamily'], 'corporate', def),
            { value: getIn(SYSTEM_STYLE, ['global', 'fontFamily']), source: '_system' });
        // _default itself only inherits from the engine
        assert.strictEqual(inheritedValue(['global', 'fontColor'], '_default', def)?.source, '_system');
        assert.strictEqual(inheritedValue(['global', 'nothing'], 'corporate', def), undefined);
    });

    test('clearing the last key of a section removes the section, never the content', () => {
        const content = { global: { fontColor: '#111111' }, 'bn-chart-time': { barStyles: { ac: { fill: '#000000' } }, padding: { top: 1 } } };
        assert.deepStrictEqual(removalPath(content, ['global', 'fontColor']), ['global']);
        assert.deepStrictEqual(removalPath(content, ['bn-chart-time', 'barStyles', 'ac', 'fill']), ['bn-chart-time', 'barStyles']);
        assert.deepStrictEqual(removalPath({ global: { a: 1, b: 2 } }, ['global', 'a']), ['global', 'a']);
        assert.deepStrictEqual(removalPath({ global: { a: 1 } }, ['global']), ['global']);
    });

    test('merging layers replaces arrays and merges objects', () => {
        assert.deepStrictEqual(mergeStyles({ a: { x: 1, y: [1, 2] } }, undefined, { a: { y: [3] } }), { a: { x: 1, y: [3] } });
    });

    test('the form has one tab per section in tab order, with the right widget per field', () => {
        const svgStyle = field('ac', 'object', { refName: 'svgStyle', children: [field('fill', 'string'), field('fillpattern', 'string', { enumValues: ['solid', 'hatched', 'outlined', 'fc'] })] });
        const fields: FieldDef[] = [
            field('bn-annotation', 'object', { children: [field('colors', 'object'), field('markerStyle', 'string', { enumValues: ['filled', 'outlined'] })] }),
            field('global', 'object', { children: [field('fontColor', 'string'), field('fontSizePx', 'number')] }),
            field('bn-chart-time', 'object', {
                children: [
                    field('barStyles', 'object', { refName: 'svgStyleGroup', children: [svgStyle] }),
                    field('stackColors', 'array'),
                    field('padding', 'object', { children: [field('top', 'number')] }),
                ],
            }),
        ];
        const sections = buildStyleSections(fields, { global: { fontColor: '#123456' } }, 'corporate', undefined);
        assert.deepStrictEqual(sections.map(s => s.key), ['global', 'bn-chart-time', 'bn-annotation']);

        const [color, size] = sections[0].nodes as Extract<StyleNode, { type: 'leaf' }>[];
        assert.strictEqual(color.input, 'color');
        assert.strictEqual(color.own, '#123456');
        assert.strictEqual(size.input, 'number');
        assert.strictEqual(size.own, undefined);
        assert.strictEqual(size.source, '_system');

        const [bars, stack, padding] = sections[1].nodes;
        assert.strictEqual(bars.type, 'slots');
        if (bars.type === 'slots') {
            assert.deepStrictEqual(bars.slots, ['ac', 'fc', 'pp', 'pl']);
            assert.deepStrictEqual(bars.keys, ['fill', 'fillpattern']);
            assert.deepStrictEqual(bars.patterns, ['solid', 'hatched', 'outlined']);
            assert.strictEqual((bars.inherited.fc as { fillpattern?: string }).fillpattern, 'hatched');
        }
        assert.strictEqual(stack.type, 'list');
        assert.strictEqual(stack.type === 'list' && stack.colors, true);
        assert.strictEqual(padding.type, 'group');

        const [colors, marker] = sections[2].nodes;
        assert.strictEqual(colors.type, 'map');
        assert.strictEqual(marker.type === 'leaf' && marker.input, 'enum');
    });
});

suite('RuleSet editor model', () => {
    test('rows are the base prefixes plus custom ones, sorted by their effective sortIndex', () => {
        const scenarios = { pl: { sortIndex: 450 }, bu: { name: 'Budget' }, dac: { sortIndex: 1 } };
        assert.deepStrictEqual(sortedPrefixes(undefined), ['pp', 'pl', 'fc', 'ac']);
        assert.deepStrictEqual(sortedPrefixes(scenarios), ['pp', 'bu', 'fc', 'ac', 'pl']);
        assert.ok(isVariancePrefix('dac') && isVariancePrefix('drfc') && !isVariancePrefix('bu'));
        assert.deepStrictEqual(effectiveRule(scenarios, 'pl'), { name: 'PL', colorIndex: 50, sortIndex: 450 });
        assert.deepStrictEqual(varianceOrder(400), { absolute: 1400, relative: 1405 });
    });

    test('a new order writes sortIndex in steps of 100, and only real overrides', () => {
        // system: pp 100, pl 200, fc 300, ac 400
        assert.deepStrictEqual(sortIndexChanges(undefined, ['pp', 'pl', 'fc', 'ac']), { set: {}, remove: [] });
        assert.deepStrictEqual(sortIndexChanges(undefined, ['ac', 'pp', 'pl', 'fc']), { set: { ac: 100, pp: 200, pl: 300, fc: 400 }, remove: [] });
        // an override that becomes the engine value again is removed
        assert.deepStrictEqual(sortIndexChanges({ pl: { sortIndex: 900 } }, ['pp', 'pl', 'fc', 'ac']), { set: {}, remove: ['pl'] });
        // a custom prefix has no engine value, so it is always written
        assert.deepStrictEqual(sortIndexChanges({ xy: {} }, ['pp', 'xy', 'pl', 'fc', 'ac']), { set: { xy: 200, pl: 300, fc: 400, ac: 500 }, remove: [] });
    });
});

suite('content editor documents', () => {
    const text = 'apiVersion: bino.bi/v1alpha1\nkind: ComponentStyle\nmetadata:\n  name: a\nspec:\n  content: \'{"global": {}}\'\n---\n' +
        'kind: LayoutPage\nmetadata:\n  name: p\nspec:\n  children:\n    - kind: Table\n      metadata:\n        name: t\n      spec:\n        selectedStyle: a\n';

    test('finds documents of a kind with their content and the offset of their name', () => {
        const [doc] = contentDocs(text, 'ComponentStyle');
        assert.strictEqual(doc.name, 'a');
        assert.strictEqual(doc.position, 1);
        assert.strictEqual(doc.content, '{"global": {}}');
        assert.strictEqual(text.slice(doc.nameOffset, doc.nameOffset! + 1), 'a');
    });

    test('a reference inside an inline child belongs to its top-level document', () => {
        assert.deepStrictEqual(topLevelDocAt(text, text.indexOf('selectedStyle')), { kind: 'LayoutPage', name: 'p', position: 2 });
    });
});
