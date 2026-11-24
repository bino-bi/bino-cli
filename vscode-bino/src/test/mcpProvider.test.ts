import * as assert from 'assert';
import * as fs from 'fs';
import * as http from 'http';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { DaemonClient } from '../daemonClient';
import { mcpServerDefinition } from '../mcpProvider';

/** A fake daemon that answers /health only with the expected token. */
async function fakeDaemon(token: string): Promise<{ server: http.Server; port: number; auth: string[] }> {
    const auth: string[] = [];
    const server = http.createServer((req, res) => {
        auth.push(`${req.url} ${req.headers.authorization ?? ''}`);
        if (token && req.headers.authorization !== `Bearer ${token}`) {
            res.writeHead(401, { 'Content-Type': 'application/json' });
            res.end('{"error":"unauthorized"}');
            return;
        }
        if (req.url === '/health') {
            res.end(JSON.stringify({ status: 'ok', version: '9.9.9', capabilities: [] }));
            return;
        }
        res.writeHead(200, { 'Content-Type': 'text/event-stream' });
        res.write('event: ready\ndata: {}\n\n');
    });
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    return { server, port: (server.address() as { port: number }).port, auth };
}

function projectWithPortFile(port: number, token?: string): string {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-mcp-'));
    fs.mkdirSync(path.join(root, '.bino'));
    fs.writeFileSync(path.join(root, '.bino', 'daemon.json'),
        JSON.stringify({ pid: process.pid, port, startedAt: new Date().toISOString(), version: '9.9.9', token }));
    return root;
}

suite('MCP server definition', () => {
    test('points at the daemon /mcp with the token header', () => {
        const def = mcpServerDefinition({ port: 4321, token: 'abc', version: '1.2.3' });
        assert.ok(def);
        assert.strictEqual(def.label, 'Bino');
        assert.strictEqual(def.uri.toString(true), 'http://127.0.0.1:4321/mcp');
        assert.deepStrictEqual(def.headers, { Authorization: 'Bearer abc' });
        assert.strictEqual(def.version, '1.2.3');
    });

    test('sends no header to an older daemon without a token', () => {
        assert.deepStrictEqual(mcpServerDefinition({ port: 4321 })?.headers, {});
    });

    test('offers nothing without a connection', () => {
        assert.strictEqual(mcpServerDefinition(undefined), undefined);
    });

    test('reads the token from the port file and sends it', async () => {
        const { server, port, auth } = await fakeDaemon('s3cret');
        const root = projectWithPortFile(port, 's3cret');
        const channel = vscode.window.createOutputChannel('bino mcp test', { log: true });
        const client = new DaemonClient(channel);
        try {
            assert.strictEqual(await client.connect(root), true);
            assert.deepStrictEqual(client.connection(), { port, token: 's3cret', version: '9.9.9' });
            assert.deepStrictEqual(mcpServerDefinition(client.connection())?.headers, { Authorization: 'Bearer s3cret' });
            assert.ok(auth.includes('/health Bearer s3cret'), auth.join(', '));
        } finally {
            client.dispose();
            channel.dispose();
            server.closeAllConnections();
            server.close();
            fs.rmSync(root, { recursive: true, force: true });
        }
    });

    test('stops on a daemon that rejects the token', async () => {
        const { server, port } = await fakeDaemon('other');
        const root = projectWithPortFile(port, 'stale');
        const channel = vscode.window.createOutputChannel('bino mcp test', { log: true });
        const client = new DaemonClient(channel);
        try {
            // Fails fast instead of spawning a second daemon next to the live one.
            assert.strictEqual(await client.connect(root), false);
            assert.strictEqual(client.getStatus(), 'error');
            assert.strictEqual(client.connection(), undefined);
        } finally {
            client.dispose();
            channel.dispose();
            server.closeAllConnections();
            server.close();
            fs.rmSync(root, { recursive: true, force: true });
        }
    });
});
