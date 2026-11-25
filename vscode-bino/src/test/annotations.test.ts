import * as assert from 'assert';
import * as vscode from 'vscode';
import {
    cleanSpec, collectPages, distinctValues, documentLines, fieldApplies, pointFieldsFor, specChanges,
} from '../annotations/annotationModel';
import { buildAnnotationTree, LoadedManifest } from '../annotations/annotationsView';
import { targetComponents } from '../annotations/annotationForm';
import { getAnnotationFormHtml } from '../annotations/annotationFormHtml';

const loaded = (json: object, file = '/p/report.yaml', position = 1, startLine = 0, endLine = 5): LoadedManifest =>
    ({ json: json as LoadedManifest['json'], file, position, startLine, endLine });

const PROJECT: LoadedManifest[] = [
    loaded({ kind: 'ReportArtefact', metadata: { name: 'monthly' }, spec: { layoutPages: ['sales_*'] } }),
    loaded({
        kind: 'LayoutPage', metadata: { name: 'sales_overview' }, spec: {
            children: [
                { kind: 'ChartTime', ref: 'revenue' },
                { kind: 'LayoutCard', metadata: { name: 'card' }, spec: { children: [{ kind: 'Table', metadata: { name: 'detail' }, spec: { dataset: 'ds' } }] } },
                { kind: 'Grid', ref: 'grid' },
            ],
        },
    }, '/p/pages.yaml'),
    loaded({ kind: 'LayoutPage', metadata: { name: 'appendix' }, spec: { children: [{ kind: 'Text', metadata: { name: 'note' }, spec: { value: 'x' } }] } }, '/p/pages.yaml', 2),
    loaded({ kind: 'ChartTime', metadata: { name: 'revenue' }, spec: { dataset: '$sales', scenarios: ['ac1', 'pl1'] } }, '/p/charts.yaml'),
    loaded({ kind: 'Grid', metadata: { name: 'grid' }, spec: { children: [{ row: 0, column: 0, kind: 'ChartStructure', metadata: { name: 'cell' }, spec: {} }] } }, '/p/charts.yaml', 2),
    loaded({ kind: 'Annotation', metadata: { name: 'peak' }, spec: { target: { kind: 'ChartTime', name: 'revenue', point: { date: '2024-01-01' } } } }, '/p/notes.yaml', 1, 0, 6),
    loaded({ kind: 'Annotation', metadata: { name: 'cell_note' }, spec: { mark: 'frame', target: { kind: 'Table', name: 'detail' } } }, '/p/notes.yaml', 2, 7, 12),
    loaded({ kind: 'Annotation', metadata: { name: 'lost' }, spec: { target: { kind: 'Table', name: 'gone' } } }, '/p/notes.yaml', 3, 13, 18),
    loaded({ kind: 'Annotation', metadata: { name: 'grid_note' }, spec: { target: { kind: 'ChartStructure', name: 'cell' } } }, '/p/notes.yaml', 4, 19, 24),
];

