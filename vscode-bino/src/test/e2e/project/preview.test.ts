import * as assert from 'assert';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as vscode from 'vscode';
import { BinoPreviewManager } from '../../../preview';
import { activateExtension, binoBin, freePort, needBino, request, waitFor, workspaceRoot } from '../helpers';

type RevealHandler = { handleRevealSource(msg: { type: string; kind?: string; name?: string; sourceFile?: string; line?: number }): Promise<void> };

suite('e2e: click to line', () => {
    let preview: cp.ChildProcess | undefined;

    teardown(async () => {
        preview?.kill();
        preview = undefined;
        await vscode.commands.executeCommand('workbench.action.closeAllEditors');
    });

    test('a preview component carries its source line and reveal opens it', async function () {
        needBino(this);
        await activateExtension();
        const root = workspaceRoot();
        const port = await freePort();
        preview = cp.spawn(binoBin!, ['preview', '--no-open', '--port', String(port), '--work-dir', root], { cwd: root, stdio: 'ignore' });

        const html = await waitFor('rendered preview', async () => {
            const res = await request(`http://127.0.0.1:${port}/__preview/context?path=%2F`).catch(() => undefined);
            return res?.status === 200 && res.body.includes('data-bino-source-line') ? res.body : undefined;
        }, 45000);
        const el = /<[^>]*data-bino-name='greeting'[^>]*>/.exec(html)?.[0];
        assert.ok(el, html);
        const file = /data-bino-source-file='([^']+)'/.exec(el)?.[1];
        const line = Number(/data-bino-source-line='(\d+)'/.exec(el)?.[1]);
        assert.strictEqual(fs.realpathSync(file!), fs.realpathSync(`${root}/report.yaml`));
        const text = fs.readFileSync(file!, 'utf8').split('\n');
        assert.match(text[line - 1], /- kind: Text/, 'the line is the component start');

        // The handler the preview webviews call for a bino:revealSource message.
        const channel = vscode.window.createOutputChannel('bino e2e preview', { log: true });
        const manager = new BinoPreviewManager(channel);
        try {
            await (manager as unknown as RevealHandler).handleRevealSource({
                type: 'bino:revealSource', kind: 'Text', name: 'greeting', sourceFile: file, line,
            });
        } finally {
            manager.dispose();
            channel.dispose();
        }
        const editor = vscode.window.activeTextEditor;
        assert.ok(editor);
        assert.strictEqual(fs.realpathSync(editor.document.uri.fsPath), fs.realpathSync(file!));
        assert.strictEqual(editor.selection.active.line, line - 1);
    });
});
