import * as assert from 'assert';
import * as fs from 'fs';
import * as http from 'http';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { daemonVersionMismatch, installedBinoVersion, parseBinoVersion, resolveBinPath } from '../bino';
import { DaemonClient, reconnectDelay } from '../daemonClient';
import { statusView, StatusState } from '../statusBar';

suite('bino binary', () => {
    test('parses `bino version` output', () => {
        assert.strictEqual(parseBinoVersion('bino 0.104.0 (commit abc, built 2026-11-01)\n'), '0.104.0');
        assert.strictEqual(parseBinoVersion('bino v0.104.0'), '0.104.0');
        assert.strictEqual(parseBinoVersion('bino 0.1.0-dev (commit none, built unknown)'), '0.1.0-dev');
        assert.strictEqual(parseBinoVersion('version 1.2.3'), '1.2.3');
        assert.strictEqual(parseBinoVersion('nothing here'), undefined);
    });

    test('detects a daemon from another bino version', () => {
        assert.strictEqual(daemonVersionMismatch('0.104.0', '0.104.0'), false);
        assert.strictEqual(daemonVersionMismatch('v0.104.0', '0.104.0'), false);
        assert.strictEqual(daemonVersionMismatch('0.103.0', '0.104.0'), true);
        // A daemon without a version comes from an older bino.
        assert.strictEqual(daemonVersionMismatch(undefined, '0.104.0'), true);
        // Without an installed version there is nothing to compare.
        assert.strictEqual(daemonVersionMismatch('0.104.0', undefined), false);
    });

    test('ignores a workspace bino.binPath in an untrusted workspace', () => {
        const setting = { globalValue: '/usr/local/bin/bino', workspaceValue: './evil' };
        assert.strictEqual(resolveBinPath(setting, true), './evil');
        assert.strictEqual(resolveBinPath(setting, false), '/usr/local/bin/bino');
        assert.strictEqual(resolveBinPath({ workspaceFolderValue: './evil' }, false), 'bino');
        assert.strictEqual(resolveBinPath({ globalValue: '  ' }, true), 'bino');
        assert.strictEqual(resolveBinPath(undefined, true), 'bino');
        assert.strictEqual(resolveBinPath({ globalValue: '/opt/my bino/bino ' }, true), '/opt/my bino/bino');
    });

    test('runs a bino whose path has spaces', async function () {
        if (process.platform === 'win32') {
            this.skip();
        }
        const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino test '));
        const bin = path.join(dir, 'my bino');
        fs.writeFileSync(bin, '#!/bin/sh\n[ "$1" = version ] && echo "bino 9.9.9 (commit none, built unknown)" && exit 0\nexit 1\n', { mode: 0o755 });
        const config = vscode.workspace.getConfiguration('bino');
        await config.update('binPath', bin, vscode.ConfigurationTarget.Global);
        try {
            assert.strictEqual(await installedBinoVersion(), '9.9.9');
        } finally {
            await config.update('binPath', undefined, vscode.ConfigurationTarget.Global);
            fs.rmSync(dir, { recursive: true, force: true });
        }
    });
});

suite('daemon reconnect', () => {
    test('backs off exponentially up to 30s', () => {
        assert.deepStrictEqual([0, 1, 2, 3, 4, 5, 6].map(reconnectDelay), [1000, 2000, 4000, 8000, 16000, 30000, 30000]);
    });
});

suite('status bar', () => {
    const idle: StatusState = {
        daemon: 'connected', preview: 'stopped', indexing: false, validating: false, errors: 0, warnings: 0,
    };

    test('shows a check when all is well', () => {
        const view = statusView(idle);
        assert.strictEqual(view.text, '$(check) Bino');
        assert.strictEqual(view.background, undefined);
    });

    test('spins while busy', () => {
        assert.strictEqual(statusView({ ...idle, indexing: true }).text, '$(sync~spin) Bino');
        assert.strictEqual(statusView({ ...idle, daemon: 'connecting' }).text, '$(sync~spin) Bino');
    });

    test('shows errors before warnings and daemon trouble', () => {
        const view = statusView({ ...idle, errors: 2, warnings: 1, daemon: 'error' });
        assert.strictEqual(view.text, '$(error) Bino: 2');
        assert.strictEqual(view.background, 'error');
        assert.strictEqual(statusView({ ...idle, daemon: 'error' }).background, 'warning');
        assert.strictEqual(statusView({ ...idle, warnings: 3 }).text, '$(warning) Bino: 3');
    });

    test('marks a running preview and describes it in the tooltip', () => {
        const view = statusView({ ...idle, preview: 'running', previewPort: 45678, daemonVersion: '0.104.0' });
        assert.strictEqual(view.text, '$(check) Bino $(eye)');
        assert.ok(view.tooltip.includes('Preview: running on port 45678'));
        assert.ok(view.tooltip.includes('Daemon: connected (bino 0.104.0)'));
        assert.ok(statusView({ ...idle, daemon: undefined }).tooltip.includes('Daemon: disabled'));
    });
});

