import * as assert from 'assert';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as http from 'http';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { daemonVersionMismatch, installedBinoVersion, parseBinoVersion, resolveBinPath } from '../bino';
import { DaemonClient, pidAlive, reconnectDelay, waitForDaemonExit } from '../daemonClient';
import { registerClientListeners } from '../languageClient';
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

/** A fake daemon: /health answers only with the token (none: always), /events with eventsStatus. */
async function fakeDaemon(token: string | undefined, eventsStatus = 200): Promise<{ server: http.Server; port: number }> {
    const server = http.createServer((req, res) => {
        if (token !== undefined && req.headers.authorization !== `Bearer ${token}`) {
            res.writeHead(401);
            res.end('{"error":"unauthorized"}');
        } else if (req.url === '/health') {
            res.end(JSON.stringify({ status: 'ok', version: '9.9.9', capabilities: [] }));
        } else if (req.url === '/events' && eventsStatus === 200) {
            res.writeHead(200, { 'Content-Type': 'text/event-stream' });
            res.write(': hello\n\n');
        } else {
            res.writeHead(req.url === '/events' ? eventsStatus : 404);
            res.end();
        }
    });
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
    return { server, port: (server.address() as { port: number }).port };
}

function closeServer(server: http.Server): void {
    server.closeAllConnections();
    server.close();
}

function projectRoot(): string {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-daemon-'));
    fs.mkdirSync(path.join(root, '.bino'));
    return root;
}

function writePortFile(root: string, pid: number, port: number, token?: string): void {
    fs.writeFileSync(path.join(root, '.bino', 'daemon.json'),
        JSON.stringify({ pid, port, startedAt: new Date().toISOString(), token }));
}

/** A fake `bino daemon` for the given roots: waits delayMs, writes the port file, stays alive. */
function fakeBino(dir: string, roots: { root: string; port: number; delayMs: number }[]): string {
    const cases = roots.map(r => `    '${r.root}') sleep ${r.delayMs / 1000}; port=${r.port} ;;`).join('\n');
    const bin = path.join(dir, 'fake-bino');
    fs.writeFileSync(bin, `#!/bin/sh
[ "$1" = daemon ] || exit 1
case "$3" in
${cases}
    *) exit 1 ;;
esac
printf '{"pid":%s,"port":%s,"token":"good","startedAt":"x"}' $$ $port > "$3/.bino/daemon.json"
exec sleep 30
`, { mode: 0o755 });
    return bin;
}

function kill(pid: number | undefined): void {
    if (pid) {
        try { process.kill(pid); } catch { /* gone */ }
    }
}

