import * as assert from 'assert';
import * as path from 'path';
import * as vscode from 'vscode';
import { activateExtension, needBino, processArgs, readPortFile, waitFor, waitForKindCompletion, workspaceRoot } from './helpers';

suite('e2e: project in a sub-folder', () => {
    test('activates, roots the language server at the sub-folder and completes there', async function () {
        needBino(this);
        this.timeout(90000);
        const ext = vscode.extensions.getExtension('BinoBI.vscode-bino');
        assert.ok(ext);
        // workspaceContains:**/bino.toml activates it without help.
        await waitFor('activation', () => ext.isActive || undefined, 20000);
        await activateExtension();

        const sub = path.join(workspaceRoot(), 'reports');
        await waitFor('daemon in the sub-folder', () => readPortFile(sub));
        const doc = await vscode.workspace.openTextDocument(path.join(sub, 'data.yaml'));
        await waitForKindCompletion(doc);
        const lsp = processArgs().filter(a => / lsp --work-dir /.test(a));
        assert.ok(lsp.some(a => a.endsWith(` lsp --work-dir ${sub}`)), lsp.join('\n'));
        assert.ok(!lsp.some(a => a.endsWith(` lsp --work-dir ${workspaceRoot()}`)), 'not rooted at the workspace folder');
    });
});
