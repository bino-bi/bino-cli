import * as assert from 'assert';
import * as vscode from 'vscode';
import { DaemonClient } from '../../../daemonClient';
import { mcpServerDefinition } from '../../../mcpProvider';
import { activateExtension, mcpCall, needBino, readPortFile, waitFor, workspaceRoot } from '../helpers';

suite('e2e: MCP server definition', () => {
    test('the definition reaches the Bino tools of the running daemon', async function () {
        needBino(this);
        await activateExtension();
        const root = workspaceRoot();
        // The daemon the extension spawned for this window.
        const pf = await waitFor('the extension daemon', () => readPortFile(root));

        // vscode.lm.mcpServerDefinitions, which lists registered definitions, is a
        // proposed API this extension cannot use, so check the provider's output.
        // provideMcpServerDefinitions returns mcpServerDefinition(client.connection()).
        const channel = vscode.window.createOutputChannel('bino e2e mcp', { log: true });
        const client = new DaemonClient(channel);
        try {
            assert.strictEqual(await client.connect(root), true);
            assert.strictEqual(client.pid, pf.pid, 'adopts the extension daemon');
            const def = mcpServerDefinition(client.connection());
            assert.ok(def);
            assert.strictEqual(def.uri.toString(true), `http://127.0.0.1:${pf.port}/mcp`);
            assert.deepStrictEqual(def.headers, { Authorization: `Bearer ${pf.token}` });

            const url = def.uri.toString(true);
            const init = await mcpCall(url, def.headers, {
                jsonrpc: '2.0', id: 1, method: 'initialize',
                params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } },
            });
            assert.strictEqual(init.res.status, 200, init.res.body);
            const session = init.res.headers['mcp-session-id'];
            const headers: Record<string, string> = { ...def.headers, ...(session ? { 'Mcp-Session-Id': String(session) } : {}) };
            await mcpCall(url, headers, { jsonrpc: '2.0', method: 'notifications/initialized' });
            const list = await mcpCall(url, headers, { jsonrpc: '2.0', id: 2, method: 'tools/list' });
            const tools: string[] = (list.result?.result?.tools ?? []).map(t => t.name);
            for (const want of ['outline_kind', 'scaffold_kind', 'validate_draft', 'preview_server_start']) {
                assert.ok(tools.includes(want), `missing ${want} in ${tools.join(', ')}`);
            }
        } finally {
            client.dispose();
            channel.dispose();
        }
    });
});
