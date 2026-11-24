import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import { DaemonClient } from '../../../daemonClient';
import { getKindCategory, loadBackendSchema, resetBackendSchema, SchemaResolver } from '../../../schemaResolver';
import { kindIcon } from '../../../tree';
import { needBino, stopDaemon, tempProject } from '../helpers';

// A built bino-science plugin binary (`bino-plugin-sci`).
const sciPlugin = process.env.BINO_TEST_SCI_PLUGIN;
const KIND = 'SciIntervalChart';
// Mirrors CATEGORY_ICONS in tree.ts.
const CATEGORY_ICONS: Record<string, string> = {
    artefact: 'file', data: 'database', layout: 'layout', embeddable: 'graph', config: 'settings-gear',
};

suite('e2e: plugin kinds from the daemon', () => {
    let root: string | undefined;

    teardown(async () => {
        resetBackendSchema();
        if (root) {
            await stopDaemon(root);
            fs.rmSync(root, { recursive: true, force: true });
            root = undefined;
        }
    });

    test('a plugin kind gets its fields and its tree icon from the backend', async function () {
        needBino(this);
        if (!sciPlugin) {
            this.skip();
        }
        root = tempProject('e2e-project', `\n[plugins.sci]\npath = ${JSON.stringify(path.resolve(sciPlugin))}\n`);
        const extensionPath = path.resolve(__dirname, '..', '..', '..', '..');
        const resolver = new SchemaResolver(extensionPath);
        assert.ok(resolver.load());
        assert.deepStrictEqual(resolver.getFieldsForKind(KIND), [], 'the bundled schema has no plugin kinds');
        assert.strictEqual(kindIcon(KIND), 'file-code', 'no icon before the backend answered');

        const channel = vscode.window.createOutputChannel('bino e2e plugin', { log: true });
        const client = new DaemonClient(channel);
        try {
            assert.strictEqual(await client.connect(root), true);
            assert.ok(await loadBackendSchema({
                getSchema: () => client.getSchema(),
                getKinds: async () => (await client.getKinds())?.kinds,
            }));
        } finally {
            client.dispose();
            channel.dispose();
        }

        assert.ok(resolver.getKindEnum().includes(KIND));
        const fields = resolver.getFieldsForKind(KIND).map(f => f.key);
        assert.ok(fields.includes('dataset'), `fields: ${fields.join(', ')}`);
        const category = getKindCategory(KIND);
        assert.ok(category, 'the daemon serves a category for the plugin kind');
        assert.strictEqual(kindIcon(KIND), CATEGORY_ICONS[category]);
    });
});
