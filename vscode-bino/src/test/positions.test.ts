import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import { BinoDocumentsResult, QueryBlock, blockContainsLine, blockText } from '../binoDocuments';
import { prqlBlockAt } from '../prql';

const range = (sl: number, sc: number, el: number, ec: number) => ({
    start: { line: sl, character: sc },
    end: { line: el, character: ec },
});

suite('positions: pure logic', () => {
    test('blockText strips the indent and keeps an indented ---', () => {
        const text = 'spec:\n  prql: |-\n    from sales\n    ---\n\n    select {region}\n';
        const block: QueryBlock = { field: 'prql', language: 'prql', range: range(2, 4, 5, 19) };
        assert.strictEqual(blockText(text, block), 'from sales\n---\n\nselect {region}');
    });

    test('blockText cuts an inline value at its columns', () => {
        const block: QueryBlock = { field: 'prql', language: 'prql', range: range(0, 8, 0, 14) };
        assert.strictEqual(blockText('  prql: from x # c', block), 'from x');
    });

    test('blockContainsLine covers the block lines only', () => {
        const block: QueryBlock = { field: 'query', language: 'sql', range: range(3, 4, 5, 10) };
        assert.deepStrictEqual([2, 3, 5, 6].map(l => blockContainsLine(block, l)), [false, true, true, false]);
    });

    test('prqlBlockAt finds the PRQL block and skips SQL', async () => {
        const doc = await vscode.workspace.openTextDocument({
            language: 'yaml',
            content: 'kind: DataSet\nmetadata:\n  name: a-b\nspec:\n  query: SELECT 1\n  prql: |\n    from t\n',
        });
        const outline: BinoDocumentsResult = {
            isBino: true,
            version: doc.version,
            documents: [{
                kind: 'DataSet', name: 'a-b', position: 1, range: range(0, 0, 7, 0),
                blocks: [
                    { field: 'query', language: 'sql', range: range(4, 9, 4, 17) },
                    { field: 'prql', language: 'prql', range: range(6, 4, 6, 10) },
                ],
            }],
        };
        assert.strictEqual(prqlBlockAt(outline, doc, new vscode.Position(4, 10)), undefined);
        const info = prqlBlockAt(outline, doc, new vscode.Position(6, 5));
        assert.ok(info);
        assert.strictEqual(info.datasetName, 'a-b');
        assert.strictEqual(info.prqlText, 'from t');
        assert.strictEqual(info.startChar, 4);
    });
});

// Needs a real bino: BINO_TEST_BIN=/path/to/bino xvfb-run -a npm test
suite('positions: language server', function () {
    const config = () => vscode.workspace.getConfiguration('bino');

    suiteSetup(async function () {
        if (!process.env.BINO_TEST_BIN) {
            this.skip();
        }
        // The daemon starts with the new binary and the language server follows it.
        await config().update('binPath', process.env.BINO_TEST_BIN, vscode.ConfigurationTarget.Global);
    });

    suiteTeardown(async function () {
        if (!process.env.BINO_TEST_BIN) {
            return;
        }
        await config().update('binPath', undefined, vscode.ConfigurationTarget.Global);
        const root = vscode.workspace.workspaceFolders![0].uri.fsPath;
        const portFile = path.join(root, '.bino', 'daemon.json');
        try {
            process.kill(JSON.parse(fs.readFileSync(portFile, 'utf8')).pid);
            await new Promise(r => setTimeout(r, 500));
        } catch { /* no daemon */ }
        fs.rmSync(path.join(root, '.bino'), { recursive: true, force: true });
    });

    async function lensesOf(doc: vscode.TextDocument, until: (l: vscode.CodeLens[]) => boolean): Promise<vscode.CodeLens[]> {
        const deadline = Date.now() + 30000;
        let lenses: vscode.CodeLens[] = [];
        while (Date.now() < deadline) {
            lenses = await vscode.commands.executeCommand<vscode.CodeLens[]>('vscode.executeCodeLensProvider', doc.uri) ?? [];
            if (until(lenses)) {
                return lenses;
            }
            await new Promise(r => setTimeout(r, 250));
        }
        return lenses;
    }

    const linesOf = (lenses: vscode.CodeLens[], command: string) =>
        lenses.filter(l => l.command?.command === command).map(l => l.range.start.line);

    test('CodeLens follows the unsaved buffer', async function () {
        this.timeout(60000);
        await vscode.extensions.getExtension('BinoBI.vscode-bino')?.activate();
        const doc = await vscode.workspace.openTextDocument({
            language: 'yaml',
            content: [
                'apiVersion: bino.bi/v1alpha1',
                'kind: DataSet',
                'metadata:',
                '  name: sales-by-region',
                'spec:',
                '  prql: |-',
                '    from sales',
                '    ---',
                '    select {region}',
                '---',
                'apiVersion: bino.bi/v1alpha1',
                'kind: DataSource',
                'metadata:',
                '  name: raw-sales',
                'spec:',
                '  type: csv',
                '  path: sales.csv',
                '',
            ].join('\n'),
        });
        await vscode.window.showTextDocument(doc);

        let lenses = await lensesOf(doc, l => linesOf(l, 'bino.previewRows').length === 2);
        assert.deepStrictEqual(linesOf(lenses, 'bino.previewRows'), [0, 10]);
        const args = lenses.filter(l => l.command?.command === 'bino.previewRows').map(l => l.command?.arguments?.[0]);
        assert.deepStrictEqual(args.map(a => [a.kind, a.name, a.position]), [
            ['DataSet', 'sales-by-region', 1],
            ['DataSource', 'raw-sales', 2],
        ]);

        // Insert a document at the top without saving: the lenses move with it.
        const edit = new vscode.WorkspaceEdit();
        edit.insert(doc.uri, new vscode.Position(0, 0),
            'apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: first\nspec:\n  query: SELECT 1\n---\n');
        assert.ok(await vscode.workspace.applyEdit(edit));
        assert.ok(doc.isDirty || doc.isUntitled);

        lenses = await lensesOf(doc, l => linesOf(l, 'bino.previewRows').length === 3);
        assert.deepStrictEqual(linesOf(lenses, 'bino.previewRows'), [0, 7, 17]);
    });
});
