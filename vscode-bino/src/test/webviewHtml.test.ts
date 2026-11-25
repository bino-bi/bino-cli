import * as assert from 'assert';
import { getDesignerHtml } from '../designer/designerHtml';
import { getTreeTableHtml, renderTreeContent, TreeDocument } from '../treeTableHtml';
import { getWizardHtml } from '../wizard/wizardHtml';
import { FieldDef } from '../schemaResolver';
import { getStyleEditorHtml } from '../styles/styleEditorHtml';
import { getRuleSetEditorHtml } from '../styles/ruleSetEditorHtml';

const INLINE_HANDLER = /<[^>]*\son[a-z]+\s*=/i;

function assertNonceCsp(html: string, nonce?: string): void {
    const csp = /http-equiv="Content-Security-Policy" content="([^"]*)"/.exec(html)?.[1];
    assert.ok(csp, 'no CSP meta tag');
    const scriptSrc = csp.split(';').map(d => d.trim()).find(d => d.startsWith('script-src'));
    assert.ok(scriptSrc, 'no script-src');
    assert.ok(!scriptSrc.includes('unsafe-inline'), scriptSrc);
    const n = nonce ?? /'nonce-([^']+)'/.exec(scriptSrc)?.[1];
    assert.ok(n && scriptSrc.includes(`'nonce-${n}'`), scriptSrc);
    const scripts = html.match(/<script[^>]*>/gi) ?? [];
    assert.ok(scripts.length > 0, 'no script tag');
    for (const tag of scripts) {
        assert.ok(tag.includes(`nonce="${n}"`), `script without nonce: ${tag}`);
    }
}

function field(key: string, type: string, extra: Partial<FieldDef> = {}): FieldDef {
    return { key, path: [key], type, required: false, ...extra };
}

const sampleDocs: TreeDocument[] = [{
    docIndex: 0,
    kind: 'Table',
    name: 'revenue',
    startLine: 0,
    endLine: 10,
    nodes: [
        { key: 'kind', value: 'Table', displayValue: 'Table', type: 'string', path: ['kind'], line: 1, column: 0 },
        {
            key: 'spec', value: undefined, displayValue: '{3}', type: 'object', path: ['spec'], line: 4, column: 0,
            children: [
                { key: 'title', value: 'Q3', displayValue: 'Q3', type: 'string', path: ['spec', 'title'], line: 5, column: 2 },
                { key: 'show', value: true, displayValue: 'true', type: 'boolean', path: ['spec', 'show'], line: 6, column: 2 },
                {
                    key: 'scenarios', value: undefined, displayValue: '[1]', type: 'array', path: ['spec', 'scenarios'], line: 7, column: 2,
                    children: [{ key: '[0]', value: 'ac', displayValue: 'ac', type: 'string', path: ['spec', 'scenarios', '0'], line: 0, column: 0 }],
                },
            ],
        },
    ],
}];

const specFields: FieldDef[] = [
    field('title', 'string', { description: 'The title' }),
    field('show', 'boolean'),
    field('mode', 'string', { enumValues: ['a', 'b'] }),
    field('scenarios', 'array'),
    field('style', 'object', { children: [field('color', 'string')] }),
    field('children', 'array', { children: [field('kind', 'string', { required: true, enumValues: ['Text'] })] }),
];

suite('webview HTML', () => {
    test('designer page uses a nonce CSP and no inline handlers', () => {
        const html = getDesignerHtml('abc123');
        assertNonceCsp(html, 'abc123');
        assert.ok(!INLINE_HANDLER.test(html), 'inline handler in designer HTML');
        assert.ok(html.includes('<label for="doc-select">'), 'document picker has no label');
    });

    test('tree editor page uses a nonce CSP and no inline handlers', () => {
        const html = getTreeTableHtml('abc123');
        assertNonceCsp(html, 'abc123');
        assert.ok(!INLINE_HANDLER.test(html), 'inline handler in tree HTML');
    });

    test('tree content has no inline handlers and accessible controls', () => {
        const html = renderTreeContent(sampleDocs, new Map([['Table', specFields]]), new Map());
        assert.ok(!INLINE_HANDLER.test(html), 'inline handler in tree content');
        assert.ok(html.includes('aria-expanded="true"'), 'toggle without aria-expanded');
        for (const tag of html.match(/<button[^>]*>/g) ?? []) {
            assert.ok(tag.includes('type="button"'), `button without type: ${tag}`);
        }
        for (const tag of html.match(/<button[^>]*class="(add-btn|remove-item-btn|clear-btn|disclosure[^"]*)"[^>]*>/g) ?? []) {
            assert.ok(tag.includes('aria-label='), `icon button without aria-label: ${tag}`);
        }
        for (const tag of html.match(/<(input|select)[^>]*>/g) ?? []) {
            assert.ok(tag.includes('aria-label='), `control without label: ${tag}`);
        }
    });

    test('ComponentStyle and RuleSet editor pages use a nonce CSP and no inline handlers', () => {
        for (const html of [getStyleEditorHtml('abc123'), getRuleSetEditorHtml('abc123')]) {
            assertNonceCsp(html, 'abc123');
            assert.ok(!INLINE_HANDLER.test(html), 'inline handler in editor HTML');
            assert.ok(!html.includes('innerHTML'), 'editor builds DOM from HTML strings');
        }
    });

    test('wizard page uses a nonce CSP and no inline handlers', () => {
        const html = getWizardHtml();
        assertNonceCsp(html);
        assert.ok(!INLINE_HANDLER.test(html), 'inline handler in wizard HTML');
    });
});
