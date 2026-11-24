import * as assert from 'assert';
import * as vscode from 'vscode';

suite('activation', () => {
    test('activates on a workspace with bino.toml', async () => {
        const ext = vscode.extensions.getExtension('BinoBI.vscode-bino');
        assert.ok(ext, 'extension not found');
        await ext.activate();
        assert.strictEqual(ext.isActive, true);
    });
});