suite('daemon client', () => {
    test('reconnects to an adopted daemon when its event stream ends', async function () {
        this.timeout(15000);
        let streams = 0;
        const server = http.createServer((req, res) => {
            if (req.url === '/health') {
                res.end(JSON.stringify({ status: 'ok', version: '9.9.9', capabilities: [] }));
            } else if (req.url === '/events') {
                streams++;
                res.writeHead(200, { 'Content-Type': 'text/event-stream' });
                res.write(': hello\n\n');
                // The first stream ends at once, as if the daemon dropped the connection.
                if (streams === 1) {
                    setTimeout(() => res.end(), 100);
                }
            } else {
                res.statusCode = 404;
                res.end();
            }
        });
        await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
        const port = (server.address() as { port: number }).port;
        const root = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-daemon-'));
        fs.mkdirSync(path.join(root, '.bino'));
        fs.writeFileSync(path.join(root, '.bino', 'daemon.json'),
            JSON.stringify({ pid: process.pid, port, startedAt: new Date().toISOString(), version: '9.9.8' }));
        const channel = vscode.window.createOutputChannel('bino test', { log: true });
        const client = new DaemonClient(channel);
        try {
            assert.strictEqual(await client.connect(root), true);
            assert.strictEqual(client.version, '9.9.8', 'the port file version wins over /health');
            const seen: string[] = [];
            await new Promise<void>((resolve, reject) => {
                const timer = setTimeout(() => reject(new Error(`no reconnect, saw ${seen.join(',')}`)), 10000);
                client.onStatusChange(status => {
                    seen.push(status);
                    if (status === 'connected') {
                        clearTimeout(timer);
                        resolve();
                    }
                });
            });
            assert.deepStrictEqual(seen, ['disconnected', 'connected']);
            assert.strictEqual(client.pid, process.pid);
        } finally {
            client.dispose();
            channel.dispose();
            server.closeAllConnections();
            server.close();
            fs.rmSync(root, { recursive: true, force: true });
        }
    });
});

// Needs a real bino: BINO_TEST_BIN=/path/to/bino xvfb-run -a npm test
suite('bino.binPath change', () => {
    test('connects the daemon with the new binary', async function () {
        const bin = process.env.BINO_TEST_BIN;
        if (!bin) {
            this.skip();
        }
        this.timeout(30000);
        const ext = vscode.extensions.getExtension('BinoBI.vscode-bino');
        await ext?.activate();
        const root = vscode.workspace.workspaceFolders![0].uri.fsPath;
        const portFile = path.join(root, '.bino', 'daemon.json');
        const config = vscode.workspace.getConfiguration('bino');
        await config.update('binPath', bin, vscode.ConfigurationTarget.Global);
        let pid: number | undefined;
        try {
            for (let i = 0; i < 100 && !fs.existsSync(portFile); i++) {
                await new Promise(r => setTimeout(r, 200));
            }
            const data = JSON.parse(fs.readFileSync(portFile, 'utf8'));
            pid = data.pid;
            assert.strictEqual(data.version, await installedBinoVersion());
        } finally {
            await config.update('binPath', undefined, vscode.ConfigurationTarget.Global);
            if (pid) {
                try { process.kill(pid); } catch { /* gone */ }
                await new Promise(r => setTimeout(r, 500));
            }
            fs.rmSync(path.join(root, '.bino'), { recursive: true, force: true });
        }
    });
});
