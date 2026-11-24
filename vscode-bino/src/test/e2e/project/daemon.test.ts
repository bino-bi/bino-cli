import * as assert from 'assert';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import { DaemonClient } from '../../../daemonClient';
import { binoBin, mcpCall, needBino, pidAlive, readPortFile, request, stopDaemon, tempProject, waitFor } from '../helpers';

suite('e2e: two clients on one daemon', () => {
    let root: string | undefined;
    const disposables: vscode.Disposable[] = [];

    teardown(async () => {
        disposables.splice(0).forEach(d => d.dispose());
        if (root) {
            await stopDaemon(root);
            fs.rmSync(root, { recursive: true, force: true });
            root = undefined;
        }
    });

    function newClient(name: string): DaemonClient {
        const channel = vscode.window.createOutputChannel(`bino e2e ${name}`, { log: true });
        const client = new DaemonClient(channel);
        disposables.push(client, channel);
        return client;
    }

    test('the second window keeps the daemon after the first one leaves', async function () {
        needBino(this);
        root = tempProject('e2e-project');
        const first = newClient('first');
        const second = newClient('second');

        assert.strictEqual(await first.connect(root), true);
        const spawned = readPortFile(root);
        assert.ok(spawned, 'the first client spawned a daemon');
        assert.strictEqual(first.pid, spawned.pid);

        assert.strictEqual(await second.connect(root), true);
        assert.strictEqual(second.pid, spawned.pid, 'the second client adopts the same daemon');
        assert.strictEqual(second.connection()?.port, spawned.port);

        // What deactivate() does in the first window.
        first.shutdown();
        await new Promise(r => setTimeout(r, 1500));

        assert.ok(pidAlive(spawned.pid), 'the daemon keeps running');
        assert.strictEqual(second.getStatus(), 'connected');
        const auth = { Authorization: `Bearer ${spawned.token}` };
        const health = await request(`http://127.0.0.1:${spawned.port}/health`, { headers: auth });
        assert.strictEqual(health.status, 200);
        assert.strictEqual(JSON.parse(health.body).status, 'ok');
        const index = await second.getIndex();
        assert.ok(index && !index.error, `index: ${JSON.stringify(index)}`);
        const names = index.documents.map((d: { kind: string; name: string }) => `${d.kind}:${d.name}`);
        assert.ok(names.includes('DataSource:people'), names.join(', '));

        // An MCP client only has the port file.
        const mcp = `http://127.0.0.1:${spawned.port}/mcp`;
        const init = {
            jsonrpc: '2.0', id: 1, method: 'initialize',
            params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } },
        };
        const ok = await mcpCall(mcp, auth, init);
        assert.strictEqual(ok.res.status, 200);
        assert.strictEqual(ok.result?.result?.serverInfo?.name, 'bino');
        const denied = await mcpCall(mcp, {}, init);
        assert.strictEqual(denied.res.status, 401);
    });

    test('the daemon survives the exit of the window that spawned it', async function () {
        needBino(this);
        root = tempProject('e2e-project');
        // Like an extension host: spawn the daemon with piped output, then exit.
        const host = cp.spawn(process.execPath, ['-e', `
            const cp = require('child_process');
            const d = cp.spawn(process.argv[1], ['daemon', '--work-dir', process.argv[2]], { stdio: ['ignore', 'pipe', 'pipe'] });
            d.stdout.on('data', () => {});
            d.stderr.on('data', () => {});
            setTimeout(() => process.exit(0), 4000);
        `, binoBin!, root], { env: { ...process.env, ELECTRON_RUN_AS_NODE: '1' }, stdio: 'ignore' });
        const spawned = await waitFor('the spawned daemon', () => readPortFile(root!), 10000);
        await new Promise(r => host.on('exit', r));

        const second = newClient('second');
        assert.strictEqual(await second.connect(root), true);
        assert.strictEqual(second.pid, spawned.pid);
        // A file change makes the daemon log to its closed pipes.
        fs.appendFileSync(path.join(root, 'data.yaml'), '# changed\n');
        await new Promise(r => setTimeout(r, 2000));
        assert.ok(pidAlive(spawned.pid), 'the daemon keeps running');
        assert.strictEqual(second.getStatus(), 'connected');
        const index = await second.getIndex();
        assert.ok(index && !index.error && index.documents.length > 0, JSON.stringify(index));
    });
});
