import * as assert from 'assert';
import * as vscode from 'vscode';
import { fullPreviewPath, previewBaseUrl, urlOrigin } from '../previewUrl';

suite('preview URLs', () => {
    test('locally the base stays localhost', async () => {
        const base = await previewBaseUrl(45678, async uri => uri);
        assert.strictEqual(base, 'http://localhost:45678');
        assert.strictEqual(urlOrigin(base), 'http://localhost:45678');
    });

    test('remotely the forwarded URL is used', async () => {
        let asked: string | undefined;
        const base = await previewBaseUrl(45678, async uri => {
            asked = uri.toString(true);
            return vscode.Uri.parse('https://box-45678.app.github.dev/');
        });
        assert.strictEqual(asked, 'http://localhost:45678/');
        assert.strictEqual(base, 'https://box-45678.app.github.dev');
        assert.strictEqual(urlOrigin(base), 'https://box-45678.app.github.dev');
    });

    test('the full preview has routes for artefacts only', () => {
        assert.strictEqual(fullPreviewPath('ReportArtefact', 'sales'), '/sales');
        assert.strictEqual(fullPreviewPath('DocumentArtefact', 'manual'), '/doc/manual');
        assert.strictEqual(fullPreviewPath('LayoutPage', 'p1'), undefined);
        assert.strictEqual(fullPreviewPath('Table', 't1'), undefined);
    });
});