suite('daemon client lifecycle', () => {
    const config = () => vscode.workspace.getConfiguration('bino');
    let dirs: string[] = [];
    let cleanups: (() => void)[] = [];

    setup(function () {
        if (process.platform === 'win32') {
            this.skip();
        }
    });

    teardown(async () => {
        await config().update('binPath', undefined, vscode.ConfigurationTarget.Global);
        cleanups.forEach(c => c());
        // The client logs the exit of a killed daemon, so close its channel after that.
        await new Promise(r => setTimeout(r, 300));
        channels.forEach(c => c.dispose());
        dirs.forEach(d => fs.rmSync(d, { recursive: true, force: true }));
        dirs = [];
        cleanups = [];
        channels = [];
    });

    let channels: vscode.Disposable[] = [];
    function client(): DaemonClient {
        const channel = vscode.window.createOutputChannel('bino lifecycle test', { log: true });
        const c = new DaemonClient(channel);
        channels.push(channel);
        cleanups.push(() => c.dispose());
        return c;
    }

    test('waits for a slow daemon to remove its port file before restarting', async function () {
        this.timeout(15000);
        const rejecting = await fakeDaemon('other');
        const fresh = await fakeDaemon('good');
        cleanups.push(() => closeServer(rejecting.server), () => closeServer(fresh.server));
        const root = projectRoot();
        const bindir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-bin-'));
        dirs.push(root, bindir);
        const file = path.join(root, '.bino', 'daemon.json');
        // Like `bino daemon`, it removes the port file only after a slow shutdown.
        const old = cp.spawn('sh', ['-c', `trap 'sleep 0.8; rm -f "${file}"; exit 0' TERM; while :; do sleep 0.1; done`]);
        cleanups.push(() => kill(old.pid));
        writePortFile(root, old.pid!, rejecting.port, 'stale');
        await config().update('binPath', fakeBino(bindir, [{ root, port: fresh.port, delayMs: 0 }]), vscode.ConfigurationTarget.Global);

        const c = client();
        assert.strictEqual(await c.connect(root), false, 'the daemon rejects the token');
        assert.strictEqual(await c.restart(root), true);
        cleanups.push(() => kill(c.pid));
        assert.strictEqual(c.connection()?.port, fresh.port);
        assert.notStrictEqual(c.pid, old.pid);
    });

    test('a project switch wins over a slow daemon spawn for the old project', async function () {
        this.timeout(15000);
        const slow = await fakeDaemon('good');
        const other = await fakeDaemon('good');
        cleanups.push(() => closeServer(slow.server), () => closeServer(other.server));
        const a = projectRoot();
        const b = projectRoot();
        const bindir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-bin-'));
        dirs.push(a, b, bindir);
        cleanups.push(() => {
            try { kill(JSON.parse(fs.readFileSync(path.join(a, '.bino', 'daemon.json'), 'utf8')).pid); } catch { /* none */ }
        });
        writePortFile(b, process.pid, other.port, 'good');
        await config().update('binPath', fakeBino(bindir, [{ root: a, port: slow.port, delayMs: 1000 }]), vscode.ConfigurationTarget.Global);

        const c = client();
        const connectA = c.connect(a);
        await new Promise(r => setTimeout(r, 300));
        assert.strictEqual(await c.switchProject(b), true);
        assert.strictEqual(await connectA, false);
        // Give the slow daemon time to write its port file.
        await new Promise(r => setTimeout(r, 1500));
        assert.strictEqual(c.getProjectRoot(), b);
        assert.strictEqual(c.getStatus(), 'connected');
        assert.strictEqual(c.connection()?.port, other.port);
    });

    test('forgets the pid of a port file whose daemon does not answer', async function () {
        this.timeout(15000);
        const gone = await fakeDaemon(undefined);
        closeServer(gone.server);
        const root = projectRoot();
        dirs.push(root);
        const stranger = cp.spawn('sleep', ['30']);
        cleanups.push(() => kill(stranger.pid));
        writePortFile(root, stranger.pid!, gone.port);
        await config().update('binPath', path.join(root, 'no-such-bino'), vscode.ConfigurationTarget.Global);

        const c = client();
        assert.strictEqual(await c.connect(root), false);
        assert.strictEqual(c.pid, undefined, 'restart() would kill an unrelated process');
    });

    for (const [status, want] of [[503, 'disconnected'], [401, 'error']] as const) {
        test(`leaves connected when /events answers ${status}`, async function () {
            this.timeout(10000);
            const daemon = await fakeDaemon(undefined, status);
            cleanups.push(() => closeServer(daemon.server));
            const root = projectRoot();
            dirs.push(root);
            writePortFile(root, process.pid, daemon.port);
            const c = client();
            const seen = new Promise<void>((resolve, reject) => {
                const timer = setTimeout(() => reject(new Error(`still ${c.getStatus()}`)), 5000);
                c.onStatusChange(s => {
                    if (s === want) {
                        clearTimeout(timer);
                        resolve();
                    }
                });
            });
            assert.strictEqual(await c.connect(root), true);
            await seen;
            assert.strictEqual(c.connection(), undefined);
        });
    }

    test('a connect after a bino.binPath change does not wait for the old binary', async function () {
        this.timeout(15000);
        const slow = await fakeDaemon('good');
        const fast = await fakeDaemon('good');
        cleanups.push(() => closeServer(slow.server), () => closeServer(fast.server));
        const root = projectRoot();
        const oldDir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-bin-'));
        const newDir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-bin-'));
        dirs.push(root, oldDir, newDir);
        // The overtaken old daemon is not the client's to stop.
        cleanups.push(() => {
            try { cp.execFileSync('pkill', ['-f', oldDir]); } catch { /* none */ }
        });
        await config().update('binPath', fakeBino(oldDir, [{ root, port: slow.port, delayMs: 8000 }]), vscode.ConfigurationTarget.Global);

        const c = client();
        const first = c.connect(root);
        await new Promise(r => setTimeout(r, 300));
        await config().update('binPath', fakeBino(newDir, [{ root, port: fast.port, delayMs: 0 }]), vscode.ConfigurationTarget.Global);
        assert.strictEqual(await c.connect(root), true);
        assert.strictEqual(c.connection()?.port, fast.port);
        assert.strictEqual(await first, false);
        cleanups.push(() => kill(c.pid));
        // The old binary's daemon is stopped, so it cannot take the port file later.
        await new Promise(r => setTimeout(r, 300));
        assert.throws(() => cp.execFileSync('pgrep', ['-f', oldDir]), 'the old daemon still runs');
    });

    test('a disposed client does not log the output of the daemon it spawned', async function () {
        this.timeout(10000);
        const daemon = await fakeDaemon('good');
        cleanups.push(() => closeServer(daemon.server));
        const root = projectRoot();
        const bindir = fs.mkdtempSync(path.join(os.tmpdir(), 'bino-bin-'));
        dirs.push(root, bindir);
        await config().update('binPath', fakeBino(bindir, [{ root, port: daemon.port, delayMs: 0 }]), vscode.ConfigurationTarget.Global);

        // A window closes while the daemon it spawned keeps running for others.
        const channel = vscode.window.createOutputChannel('bino dispose test', { log: true });
        const c = new DaemonClient(channel);
        assert.strictEqual(await c.connect(root), true);
        const pid = c.pid;
        c.dispose();
        channel.dispose();
        // Its exit used to be logged to the closed channel: an uncaught error.
        kill(pid);
        await new Promise(r => setTimeout(r, 300));
    });

    test('a process of another user is not our daemon', function () {
        if (process.getuid?.() === 0) {
            this.skip(); // root may signal pid 1
        }
        assert.strictEqual(pidAlive(process.pid), true);
        assert.strictEqual(pidAlive(1), false);
    });

    test('removes the port file of a daemon that is already gone', async () => {
        const root = projectRoot();
        dirs.push(root);
        const dead = cp.spawn('sh', ['-c', 'exit 0']);
        await new Promise(r => dead.on('exit', r));
        writePortFile(root, dead.pid!, 1);
        await waitForDaemonExit(root, dead.pid);
        assert.strictEqual(fs.existsSync(path.join(root, '.bino', 'daemon.json')), false);
    });
});

suite('language client', () => {
    test('registers its listeners once across restarts', () => {
        const context = { subscriptions: [] as vscode.Disposable[] } as unknown as vscode.ExtensionContext;
        registerClientListeners(context);
        registerClientListeners(context);
        assert.strictEqual(context.subscriptions.length, 2);
        context.subscriptions.forEach(d => d.dispose());
    });
});
