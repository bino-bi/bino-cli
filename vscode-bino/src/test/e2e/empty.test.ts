import * as assert from 'assert';
import * as vscode from 'vscode';
import { activateExtension } from './helpers';

suite('e2e: workspace without bino.toml', () => {
    test('the onboarding commands and the Getting Started view exist', async () => {
        await activateExtension();
        const commands = await vscode.commands.getCommands(true);
        for (const c of ['bino.initProject', 'bino.checkSetup', 'bino.openSettings']) {
            assert.ok(commands.includes(c), `missing ${c}`);
        }
        const ext = vscode.extensions.getExtension('BinoBI.vscode-bino')!;
        const views: { id: string; when?: string }[] = ext.packageJSON.contributes.views.bino;
        assert.strictEqual(views.find(v => v.id === 'binoGettingStarted')?.when, '!bino.projectDetected');
        // Focusing the view fails when no tree data provider is registered for it.
        await vscode.commands.executeCommand('binoGettingStarted.focus');
        // Project commands are not registered without a project.
        assert.ok(!commands.includes('bino.restartDaemon'));
    });
});
