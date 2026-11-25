import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import { AnnotationFormPanel } from '../../../annotations/annotationForm';
import { annotationEntries, loadManifests } from '../../../annotations/annotationsView';
import { WorkspaceIndexer } from '../../../indexer';
import { activateExtension, needBino, waitFor, workspaceRoot } from '../helpers';

suite('e2e: annotation form', () => {
    const disposables: vscode.Disposable[] = [];
    let indexer: WorkspaceIndexer;
    let report: string;
    let original: string;

    suiteSetup(async function () {
        await activateExtension();
        const channel = vscode.window.createOutputChannel('bino e2e annotations', { log: true });
        const context = { subscriptions: disposables } as unknown as vscode.ExtensionContext;
        indexer = new WorkspaceIndexer(context, channel);
        disposables.push(channel);
        report = path.join(workspaceRoot(), 'report.yaml');
        original = fs.readFileSync(report, 'utf8');
    });

    suiteTeardown(async () => {
        fs.writeFileSync(report, original);
        await vscode.commands.executeCommand('workbench.action.closeAllEditors');
        disposables.splice(0).forEach(d => d.dispose());
    });

    test('creates an annotation next to its target, then edits only what changed', async function () {
        needBino(this);
        await indexer.refreshIndex();
        const form = new AnnotationFormPanel(indexer, () => undefined);
        disposables.push({ dispose: () => form.dispose() });
        await form.open();

        assert.strictEqual(await form.save('greeting_note', { mark: 'frame', title: 'Hello', target: { kind: 'Text', name: 'greeting' } }), undefined);
        const created = await waitFor('the new annotation', async () => {
            await indexer.refreshIndex();
            return annotationEntries(await loadManifests(indexer)).find(e => e.name === 'greeting_note');
        }, 15000);
        assert.strictEqual(created.file, report, 'a new annotation goes into the file of its target');
        assert.deepStrictEqual(created.spec, { mark: 'frame', title: 'Hello', target: { kind: 'Text', name: 'greeting' } });

        // the form now edits it: a second save must not create a twin
        assert.strictEqual(await form.save('greeting_note', { mark: 'frame', title: 'Hello again', target: { kind: 'Text', name: 'greeting' } }), undefined);
        const edited = await waitFor('the edit', async () => {
            const text = vscode.workspace.textDocuments.find(d => d.uri.fsPath === report)?.getText() ?? fs.readFileSync(report, 'utf8');
            return text.includes('Hello again') ? text : undefined;
        }, 15000);
        assert.strictEqual(edited.split('name: greeting_note').length, 2, edited);
        assert.ok(edited.includes('mark: frame'), edited);
    });
});