suite('annotation model', () => {
    test('fields follow the mark', () => {
        assert.ok(fieldApplies('title', 'marker'));
        assert.ok(!fieldApplies('to', 'marker') && !fieldApplies('to', undefined));
        assert.ok(fieldApplies('to', 'arrow') && fieldApplies('to', 'frame'));
        assert.ok(fieldApplies('digits', 'difference') && !fieldApplies('digits', 'bracket'));
        assert.ok(fieldApplies('percent', 'bracket') && fieldApplies('markText', 'bracket') && !fieldApplies('markTextPosition', 'bracket'));
    });

    test('point fields per kind, part only where the engine has parts', () => {
        assert.deepStrictEqual(pointFieldsFor('ChartTime'), ['date', 'scenario', 'category', 'row', 'part']);
        assert.deepStrictEqual(pointFieldsFor('Table'), ['rowGroup', 'category', 'subCategory', 'date', 'scenario', 'row']);
        assert.deepStrictEqual(pointFieldsFor('Text'), []);
        assert.deepStrictEqual(pointFieldsFor('SmallMultiple', 'ChartScatter'), ['rowGroup', 'category', 'subCategory', 'part']);
        assert.deepStrictEqual(pointFieldsFor('SmallMultiple'), []);
    });

    test('values are distinct, sorted, capped, and dates are cut to the day', () => {
        const rows = [{ date: '2024-01-01T00:00:00Z', c: 'b' }, { date: '2023-01-01', c: 'a' }, { date: '2024-01-01', c: 'c10' }, { c: 'c9' }, { c: null }];
        assert.deepStrictEqual(distinctValues(rows, 'date', 10), { values: ['2023-01-01', '2024-01-01'], truncated: false });
        assert.deepStrictEqual(distinctValues(rows, 'c', 3), { values: ['a', 'b', 'c9'], truncated: true });
    });

    test('pages list the components in them, through cards, grids and refs; artefacts by glob', () => {
        const pages = collectPages(PROJECT.map(m => m.json));
        const overview = pages.find(p => p.name === 'sales_overview')!;
        assert.deepStrictEqual(overview.artefacts, ['monthly']);
        assert.deepStrictEqual(overview.components.map(c => `${c.kind}:${c.name}`), ['ChartTime:revenue', 'LayoutCard:card', 'Table:detail', 'Grid:grid', 'ChartStructure:cell']);
        assert.deepStrictEqual(pages.find(p => p.name === 'appendix')!.artefacts, []);
        // no layoutPages means every page
        const all = collectPages([{ kind: 'ReportArtefact', metadata: { name: 'a' }, spec: {} }, { kind: 'LayoutPage', metadata: { name: 'x' }, spec: {} }]);
        assert.deepStrictEqual(all[0].artefacts, ['a']);
    });

    test('cards with the same name or none, tree nodes, an empty layoutPages and ref children with own children', () => {
        const pages = collectPages([
            { kind: 'ReportArtefact', metadata: { name: 'all' }, spec: { layoutPages: [] } },
            { kind: 'Tree', metadata: { name: 'tree' }, spec: { nodes: [{ id: 'n1', kind: 'Table', ref: 'city_table' }] } },
            { kind: 'LayoutCard', metadata: { name: 'shared' }, spec: { children: [{ kind: 'Text', metadata: { name: 'from_ref' } }] } },
            {
                kind: 'LayoutPage', metadata: { name: 'p' }, spec: {
                    children: [
                        { kind: 'LayoutCard', metadata: { name: 'Card' }, spec: { children: [{ kind: 'Table', metadata: { name: 'first' } }] } },
                        { kind: 'LayoutCard', metadata: { name: 'Card' }, spec: { children: [{ kind: 'ChartTime', metadata: { name: 'second' } }] } },
                        { kind: 'LayoutCard', spec: { children: [{ kind: 'Text', metadata: { name: 'unnamed_card_child' } }] } },
                        { kind: 'Tree', ref: 'tree' },
                        { kind: 'LayoutCard', ref: 'shared', spec: { children: [{ kind: 'Text', metadata: { name: 'own_child' } }] } },
                    ],
                },
            },
        ]);
        const names = pages[0].components.map(c => c.name);
        assert.deepStrictEqual(pages[0].artefacts, ['all']);
        for (const want of ['first', 'second', 'unnamed_card_child', 'city_table', 'own_child']) {
            assert.ok(names.includes(want), `${want} missing in ${names.join(', ')}`);
        }
        assert.ok(!names.includes('from_ref'), 'the own children of a ref replace the referenced ones');
    });

    test('a saved spec drops empty values, keys the mark does not read, and the default mark', () => {
        assert.deepStrictEqual(cleanSpec({
            mark: 'marker', color: '', title: 'T', digits: 2, percent: false,
            target: { kind: 'Table', name: 't', panel: 'x', point: { category: 'A', date: '' } },
            to: { kind: 'Table', name: 't' },
        }), { title: 'T', target: { kind: 'Table', name: 't', point: { category: 'A' } } });
        assert.deepStrictEqual(cleanSpec({ mark: 'difference', digits: 0, target: { kind: 'ChartTime', name: 'c' }, to: { kind: 'ChartTime', name: 'c', point: { date: '2024-01-01' } } }),
            { mark: 'difference', digits: 0, target: { kind: 'ChartTime', name: 'c' }, to: { kind: 'ChartTime', name: 'c', point: { date: '2024-01-01' } } });
    });

    test('an edit writes changed keys and removes the gone ones only', () => {
        const before = { target: { kind: 'Table', name: 't' }, title: 'Old', text: 'Same', color: 'negative' };
        const after = { target: { kind: 'Table', name: 't' }, title: 'New', text: 'Same' };
        assert.deepStrictEqual(specChanges(before, after), { patch: { 'spec.title': 'New' }, remove: ['spec.color'] });
    });

    test('a document is deleted with one separator next to it', () => {
        const text = 'kind: A\nx: 1\n---\nkind: B\n---\nkind: C\n';
        assert.deepStrictEqual(documentLines(text, 1), [0, 3]);
        assert.deepStrictEqual(documentLines(text, 2), [3, 5]);
        assert.deepStrictEqual(documentLines(text, 3), [4, 7]);
        assert.strictEqual(documentLines(text, 4), undefined);
        // a leading separator opens the first document
        assert.deepStrictEqual(documentLines('---\nkind: A\n---\nkind: B\n', 2), [2, 5]);
    });
});

