import * as assert from 'assert';
import * as path from 'path';
import * as vscode from 'vscode';
import { activateExtension, needBino, processPids, readPortFile, request, waitFor, waitForKindCompletion, workspaceRoot } from '../helpers';

function errors(uri: vscode.Uri): vscode.Diagnostic[] {
    return vscode.languages.getDiagnostics(uri).filter(d => d.severity === vscode.DiagnosticSeverity.Error);
}

function lspPids(root: string): string[] {
    return processPids(` lsp --work-dir ${root}`);
}

suite('e2e: Restart Daemon', () => {
    teardown(async () => {
        await vscode.commands.executeCommand('workbench.action.closeAllEditors');
    });

    test('completion and diagnostics work after a restart', async function () {
        needBino(this);
        this.timeout(120000);
        await activateExtension();
        const root = workspaceRoot();
        const data = await vscode.workspace.openTextDocument(path.join(root, 'data.yaml'));
        await waitForKindCompletion(data);
        const before = await waitFor('the extension daemon', () => readPortFile(root));

        const lspBefore = lspPids(root);
        assert.strictEqual(lspBefore.length, 1, 'one language server');

        await vscode.commands.executeCommand('bino.restartDaemon');
        const after = await waitFor('a new daemon', () => {
            const pf = readPortFile(root);
            return pf && pf.pid !== before.pid ? pf : undefined;
        });
        const health = await request(`http://127.0.0.1:${after.port}/health`, { headers: { Authorization: `Bearer ${after.token}` } });
        assert.strictEqual(health.status, 200);

        // The language server attaches to a daemon once, so it must restart too.
        await waitFor('a new language server', () => {
            const now = lspPids(root);
            return now.length === 1 && now[0] !== lspBefore[0] || undefined;
        });
        const labels = await waitForKindCompletion(data);
        assert.ok(labels.includes('DataSource'), labels.join(', '));

        // An unknown DataSource type is a schema error.
        const editor = await vscode.window.showTextDocument(data);
        const text = data.getText();
        const at = text.indexOf('type: inline') + 'type: '.length;
        await editor.edit(e => e.replace(new vscode.Range(data.positionAt(at), data.positionAt(at + 'inline'.length)), 'no-such-type'));
        try {
            await waitFor('an error diagnostic', () => errors(data.uri).length > 0 || undefined);
        } finally {
            await vscode.commands.executeCommand('workbench.action.files.revert');
        }
        await waitFor('errors cleared', () => errors(data.uri).length === 0 || undefined);
    });
});
