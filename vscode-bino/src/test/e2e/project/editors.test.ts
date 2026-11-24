import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import { AuthoringClient } from '../../../authoringClient';
import { WorkspaceIndexer } from '../../../indexer';
import { activateExtension, needBino, waitFor, workspaceRoot } from '../helpers';

const EDITORS = ['bino.designer', 'bino.treeEditor'];

function customTab(viewType: string, uri: vscode.Uri): vscode.Tab | undefined {
    return vscode.window.tabGroups.all.flatMap(g => g.tabs).find(t =>
        t.input instanceof vscode.TabInputCustom
        && t.input.viewType === viewType
        && t.input.uri.toString() === uri.toString());
}

suite('e2e: designer and tree editor', () => {
    const disposables: vscode.Disposable[] = [];
    let authoring: AuthoringClient;

    suiteSetup(async function () {
        await activateExtension();
        const channel = vscode.window.createOutputChannel('bino e2e editors', { log: true });
        const context = { subscriptions: disposables } as unknown as vscode.ExtensionContext;
        // The same write path the designer and the tree editor use.
        authoring = new AuthoringClient(new WorkspaceIndexer(context, channel));
        disposables.push(channel);
    });

    suiteTeardown(() => disposables.splice(0).forEach(d => d.dispose()));

    teardown(async () => {
        // A dirty editor would block closeAllEditors with a save prompt.
        for (const doc of vscode.workspace.textDocuments.filter(d => d.isDirty && d.uri.scheme === 'file')) {
            const edit = new vscode.WorkspaceEdit();
            edit.replace(doc.uri, new vscode.Range(doc.positionAt(0), doc.positionAt(doc.getText().length)),
                fs.readFileSync(doc.uri.fsPath, 'utf8'));
            await vscode.workspace.applyEdit(edit);
            await doc.save();
        }
        await vscode.commands.executeCommand('workbench.action.closeAllEditors');
    });

    for (const viewType of EDITORS) {
        test(`${viewType}: an edit through the write path is undone, the editor reopens`, async function () {
            needBino(this);
            const uri = vscode.Uri.file(path.join(workspaceRoot(), 'report.yaml'));
            await vscode.commands.executeCommand('vscode.openWith', uri, viewType);
            const tab = await waitFor(`${viewType} tab`, () => customTab(viewType, uri), 10000);
            assert.ok(tab.input instanceof vscode.TabInputCustom);
            assert.strictEqual(tab.input.viewType, viewType);

            const doc = await vscode.workspace.openTextDocument(uri);
            const before = doc.getText();
            const result = await authoring.edit({ file: uri.fsPath, position: 1, patch: { 'spec.title': 'Changed by e2e' } });
            assert.deepStrictEqual(result, { ok: true, applied: 'workspaceEdit' });
            assert.ok(doc.getText().includes('Changed by e2e'), doc.getText());
            assert.ok(doc.isDirty);

            // undo acts on the active custom editor.
            assert.ok(customTab(viewType, uri)?.isActive);
            await vscode.commands.executeCommand('undo');
            await waitFor('undo', () => doc.getText() === before || undefined, 5000);
            assert.strictEqual(doc.getText(), before);

            await vscode.commands.executeCommand('workbench.action.closeAllEditors');
            assert.strictEqual(customTab(viewType, uri), undefined);
            await vscode.commands.executeCommand('vscode.openWith', uri, viewType);
            const reopened = await waitFor(`${viewType} tab after reopen`, () => customTab(viewType, uri), 10000);
            assert.ok(reopened.input instanceof vscode.TabInputCustom);
            assert.strictEqual(reopened.input.viewType, viewType);
        });
    }
});