suite('Annotations view', () => {
    test('builds artefact, page, component, annotation, and shows a missing target and its finding', () => {
        const finding = new vscode.Diagnostic(new vscode.Range(14, 0, 14, 5), 'no Table gone', vscode.DiagnosticSeverity.Warning);
        finding.code = 'annotation-target-missing';
        const tree = buildAnnotationTree(PROJECT, e => e.name === 'lost' ? [finding] : []);
        const outline = (nodes: ReturnType<typeof buildAnnotationTree>): unknown[] => nodes.map(n =>
            n.type === 'annotation' ? `${n.entry.name}${n.problems.length ? ' !' : ''}`
                : n.type === 'info' ? n.text
                    : { [n.type === 'component' ? `${n.kind} ${n.name}` : n.name]: outline(n.children) });
        assert.deepStrictEqual(outline(tree), [
            { monthly: [{ sales_overview: [{ 'ChartTime revenue': ['peak'] }, { 'Table detail': ['cell_note'] }, { 'ChartStructure cell': ['grid_note'] }] }] },
            { '(target on no page)': ['lost !'] },
        ]);
    });

    test('says so when there is no annotation', () => {
        const tree = buildAnnotationTree(PROJECT.filter(m => m.json.kind !== 'Annotation'), () => []);
        assert.strictEqual(tree.length, 1);
        assert.strictEqual(tree[0].type, 'info');
    });
});

suite('annotation form', () => {
    test('targets are the top-level and inline components of a target kind, with the panel kind of a SmallMultiple', () => {
        const comps = targetComponents([...PROJECT, loaded({ kind: 'SmallMultiple', metadata: { name: 'by_city' }, spec: { base: { kind: 'ChartTime', ref: 'revenue' } } })]);
        assert.deepStrictEqual(comps.map(c => `${c.kind}:${c.name}`), ['ChartStructure:cell', 'ChartTime:revenue', 'Grid:grid', 'SmallMultiple:by_city', 'Table:detail', 'Text:note']);
        assert.strictEqual(comps.find(c => c.name === 'by_city')?.inner, 'ChartTime');
        assert.strictEqual(comps.find(c => c.name === 'detail')?.file, '/p/pages.yaml');
    });

    test('the form page uses a nonce CSP and builds no HTML from strings', () => {
        const html = getAnnotationFormHtml('abc123');
        assert.ok(html.includes("script-src 'nonce-abc123'"));
        assert.ok(!/<[^>]*\son[a-z]+\s*=/i.test(html), 'inline handler');
        assert.ok(!html.includes('innerHTML'));
    });
});
